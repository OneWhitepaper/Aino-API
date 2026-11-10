package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type desktopIdentityAfterSyncRemoval struct {
	service.DesktopCredentialRepository
	service.DesktopDeviceKeyRepository
	key  service.APIKey
	info service.DesktopDeviceInfo
}

func (r *desktopIdentityAfterSyncRemoval) GetDeviceKey(_ context.Context, owner int64, device string) (*service.APIKey, error) {
	if owner != r.key.UserID || device != r.key.DesktopDeviceID {
		return nil, service.ErrDesktopDeviceKeyNotFound
	}
	return &r.key, nil
}

func (r *desktopIdentityAfterSyncRemoval) ListDeviceKeys(context.Context, int64) ([]service.APIKey, error) {
	return []service.APIKey{r.key}, nil
}

func (*desktopIdentityAfterSyncRemoval) ListByUser(context.Context, int64) ([]service.DesktopCredential, error) {
	return nil, nil
}

func (r *desktopIdentityAfterSyncRemoval) ListDeviceInfo(context.Context, int64) (map[string]service.DesktopDeviceInfo, error) {
	return map[string]service.DesktopDeviceInfo{r.key.DesktopDeviceID: r.info}, nil
}

func (r *desktopIdentityAfterSyncRemoval) UpsertDeviceInfo(_ context.Context, _ int64, _ string, info service.DesktopDeviceInfo) (*service.DesktopDeviceInfo, error) {
	r.info = info
	return &r.info, nil
}

func TestDesktopCloudSyncRemovedBeforeAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	authCalls := 0
	auth := middleware.JWTAuthMiddleware(func(c *gin.Context) {
		authCalls++
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
		c.Next()
	})
	device := "00000000-0000-4000-8000-000000000001"
	repo := &desktopIdentityAfterSyncRemoval{key: service.APIKey{ID: 42, UserID: 7, Key: "private-key-hash", Status: service.StatusActive, DesktopDeviceID: device, DesktopKeyVersion: 3}, info: service.DesktopDeviceInfo{InstallationName: "Work laptop", OS: "macos", ClientType: "desktop", ClientVersion: "0.23.0"}}
	desktop := handler.NewDesktopHandler(nil)
	desktop.SetCredentialService(service.NewDesktopCredentialService(repo, nil, nil, nil, nil))
	desktop.SetDeviceKeyService(service.NewDesktopDeviceKeyService(repo, nil))
	RegisterDesktopRoutes(router.Group("/api/v1"), &handler.Handlers{Desktop: desktop}, auth, nil, nil, nil)
	call := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/api/v1/desktop/"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		return w
	}
	for _, request := range []struct{ method, path string }{
		{http.MethodPost, "sync/exchange"},
		{http.MethodPut, "sync/blobs/" + strings.Repeat("a", 64)},
		{http.MethodGet, "sync/blobs/" + strings.Repeat("a", 64)},
	} {
		w := call(request.method, request.path, `{}`)
		require.Equal(t, http.StatusNotFound, w.Code, request.path)
	}
	require.Zero(t, authCalls, "retired sync requests must not trigger authentication or database-backed handlers")
	key := call(http.MethodGet, "device-keys/"+device, "")
	require.Equal(t, http.StatusOK, key.Code, key.Body.String())
	require.Contains(t, key.Body.String(), `"status":"active"`)
	require.Contains(t, key.Body.String(), `"version":3`)
	require.NotContains(t, key.Body.String(), "private-key-hash")
	info := call(http.MethodPut, "devices/"+device+"/info", `{"installation_name":"Updated laptop","os":"macos","client_type":"desktop_dev","client_version":"0.23.0"}`)
	require.Equal(t, http.StatusOK, info.Code, info.Body.String())
	devices := call(http.MethodGet, "devices", "")
	require.Equal(t, http.StatusOK, devices.Code, devices.Body.String())
	require.Contains(t, devices.Body.String(), `"installation_name":"Updated laptop"`)
	require.Equal(t, 3, authCalls)
	require.Equal(t, "private-key-hash", repo.key.Key)
	require.Equal(t, 3, repo.key.DesktopKeyVersion)
}
