//go:build integration

package repository_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent/apikey"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDesktopDeviceRolloutConstrainsMutationsAndPreservesExistingAccess(t *testing.T) {
	r := newDesktopCredentialRig(t)
	r.desktopHandler.SetDeviceKeyService(service.NewDesktopDeviceKeyService(repository.NewDesktopDeviceKeyRepository(r.client), r.keys))
	previous := service.DefaultIdempotencyCoordinator()
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(repository.NewIdempotencyRepository(r.client, repository.GetIntegrationDB()), service.DefaultIdempotencyConfig()))
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(previous) })
	call := func(token, idempotency string, body service.DesktopDeviceKeyRequest) *httptest.ResponseRecorder {
		t.Helper()
		blob, err := json.Marshal(body)
		require.NoError(t, err)
		request := httptest.NewRequest(http.MethodPost, "/api/v1/desktop/device-keys", bytes.NewReader(blob))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Idempotency-Key", idempotency)
		response := httptest.NewRecorder()
		r.router.ServeHTTP(response, request)
		return response
	}
	decode := func(response *httptest.ResponseRecorder) service.DesktopDeviceKeyResponse {
		t.Helper()
		require.Equal(t, 200, response.Code, response.Body.String())
		var envelope struct {
			Data service.DesktopDeviceKeyResponse
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
		return envelope.Data
	}
	claims, err := r.auth.ValidateToken(r.token)
	require.NoError(t, err)
	claims.AuthTime = time.Now().Add(-time.Hour).Unix()
	stale, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("integration-phone-binding-secret"))
	require.NoError(t, err)
	request := service.DesktopDeviceKeyRequest{DeviceID: r.device}
	denied := call(r.token, "closed-create", request)
	require.Equal(t, 403, denied.Code)
	require.Contains(t, denied.Body.String(), "DESKTOP_DEVICE_KEY_ROLLOUT_DENIED")
	count, err := r.client.APIKey.Query().Where(apikey.UserIDEQ(r.userID), apikey.DesktopCredentialTypeEQ("device")).Count(r.ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	other := r.user(t, service.StatusActive, false)
	otherToken, err := r.auth.GenerateTokenPair(r.ctx, other, "")
	require.NoError(t, err)
	otherDevice := uuid.NewString()
	policy := config.DesktopDeviceKeysConfig{Enabled: true, RolloutAllowlist: []config.DesktopDeviceKeyRolloutEntry{{UserID: r.userID, DeviceID: r.device}, {UserID: other.ID, DeviceID: otherDevice}}}
	r.desktopHandler.SetDeviceKeyPolicy(policy)
	for _, crossed := range []struct{ token, device string }{{r.token, otherDevice}, {otherToken.AccessToken, r.device}} {
		response := call(crossed.token, uuid.NewString(), service.DesktopDeviceKeyRequest{DeviceID: crossed.device})
		require.Equal(t, 403, response.Code)
		require.Contains(t, response.Body.String(), "DESKTOP_DEVICE_KEY_ROLLOUT_DENIED")
	}
	denied = call(stale, "stale-create", request)
	require.Equal(t, 403, denied.Code)
	require.Contains(t, denied.Body.String(), "RECENT_AUTH_REQUIRED")
	created := decode(call(r.token, "allowed-create", request))
	require.NotEmpty(t, created.APIKey)
	r.desktopHandler.SetDeviceKeyPolicy(config.DesktopDeviceKeysConfig{})
	replay := call(stale, "allowed-create", request)
	require.Equal(t, "true", replay.Header().Get("X-Idempotency-Replayed"))
	metadata := decode(replay)
	require.Equal(t, created.KeyID, metadata.KeyID)
	require.Equal(t, created.Version, metadata.Version)
	require.Empty(t, metadata.APIKey)
	require.NotContains(t, replay.Body.String(), created.APIKey)
	authenticated, err := r.keys.GetByKey(r.ctx, created.APIKey)
	require.NoError(t, err, "closing issuance must preserve authentication of the already-issued identity")
	require.Equal(t, created.KeyID, strconv.FormatInt(authenticated.ID, 10))
	rotation := service.DesktopDeviceKeyRequest{DeviceID: r.device, Rotate: true, ExpectedVersion: &created.Version}
	denied = call(r.token, "closed-rotate", rotation)
	require.Equal(t, 403, denied.Code)
	require.Contains(t, denied.Body.String(), "DESKTOP_DEVICE_KEY_ROLLOUT_DENIED")
	r.desktopHandler.SetDeviceKeyPolicy(policy)
	denied = call(stale, "stale-rotate", rotation)
	require.Contains(t, denied.Body.String(), "RECENT_AUTH_REQUIRED")
	rotated := decode(call(r.token, "allowed-rotate", rotation))
	require.Equal(t, created.KeyID, rotated.KeyID)
	require.Equal(t, created.Version+1, rotated.Version)
	r.desktopHandler.SetDeviceKeyPolicy(config.DesktopDeviceKeysConfig{})
	require.Equal(t, 200, r.request(http.MethodGet, "/api/v1/desktop/device-keys/"+r.device, "", r.token).Code)
	require.Equal(t, 200, r.request(http.MethodDelete, "/api/v1/desktop/devices/"+r.device, "", r.token).Code)
	state := r.request(http.MethodGet, "/api/v1/desktop/device-keys/"+r.device, "", r.token)
	revoked := decode(state)
	require.Equal(t, "revoked", revoked.Status)
	reauthorization := service.DesktopDeviceKeyRequest{DeviceID: r.device, Reauthorize: true, ExpectedVersion: &revoked.Version}
	denied = call(r.token, "closed-reauthorize", reauthorization)
	require.Equal(t, 403, denied.Code)
	require.Contains(t, denied.Body.String(), "DESKTOP_DEVICE_KEY_ROLLOUT_DENIED")
	r.desktopHandler.SetDeviceKeyPolicy(policy)
	denied = call(stale, "stale-reauthorize", reauthorization)
	require.Contains(t, denied.Body.String(), "RECENT_AUTH_REQUIRED")
	reauthorized := decode(call(r.token, "allowed-reauthorize", reauthorization))
	require.Equal(t, created.KeyID, reauthorized.KeyID)
	require.Equal(t, revoked.Version+1, reauthorized.Version)
	r.desktopHandler.SetDeviceKeyPolicy(config.DesktopDeviceKeysConfig{})
	legacy := r.issue(t, "fixture-a")
	require.Equal(t, 200, r.request(http.MethodGet, "/v1/models", "", legacy.APIKey).Code, "closing permanent-key rollout must preserve the existing lease path")
	manual := &service.APIKey{UserID: r.userID, Key: "sk-manual-" + uuid.NewString(), Name: "manual", Status: service.StatusActive}
	require.NoError(t, repository.NewAPIKeyRepository(r.client, repository.GetIntegrationDB()).Create(r.ctx, manual))
	key, err := r.keys.GetByKey(r.ctx, manual.Key)
	require.NoError(t, err)
	require.Equal(t, manual.ID, key.ID)
	keyID, err := strconv.ParseInt(created.KeyID, 10, 64)
	require.NoError(t, err)
	stored, err := r.client.APIKey.Get(r.ctx, keyID)
	require.NoError(t, err)
	require.Equal(t, reauthorized.Version, stored.DesktopKeyVersion)
}
