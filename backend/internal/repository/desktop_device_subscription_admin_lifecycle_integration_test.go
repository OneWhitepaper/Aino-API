//go:build integration && nativeconsumer

package repository_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Every administration change crosses the real HTTP boundary after both API
// instances have served the same identity. The database writes below only seed
// disposable users and a short natural expiry; none bypass an admin operation.
func TestDeviceSubscriptionAdminLifecycleSameKeyHTTP(t *testing.T) {
	f := newDevicePreflight(t, t.TempDir())
	admin := f.r.user(t, service.StatusActive, false)
	db := repository.GetIntegrationDB()
	_, err := db.ExecContext(f.r.ctx, "UPDATE users SET role='admin' WHERE id=$1", admin.ID)
	require.NoError(t, err)
	// Seed an already-onboarded synthetic administrator, not a real operator's
	// legal acknowledgement or a bypass of the production admin middleware.
	ack, err := json.Marshal(service.AdminComplianceAcknowledgement{Version: service.AdminComplianceVersion, AdminUserID: admin.ID, AcceptedAt: time.Now()})
	require.NoError(t, err)
	ackKey := fmt.Sprintf("admin_compliance_acknowledgement:%d", admin.ID)
	require.NoError(t, f.r.settingRepo.Set(f.r.ctx, ackKey, string(ack)))
	t.Cleanup(func() { _ = f.r.settingRepo.Delete(f.r.ctx, ackKey) })
	f.startServers(t)
	adminLogin := &devicePreflight{origins: f.origins, email: admin.Email, password: f.password}
	adminToken := adminLogin.login(t)
	identity := f.device(t, f.login(t), service.DesktopDeviceKeyRequest{DeviceID: uuid.NewString()})
	require.NotEmpty(t, identity.APIKey)
	keyRepo := repository.NewDesktopDeviceKeyRepository(f.r.client)
	initial, err := keyRepo.GetDeviceKey(f.r.ctx, f.userID, identity.DeviceID)
	require.NoError(t, err)
	assertIdentity := func() {
		t.Helper()
		stored, getErr := keyRepo.GetDeviceKey(f.r.ctx, f.userID, identity.DeviceID)
		require.NoError(t, getErr)
		require.Equal(t, identity.KeyID, fmt.Sprint(stored.ID))
		require.Equal(t, initial.Key, stored.Key, "package or model permission changes must not rotate the device secret")
		require.Equal(t, initial.DesktopKeyVersion, stored.DesktopKeyVersion)
		require.Nil(t, stored.GroupID)
		require.Nil(t, stored.ExpiresAt)
	}
	adminChange := func(instance int, method, path string, body any) {
		t.Helper()
		status, raw, _ := f.call(t, instance, method, path, adminToken, body, map[string]string{"Idempotency-Key": uuid.NewString()})
		require.Equal(t, http.StatusOK, status, string(raw))
		assertIdentity()
	}
	packages := []struct {
		sub    *service.UserSubscription
		option string
		cost   float64
	}{
		{f.subText, "fixture-text-subscription", 0.0001},
		{f.subImage, nativeSubscriptionImageCatalogID, 0.08},
	}
	acceptPackages := func() {
		t.Helper()
		for instance := range 2 {
			for _, pkg := range packages {
				subscriptionLifecycleAccept(t, f, instance, identity, pkg.option, pkg.sub.GroupID, &pkg.sub.ID, pkg.cost)
			}
		}
		assertIdentity()
	}

	// Seed after process startup, then warm all caches before natural expiry.
	expires := time.Now().Add(20 * time.Second)
	for _, pkg := range packages {
		pkg.sub.ExpiresAt = expires
		require.NoError(t, f.r.subs.Update(f.r.ctx, pkg.sub))
	}
	acceptPackages()
	subscriptionLifecycleAssertState(t, f, 37.5, 0.0002, 0.16, 4)
	t.Log("ADMIN_LIFECYCLE stage=warm accepted=4 api_processes=2 key_issuance=1 wallet_debit=0")

	// A completed revoke must deny the first subsequent request on both warm
	// processes. A funded wallet must never rescue a selected package silently.
	for i, pkg := range packages {
		adminChange(i%2, http.MethodPost, fmt.Sprintf("/api/v1/admin/subscriptions/%d/revoke", pkg.sub.ID), nil)
		for instance := range 2 {
			subscriptionLifecycleReject(t, f, instance, identity.APIKey, pkg.option, http.StatusForbidden)
		}
	}
	subscriptionLifecycleAssertState(t, f, 37.5, 0.0002, 0.16, 4)
	t.Log("ADMIN_LIFECYCLE stage=revoked denied=4 first_request_denied=true wallet_fallback=false")
	for i, pkg := range packages {
		adminChange((i+1)%2, http.MethodPost, fmt.Sprintf("/api/v1/admin/subscriptions/%d/restore", pkg.sub.ID), nil)
	}
	acceptPackages()
	subscriptionLifecycleAssertState(t, f, 37.5, 0.0004, 0.32, 8)
	require.Positive(t, time.Until(expires), "revocation/restoration must complete before the seeded natural expiry")
	t.Log("ADMIN_LIFECYCLE stage=restored accepted=4 original_subscription_ids=true immediate_settlement=true")

	require.Eventually(t, func() bool { return time.Now().After(expires) }, 21*time.Second, 20*time.Millisecond)
	for instance := range 2 {
		for _, pkg := range packages {
			subscriptionLifecycleReject(t, f, instance, identity.APIKey, pkg.option, http.StatusForbidden)
		}
	}
	assertIdentity()
	t.Log("ADMIN_LIFECYCLE stage=expired natural_lifetime_seconds=20 denied=4 wallet_fallback=false")
	for i, pkg := range packages {
		adminChange(i%2, http.MethodPost, fmt.Sprintf("/api/v1/admin/subscriptions/%d/extend", pkg.sub.ID), map[string]int{"days": 1})
	}
	acceptPackages()
	subscriptionLifecycleAssertState(t, f, 37.5, 0.0006, 0.48, 12)
	t.Log("ADMIN_LIFECYCLE stage=extended accepted=4 same_key_and_subscription_ids=true")

	// Permission is checked against the current group policy even when the UI
	// catalog and both inference identities were warmed by successful requests.
	textGroup, err := f.r.groups.GetByID(f.r.ctx, f.subText.GroupID)
	require.NoError(t, err)
	groupPath := fmt.Sprintf("/api/v1/admin/groups/%d", textGroup.ID)
	adminChange(0, http.MethodPut, groupPath, map[string]any{"model_allowlist": service.GroupModelAllowlist{Enabled: true, Models: []string{"fixture-other-model"}}})
	for instance := range 2 {
		subscriptionLifecycleReject(t, f, instance, identity.APIKey, "fixture-text-subscription", http.StatusForbidden)
		subscriptionLifecycleAccept(t, f, instance, identity, nativeSubscriptionImageCatalogID, f.subImage.GroupID, &f.subImage.ID, 0.08)
	}
	subscriptionLifecycleAssertState(t, f, 37.5, 0.0006, 0.64, 14)
	adminChange(1, http.MethodPut, groupPath, map[string]any{"model_allowlist": textGroup.ModelAllowlist})
	for instance := range 2 {
		subscriptionLifecycleAccept(t, f, instance, identity, "fixture-text-subscription", f.subText.GroupID, &f.subText.ID, 0.0001)
	}
	subscriptionLifecycleAssertState(t, f, 37.5, 0.0008, 0.64, 16)
	t.Log("ADMIN_LIFECYCLE stage=model_permission denied=2 restored_accepted=2 other_package_unaffected=true")

	subscriptionLifecycleAccept(t, f, 0, identity, "fixture-tool-model", f.textGroup, nil, 0.0001)
	subscriptionLifecycleAccept(t, f, 1, identity, nativeImageCatalogID, f.textGroup, nil, 0.08)
	subscriptionLifecycleAssertState(t, f, 37.5-0.0801, 0.0008, 0.64, 18)
	assertIdentity()
	require.Equal(t, int64(1), f.keyPosts.Load(), "all lifecycle stages must reuse the initial device issuance")
	t.Log("ADMIN_LIFECYCLE final accepted=18 denied=10 key_id_version_secret_unchanged=true stored_group_and_expiry_null=true explicit_wallet_debit=0.08010000 text_package_usage=0.00080000 image_package_usage=0.64000000 production_changes=false")
}
