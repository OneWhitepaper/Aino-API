//go:build integration

package repository_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Hash the stored credential before assertions so a failed comparison cannot log it.
type desktopDeviceInfoKeyState struct {
	ID, UserID                   int64
	SecretDigest                 [32]byte
	Version                      int
	Name, Status, CredentialType string
	DeviceID, HashType           string
	GroupID                      *int64
	ExpiresAt, LastUsedAt        *time.Time
	DeletedAt                    *time.Time
	Quota, QuotaUsed             float64
	RateLimits, Usage            [3]float64
	WindowStarts                 [3]*time.Time
}

func desktopDeviceInfoKeySnapshot(t *testing.T, r *desktopCredentialRig, userID int64, deviceID string) desktopDeviceInfoKeyState {
	t.Helper()
	key, err := repository.NewDesktopDeviceKeyRepository(r.client).GetDeviceKey(r.ctx, userID, deviceID)
	require.NoError(t, err)
	return desktopDeviceInfoKeyState{
		ID: key.ID, UserID: key.UserID, SecretDigest: sha256.Sum256([]byte(key.Key)),
		Version: key.DesktopKeyVersion, Name: key.Name, Status: key.Status,
		CredentialType: key.DesktopCredentialType, DeviceID: key.DesktopDeviceID, HashType: key.KeyHashType,
		GroupID: key.GroupID, ExpiresAt: key.ExpiresAt, LastUsedAt: key.LastUsedAt, DeletedAt: key.DeletedAt,
		Quota: key.Quota, QuotaUsed: key.QuotaUsed,
		RateLimits:   [3]float64{key.RateLimit5h, key.RateLimit1d, key.RateLimit7d},
		Usage:        [3]float64{key.Usage5h, key.Usage1d, key.Usage7d},
		WindowStarts: [3]*time.Time{key.Window5hStart, key.Window1dStart, key.Window7dStart},
	}
}

