//go:build unit

package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFundingResponsesInputTokensOnlyExactSubpathIsFree(t *testing.T) {
	for _, prefix := range []string{"/v1", "", "/backend-api/codex"} {
		for _, subpath := range []string{"/input_tokens", "/compact", "/input_tokens/generate"} {
			t.Run(prefix+"/responses"+subpath, func(t *testing.T) {
				auth, funding := readOnlyFundingAuth(t, false, nil)
				r := gin.New()
				r.Use(auth)
				body := `{"model":"gpt-5","input":"hello"}`
				called := false
				r.POST(prefix+"/responses/*subpath", func(c *gin.Context) {
					called = true
					require.Equal(t, "/input_tokens", c.Param("subpath"))
					key, ok := GetAPIKeyFromContext(c)
					require.True(t, ok)
					require.Empty(t, service.FundingDispatchRequestID(c.Request.Context()))
					require.False(t, key.Funding.HasReservation())
					actual, err := io.ReadAll(c.Request.Body)
					require.NoError(t, err)
					require.Equal(t, body, string(actual))
					c.Status(http.StatusOK)
				})
				w := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost, prefix+"/responses"+subpath, strings.NewReader(body))
				req.Header.Set("x-api-key", "fixture")
				req.Header.Set("Content-Type", "application/json")
				r.ServeHTTP(w, req)
				if subpath == "/input_tokens" {
					require.True(t, called)
					require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				} else {
					require.False(t, called)
					require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
					require.Contains(t, w.Body.String(), "FUNDING_PRICING_UNAVAILABLE")
				}
				require.Zero(t, funding.holds)
			})
		}
	}
}
