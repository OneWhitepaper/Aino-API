//go:build integration && nativeconsumer

package repository_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type subscriptionLifecycleState struct {
	Balance                   float64
	Text, Image               [3]float64
	LedgerRows, UpstreamCalls int
}

func subscriptionLifecycleSnapshot(t *testing.T, f *devicePreflight) subscriptionLifecycleState {
	t.Helper()
	db := repository.GetIntegrationDB()
	var state subscriptionLifecycleState
	require.NoError(t, db.QueryRowContext(f.r.ctx, "SELECT balance FROM users WHERE id=$1", f.userID).Scan(&state.Balance))
	for i, sub := range []*service.UserSubscription{f.subText, f.subImage} {
		var usage [3]float64
		require.NoError(t, db.QueryRowContext(f.r.ctx, "SELECT daily_usage_usd,weekly_usage_usd,monthly_usage_usd FROM user_subscriptions WHERE id=$1", sub.ID).Scan(&usage[0], &usage[1], &usage[2]))
		if i == 0 {
			state.Text = usage
		} else {
			state.Image = usage
		}
	}
	require.NoError(t, db.QueryRowContext(f.r.ctx, "SELECT count(*) FROM usage_logs WHERE user_id=$1", f.userID).Scan(&state.LedgerRows))
	state.UpstreamCalls = int(f.r.modelCalls.Load()) + len(f.protocol.images.snapshot())
	return state
}

func subscriptionLifecycleAssertState(t *testing.T, f *devicePreflight, balance, text, image float64, rows int) {
	t.Helper()
	state := subscriptionLifecycleSnapshot(t, f)
	require.InDelta(t, balance, state.Balance, 1e-8)
	for _, usage := range state.Text {
		require.InDelta(t, text, usage, 1e-8, "text subscription owns all three usage windows")
	}
	for _, usage := range state.Image {
		require.InDelta(t, image, usage, 1e-8, "image subscription owns all three usage windows")
	}
	require.Equal(t, rows, state.LedgerRows)
	require.Equal(t, rows, state.UpstreamCalls)
}

func subscriptionLifecycleCall(t *testing.T, f *devicePreflight, instance int, key, option string) (string, int, []byte) {
	t.Helper()
	callID := uuid.NewString()
	path, purpose := "/v1/chat/completions", "chat"
	body := map[string]any{"model": "fixture-tool-model", "messages": []any{map[string]string{"role": "user", "content": "subscription lifecycle " + callID}}, "stream": false}
	if strings.Contains(option, "image") {
		path, purpose = "/v1/images/generations", "image"
		body = map[string]any{"model": nativeImageUpstreamModel, "prompt": "fixture-image-generate " + callID, "quality": "auto", "size": "1024x1024", "n": 1}
	}
	status, raw, _ := f.call(t, instance, http.MethodPost, path, key, body, map[string]string{"X-Aino-Model-ID": option, "X-Aino-Session-Id": uuid.NewString(), "X-Aino-Turn-Id": uuid.NewString(), "X-Aino-Call-Id": callID, "X-Aino-Purpose": purpose})
	return callID, status, raw
}

func subscriptionLifecycleAccept(t *testing.T, f *devicePreflight, instance int, identity *service.DesktopDeviceKeyResponse, option string, groupID int64, subscriptionID *int64, cost float64) {
	t.Helper()
	callID, status, raw := subscriptionLifecycleCall(t, f, instance, identity.APIKey, option)
	require.Equal(t, http.StatusOK, status, string(raw))
	db := repository.GetIntegrationDB()
	var rows int
	require.Eventually(t, func() bool {
		return db.QueryRowContext(f.r.ctx, "SELECT count(*) FROM usage_logs WHERE user_id=$1 AND desktop_call_id=$2", f.userID, callID).Scan(&rows) == nil && rows == 1
	}, 10*time.Second, 20*time.Millisecond, "accepted request must settle through the production usage worker")
	var keyID, actualGroup int64
	var actualSub *int64
	var actualCost float64
	var settlement string
	require.NoError(t, db.QueryRowContext(f.r.ctx, "SELECT api_key_id,group_id,subscription_id,actual_cost,settlement_status FROM usage_logs WHERE user_id=$1 AND desktop_call_id=$2", f.userID, callID).Scan(&keyID, &actualGroup, &actualSub, &actualCost, &settlement))
	require.Equal(t, identity.KeyID, fmt.Sprint(keyID))
	require.Equal(t, groupID, actualGroup, "catalog choice must retain its requested billing group")
	require.Equal(t, subscriptionID, actualSub)
	require.InDelta(t, cost, actualCost, 1e-8)
	require.Equal(t, "settled", settlement)
}

