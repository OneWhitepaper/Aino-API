package dto

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestManagedKeySecretOnlyReturnedByProvisioning(t *testing.T) {
	key := &service.APIKey{ID: 42, Key: "sk-fixture-managed-secret", DesktopManaged: true}
	data, err := json.Marshal(APIKeyFromService(key))
	require.NoError(t, err)
	require.NotContains(t, string(data), key.Key)
	require.Contains(t, string(data), `"desktop_managed":true`)
	require.NotEmpty(t, key.Key)
	key.DesktopManaged = false
	require.Equal(t, key.Key, APIKeyFromService(key).Key)
}

func TestAPIKeyDTOShowsEffectiveExpiryWithoutChangingStoredStatus(t *testing.T) {
	past := time.Now().Add(-time.Minute)
	for _, status := range []string{service.StatusAPIKeyActive, "inactive", service.StatusAPIKeyDisabled, service.StatusAPIKeyQuotaExhausted} {
		key := &service.APIKey{Status: status, ExpiresAt: &past}
		want := status
		if status == service.StatusAPIKeyActive {
			want = service.StatusAPIKeyExpired
		}
		require.Equal(t, want, APIKeyFromService(key).Status)
		require.Equal(t, status, key.Status)
	}
}
