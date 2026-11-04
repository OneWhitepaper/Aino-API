//go:build integration

package repository_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent/apikey"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDeviceIdentityConcurrentCreateRotationAndRevocation(t *testing.T) {
	r := newDesktopCredentialRig(t)
	repo := repository.NewDesktopDeviceKeyRepository(r.client)
	svc := service.NewDesktopDeviceKeyService(repo, r.keys)
	request := service.DesktopDeviceKeyRequest{DeviceID: r.device}
	const callers = 8
	results := make(chan *service.DesktopDeviceKeyResponse, callers)
	errors := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := svc.Ensure(r.ctx, r.userID, request)
			results <- res
			errors <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	var issued *service.DesktopDeviceKeyResponse
	var keyID string
	secrets := 0
	for result := range results {
		if keyID == "" {
			keyID = result.KeyID
		}
		require.Equal(t, keyID, result.KeyID)
		if result.APIKey != "" {
			secrets++
			issued = result
		}
	}
	require.Equal(t, 1, secrets)
	count, err := r.client.APIKey.Query().Where(apikey.UserIDEQ(r.userID), apikey.DesktopDeviceIDEQ(r.device), apikey.DesktopCredentialTypeEQ("device")).Count(r.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	stored, err := repo.GetDeviceKey(r.ctx, r.userID, r.device)
	require.NoError(t, err)
	require.Nil(t, stored.ExpiresAt)
	require.Nil(t, stored.GroupID)
	require.Equal(t, service.HashDesktopDeviceSecret(issued.APIKey), stored.Key)
	loaded, err := r.keys.GetByKey(r.ctx, issued.APIKey)
	require.NoError(t, err)
	require.Equal(t, stored.ID, loaded.ID)
	_, err = r.keys.GetByKey(r.ctx, stored.Key)
	require.Error(t, err, "stored hash must not be a Bearer credential")
	rotation := service.DesktopDeviceKeyRequest{DeviceID: r.device, Rotate: true, ExpectedVersion: &issued.Version}
	rotated, err := svc.Ensure(r.ctx, r.userID, rotation)
	require.NoError(t, err)
	require.Equal(t, keyID, rotated.KeyID)
	require.Equal(t, issued.Version+1, rotated.Version)
	require.NotEmpty(t, rotated.APIKey)
	require.NotEqual(t, issued.APIKey, rotated.APIKey)
	_, err = r.keys.GetByKey(r.ctx, issued.APIKey)
	require.Error(t, err)
	_, err = r.keys.GetByKey(r.ctx, rotated.APIKey)
	require.NoError(t, err)
	_, err = svc.Ensure(r.ctx, r.userID, rotation)
	require.ErrorIs(t, err, service.ErrDesktopDeviceKeyVersion)
	var invalidations int
	require.NoError(t, repository.GetIntegrationDB().QueryRowContext(r.ctx, "SELECT count(*) FROM auth_cache_invalidation_outbox WHERE cache_key = $1", stored.Key[len("sha256:"):]).Scan(&invalidations))
	require.Positive(t, invalidations, "rotation must enqueue the original secret's cache index without double hashing")
	require.NoError(t, svc.Revoke(r.ctx, r.userID, r.device))
	_, err = svc.Ensure(r.ctx, r.userID, request)
	require.ErrorIs(t, err, service.ErrDesktopDeviceKeyRevoked)
	_, err = svc.Ensure(r.ctx, r.userID, service.DesktopDeviceKeyRequest{DeviceID: r.device, Rotate: true, ExpectedVersion: &rotated.Version})
	require.Error(t, err)
	state, err := svc.Get(r.ctx, r.userID, r.device)
	require.NoError(t, err)
	require.Equal(t, "revoked", state.Status)
	reauthorized, err := svc.Ensure(r.ctx, r.userID, service.DesktopDeviceKeyRequest{DeviceID: r.device, Reauthorize: true, ExpectedVersion: &state.Version})
	require.NoError(t, err)
	require.Equal(t, keyID, reauthorized.KeyID)
	require.NotEmpty(t, reauthorized.APIKey)
	// Deleting the key preserves association and requires explicit reauthorization.
	keyRepo := repository.NewAPIKeyRepository(r.client, repository.GetIntegrationDB())
	require.NoError(t, r.keys.Delete(r.ctx, stored.ID, r.userID))
	state, err = svc.Get(r.ctx, r.userID, r.device)
	require.NoError(t, err)
	require.Equal(t, "revoked", state.Status)
	_, err = svc.Ensure(r.ctx, r.userID, request)
	require.ErrorIs(t, err, service.ErrDesktopDeviceKeyRevoked)
	restored, err := svc.Ensure(r.ctx, r.userID, service.DesktopDeviceKeyRequest{DeviceID: r.device, Reauthorize: true, ExpectedVersion: &state.Version})
	require.NoError(t, err)
	require.Equal(t, keyID, restored.KeyID)
	manual := &service.APIKey{UserID: r.userID, Key: "sk-manual-fixture-" + uuid.NewString(), Name: "manual", Status: service.StatusActive}
	require.NoError(t, keyRepo.Create(r.ctx, manual))
	_, err = keyRepo.GetByKeyForAuth(r.ctx, manual.Key)
	require.NoError(t, err)
	// Password policy is deliberately unchanged for new identities; an account
	// disable, and the explicit all-devices action, must still revoke them.
	_, err = r.client.User.UpdateOneID(r.userID).SetPasswordHash("changed-fixture").Save(r.ctx)
	require.NoError(t, err)
	state, err = svc.Get(r.ctx, r.userID, r.device)
	require.NoError(t, err)
	require.Equal(t, "active", state.Status)
	require.NoError(t, r.auth.RevokeAllUserSessions(r.ctx, r.userID))
	state, err = svc.Get(r.ctx, r.userID, r.device)
	require.NoError(t, err)
	require.Equal(t, "revoked", state.Status)
	manualState, err := keyRepo.GetByID(r.ctx, manual.ID)
	require.NoError(t, err)
	require.Equal(t, service.StatusActive, manualState.Status)
}