func subscriptionLifecycleReject(t *testing.T, f *devicePreflight, instance int, key, option string, expectedStatus int) {
	t.Helper()
	before := subscriptionLifecycleSnapshot(t, f)
	_, status, raw := subscriptionLifecycleCall(t, f, instance, key, option)
	require.Equal(t, expectedStatus, status, string(raw))
	require.Equal(t, before, subscriptionLifecycleSnapshot(t, f), "a rejected catalog choice must not reach an upstream, consume another package, or debit the wallet")
	t.Logf("DENIED option=%s instance=%d status=%d ledger_and_upstream_unchanged=true", option, instance+1, status)
}

func subscriptionLifecycleSetBalance(t *testing.T, f *devicePreflight, balance float64) {
	t.Helper()
	_, err := f.r.userRepo.SetBalance(f.r.ctx, f.userID, balance)
	require.NoError(t, err)
	require.NoError(t, repository.NewBillingCache(repository.GetIntegrationRedis()).InvalidateUserBalance(f.r.ctx, f.userID))
}

func subscriptionLifecycleCatalog(t *testing.T, f *devicePreflight, token string, states map[string]string) {
	t.Helper()
	status, raw, _ := f.call(t, 0, http.MethodGet, "/api/v1/desktop/models?include_images=true", token, nil, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	var result struct {
		Data []service.PlatformModel `json:"data"`
	}
	require.NoError(t, json.Unmarshal(raw, &result))
	seen := map[string]bool{}
	for _, model := range result.Data {
		state, wanted := states[model.ID]
		if !wanted {
			continue
		}
		seen[model.ID] = true
		require.Equal(t, state, model.State, model.ID)
		billing := "balance"
		if strings.Contains(model.ID, "subscription") {
			billing = "subscription"
		}
		require.Equal(t, billing, model.BillingSource, model.ID)
	}
	require.Len(t, seen, len(states), "all requested catalog options must be represented")
}

func TestDeviceSubscriptionLifecycleQuotaAndWalletHTTP(t *testing.T) {
	f := newDevicePreflight(t, t.TempDir())
	for i, sub := range []*service.UserSubscription{f.subText, f.subImage} {
		group, err := f.r.groups.GetByID(f.r.ctx, sub.GroupID)
		require.NoError(t, err)
		limit := 0.0001
		if i == 1 {
			limit = 0.16
		}
		group.DailyLimitUSD = &limit
		// The request router explicitly clears legacy group fallbacks for device keys.
		group.FallbackGroupID = &f.textGroup
		require.NoError(t, f.r.groups.Update(f.r.ctx, group))
	}
	f.startServers(t)
	token := f.login(t)
	identity := f.device(t, token, service.DesktopDeviceKeyRequest{DeviceID: uuid.NewString()})
	repeated := f.device(t, token, service.DesktopDeviceKeyRequest{DeviceID: identity.DeviceID})
	require.Equal(t, identity.KeyID, repeated.KeyID)
	require.Empty(t, repeated.APIKey)
	subscriptionLifecycleCatalog(t, f, token, map[string]string{"fixture-tool-model": "available", nativeImageCatalogID: "available", "fixture-text-subscription": "available", nativeSubscriptionImageCatalogID: "available"})

	subscriptionLifecycleAccept(t, f, 0, identity, "fixture-tool-model", f.textGroup, nil, 0.0001)
	subscriptionLifecycleAccept(t, f, 1, identity, nativeImageCatalogID, f.textGroup, nil, 0.08)
	subscriptionLifecycleAssertState(t, f, 37.5-0.0801, 0, 0, 2)

	// A paid package remains usable with an empty wallet; each request chooses its owner.
	subscriptionLifecycleSetBalance(t, f, 0)
	subscriptionLifecycleAccept(t, f, 0, identity, "fixture-text-subscription", f.subText.GroupID, &f.subText.ID, 0.0001)
	subscriptionLifecycleAccept(t, f, 1, identity, nativeSubscriptionImageCatalogID, f.subImage.GroupID, &f.subImage.ID, 0.08)
	subscriptionLifecycleAssertState(t, f, 0, 0.0001, 0.08, 4)
	subscriptionLifecycleReject(t, f, 1, identity.APIKey, "fixture-text-subscription", http.StatusForbidden)
	// Text exhaustion must not consume the image package's remaining allowance.
	subscriptionLifecycleAccept(t, f, 0, identity, nativeSubscriptionImageCatalogID, f.subImage.GroupID, &f.subImage.ID, 0.08)
	subscriptionLifecycleAssertState(t, f, 0, 0.0001, 0.16, 5)
	subscriptionLifecycleReject(t, f, 1, identity.APIKey, nativeSubscriptionImageCatalogID, http.StatusForbidden)
	subscriptionLifecycleReject(t, f, 0, identity.APIKey, "fixture-tool-model", http.StatusForbidden)
	subscriptionLifecycleReject(t, f, 1, identity.APIKey, nativeImageCatalogID, http.StatusForbidden)

	// A funded wallet is an explicit catalog choice, never an automatic subscription fallback.
	subscriptionLifecycleSetBalance(t, f, 37.5)
	subscriptionLifecycleReject(t, f, 0, identity.APIKey, "fixture-text-subscription", http.StatusForbidden)
	subscriptionLifecycleReject(t, f, 1, identity.APIKey, nativeSubscriptionImageCatalogID, http.StatusForbidden)
	subscriptionLifecycleAccept(t, f, 1, identity, "fixture-tool-model", f.textGroup, nil, 0.0001)
	subscriptionLifecycleAccept(t, f, 0, identity, nativeImageCatalogID, f.textGroup, nil, 0.08)
	subscriptionLifecycleAssertState(t, f, 37.5-0.0801, 0.0001, 0.16, 7)
	subscriptionLifecycleCatalog(t, f, token, map[string]string{"fixture-tool-model": "available", nativeImageCatalogID: "available", "fixture-text-subscription": "quota_exhausted", nativeSubscriptionImageCatalogID: "quota_exhausted"})
	stored, err := repository.NewDesktopDeviceKeyRepository(f.r.client).GetDeviceKey(f.r.ctx, f.userID, identity.DeviceID)
	require.NoError(t, err)
	require.Nil(t, stored.GroupID)
	require.Nil(t, stored.ExpiresAt)
	t.Log("QUOTA accepted=7 denied=6 wallet_debit_per_explicit_pair=0.08010000 text_usage=0.00010000 image_usage=0.16000000 shared_device_identity=true")
}

func TestDeviceSubscriptionLifecycleExpiryHTTP(t *testing.T) {
	f := newDevicePreflight(t, t.TempDir())
	// Each real process may need 45 seconds to start; expiry measures requests, not startup.
	f.startServers(t)
	expires := time.Now().Add(20 * time.Second)
	for _, sub := range []*service.UserSubscription{f.subText, f.subImage} {
		sub.ExpiresAt = expires
		require.NoError(t, f.r.subs.Update(f.r.ctx, sub))
	}
	identity := f.device(t, f.login(t), service.DesktopDeviceKeyRequest{DeviceID: uuid.NewString()})
	for instance := range 2 {
		subscriptionLifecycleAccept(t, f, instance, identity, "fixture-text-subscription", f.subText.GroupID, &f.subText.ID, 0.0001)
		subscriptionLifecycleAccept(t, f, instance, identity, nativeSubscriptionImageCatalogID, f.subImage.GroupID, &f.subImage.ID, 0.08)
	}
	subscriptionLifecycleAssertState(t, f, 37.5, 0.0002, 0.16, 4)
	warmupRemaining := time.Until(expires)
	require.Positive(t, warmupRemaining, "both process caches must be warm before the stored expiry")
	t.Logf("EXPIRY warmup_remaining_ms=%d startup_excluded=true", warmupRemaining.Milliseconds())
	// Warm both processes, then cross the stored timestamp without invalidating caches.
	require.Eventually(t, func() bool { return time.Now().After(expires) }, 21*time.Second, 20*time.Millisecond)
	for instance := range 2 {
		subscriptionLifecycleReject(t, f, instance, identity.APIKey, "fixture-text-subscription", http.StatusForbidden)
		subscriptionLifecycleReject(t, f, instance, identity.APIKey, nativeSubscriptionImageCatalogID, http.StatusForbidden)
	}
	subscriptionLifecycleAccept(t, f, 0, identity, "fixture-tool-model", f.textGroup, nil, 0.0001)
	subscriptionLifecycleAccept(t, f, 1, identity, nativeImageCatalogID, f.textGroup, nil, 0.08)
	subscriptionLifecycleAssertState(t, f, 37.5-0.0801, 0.0002, 0.16, 6)
	stored, err := repository.NewDesktopDeviceKeyRepository(f.r.client).GetDeviceKey(f.r.ctx, f.userID, identity.DeviceID)
	require.NoError(t, err)
	require.Nil(t, stored.ExpiresAt, "package expiry does not expire the reusable device identity")
	t.Log("EXPIRY synthetic_lifetime_seconds=20 cached_instances=2 expired_package_denials=4 wallet_fallback=false device_key_remains_usable=true real_24h_observation=false")
}
