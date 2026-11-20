package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDesktopBillingPolicyQueryIsExplicitAndValidated(t *testing.T) {
	settings := service.NewSettingService(desktopHandlerSettingRepo{values: map[string]string{}}, &config.Config{})
	h := NewDesktopHandler(service.NewDesktopModelService(settings, nil, nil, nil, nil, nil))
	for _, path := range []string{"bootstrap", "models"} {
		for _, policy := range []string{"balance_only", "unknown"} {
			t.Run(path+"/"+policy, func(t *testing.T) {
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/desktop/"+path+"?billing_policy="+policy, nil)
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
				if path == "bootstrap" {
					h.GetBootstrap(c)
				} else {
					h.ListModels(c)
				}
				if policy == "unknown" {
					require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
					return
				}
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				if path == "bootstrap" {
					var response struct {
						Data struct {
							Capabilities map[string]int `json:"capabilities"`
						} `json:"data"`
					}
					require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
					require.Equal(t, 1, response.Data.Capabilities["managed_billing_policy_balance_only"])
				}
			})
		}
	}
}

func TestDeviceKeyIssuanceAcknowledgesBillingPolicy(t *testing.T) {
	repo := &deviceKeyHandlerRepo{}
	keys := service.NewAPIKeyService(nil, deviceKeyHandlerUsers{}, nil, nil, nil, nil, nil)
	h := NewDesktopHandler(nil)
	h.SetDeviceKeyService(service.NewDesktopDeviceKeyService(repo, keys))
	var request service.DesktopDeviceKeyRequest
	require.NoError(t, json.Unmarshal([]byte(`{"device_id":"00000000-0000-4000-8000-000000000001","billing_policy":"balance_only"}`), &request))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/desktop/device-keys", nil)
	h.executeDeviceKeyMutation(c, 7, request)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var response struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, "balance_only", response.Data["billing_policy"])
	require.NotEmpty(t, response.Data["api_key"])
}
