//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// Exercise the production constructor, not a manually repaired test handler:
// missing step-up wiring must not turn an ordinary TOTP challenge into HTTP 500.
func TestUserHandlerPhoneBindingChallengesTotpUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &userHandlerRepoStub{user: &service.User{ID: 42, TotpEnabled: true}}
	users := service.NewUserService(repo, nil, nil, nil)
	rdb := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	cache := repository.NewTotpCache(rdb)
	totp := service.NewTotpService(repo, nil, cache, nil, nil, nil)
	h := NewUserHandler(users, nil, nil, nil, nil, nil, totp, nil)
	for _, endpoint := range []struct {
		name   string
		body   string
		handle gin.HandlerFunc
	}{
		{"send-code", `{"phone":"13800138000"}`, h.SendPhoneBindingCode},
		{"bind", `{"phone":"13800138000","challenge_id":"test","code":"123456"}`, h.BindPhone},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/"+endpoint.name, strings.NewReader(endpoint.body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
			endpoint.handle(c)
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), "STEP_UP_REQUIRED")
		})
	}
	// An existing grant must be consumed by the same constructor-wired gate,
	// and must remain isolated to the authenticated session.
	require.NoError(t, cache.SetStepUpGrant(context.Background(), 42, "verified-session", time.Minute))
	for _, session := range []string{"verified-session", "other-session"} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/bind", nil)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
		c.Set(middleware.ContextKeySessionID, session)
		require.Equal(t, session == "verified-session", h.enforcePhoneBindingSecurity(c, middleware.AuthSubject{UserID: 42}))
		if session != "verified-session" {
			require.Equal(t, http.StatusForbidden, w.Code)
			require.Contains(t, w.Body.String(), "STEP_UP_REQUIRED")
		}
	}
}
