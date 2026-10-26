//go:build unit

package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type contactHandlerRepo struct {
	service.SettingRepository
	value string
}

func (r *contactHandlerRepo) GetValue(context.Context, string) (string, error) { return r.value, nil }
func (r *contactHandlerRepo) Set(_ context.Context, _ string, value string) error {
	r.value = value
	return nil
}

func TestContactSettingsHTTPValidationAndPersistence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &contactHandlerRepo{}
	h := &SettingHandler{settingService: service.NewSettingService(repo, &config.Config{})}
	router := gin.New()
	router.GET("/contact", h.GetContactPage)
	router.PUT("/contact", h.UpdateContactPage)
	request := func(method, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/contact", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		return w
	}
	w := request(http.MethodPut, `{"description":"联系支持","methods":[{"type":"wechat","label":"客服微信","value":"support","enabled":false}],"groups":[]}`)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"value":"support"`)
	require.Contains(t, request(http.MethodGet, "").Body.String(), `"enabled":false`)
	saved := repo.value
	for _, body := range []string{`null`, `{`, `{"methods":[{"type":"link","label":"link","value":"link","url":"javascript:alert(1)","enabled":true}]}`} {
		require.Equal(t, http.StatusBadRequest, request(http.MethodPut, body).Code)
		require.Equal(t, saved, repo.value)
	}
	w = request(http.MethodPut, `{"description":"`+strings.Repeat("x", 6*1024*1024)+`"}`)
	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	require.Equal(t, saved, repo.value)
}
