package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAdminDesktopSettingsEndpointPreservesOtherSettings(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{service.SettingKeySiteName: "OmniFlow", service.SettingKeyOpsMonitoringEnabled: "true"})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/desktop", strings.NewReader(`{"enabled":false,"models":[],"credential_ttl_seconds":600,"default_model_id":null,"default_image_model_id":null}`))
	c.Request.Header.Set("Content-Type", "application/json")
	h.UpdateDesktopSettings(c)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, repo.lastUpdates, 5)
	require.Equal(t, "OmniFlow", repo.values[service.SettingKeySiteName])
	require.Equal(t, "true", repo.values[service.SettingKeyOpsMonitoringEnabled])
	require.Contains(t, rec.Body.String(), `"credential_ttl_seconds":600`)

	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings/desktop", nil)
	h.GetDesktopSettings(c)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), "site_name")
	require.Contains(t, rec.Body.String(), `"enabled":false`)
	require.Contains(t, rec.Body.String(), `"models":[]`)
}

func TestAdminDesktopSettingsEndpointRejectsPartialDocuments(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `{"enabled":true}`, `{"enabled":true,"models":null,"credential_ttl_seconds":900}`, `{"enabled":true,"models":[],"credential_ttl_seconds":100}`, `{"enabled":true,"models":[],"credential_ttl_seconds":0}`, `{"enabled":true,"models":[],"credential_ttl_seconds":3601}`} {
		t.Run(raw, func(t *testing.T) {
			h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/desktop", strings.NewReader(raw))
			c.Request.Header.Set("Content-Type", "application/json")
			h.UpdateDesktopSettings(c)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			require.Empty(t, repo.lastUpdates)
		})
	}
}
