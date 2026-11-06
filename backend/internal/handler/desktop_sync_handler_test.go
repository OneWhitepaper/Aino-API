package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type syncHandlerRepo struct {
	service.DesktopSyncRepository
	owner int64
}

func (r *syncHandlerRepo) Exchange(_ context.Context, owner int64, _ service.DesktopSyncRequest, _ int64) (*service.DesktopSyncResponse, error) {
	r.owner = owner
	return &service.DesktopSyncResponse{Cursor: "0", Results: []service.DesktopSyncResult{}, Changes: []service.DesktopSyncRecord{}}, nil
}
func TestDesktopSyncHandlerStrictBodyAndAuthenticatedOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &syncHandlerRepo{}
	h := NewDesktopHandler(nil)
	h.SetSyncService(service.NewDesktopSyncService(repo))
	valid := `{"version":1,"device_id":"a","after":"0","limit":50,"operations":[]}`
	call := func(body string, authenticated bool) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/desktop/sync/exchange", strings.NewReader(body))
		if authenticated {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
		}
		h.SyncExchange(c)
		return w
	}
	require.Equal(t, 401, call(valid, false).Code)
	for _, bad := range []string{strings.Replace(valid, `"device_id":"a"`, "\"device_id\":\""+string([]byte{255})+"\"", 1), strings.Replace(valid, `"version":1`, `"version":1,"user_id":99`, 1), valid + `{}`, strings.Repeat(" ", 16*1024*1024) + valid, `{}`} {
		require.Equal(t, 400, call(bad, true).Code)
	}
	good := call(valid, true)
	require.Equal(t, 200, good.Code, good.Body.String())
	require.Equal(t, int64(7), repo.owner)
	require.Equal(t, "no-store", good.Header().Get("Cache-Control"))
	var envelope struct {
		Data service.DesktopSyncResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(good.Body.Bytes(), &envelope))
	require.Equal(t, "0", envelope.Data.Cursor)
	require.NotNil(t, envelope.Data.Changes)
	require.NotNil(t, envelope.Data.Results)
}
