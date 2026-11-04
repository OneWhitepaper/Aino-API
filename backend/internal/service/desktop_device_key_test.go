package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeviceKeyAuthSnapshotAndInvalidationNeverRetainSecret(t *testing.T) {
	secret := "sk-aino-device-fixture-secret"
	stored := HashDesktopDeviceSecret(secret)
	require.NotEqual(t, secret, stored)
	svc := &APIKeyService{}
	key := &APIKey{ID: 7, UserID: 9, Key: stored, DesktopManaged: true, DesktopCredentialType: "device", DesktopDeviceID: "installation", DesktopKeyVersion: 3, KeyHashType: "sha256", User: &User{ID: 9}}
	snapshot := svc.snapshotFromAPIKey(context.Background(), key)
	data, err := json.Marshal(snapshot)
	require.NoError(t, err)
	require.NotContains(t, string(data), secret)
	require.NotContains(t, string(data), stored)
	restored := svc.snapshotToAPIKey(secret, snapshot)
	require.Equal(t, "device", restored.DesktopCredentialType)
	require.Equal(t, "installation", restored.DesktopDeviceID)
	require.Equal(t, 3, restored.DesktopKeyVersion)
	require.Equal(t, svc.authCacheKey(secret), APIKeyAuthCacheIndex(stored, "sha256"))
}
