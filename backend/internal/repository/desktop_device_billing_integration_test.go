//go:build integration

package repository_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDeviceBillingHTTPToOriginalGroupLedger(t *testing.T) {
	r := newDesktopModelRig(t)
	user := r.user(t, service.StatusActive, false)
	balanceGroup, subGroup := r.group(t, false), r.group(t, true)
	input, output := 2e-6, 6e-6
	for _, group := range []*service.Group{balanceGroup, subGroup} {
		group.ModelPricing = []service.ChannelModelPricing{{Models: []string{"fixture-shared"}, InputPrice: &input, OutputPrice: &output}}
		require.NoError(t, r.groups.Update(r.ctx, group))
	}
	require.NoError(t, r.userRepo.AddGroupToAllowedGroups(r.ctx, user.ID, balanceGroup.ID))
	now := time.Now()
	sub := &service.UserSubscription{UserID: user.ID, GroupID: subGroup.ID, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Status: service.SubscriptionStatusActive, DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now}
	require.NoError(t, r.subs.Create(r.ctx, sub))
	balanceEntry, subEntry := desktopEntry("balance-option", balanceGroup.ID), desktopEntry("subscription-option", subGroup.ID)
	balanceEntry.Model, subEntry.Model = "fixture-shared", "fixture-shared"
	balanceEntry.APIMode, subEntry.APIMode = "responses", "responses"
	r.catalog(t, []service.DesktopModelEntry{balanceEntry, subEntry}, balanceEntry.ID)
	device := service.NewDesktopDeviceKeyService(repository.NewDesktopDeviceKeyRepository(r.client), r.keys)
	identity, err := device.Ensure(r.ctx, user.ID, service.DesktopDeviceKeyRequest{DeviceID: uuid.NewString()})
	require.NoError(t, err)
	require.NotEmpty(t, identity.APIKey)
	db := repository.GetIntegrationDB()
	logs := repository.NewUsageLogRepository(r.client, db)
	accountRow, err := r.client.Account.Create().SetName("device-billing-fixture").SetPlatform(service.PlatformOpenAI).SetType(service.AccountTypeAPIKey).SetCredentials(map[string]any{}).Save(r.ctx)
	require.NoError(t, err)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", req.Header.Get("X-Test-Request"))
		_, _ = io.WriteString(w, `{"id":"resp_fixture","status":"completed","model":"fixture-shared","output":[],"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`)
	}))
	defer upstream.Close()
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.AllowPrivateHosts = true
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	billingCache := service.NewBillingCacheService(nil, r.userRepo, r.subs, nil, nil, r.rates, cfg, nil)
	t.Cleanup(billingCache.Stop)
	gateway := service.NewOpenAIGatewayService(nil, logs, repository.NewUsageBillingRepository(r.client, db), r.userRepo, r.subs, r.rates, nil, cfg, nil, nil, r.billing, nil, billingCache, repository.NewHTTPUpstream(cfg), &service.DeferredService{}, nil, nil, r.pricing, nil, nil, nil, nil)
	subscriptions := service.NewSubscriptionService(r.groups, r.subs, billingCache, r.client, cfg)
	t.Cleanup(subscriptions.Stop)
	router := gin.New()
	router.POST("/v1/responses", gin.HandlerFunc(middleware.ProvideAPIKeyAuthMiddleware(r.keys, subscriptions, cfg, r.models, nil)), func(c *gin.Context) {
		key, _ := middleware.GetAPIKeyFromContext(c)
		subscription, _ := middleware.GetSubscriptionFromContext(c)
		body, readErr := io.ReadAll(c.Request.Body)
		require.NoError(t, readErr)
		account := &service.Account{ID: accountRow.ID, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"base_url": upstream.URL, "api_key": "upstream-fixture"}}
		result, forwardErr := gateway.Forward(c.Request.Context(), c, account, body)
		require.NoError(t, forwardErr)
		require.NotNil(t, result)
		// This is the same detached usage service and transactional ledger called
		// by the production gateway after a controlled upstream completes.
		result.RequestID = c.GetHeader("X-Test-Request")
		require.NoError(t, gateway.RecordUsage(context.Background(), &service.OpenAIRecordUsageInput{Result: result, APIKey: key, User: key.User, Account: account, Subscription: subscription}))
	})
	dispatch := func(option, requestID string) {
		req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"fixture-shared","input":"hi","stream":false}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+identity.APIKey)
		req.Header.Set("X-Aino-Model-ID", option)
		req.Header.Set("X-Test-Request", requestID)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	}
	balanceRequest, subRequest := uuid.NewString(), uuid.NewString()
	var before, after float64
	require.NoError(t, db.QueryRowContext(r.ctx, "SELECT balance FROM users WHERE id=$1", user.ID).Scan(&before))
	dispatch(balanceEntry.ID, balanceRequest)
	dispatch(subEntry.ID, subRequest)
	// A replayed upstream request may be logged twice but must debit exactly once.
	dispatch(subEntry.ID, subRequest)
	require.NoError(t, db.QueryRowContext(r.ctx, "SELECT balance FROM users WHERE id=$1", user.ID).Scan(&after))
	require.InDelta(t, 0.0001, before-after, 1e-8)
	var subUsed float64
	require.NoError(t, db.QueryRowContext(r.ctx, "SELECT daily_usage_usd FROM user_subscriptions WHERE id=$1", sub.ID).Scan(&subUsed))
	require.InDelta(t, 0.0001, subUsed, 1e-8)
	for _, expected := range []struct {
		request      string
		group        int64
		subscription *int64
	}{{balanceRequest, balanceGroup.ID, nil}, {subRequest, subGroup.ID, &sub.ID}} {
		var rowID int64
		require.NoError(t, db.QueryRowContext(r.ctx, "SELECT id FROM usage_logs WHERE request_id=$1 ORDER BY id LIMIT 1", expected.request).Scan(&rowID))
		row, err := logs.GetByID(r.ctx, rowID)
		require.NoError(t, err)
		require.Equal(t, expected.group, *row.GroupID)
		require.Equal(t, expected.subscription, row.SubscriptionID)
		require.Equal(t, fmt.Sprint(row.APIKeyID), identity.KeyID)
	}
	stored, err := repository.NewDesktopDeviceKeyRepository(r.client).GetDeviceKey(r.ctx, user.ID, identity.DeviceID)
	require.NoError(t, err)
	require.Nil(t, stored.GroupID)
	require.Nil(t, stored.ExpiresAt)
}
