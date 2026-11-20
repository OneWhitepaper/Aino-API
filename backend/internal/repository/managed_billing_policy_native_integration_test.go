//go:build integration && nativeconsumer

package repository_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDesktopBalanceOnlyHTTPWithoutAttributionHasSettlementReceipt(t *testing.T) {
	f := newDevicePreflight(t, t.TempDir())
	f.startServers(t)
	operationFaultSeedWindows(t, f)
	token := f.login(t)
	identity := f.device(t, token, service.DesktopDeviceKeyRequest{DeviceID: uuid.NewString(), BillingPolicy: service.BillingPolicyBalanceOnly})
	request := newOperationFaultRequest(t, "fixture-tool-model")
	request.headers = map[string]string{"X-Aino-Model-ID": "fixture-tool-model", "X-Aino-Operation-ID": request.operation}
	var body map[string]any
	require.NoError(t, json.Unmarshal(request.body, &body))
	body["max_completion_tokens"] = 128
	var err error
	request.body, err = json.Marshal(body)
	require.NoError(t, err)
	before := subscriptionLifecycleSnapshot(t, f)
	response := operationFaultDispatch(f.origins[0], identity.APIKey, request)
	require.NoError(t, response.err)
	require.Equal(t, http.StatusOK, response.status, string(response.body))
	db := repository.GetIntegrationDB()
	var rowID int64
	require.Eventually(t, func() bool {
		return db.QueryRowContext(f.r.ctx, "SELECT id FROM usage_logs WHERE user_id=$1 AND api_key_id=$2", f.userID, identity.KeyID).Scan(&rowID) == nil
	}, 10*time.Second, 20*time.Millisecond)
	row, err := repository.NewUsageLogRepository(f.r.client, db).GetByID(f.r.ctx, rowID)
	require.NoError(t, err)
	require.Nil(t, row.SessionID)
	require.Nil(t, row.DesktopTurnID)
	require.Nil(t, row.DesktopCallID)
	require.Nil(t, row.DesktopPurpose)
	require.Equal(t, "settled", row.SettlementStatus)
	require.NotNil(t, row.ActualCostDecimal)
	require.Equal(t, "0.00010000", *row.ActualCostDecimal)
	status, raw, _ := f.call(t, 1, http.MethodGet, "/api/v1/usage/"+strconv.FormatInt(rowID, 10), token, nil, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	var receipt struct {
		Data struct {
			SettlementStatus string  `json:"settlement_status"`
			ActualCost       *string `json:"actual_cost_decimal"`
		}
	}
	require.NoError(t, json.Unmarshal(raw, &receipt))
	require.Equal(t, "settled", receipt.Data.SettlementStatus)
	require.NotNil(t, receipt.Data.ActualCost)
	require.Equal(t, "0.00010000", *receipt.Data.ActualCost)
	operationFaultAssertCharge(t, f, before, "fixture-tool-model", true)
	t.Log("BALANCE_ONLY_HTTP_RECEIPT attribution_headers=0 actual_cost_decimal=0.00010000 settlement_status=settled wallet_debits=1 subscription_debits=0 user_receipt=true")
}

func TestDesktopBalanceOnlyHTTPResponseLossCannotReplay(t *testing.T) {
	f := newDevicePreflight(t, t.TempDir())
	f.startServers(t)
	operationFaultSeedWindows(t, f)
	funding := service.NewTeamFundingService(repository.NewTeamFundingRepository(f.r.client, repository.GetIntegrationDB()))
	_, err := funding.UpdateFunding(f.r.ctx, f.userID, &service.FundingUpdateCommand{Mode: service.FundingModeSubscriptionOnly, AllowPersonalSubscription: true})
	require.NoError(t, err)
	identity := f.device(t, f.login(t), service.DesktopDeviceKeyRequest{DeviceID: uuid.NewString(), BillingPolicy: service.BillingPolicyBalanceOnly})
	require.Equal(t, service.BillingPolicyBalanceOnly, identity.BillingPolicy)
	request := newOperationFaultRequest(t, "fixture-tool-model")
	var body map[string]any
	require.NoError(t, json.Unmarshal(request.body, &body))
	body["max_completion_tokens"] = 128
	request.body, err = json.Marshal(body)
	require.NoError(t, err)
	before := subscriptionLifecycleSnapshot(t, f)
	front, completed := operationFaultDropResponse(t, f.origins[0])
	lost := operationFaultDispatch(front.URL, identity.APIKey, request)
	require.Error(t, lost.err)
	select {
	case original := <-completed:
		require.NoError(t, original.err)
		require.Equal(t, http.StatusOK, original.status, string(original.body))
	case <-time.After(5 * time.Second):
		t.Fatal("response-loss front did not observe completed API response")
	}
	operationFaultSettle(t, f, request)
	var workers sync.WaitGroup
	duplicates := make(chan operationFaultResponse, 4)
	for i := range 4 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			duplicates <- operationFaultDispatch(f.origins[i%2], identity.APIKey, operationFaultRetry(request))
		}()
	}
	workers.Wait()
	close(duplicates)
	for response := range duplicates {
		require.NoError(t, response.err)
		require.Equal(t, http.StatusConflict, response.status, string(response.body))
		var failure struct {
			Code string `json:"code"`
		}
		require.NoError(t, json.Unmarshal(response.body, &failure))
		require.Contains(t, []string{"TEAM_FUNDING_CONFLICT", "DESKTOP_OPERATION_UNKNOWN"}, failure.Code)
	}
	operationFaultAssertCharge(t, f, before, "fixture-tool-model", true)
	var mode string
	var balanceAllowed, subscriptionAllowed bool
	require.NoError(t, repository.GetIntegrationDB().QueryRowContext(f.r.ctx, "SELECT mode,allow_personal_balance,allow_personal_subscription FROM funding_preferences WHERE user_id=$1", f.userID).Scan(&mode, &balanceAllowed, &subscriptionAllowed))
	require.Equal(t, service.FundingModeSubscriptionOnly, mode)
	require.False(t, balanceAllowed)
	require.True(t, subscriptionAllowed)
	var reservations, settled int
	require.NoError(t, repository.GetIntegrationDB().QueryRowContext(f.r.ctx, "SELECT count(*),count(*) FILTER (WHERE status='settled' AND settled_amount=0.0001 AND unauthorized_amount=0) FROM personal_funding_reservations WHERE api_key_id=$1", identity.KeyID).Scan(&reservations, &settled))
	require.Equal(t, 1, reservations)
	require.Equal(t, 1, settled)
	t.Log("BALANCE_ONLY_HTTP_RESPONSE_LOSS policy=balance_only original_executions=1 settled_wallet_debits=1 charged=0.00010000 concurrent_duplicates=4 api_instances=2 subscription_debits=0 account_preferences_preserved=true client_response_lost=true")
}
