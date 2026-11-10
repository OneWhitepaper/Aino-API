package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type deviceInfoLeaseRepo struct {
	DesktopCredentialRepository
	leases []DesktopCredential
}

func (r *deviceInfoLeaseRepo) ListByUser(context.Context, int64) ([]DesktopCredential, error) {
	return r.leases, nil
}

type deviceInfoKeyRepo struct {
	DesktopDeviceKeyRepository
	keys []APIKey
}

func (r *deviceInfoKeyRepo) ListDeviceKeys(context.Context, int64) ([]APIKey, error) {
	return r.keys, nil
}

func TestDesktopDeviceListingDistinguishesLeaseAndPermanentIdentity(t *testing.T) {
	ctx := context.Background()
	leases := &deviceInfoLeaseRepo{leases: []DesktopCredential{
		{DeviceID: "lease-only", ExpiresAt: time.Now().Add(time.Hour)},
		{DeviceID: "upgraded", ExpiresAt: time.Now().Add(time.Hour)},
	}}
	devices, err := NewDesktopCredentialService(leases, nil, nil, nil, nil).ListDevices(ctx, 7)
	require.NoError(t, err)
	keys := &deviceInfoKeyRepo{keys: []APIKey{{DesktopDeviceID: "upgraded", Status: StatusAPIKeyDisabled}}}
	devices, err = NewDesktopDeviceKeyService(keys, nil).MergeDevices(ctx, 7, devices)
	require.NoError(t, err)
	raw, err := json.Marshal(devices)
	require.NoError(t, err)
	var rows []map[string]any
	require.NoError(t, json.Unmarshal(raw, &rows))
	require.Equal(t, "lease", rows[0]["credential_type"])
	require.Equal(t, "device", rows[1]["credential_type"])
	require.NotContains(t, rows[0], "device_info", "old clients should not gain fabricated device details")
}