func TestDeviceIdentityRevokeWinsConcurrentRotation(t *testing.T) {
	r := newDesktopCredentialRig(t)
	svc := service.NewDesktopDeviceKeyService(repository.NewDesktopDeviceKeyRepository(r.client), r.keys)
	first, err := svc.Ensure(r.ctx, r.userID, service.DesktopDeviceKeyRequest{DeviceID: r.device})
	require.NoError(t, err)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = svc.Ensure(context.Background(), r.userID, service.DesktopDeviceKeyRequest{DeviceID: r.device, Rotate: true, ExpectedVersion: &first.Version})
	}()
	go func() { defer wg.Done(); require.NoError(t, svc.Revoke(context.Background(), r.userID, r.device)) }()
	wg.Wait()
	state, err := svc.Get(r.ctx, r.userID, r.device)
	require.NoError(t, err)
	require.Equal(t, "revoked", state.Status)
	_, err = svc.Ensure(r.ctx, r.userID, service.DesktopDeviceKeyRequest{DeviceID: r.device})
	require.ErrorIs(t, err, service.ErrDesktopDeviceKeyRevoked)
	blob, err := json.Marshal(state)
	require.NoError(t, err)
	require.NotContains(t, string(blob), "api_key")
}

func TestDeviceIdentityStaleRedisSnapshotRechecksRevocation(t *testing.T) {
	r := newDesktopCredentialRig(t)
	repo := repository.NewDesktopDeviceKeyRepository(r.client)
	svc := service.NewDesktopDeviceKeyService(repo, r.keys)
	first, err := svc.Ensure(r.ctx, r.userID, service.DesktopDeviceKeyRequest{DeviceID: r.device})
	require.NoError(t, err)
	_, err = r.keys.GetByKey(r.ctx, first.APIKey)
	require.NoError(t, err)
	cache := repository.NewAPIKeyCache(r.redis)
	index := strings.TrimPrefix(service.HashDesktopDeviceSecret(first.APIKey), "sha256:")
	entry, err := cache.GetAuthCache(r.ctx, index)
	require.NoError(t, err)
	checkedAt := time.Now().Add(-56 * time.Second)
	entry.Snapshot.AuthorityVerifiedAt = checkedAt
	require.NoError(t, cache.SetAuthCache(r.ctx, index, entry, 300*time.Second))
	// Simulate a missed invalidation event: mutate only the database.
	require.NoError(t, repo.RevokeDeviceKey(r.ctx, r.userID, r.device))
	key, err := r.keys.GetByKey(r.ctx, first.APIKey)
	require.NoError(t, err)
	require.False(t, key.IsActive(), "a stale active L2 entry must never revive a revoked device")
	entry, err = cache.GetAuthCache(r.ctx, index)
	require.NoError(t, err)
	require.Equal(t, service.StatusAPIKeyDisabled, entry.Snapshot.Status)
	require.True(t, entry.Snapshot.AuthorityVerifiedAt.After(checkedAt))
}

func TestDeviceIdentityHashPrefixDoesNotChangeLegacyManualCredentials(t *testing.T) {
	r := newDesktopCredentialRig(t)
	repo := repository.NewAPIKeyRepository(r.client, repository.GetIntegrationDB())
	raw := "sha256:" + strings.ReplaceAll(uuid.NewString(), "-", "") + strings.ReplaceAll(uuid.NewString(), "-", "")
	manual := &service.APIKey{UserID: r.userID, Key: raw, Name: "legacy hash prefix", Status: service.StatusActive}
	require.NoError(t, repo.Create(r.ctx, manual))
	key, err := r.keys.GetByKey(r.ctx, raw)
	require.NoError(t, err)
	require.Equal(t, manual.ID, key.ID)
	index := service.APIKeyAuthCacheIndex(raw, "")
	cache := repository.NewAPIKeyCache(r.redis)
	_, err = cache.GetAuthCache(r.ctx, index)
	require.NoError(t, err)
	r.keys.InvalidateAuthCacheByKey(r.ctx, raw)
	_, err = cache.GetAuthCache(r.ctx, index)
	require.Error(t, err)
	_, err = r.keys.GetByKey(r.ctx, raw)
	require.NoError(t, err)
	r.keys.InvalidateAuthCacheByUserID(r.ctx, r.userID)
	_, err = cache.GetAuthCache(r.ctx, index)
	require.Error(t, err)
	_, err = r.client.APIKey.UpdateOneID(manual.ID).SetStatus(service.StatusAPIKeyDisabled).Save(r.ctx)
	require.NoError(t, err)
	var hits int
	require.NoError(t, repository.GetIntegrationDB().QueryRowContext(r.ctx, "SELECT count(*) FROM auth_cache_invalidation_outbox WHERE cache_key=$1", index).Scan(&hits))
	require.Positive(t, hits, "legacy raw prefix must enqueue hash(raw), not its suffix")
}
