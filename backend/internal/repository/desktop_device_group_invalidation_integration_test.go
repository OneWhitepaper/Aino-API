//go:build integration

package repository_test

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDeviceGroupImageCountBillingInvalidatesTypedKeys(t *testing.T) {
	r := newDesktopModelRig(t)
	u := r.user(t, service.StatusActive, false)
	g := r.group(t, false)
	require.NoError(t, r.userRepo.AddGroupToAllowedGroups(r.ctx, u.ID, g.ID))
	manual := &service.APIKey{UserID: u.ID, GroupID: &g.ID, Key: "sha256:" + strings.Repeat("b", 64), Name: "manual-prefix-compatible", Status: service.StatusActive}
	repo := repository.NewAPIKeyRepository(r.client, repository.GetIntegrationDB())
	require.NoError(t, repo.Create(r.ctx, manual))
	device := service.NewDesktopDeviceKeyService(repository.NewDesktopDeviceKeyRepository(r.client), r.keys)
	issued, err := device.Ensure(r.ctx, u.ID, service.DesktopDeviceKeyRequest{DeviceID: uuid.NewString()})
	require.NoError(t, err)
	manualIndex := service.APIKeyAuthCacheIndex(manual.Key, "")
	deviceIndex := strings.TrimPrefix(service.HashDesktopDeviceSecret(issued.APIKey), "sha256:")
	db := repository.GetIntegrationDB()
	_, err = db.ExecContext(r.ctx, "DELETE FROM auth_cache_invalidation_outbox WHERE cache_key IN ($1,$2)", manualIndex, deviceIndex)
	require.NoError(t, err)
	_, err = db.ExecContext(r.ctx, "UPDATE groups SET image_count_billing=NOT image_count_billing WHERE id=$1", g.ID)
	require.NoError(t, err)
	var count int
	require.NoError(t, db.QueryRowContext(r.ctx, "SELECT count(*) FROM auth_cache_invalidation_outbox WHERE cache_key=$1", manualIndex).Scan(&count))
	require.Equal(t, 1, count, "a billable image mode change must invalidate an ordinary grouped key")
	// Device identities have no group: group authority is a separate bounded
	// 15-second projection. User-identity invalidation still uses its typed index.
	_, err = db.ExecContext(r.ctx, "UPDATE users SET role='admin' WHERE id=$1", u.ID)
	require.NoError(t, err)
	require.NoError(t, db.QueryRowContext(r.ctx, "SELECT count(*) FROM auth_cache_invalidation_outbox WHERE cache_key=$1", deviceIndex).Scan(&count))
	require.Equal(t, 1, count, "device invalidation must use its stored digest without hashing it again")
}
