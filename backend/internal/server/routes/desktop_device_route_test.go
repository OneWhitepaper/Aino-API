package routes

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDesktopDeviceCompositeRouteKeepsAcceptedPlatformAndAlias(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// A catalog authorization already chose OpenAI; a later configuration read
	// exposes a Grok route. This dispatch must keep the accepted route.
	resolver := service.NewCompositeRouteResolver(compositeRouteRepoStub{routes: []service.CompositeModelRoute{{GroupID: 1, PublicModel: "alias", MatchType: service.CompositeRouteMatchExact, TargetPlatform: service.PlatformGrok, UpstreamModel: "changed", Endpoint: service.CompositeRouteEndpointAny, Enabled: true}}})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		groupID := int64(1)
		c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{DesktopCredentialType: "device", GroupID: &groupID, Group: &service.Group{ID: 1, Platform: service.PlatformComposite}})
		c.Request = c.Request.WithContext(service.WithCompositeRouteDecision(c.Request.Context(), service.CompositeRouteDecision{Matched: true, GroupID: 1, PublicModel: "alias", TargetPlatform: service.PlatformOpenAI, UpstreamModel: "accepted"}))
	})
	router.Use(compositeTargetPlatformMiddleware(resolver))
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		platform, _ := service.ResolvedTargetPlatformFromContext(c.Request.Context())
		require.Equal(t, service.PlatformOpenAI, platform)
		body, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"model":"accepted"}`, string(body))
		c.Status(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"alias"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
}
