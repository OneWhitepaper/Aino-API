package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type deviceKeyHandlerUsers struct{ service.UserRepository }

func (deviceKeyHandlerUsers) GetByID(context.Context, int64) (*service.User, error) {
	return &service.User{ID: 7, Status: service.StatusActive}, nil
}

type deviceKeyHandlerRepo struct {
	service.DesktopDeviceKeyRepository
	key    *service.APIKey
	writes int
}

func (r *deviceKeyHandlerRepo) EnsureDeviceKey(_ context.Context, key *service.APIKey, _ *int, _, _ bool) (*service.APIKey, bool, error) {
	r.writes++
	key.ID = 42
	r.key = key
	return key, true, nil
}
func (r *deviceKeyHandlerRepo) GetDeviceKey(context.Context, int64, string) (*service.APIKey, error) {
	return r.key, nil
}

func TestDeviceKeyIdempotencyStoresMetadataAndNeverReplaysSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &deviceKeyHandlerRepo{}
	keys := service.NewAPIKeyService(nil, deviceKeyHandlerUsers{}, nil, nil, nil, nil, nil)
	h := NewDesktopHandler(nil)
	h.SetDeviceKeyService(service.NewDesktopDeviceKeyService(repo, keys))
	store := newUserMemoryIdempotencyRepoStub()
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(store, service.DefaultIdempotencyConfig()))
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(nil) })
	request := service.DesktopDeviceKeyRequest{DeviceID: "00000000-0000-4000-8000-000000000001"}
	call := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/desktop/device-keys", nil)
		c.Request.Header.Set("Idempotency-Key", "create-device-fixture")
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
		h.executeDeviceKeyMutation(c, 7, request)
		return w
	}
	first := call()
	require.Equal(t, 200, first.Code, first.Body.String())
	var envelope struct {
		Data service.DesktopDeviceKeyResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &envelope))
	require.NotEmpty(t, envelope.Data.APIKey)
	secret := envelope.Data.APIKey
	for _, record := range store.data {
		require.NotNil(t, record.ResponseBody)
		require.NotContains(t, *record.ResponseBody, secret)
		require.NotContains(t, *record.ResponseBody, "api_key")
	}
	second := call()
	require.Equal(t, 200, second.Code, second.Body.String())
	require.Equal(t, "true", second.Header().Get("X-Idempotency-Replayed"))
	require.NotContains(t, second.Body.String(), secret)
	require.NotContains(t, second.Body.String(), "api_key")
	require.Equal(t, 1, repo.writes)
	repo.key.Status = service.StatusAPIKeyDisabled
	third := call()
	require.Contains(t, third.Body.String(), `"status":"revoked"`)
}