func desktopDeviceInfoListing(t *testing.T, r *desktopCredentialRig, token string) map[string]map[string]json.RawMessage {
	t.Helper()
	response := r.request(http.MethodGet, "/api/v1/desktop/devices", "", token)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	var envelope struct {
		Data []map[string]json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	devices := make(map[string]map[string]json.RawMessage, len(envelope.Data))
	for _, device := range envelope.Data {
		var id string
		require.NoError(t, json.Unmarshal(device["device_id"], &id))
		require.NotContains(t, device, "api_key")
		devices[id] = device
	}
	return devices
}

func TestDesktopDeviceInfoPersistsWithoutChangingIdentityOrBilling(t *testing.T) {
	r := newDesktopCredentialRig(t)
	device := service.NewDesktopDeviceKeyService(repository.NewDesktopDeviceKeyRepository(r.client), r.keys)
	identity, err := device.Ensure(r.ctx, r.userID, service.DesktopDeviceKeyRequest{DeviceID: r.device})
	require.NoError(t, err)
	key, err := repository.NewDesktopDeviceKeyRepository(r.client).GetDeviceKey(r.ctx, r.userID, r.device)
	require.NoError(t, err)
	window := time.Now().UTC().Truncate(time.Second)
	_, err = r.client.APIKey.UpdateOneID(key.ID).SetQuota(19.5).SetQuotaUsed(2.25).
		SetRateLimit5h(5).SetRateLimit1d(10).SetRateLimit7d(15).
		SetUsage5h(0.25).SetUsage1d(0.5).SetUsage7d(1).
		SetWindow5hStart(window).SetWindow1dStart(window).SetWindow7dStart(window).Save(r.ctx)
	require.NoError(t, err)
	before := desktopDeviceInfoKeySnapshot(t, r, r.userID, r.device)
	userBefore, err := r.userRepo.GetByID(r.ctx, r.userID)
	require.NoError(t, err)
	wanted := service.DesktopDeviceInfo{InstallationName: "Work Mac", OS: "macos", ClientType: "desktop", ClientVersion: "1.2.3"}
	updated, err := r.credentials.UpdateDeviceInfo(r.ctx, r.userID, r.device, wanted)
	require.NoError(t, err)
	require.Equal(t, wanted, *updated)

	fresh := service.NewDesktopCredentialService(repository.NewDesktopCredentialRepository(r.client), r.keys, r.models, r.auth, repository.NewRefreshTokenCache(r.redis))
	attached, err := fresh.AttachDeviceInfo(r.ctx, r.userID, []service.DesktopDevice{{DeviceID: r.device}})
	require.NoError(t, err)
	require.Len(t, attached, 1)
	require.NotNil(t, attached[0].DeviceInfo)
	require.Equal(t, wanted, *attached[0].DeviceInfo)

	wanted.InstallationName, wanted.ClientVersion = "Renamed Mac", "1.2.4"
	body, err := json.Marshal(wanted)
	require.NoError(t, err)
	response := r.request(http.MethodPut, "/api/v1/desktop/devices/"+r.device+"/info", string(body), r.token)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	attached, err = fresh.AttachDeviceInfo(r.ctx, r.userID, []service.DesktopDevice{{DeviceID: r.device}})
	require.NoError(t, err)
	require.NotNil(t, attached[0].DeviceInfo)
	require.Equal(t, wanted, *attached[0].DeviceInfo)
	require.Equal(t, before, desktopDeviceInfoKeySnapshot(t, r, r.userID, r.device))
	userAfter, err := r.userRepo.GetByID(r.ctx, r.userID)
	require.NoError(t, err)
	require.Equal(t, userBefore.Balance, userAfter.Balance)
	state, err := device.Ensure(r.ctx, r.userID, service.DesktopDeviceKeyRequest{DeviceID: r.device})
	require.NoError(t, err)
	require.Equal(t, identity.KeyID, state.KeyID)
	require.Equal(t, identity.Version, state.Version)
	require.Empty(t, state.APIKey, "updating display information must not issue another secret")
}

func TestDesktopDeviceInfoConcurrentUpsertsAreAtomic(t *testing.T) {
	r := newDesktopCredentialRig(t)
	device := service.NewDesktopDeviceKeyService(repository.NewDesktopDeviceKeyRepository(r.client), r.keys)
	_, err := device.Ensure(r.ctx, r.userID, service.DesktopDeviceKeyRequest{DeviceID: r.device})
	require.NoError(t, err)
	before := desktopDeviceInfoKeySnapshot(t, r, r.userID, r.device)
	const callers = 8
	results := make(chan error, callers)
	start := make(chan struct{})
	wanted := make([]service.DesktopDeviceInfo, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wanted[i] = service.DesktopDeviceInfo{InstallationName: fmt.Sprintf("Workstation %d", i), OS: "linux", ClientType: "desktop", ClientVersion: fmt.Sprintf("3.0.%d", i)}
		wg.Add(1)
		go func(info service.DesktopDeviceInfo) {
			defer wg.Done()
			<-start
			actual, err := r.credentials.UpdateDeviceInfo(r.ctx, r.userID, r.device, info)
			if err == nil && (actual == nil || *actual != info) {
				err = fmt.Errorf("upsert did not return its complete submitted information")
			}
			results <- err
		}(wanted[i])
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	attached, err := r.credentials.AttachDeviceInfo(r.ctx, r.userID, []service.DesktopDevice{{DeviceID: r.device}})
	require.NoError(t, err)
	require.NotNil(t, attached[0].DeviceInfo)
	require.Contains(t, wanted, *attached[0].DeviceInfo, "the stored row must contain one complete update")
	var rows int
	require.NoError(t, repository.GetIntegrationDB().QueryRowContext(r.ctx, "SELECT count(*) FROM desktop_device_info WHERE user_id=$1 AND device_id=$2", r.userID, r.device).Scan(&rows))
	require.Equal(t, 1, rows)
	require.Equal(t, before, desktopDeviceInfoKeySnapshot(t, r, r.userID, r.device))
}

func TestDesktopDeviceInfoRequiresOwnedAssociationAndIsolatesAccounts(t *testing.T) {
	r := newDesktopCredentialRig(t)
	device := service.NewDesktopDeviceKeyService(repository.NewDesktopDeviceKeyRepository(r.client), r.keys)
	_, err := device.Ensure(r.ctx, r.userID, service.DesktopDeviceKeyRequest{DeviceID: r.device})
	require.NoError(t, err)
	ownerInfo := service.DesktopDeviceInfo{InstallationName: "Owner installation", OS: "macos", ClientType: "desktop", ClientVersion: "1.0"}
	_, err = r.credentials.UpdateDeviceInfo(r.ctx, r.userID, r.device, ownerInfo)
	require.NoError(t, err)
	other := r.user(t, service.StatusActive, false)
	_, err = r.credentials.UpdateDeviceInfo(r.ctx, other.ID, r.device, ownerInfo)
	require.Error(t, err, "another account's existing device is not an ownership grant")
	unregistered := uuid.NewString()
	_, err = r.credentials.UpdateDeviceInfo(r.ctx, r.userID, unregistered, ownerInfo)
	require.Error(t, err, "display information cannot register a device")
	attached, err := r.credentials.AttachDeviceInfo(r.ctx, other.ID, []service.DesktopDevice{{DeviceID: r.device}})
	require.NoError(t, err)
	require.Nil(t, attached[0].DeviceInfo)
	var rejectedRows int
	require.NoError(t, repository.GetIntegrationDB().QueryRowContext(r.ctx, "SELECT count(*) FROM desktop_device_info WHERE user_id=$1 OR device_id=$2", other.ID, unregistered).Scan(&rejectedRows))
	require.Zero(t, rejectedRows)

	// Device UUIDs are installation coordinates, scoped separately to each owner.
	_, err = device.Ensure(r.ctx, other.ID, service.DesktopDeviceKeyRequest{DeviceID: r.device})
	require.NoError(t, err)
	otherInfo := service.DesktopDeviceInfo{InstallationName: "Other account", OS: "windows", ClientType: "desktop", ClientVersion: "2.0"}
	_, err = r.credentials.UpdateDeviceInfo(r.ctx, other.ID, r.device, otherInfo)
	require.NoError(t, err)
	for _, owner := range []struct {
		id   int64
		info service.DesktopDeviceInfo
	}{{r.userID, ownerInfo}, {other.ID, otherInfo}} {
		attached, err := r.credentials.AttachDeviceInfo(r.ctx, owner.id, []service.DesktopDevice{{DeviceID: r.device}})
		require.NoError(t, err)
		require.NotNil(t, attached[0].DeviceInfo)
		require.Equal(t, owner.info, *attached[0].DeviceInfo)
	}
}

func TestDesktopDeviceInfoAcceptsRevokedAndExpiredLeaseHistory(t *testing.T) {
	for _, history := range []string{"revoked", "expired"} {
		t.Run(history, func(t *testing.T) {
			r := newDesktopCredentialRig(t)
			lease := r.issue(t, "fixture-a")
			key, err := r.keys.GetByKey(r.ctx, lease.APIKey)
			require.NoError(t, err)
			if history == "revoked" {
				require.NoError(t, repository.NewDesktopCredentialRepository(r.client).RevokeFamily(r.ctx, r.family))
			} else {
				_, err = repository.GetIntegrationDB().ExecContext(r.ctx, "UPDATE desktop_model_credentials SET created_at=$1, expires_at=$2 WHERE api_key_id=$3", time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour), key.ID)
				require.NoError(t, err)
			}
			wanted := service.DesktopDeviceInfo{InstallationName: "Historical installation", OS: "linux", ClientType: "desktop", ClientVersion: "0.9"}
			actual, err := r.credentials.UpdateDeviceInfo(r.ctx, r.userID, r.device, wanted)
			require.NoError(t, err)
			require.Equal(t, wanted, *actual)
			devices := desktopDeviceInfoListing(t, r, r.token)
			require.Contains(t, devices, r.device)
			require.JSONEq(t, `"lease"`, string(devices[r.device]["credential_type"]))
			var stored service.DesktopDeviceInfo
			require.NoError(t, json.Unmarshal(devices[r.device]["device_info"], &stored))
			require.Equal(t, wanted, stored)
			other := r.user(t, service.StatusActive, false)
			_, err = r.credentials.UpdateDeviceInfo(r.ctx, other.ID, r.device, wanted)
			require.Error(t, err, "historical ownership is still scoped to its account")
		})
	}
}

func TestDesktopDeviceInfoAcceptsSoftDeletedPermanentIdentity(t *testing.T) {
	r := newDesktopCredentialRig(t)
	device := service.NewDesktopDeviceKeyService(repository.NewDesktopDeviceKeyRepository(r.client), r.keys)
	r.desktopHandler.SetDeviceKeyService(device)
	_, err := device.Ensure(r.ctx, r.userID, service.DesktopDeviceKeyRequest{DeviceID: r.device})
	require.NoError(t, err)
	key, err := repository.NewDesktopDeviceKeyRepository(r.client).GetDeviceKey(r.ctx, r.userID, r.device)
	require.NoError(t, err)
	require.NoError(t, r.keys.Delete(r.ctx, key.ID, r.userID))
	before := desktopDeviceInfoKeySnapshot(t, r, r.userID, r.device)
	require.NotNil(t, before.DeletedAt)
	wanted := service.DesktopDeviceInfo{InstallationName: "Archived laptop", OS: "windows", ClientType: "desktop", ClientVersion: "1.0"}
	actual, err := r.credentials.UpdateDeviceInfo(r.ctx, r.userID, r.device, wanted)
	require.NoError(t, err)
	require.Equal(t, wanted, *actual)
	require.Equal(t, before, desktopDeviceInfoKeySnapshot(t, r, r.userID, r.device))
	state, err := device.Get(r.ctx, r.userID, r.device)
	require.NoError(t, err)
	require.Equal(t, "revoked", state.Status)
	devices := desktopDeviceInfoListing(t, r, r.token)
	require.JSONEq(t, `"device"`, string(devices[r.device]["credential_type"]))
	var stored service.DesktopDeviceInfo
	require.NoError(t, json.Unmarshal(devices[r.device]["device_info"], &stored))
	require.Equal(t, wanted, stored)
}

func TestDesktopDeviceInfoListingKeepsOptionalInfoAndCredentialType(t *testing.T) {
	r := newDesktopCredentialRig(t)
	device := service.NewDesktopDeviceKeyService(repository.NewDesktopDeviceKeyRepository(r.client), r.keys)
	r.desktopHandler.SetDeviceKeyService(device)
	r.issue(t, "fixture-a")
	_, err := device.Ensure(r.ctx, r.userID, service.DesktopDeviceKeyRequest{DeviceID: r.device})
	require.NoError(t, err)
	wanted := service.DesktopDeviceInfo{InstallationName: "Mixed credentials", OS: "macos", ClientType: "desktop", ClientVersion: "1.0"}
	_, err = r.credentials.UpdateDeviceInfo(r.ctx, r.userID, r.device, wanted)
	require.NoError(t, err)
	withoutInfo := uuid.NewString()
	_, err = device.Ensure(r.ctx, r.userID, service.DesktopDeviceKeyRequest{DeviceID: withoutInfo})
	require.NoError(t, err)
	leaseOnly := uuid.NewString()
	sharedDevice := r.device
	r.device = leaseOnly
	r.issue(t, "fixture-a")
	r.device = sharedDevice
	devices := desktopDeviceInfoListing(t, r, r.token)
	require.Len(t, devices, 3)
	require.JSONEq(t, `"device"`, string(devices[r.device]["credential_type"]))
	require.JSONEq(t, `"device"`, string(devices[withoutInfo]["credential_type"]))
	require.JSONEq(t, `"lease"`, string(devices[leaseOnly]["credential_type"]))
	require.NotContains(t, devices[withoutInfo], "device_info")
	require.NotContains(t, devices[leaseOnly], "device_info")
	var actual service.DesktopDeviceInfo
	require.NoError(t, json.Unmarshal(devices[r.device]["device_info"], &actual))
	require.Equal(t, wanted, actual)
}
