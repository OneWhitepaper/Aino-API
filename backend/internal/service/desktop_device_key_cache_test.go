//go:build unit

package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestDeviceKeyInvalidationUsesExplicitStorageTypeNotPrefix(t *testing.T) {
	raw := "sha256:" + strings.Repeat("a", 64)
	cache := &authCacheStub{}
	svc := NewAPIKeyService(nil, nil, nil, nil, nil, cache, nil)
	svc.InvalidateAuthCacheByKey(context.Background(), raw)
	svc.InvalidateAuthCacheByStoredKey(context.Background(), raw, "")
	svc.InvalidateAuthCacheByStoredKey(context.Background(), raw, "sha256")
	require.Equal(t, []string{svc.authCacheKey(raw), svc.authCacheKey(raw), strings.Repeat("a", 64)}, cache.deleteAuthKeys)
}

func TestDeviceKeyStaleL2CannotExtendAuthorityOrMaskDatabaseFailure(t *testing.T) {
	for _, verified := range []time.Time{{}, time.Now().Add(-56 * time.Second)} {
		t.Run(verified.String(), func(t *testing.T) {
			entry := &APIKeyAuthCacheEntry{Snapshot: &APIKeyAuthSnapshot{Version: apiKeyAuthSnapshotVersion, APIKeyID: 42, DesktopCredentialType: DesktopCredentialTypeDevice, AuthorityVerifiedAt: verified, Status: StatusActive}}
			cache := &authCacheStub{getAuthCache: func(context.Context, string) (*APIKeyAuthCacheEntry, error) { return entry, nil }}
			calls := 0
			repo := &authRepoStub{getByKeyForAuth: func(context.Context, string) (*APIKey, error) {
				calls++
				return nil, errors.New("isolated database unavailable")
			}}
			svc := NewAPIKeyService(repo, nil, nil, nil, nil, cache, &config.Config{APIKeyAuth: config.APIKeyAuthCacheConfig{L2TTLSeconds: 300}})
			_, err := svc.GetByKey(context.Background(), "sk-aino-device-fixture")
			require.ErrorContains(t, err, "isolated database unavailable")
			require.Equal(t, 1, calls)
			require.Equal(t, verified, entry.Snapshot.AuthorityVerifiedAt)
			require.Empty(t, cache.setAuthKeys)
		})
	}
}
