package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDesktopUsageTaskIsValidatedPrivateAndRequestScoped(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, purpose, wantPurpose string
		tasks                      []string
		ordinary                   bool
		wantStatus                 int
	}{
		{name: "legacy", purpose: "other_auxiliary", wantPurpose: "other_auxiliary", wantStatus: http.StatusOK},
		{name: "summary", purpose: "other_auxiliary", tasks: []string{"session_summary"}, wantPurpose: "session_summary", wantStatus: http.StatusOK},
		{name: "approval", purpose: "other_auxiliary", tasks: []string{"approval"}, wantPurpose: "approval", wantStatus: http.StatusOK},
		{name: "detail requires task header", purpose: "session_summary", wantStatus: http.StatusBadRequest},
		{name: "unknown", purpose: "other_auxiliary", tasks: []string{"invented"}, wantStatus: http.StatusBadRequest},
		{name: "empty", purpose: "other_auxiliary", tasks: []string{""}, wantStatus: http.StatusBadRequest},
		{name: "duplicate", purpose: "other_auxiliary", tasks: []string{"session_summary", "session_summary"}, wantStatus: http.StatusBadRequest},
		{name: "conflicting", purpose: "chat", tasks: []string{"session_summary"}, wantStatus: http.StatusBadRequest},
		{name: "ordinary", purpose: "other_auxiliary", tasks: []string{"invented"}, ordinary: true, wantStatus: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := &service.APIKey{DesktopManaged: !tc.ordinary}
			router := gin.New()
			router.POST("/messages", func(c *gin.Context) {
				bound, ok := bindDesktopUsage(c, key)
				for name := range c.Request.Header {
					require.False(t, strings.HasPrefix(strings.ToLower(name), "x-aino-"))
				}
				if !ok {
					return
				}
				if tc.ordinary {
					require.Nil(t, bound.DesktopUsage)
				} else {
					require.Equal(t, tc.wantPurpose, bound.DesktopUsage.Purpose)
				}
				c.Status(http.StatusOK)
			})
			req := httptest.NewRequest(http.MethodPost, "/messages", nil)
			for _, name := range []string{"Session", "Turn", "Call"} {
				req.Header.Set("X-Aino-"+name+"-Id", uuid.NewString())
			}
			req.Header.Set("X-Aino-Purpose", tc.purpose)
			for _, task := range tc.tasks {
				req.Header.Add("X-Aino-Task", task)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			require.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())
			require.Nil(t, key.DesktopUsage, "cached credentials must not retain request attribution")
		})
	}
}
