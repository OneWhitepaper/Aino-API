package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type deviceInfoHandlerRepo struct {
	service.DesktopCredentialRepository
	writes int
	owner  int64
}

func (r *deviceInfoHandlerRepo) UpsertDeviceInfo(_ context.Context, userID int64, _ string, info service.DesktopDeviceInfo) (*service.DesktopDeviceInfo, error) {
	if userID != r.owner {
		return nil, service.ErrDesktopCredentialNotFound
	}
	r.writes++
	return &info, nil
}

func (*deviceInfoHandlerRepo) ListDeviceInfo(context.Context, int64) (map[string]service.DesktopDeviceInfo, error) {
	return nil, nil
}

func TestDesktopDeviceInfoRejectsMalformedBodiesWithoutWrites(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &deviceInfoHandlerRepo{owner: 7}
	h := NewDesktopHandler(nil)
	h.SetCredentialService(service.NewDesktopCredentialService(repo, nil, nil, nil, nil))
	valid := `{"installation_name":"工作电脑","os":"macos","client_type":"desktop_dev","client_version":"0.23.0"}`
	call := func(body, deviceID string, userID int64) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/desktop/devices/"+deviceID+"/info", strings.NewReader(body))
		c.Params = gin.Params{{Key: "device_id", Value: deviceID}}
		if userID > 0 {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID})
		}
		h.UpdateDeviceInfo(c)
		return w
	}
	deviceID := "00000000-0000-4000-8000-000000000001"
	for _, body := range []string{
		`{}`, `null`, `[]`, `"info"`,
		strings.Replace(valid, `"os":"macos"`, `"os":null`, 1),
		strings.Replace(valid, `"os":"macos"`, `"os":1`, 1),
		strings.Replace(valid, `"os":"macos"`, `"os":"macos","os":"linux"`, 1),
		strings.Replace(valid, `"os":"macos"`, `"OS":"macos"`, 1),
		strings.Replace(valid, `"os":"macos",`, ``, 1),
		strings.Replace(valid, `"os":"macos"`, `"os":"macos","user_id":99`, 1),
		strings.Replace(valid, `"os":"macos"`, `"os":"macos","group_id":99`, 1),
		valid + `{}`, valid + " trailing", valid + strings.Repeat(" ", 4096),
		strings.Replace(valid, "工作电脑", string([]byte{0xff}), 1),
		strings.Replace(valid, "工作电脑", `bad\nname`, 1),
		strings.Replace(valid, "工作电脑", "hidden\u202ename", 1),
		strings.Replace(valid, "工作电脑", "", 1),
		strings.Replace(valid, "工作电脑", "   ", 1),
		strings.Replace(valid, "工作电脑", "\u3000\u00a0", 1),
		strings.Replace(valid, "工作电脑", strings.Repeat("界", 81), 1),
		strings.Replace(valid, "0.23.0", strings.Repeat("v", 65), 1),
		strings.Replace(valid, "0.23.0", "", 1),
		strings.Replace(valid, "0.23.0", "   ", 1),
		strings.Replace(valid, "macos", "android", 1),
		strings.Replace(valid, "desktop_dev", "admin", 1),
	} {
		w := call(body, deviceID, 7)
		require.Equal(t, http.StatusBadRequest, w.Code, body)
	}
	require.Zero(t, repo.writes)
	require.Equal(t, http.StatusUnauthorized, call(valid, deviceID, 0).Code)
	require.Equal(t, http.StatusNotFound, call(valid, deviceID, 8).Code)
	require.Equal(t, http.StatusBadRequest, call(valid, "invalid-device", 7).Code)
	require.Zero(t, repo.writes)
	w := call(valid, deviceID, 7)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Contains(t, w.Body.String(), `"installation_name":"工作电脑"`)
	require.NotContains(t, w.Body.String(), "api_key")
	require.Equal(t, 1, repo.writes)
	maxLengths := strings.Replace(strings.Replace(valid, "工作电脑", strings.Repeat("界", 80), 1), "0.23.0", strings.Repeat("v", 64), 1)
	require.Equal(t, http.StatusOK, call(maxLengths, deviceID, 7).Code)
	paddedName := strings.Replace(valid, "工作电脑", "  工作电脑  ", 1)
	require.Contains(t, call(paddedName, deviceID, 7).Body.String(), `"installation_name":"工作电脑"`)
	require.Equal(t, 3, repo.writes)
}
