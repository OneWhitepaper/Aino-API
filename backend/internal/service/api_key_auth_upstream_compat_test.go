//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAuthCacheRejectsPreV0220Snapshot(t *testing.T) {
	// Aino and upstream independently used version 27 for different fields.
	// Reusing either snapshot would silently default the other branch's policy.
	for _, credentialType := range []string{"", DesktopCredentialTypeDevice} {
		t.Run(credentialType, func(t *testing.T) {
			svc := &APIKeyService{}
			entry := &APIKeyAuthCacheEntry{Snapshot: &APIKeyAuthSnapshot{
				Version: 27, APIKeyID: 42, UserID: 7, Status: StatusActive,
				DesktopCredentialType: credentialType,
			}}
			key, hit, err := svc.applyAuthCacheEntry("sk-compat-fixture", entry)
			require.NoError(t, err)
			require.False(t, hit, "pre-upgrade snapshots must be reloaded")
			require.Nil(t, key)
		})
	}
}

func TestAuthCacheCombinesDeviceImageBillingAndPrivacyPolicy(t *testing.T) {
	svc := &APIKeyService{}
	groupID := int64(3)
	price := 0.49
	input := &APIKey{
		ID: 42, UserID: 7, GroupID: &groupID, Status: StatusActive,
		DesktopManaged: true, DesktopCredentialType: DesktopCredentialTypeDevice,
		DesktopDeviceID:   "00000000-0000-4000-8000-000000000001",
		DesktopKeyVersion: 4, KeyHashType: "sha256",
		User: &User{ID: 7, Status: StatusActive, Balance: 10},
		Group: &Group{
			ID: groupID, Status: StatusActive, Platform: PlatformOpenAI,
			AllowImageGeneration: true, ImageCountBilling: true,
			ImageRateIndependent: true, ImageRateMultiplier: 0.98, ImagePrice1K: &price,
			RequirePrivacySet: true,
		},
	}
	snapshot := svc.snapshotFromAPIKey(t.Context(), input)
	require.NotNil(t, snapshot)
	key, hit, err := svc.applyAuthCacheEntry("sk-compat-fixture", &APIKeyAuthCacheEntry{Snapshot: snapshot})
	require.NoError(t, err)
	require.True(t, hit)
	require.NotNil(t, key)
	require.Equal(t, input.DesktopCredentialType, key.DesktopCredentialType)
	require.Equal(t, input.DesktopDeviceID, key.DesktopDeviceID)
	require.Equal(t, input.DesktopKeyVersion, key.DesktopKeyVersion)
	require.Equal(t, input.KeyHashType, key.KeyHashType)
	require.True(t, key.DesktopManaged)
	require.True(t, key.Group.AllowImageGeneration)
	require.True(t, key.Group.ImageCountBilling)
	require.True(t, key.Group.ImageRateIndependent)
	require.Equal(t, 0.98, key.Group.ImageRateMultiplier)
	require.Equal(t, price, *key.Group.ImagePrice1K)
	require.True(t, key.Group.RequirePrivacySet)
}
