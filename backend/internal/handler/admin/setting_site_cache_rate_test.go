package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type cacheVisibilityRepo struct {
	service.SettingRepository
	values map[string]string
}

func (r *cacheVisibilityRepo) GetValue(_ context.Context, key string) (string, error) {
	return r.values[key], nil
}

func (r *cacheVisibilityRepo) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	return nil
}

func TestSiteCacheRateSettingsPersistenceAndValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &cacheVisibilityRepo{values: map[string]string{"unrelated": "preserved"}}
	h := &SettingHandler{settingService: service.NewSettingService(repo, nil)}
	router := gin.New()
	router.PUT("/site-cache-rate", h.UpdateSiteCacheRateVisibility)
	request := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/site-cache-rate", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		return w
	}
	for _, enabled := range []string{"true", "false"} {
		w := request(`{"visible_to_users":` + enabled + `}`)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"visible_to_users":`+enabled)
		// Reload the service to prove this is persisted rather than process state.
		visible, err := service.NewSettingService(repo, nil).GetSiteCacheRateVisibility(context.Background())
		require.NoError(t, err)
		require.Equal(t, enabled == "true", visible)
	}
	for _, body := range []string{`null`, `{}`, `{"visible_to_users":null}`, `{"visible_to_users":"true"}`, `{`} {
		require.Equal(t, http.StatusBadRequest, request(body).Code, body)
	}
	require.Equal(t, "preserved", repo.values["unrelated"])
}
