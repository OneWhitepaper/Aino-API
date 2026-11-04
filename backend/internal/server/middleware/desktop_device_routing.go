package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type desktopModelRouter interface {
	ResolveRequest(context.Context, *service.APIKey, string, string, []string, string, bool) (*service.DesktopModelRequest, error)
	ListForUser(context.Context, int64) ([]service.PlatformModel, error)
}

func ProvideAPIKeyAuthMiddleware(keys *service.APIKeyService, subscriptions *service.SubscriptionService, cfg *config.Config, models *service.DesktopModelService, tasks *service.ImageTaskService) APIKeyAuthMiddleware {
	return APIKeyAuthMiddleware(apiKeyAuthWithDeviceRouting(keys, subscriptions, cfg, models, tasks))
}

func desktopDeviceEndpoint(path string) string {
	switch path {
	case "/v1/chat/completions":
		return "chat_completions"
	case "/v1/responses":
		return "responses"
	case "/v1/messages", "/v1/messages/count_tokens":
		return "anthropic_messages"
	case "/v1/images/generations", "/v1/images/edits", "/v1/images/generations/async", "/v1/images/edits/async":
		return "images_generations"
	}
	return ""
}

func enforceDesktopDeviceScope(c *gin.Context) bool {
	path := c.Request.URL.Path
	allowed := c.Request.Method == http.MethodPost && desktopDeviceEndpoint(path) != "" ||
		c.Request.Method == http.MethodGet && (path == "/v1/models" || path == "/v1/usage" || path == "/v1/sub2api/billing" || c.FullPath() == "/v1/models/:model" || isAsyncImageTaskRead(c.Request.Method, path))
	if !allowed || strings.EqualFold(c.GetHeader("Upgrade"), "websocket") {
		message := "This endpoint is outside desktop device authorization"
		if strings.HasPrefix(path, "/v1/images/batches") {
			message = "Desktop device keys do not support the balance-only batch image billing path; use ordinary image generation"
		}
		AbortWithError(c, http.StatusForbidden, "DESKTOP_CREDENTIAL_SCOPE", message)
		return false
	}
	return true
}

func resolveDesktopDeviceRoute(c *gin.Context, key *service.APIKey, models desktopModelRouter, tasks *service.ImageTaskService) (*service.APIKey, bool) {
	if key.DesktopCredentialType != service.DesktopCredentialTypeDevice {
		return key, true
	}
	path := c.Request.URL.Path
	if isAsyncImageTaskRead(c.Request.Method, path) {
		route, err := tasks.GetRoute(c.Request.Context(), service.ImageTaskOwner{UserID: key.UserID, APIKeyID: key.ID}, c.Param("task_id"))
		if err != nil {
			abortDesktopRouteError(c, err)
			return nil, false
		}
		copy := *key
		copy.Group, copy.GroupID = route.Group, &route.Group.ID
		copy.Group.Platform = route.Platform
		if route.Subscription != nil {
			c.Set(string(ContextKeySubscription), route.Subscription)
		}
		c.Request = c.Request.WithContext(service.WithDesktopCredentialGroup(c.Request.Context(), route.Group.ID))
		return &copy, true
	}
	if path == "/v1/usage" {
		return key, true
	}
	if models == nil {
		AbortWithError(c, http.StatusServiceUnavailable, "DESKTOP_ROUTING_UNAVAILABLE", "Desktop model routing is unavailable")
		return nil, false
	}
	if c.Request.Method == http.MethodGet && (path == "/v1/models" || c.FullPath() == "/v1/models/:model") {
		list, err := models.ListForUser(c.Request.Context(), key.UserID)
		if err != nil {
			abortDesktopRouteError(c, err)
			return nil, false
		}
		data := make([]gin.H, 0, len(list))
		for _, model := range list {
			if c.Param("model") != "" && c.Param("model") != model.ID {
				continue
			}
			data = append(data, gin.H{"id": model.ID, "object": "model", "owned_by": "aino", "model": model.Model, "aino": model})
		}
		c.Header("Cache-Control", "no-store")
		if c.Param("model") != "" {
			if len(data) == 0 {
				AbortWithError(c, http.StatusNotFound, "DESKTOP_MODEL_NOT_ALLOWED", "Catalog model not found")
			} else {
				c.JSON(http.StatusOK, data[0])
				c.Abort()
			}
		} else {
			c.JSON(http.StatusOK, gin.H{"object": "list", "data": data})
			c.Abort()
		}
		return nil, false
	}
	ids := c.Request.Header.Values("X-Aino-Model-ID")
	if len(ids) != 1 || strings.TrimSpace(ids[0]) == "" || len(ids[0]) > 128 {
		AbortWithError(c, http.StatusBadRequest, "DESKTOP_MODEL_ID_REQUIRED", "Select one catalog option with X-Aino-Model-ID")
		return nil, false
	}
	billingRead := path == "/v1/sub2api/billing"
	var candidates []string
	if !billingRead {
		var ok bool
		candidates, ok = groupModelAllowlistModelsFromBody(c)
		if !ok {
			return nil, false
		}
	}
	resolved, err := models.ResolveRequest(c.Request.Context(), key, strings.TrimSpace(ids[0]), desktopDeviceEndpoint(path), candidates, c.GetHeader("X-Aino-Purpose"), !billingRead)
	if err != nil {
		abortDesktopRouteError(c, err)
		return nil, false
	}
	ctx := service.WithDesktopCredentialGroup(c.Request.Context(), *resolved.Key.GroupID)
	if resolved.Route != nil {
		ctx = service.WithCompositeRouteDecision(ctx, *resolved.Route)
	}
	c.Request = c.Request.WithContext(ctx)
	return resolved.Key, true
}

func abortDesktopRouteError(c *gin.Context, err error) {
	AbortWithError(c, infraerrors.Code(err), infraerrors.Reason(err), infraerrors.Message(err))
}
