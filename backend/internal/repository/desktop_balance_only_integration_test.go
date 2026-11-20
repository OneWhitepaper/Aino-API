//go:build integration

package repository_test

import (
	"encoding/json"
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

func TestDesktopBalanceOnlyCatalogAndPermanentKeyEnforceOwnWallet(t *testing.T) {
	r := newDesktopModelRig(t)
	u := r.user(t, service.StatusActive, false)
	_, err := r.client.User.UpdateOneID(u.ID).SetBalance(25).Save(r.ctx)
	require.NoError(t, err)
	balance, subscription := r.group(t, false), r.group(t, true)
	input, output := 2e-6, 6e-6
	for _, group := range []*service.Group{balance, subscription} {
		group.ModelPricing = []service.ChannelModelPricing{{Models: []string{"fixture-policy"}, InputPrice: &input, OutputPrice: &output}}
		require.NoError(t, r.groups.Update(r.ctx, group))
	}
	require.NoError(t, r.userRepo.AddGroupToAllowedGroups(r.ctx, u.ID, balance.ID))
	sub := &service.UserSubscription{UserID: u.ID, GroupID: subscription.ID, StartsAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour), Status: service.SubscriptionStatusActive}
	require.NoError(t, r.subs.Create(r.ctx, sub))
	balanceEntry, subEntry := desktopEntry("personal-option", balance.ID), desktopEntry("subscription-option", subscription.ID)
	balanceEntry.Model, subEntry.Model = "fixture-policy", "fixture-policy"
	r.catalog(t, []service.DesktopModelEntry{balanceEntry, subEntry}, balanceEntry.ID)
	funding := service.NewTeamFundingService(repository.NewTeamFundingRepository(r.client, repository.GetIntegrationDB()))
	r.keys.SetTeamFundingService(funding)
	_, err = funding.UpdateFunding(r.ctx, u.ID, &service.FundingUpdateCommand{Mode: service.FundingModeSubscriptionOnly, AllowPersonalSubscription: true})
	require.NoError(t, err)
	token, err := r.auth.GenerateToken(r.ctx, u)
	require.NoError(t, err)
	w := r.request(http.MethodGet, "/api/v1/desktop/models?billing_policy=balance_only", "", token)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var catalog struct {
		Data []service.PlatformModel `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &catalog))
	require.Len(t, catalog.Data, 1)
	require.Equal(t, "personal-option", catalog.Data[0].ID)
	require.Equal(t, "balance", catalog.Data[0].BillingSource)
	devices := service.NewDesktopDeviceKeyService(repository.NewDesktopDeviceKeyRepository(r.client), r.keys)
	deviceID := uuid.NewString()
	issued, err := devices.Ensure(r.ctx, u.ID, service.DesktopDeviceKeyRequest{DeviceID: deviceID, BillingPolicy: "balance_only"})
	require.NoError(t, err)
	require.Equal(t, "balance_only", issued.BillingPolicy)
	for range 2 {
		key, err := r.keys.GetByKey(r.ctx, issued.APIKey)
		require.NoError(t, err)
		require.Equal(t, "balance_only", key.BillingPolicy)
	}
	rotated, err := devices.Ensure(r.ctx, u.ID, service.DesktopDeviceKeyRequest{DeviceID: deviceID, Rotate: true, ExpectedVersion: &issued.Version})
	require.NoError(t, err)
	require.Equal(t, "balance_only", rotated.BillingPolicy)
	router := gin.New()
	cfg := &config.Config{RunMode: config.RunModeStandard}
	router.POST("/v1/chat/completions", gin.HandlerFunc(middleware.ProvideAPIKeyAuthMiddleware(r.keys, nil, cfg, r.models, nil)), func(c *gin.Context) {
		key, _ := middleware.GetAPIKeyFromContext(c)
		require.Equal(t, "personal_balance", key.Funding.Source)
		require.Equal(t, u.ID, key.Funding.PayerUserID)
		require.Nil(t, key.Funding.SubscriptionID)
		c.Status(http.StatusOK)
	})
	dispatch := func(option string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"fixture-policy","messages":[{"role":"user","content":"fixture"}]}`))
		req.Header.Set("Authorization", "Bearer "+rotated.APIKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Aino-Model-ID", option)
		result := httptest.NewRecorder()
		router.ServeHTTP(result, req)
		return result
	}
	require.Equal(t, http.StatusOK, dispatch(balanceEntry.ID).Code)
	require.Equal(t, http.StatusForbidden, dispatch(subEntry.ID).Code)
	_, err = r.client.User.UpdateOneID(u.ID).SetBalance(0).Save(r.ctx)
	require.NoError(t, err)
	require.Equal(t, http.StatusForbidden, dispatch(balanceEntry.ID).Code)
	view, err := funding.GetFunding(r.ctx, u.ID)
	require.NoError(t, err)
	require.Equal(t, "subscription_only", view.Mode)
	require.False(t, view.AllowPersonalBalance)
	legacyDevice := uuid.NewString()
	_, err = devices.Ensure(r.ctx, u.ID, service.DesktopDeviceKeyRequest{DeviceID: legacyDevice})
	require.NoError(t, err)
	_, err = devices.Ensure(r.ctx, u.ID, service.DesktopDeviceKeyRequest{DeviceID: legacyDevice, BillingPolicy: "balance_only"})
	require.ErrorIs(t, err, service.ErrDesktopBillingPolicyConflict)
}
