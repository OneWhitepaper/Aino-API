package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDesktopOperationBindingPrivateAndValidated(t *testing.T) {
	for _, tc := range []struct {
		name     string
		values   []string
		managed  bool
		wantCode int
	}{
		{"managed", []string{uuid.NewString()}, true, 200},
		{"legacy missing", nil, true, 200},
		{"invalid", []string{"bad"}, true, 400},
		{"duplicate", []string{uuid.NewString(), uuid.NewString()}, true, 400},
		{"ordinary private tag", []string{"bad"}, false, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.POST("/v1/responses", func(c *gin.Context) {
				key, ok := bindDesktopUsage(c, &service.APIKey{DesktopManaged: tc.managed})
				if !ok {
					return
				}
				require.Nil(t, key.DesktopUsage)
				require.Empty(t, c.GetHeader("X-Aino-Operation-ID"))
				operation := service.DesktopInferenceOperationID(c.Request.Context())
				if tc.managed && len(tc.values) == 1 {
					require.Equal(t, tc.values[0], operation)
				} else {
					require.Empty(t, operation)
				}
				c.Status(http.StatusOK)
			})
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			for _, value := range tc.values {
				req.Header.Add("X-Aino-Operation-ID", value)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			require.Equal(t, tc.wantCode, rec.Code)
		})
	}
}

func TestDesktopOperationGateFailsClosedAndExemptsReads(t *testing.T) {
	previous := service.DefaultIdempotencyCoordinator()
	service.SetDefaultIdempotencyCoordinator(nil)
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(previous) })
	for _, path := range []string{"/v1/responses", "/v1/chat/completions", "/v1/messages", "/v1/images/generations", "/v1/images/edits", "/v1/images/generations/async", "/v1/images/edits/async", "/v1/messages/count_tokens"} {
		t.Run(path, func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(ContextKeyAPIKey), &service.APIKey{ID: 2, UserID: 3, DesktopManaged: true})
				c.Request = c.Request.WithContext(service.WithDesktopInferenceOperation(context.Background(), uuid.NewString()))
			})
			router.Use(DesktopInferenceOperation())
			router.POST(path, func(c *gin.Context) {
				body, err := io.ReadAll(c.Request.Body)
				require.NoError(t, err)
				require.Equal(t, "original bytes", string(body))
				c.Status(200)
			})
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader("original bytes")))
			want := 503
			if strings.HasSuffix(path, "/count_tokens") {
				want = 200
			}
			require.Equal(t, want, rec.Code, rec.Body.String())
		})
	}
}
