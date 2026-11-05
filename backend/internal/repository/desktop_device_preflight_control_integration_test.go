//go:build integration && nativeconsumer

package repository_test

import (
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDevicePreflightControlReadsRealCorrelationAndSurfacesFailures(t *testing.T) {
	f := newDevicePreflight(t, t.TempDir())
	identity, err := service.NewDesktopDeviceKeyService(repository.NewDesktopDeviceKeyRepository(f.r.client), f.r.keys).Ensure(f.r.ctx, f.userID, service.DesktopDeviceKeyRequest{DeviceID: uuid.NewString()})
	require.NoError(t, err)
	keyID, err := strconv.ParseInt(identity.KeyID, 10, 64)
	require.NoError(t, err)
	var accountID int64
	require.NoError(t, repository.GetIntegrationDB().QueryRow("SELECT id FROM accounts WHERE name=$1", "fixture-"+f.r.runID).Scan(&accountID))
	session, turn, call, purpose := uuid.NewString(), uuid.NewString(), uuid.NewString(), "chat"
	_, err = repository.NewUsageLogRepository(f.r.client, repository.GetIntegrationDB()).Create(f.r.ctx, &service.UsageLog{UserID: f.userID, APIKeyID: keyID, AccountID: accountID, GroupID: &f.subText.GroupID, SubscriptionID: &f.subText.ID, RequestID: uuid.NewString(), Model: "fixture-tool-model", ActualCost: 0.0001, SessionID: &session, DesktopTurnID: &turn, DesktopCallID: &call, DesktopPurpose: &purpose, SettlementStatus: "settled", CreatedAt: time.Now()})
	require.NoError(t, err)
	request := httptest.NewRequest("GET", "/fixture/control/state", nil)
	request.Header.Set("X-Fixture-Nonce", f.nonce)
	response := httptest.NewRecorder()
	f.control(response, request)
	require.Equal(t, 200, response.Code, response.Body.String())
	var state struct {
		Calls  int              `json:"usage_calls"`
		Ledger []map[string]any `json:"usage_ledger"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &state))
	require.Equal(t, 1, state.Calls)
	require.Len(t, state.Ledger, 1)
	require.Equal(t, session, state.Ledger[0]["desktop_session_id"])
	require.Equal(t, turn, state.Ledger[0]["desktop_turn_id"])
	require.Equal(t, call, state.Ledger[0]["desktop_call_id"])
	require.Equal(t, float64(f.subText.ID), state.Ledger[0]["subscription_id"])
	require.NotContains(t, response.Body.String(), identity.APIKey)
	f.userID = -1
	response = httptest.NewRecorder()
	f.control(response, request)
	require.Equal(t, 500, response.Code, "a failed database query must not masquerade as an empty ledger")
}
