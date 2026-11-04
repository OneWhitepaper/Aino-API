//go:build integration

package repository_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func archivedDesktopUsageRig(t *testing.T) (*desktopCredentialRig, int64, string) {
	t.Helper()
	r := newDesktopCredentialRig(t)
	issued := r.issue(t, "fixture-a")
	key, err := r.keys.GetByKey(r.ctx, issued.APIKey)
	require.NoError(t, err)
	account, err := r.client.Account.Create().SetName("archived-desktop-usage").SetPlatform(service.PlatformOpenAI).SetType(service.AccountTypeAPIKey).SetCredentials(map[string]any{}).Save(r.ctx)
	require.NoError(t, err)
	db := repository.GetIntegrationDB()
	logs := repository.NewUsageLogRepository(r.client, db)
	requestID := uuid.NewString()
	result, err := repository.NewUsageBillingRepository(r.client, db).Apply(r.ctx, &service.UsageBillingCommand{RequestID: requestID, UserID: r.userID, APIKeyID: key.ID, AccountID: account.ID, Model: "fixture-a", BalanceCost: 0.125})
	require.NoError(t, err)
	require.True(t, result.Applied)
	_, err = logs.Create(r.ctx, &service.UsageLog{UserID: r.userID, APIKeyID: key.ID, AccountID: account.ID, RequestID: requestID, Model: "fixture-a", ActualCost: 0.125, TotalCost: 0.125, SettlementStatus: "settled", CreatedAt: time.Now()})
	require.NoError(t, err)
	_, err = db.ExecContext(r.ctx, "UPDATE desktop_model_credentials SET created_at=$1, expires_at=$2 WHERE api_key_id=$3", time.Now().Add(-time.Hour), time.Now().Add(-time.Minute), key.ID)
	require.NoError(t, err)
	usage := handler.NewUsageHandler(service.NewUsageService(logs, r.userRepo, r.client, nil), r.keys, nil, r.settings)
	auth := gin.HandlerFunc(middleware.NewJWTAuthMiddleware(r.auth, r.users, r.settings, nil))
	r.router.GET("/history/keys", auth, handler.NewAPIKeyHandler(r.keys).List)
	r.router.GET("/history/usage", auth, usage.List)
	r.router.GET("/history/usage/stats", auth, usage.Stats)
	r.router.GET("/history/keys/:id/daily", auth, usage.GetMyAPIKeyDailyUsage)
	r.router.POST("/history/usage/batch", auth, usage.DashboardAPIKeysUsage)
	require.Equal(t, http.StatusOK, r.request("GET", "/history/keys", "", r.token).Code)
	return r, key.ID, issued.APIKey
}

func TestDesktopLeaseArchivedUsageKeepsOwnershipNameAndBilling(t *testing.T) {
	r, keyID, secret := archivedDesktopUsageRig(t)
	for _, path := range []string{
		fmt.Sprintf("/history/usage?api_key_id=%d", keyID),
		fmt.Sprintf("/history/usage/stats?api_key_id=%d", keyID),
		fmt.Sprintf("/history/keys/%d/daily?days=1", keyID),
	} {
		w := r.request("GET", path, "", r.token)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), "0.125", "usage costs must survive retirement")
		require.NotContains(t, w.Body.String(), secret)
		if path == fmt.Sprintf("/history/usage?api_key_id=%d", keyID) {
			require.Contains(t, w.Body.String(), `"name":"Aino desktop"`, "usage rows keep their original key name")
		}
	}
	other := r.user(t, service.StatusActive, false)
	pair, err := r.auth.GenerateTokenPair(r.ctx, other, "")
	require.NoError(t, err)
	for _, path := range []string{fmt.Sprintf("/history/usage?api_key_id=%d", keyID), fmt.Sprintf("/history/keys/%d/daily", keyID)} {
		require.Equal(t, http.StatusForbidden, r.request("GET", path, "", pair.AccessToken).Code)
	}
	body := fmt.Sprintf(`{"api_key_ids":[%d]}`, keyID)
	w := r.request("POST", "/history/usage/batch", body, r.token)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), fmt.Sprintf(`"%d"`, keyID), "dashboard usage must retain archived key IDs")
	require.Equal(t, http.StatusUnauthorized, r.request("GET", "/v1/models", "", secret).Code)
	_, err = r.keys.GetByID(r.ctx, keyID)
	require.ErrorIs(t, err, service.ErrAPIKeyNotFound, "ordinary key lookup must keep its deletion boundary")
}

func TestDesktopLeaseArchivedMetadataIsOptInScopedAndRedacted(t *testing.T) {
	r, keyID, secret := archivedDesktopUsageRig(t)
	manual, err := r.keys.Create(r.ctx, r.userID, service.CreateAPIKeyRequest{Name: "deleted-manual"})
	require.NoError(t, err)
	require.NoError(t, r.keys.Delete(r.ctx, manual.ID, r.userID))
	for _, sortBy := range []string{"created_at", "current_concurrency"} {
		w := r.request("GET", "/history/keys?include_archived=true&desktop_managed=true&sort_by="+sortBy, "", r.token)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var result struct {
			Data struct {
				Items []struct {
					ID     int64  `json:"id"`
					Key    string `json:"key"`
					Status string `json:"status"`
				} `json:"items"`
				Total int `json:"total"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		require.Equal(t, 1, result.Data.Total)
		require.Len(t, result.Data.Items, 1)
		require.Equal(t, keyID, result.Data.Items[0].ID)
		require.Empty(t, result.Data.Items[0].Key)
		require.Equal(t, service.StatusAPIKeyDisabled, result.Data.Items[0].Status)
		require.NotContains(t, w.Body.String(), secret)
		require.NotContains(t, w.Body.String(), "__deleted__")
		w = r.request("GET", "/history/keys?include_archived=true&desktop_managed=false", "", r.token)
		require.NotContains(t, w.Body.String(), manual.Name, "deleted ordinary keys must stay excluded")
		w = r.request("GET", "/history/keys", "", r.token)
		require.NotContains(t, w.Body.String(), "Aino desktop", "key management stays uncluttered by default")
	}
	other := r.user(t, service.StatusActive, false)
	pair, err := r.auth.GenerateTokenPair(r.ctx, other, "")
	require.NoError(t, err)
	w := r.request("GET", "/history/keys?include_archived=true", "", pair.AccessToken)
	require.Equal(t, http.StatusOK, w.Code)
	require.NotContains(t, w.Body.String(), "Aino desktop")
	require.Equal(t, http.StatusBadRequest, r.request("GET", "/history/keys?include_archived=invalid", "", r.token).Code)
}
