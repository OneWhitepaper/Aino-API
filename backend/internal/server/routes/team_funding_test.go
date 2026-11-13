//go:build unit

package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTeamFundingRoutesRequireUserAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	user := router.Group("/api/v1/user")
	user.Use(func(c *gin.Context) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"reason": "AUTH_REQUIRED"})
	})
	registerTeamFundingRoutes(user, handler.NewTeamFundingHandler(nil))
	for _, endpoint := range []struct{ method, path string }{
		{http.MethodGet, "/funding"}, {http.MethodPut, "/funding"},
		{http.MethodGet, "/teams"}, {http.MethodPost, "/teams"},
		{http.MethodGet, "/teams/1"}, {http.MethodPatch, "/teams/1"},
		{http.MethodPost, "/teams/1/invitations"},
		{http.MethodGet, "/team-invitations"},
		{http.MethodPost, "/team-invitations/1/accept"},
		{http.MethodPost, "/team-invitations/1/decline"},
		{http.MethodDelete, "/teams/1/invitations/1"},
		{http.MethodPatch, "/teams/1/members/2"},
		{http.MethodDelete, "/teams/1/members/2"},
		{http.MethodGet, "/teams/1/ledger"},
	} {
		t.Run(endpoint.method+endpoint.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(endpoint.method, "/api/v1/user"+endpoint.path, nil))
			require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
		})
	}
}
