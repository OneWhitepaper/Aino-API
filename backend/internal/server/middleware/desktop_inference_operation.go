package middleware

import (
	"crypto/sha256"
	"fmt"
	"net/http"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestmodel"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const desktopOperationModelKey = "desktop_operation_model"

func bindDesktopOperation(c *gin.Context, key *service.APIKey) bool {
	values := c.Request.Header.Values("X-Aino-Operation-ID")
	if !key.DesktopManaged || len(values) == 0 {
		return true
	}
	if len(values) != 1 || !service.ValidDesktopUsageID(values[0]) {
		AbortWithError(c, http.StatusBadRequest, "INVALID_DESKTOP_OPERATION", "Invalid desktop operation identifier")
		return false
	}
	c.Set(desktopOperationModelKey, c.GetHeader("X-Aino-Model-ID"))
	c.Request = c.Request.WithContext(service.WithDesktopInferenceOperation(c.Request.Context(), values[0]))
	return true
}

// DesktopInferenceOperation reserves before any provider dispatch. Read-only
// endpoints and legacy clients without an operation ID retain their contract.
func DesktopInferenceOperation() gin.HandlerFunc {
	return func(c *gin.Context) {
		operationID := service.DesktopInferenceOperationID(c.Request.Context())
		if operationID == "" || c.Request.Method != http.MethodPost || desktopDeviceEndpoint(c.Request.URL.Path) == "" || c.Request.URL.Path == "/v1/messages/count_tokens" {
			c.Next()
			return
		}
		key, ok := GetAPIKeyFromContext(c)
		if !ok || !key.DesktopManaged {
			AbortWithError(c, http.StatusUnauthorized, "API_KEY_INVALID", "Invalid desktop authorization")
			return
		}
		body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
		if err != nil {
			AbortWithError(c, http.StatusBadRequest, "INVALID_DESKTOP_OPERATION", "Unable to read desktop operation body")
			return
		}
		requestmodel.ResetRequestBody(c.Request, body)
		var subscriptionID int64
		if subscription, ok := GetSubscriptionFromContext(c); ok && subscription != nil {
			subscriptionID = subscription.ID
		}
		scope := fmt.Sprintf("desktop_inference:user:%d:key:%d", key.UserID, key.ID)
		err = service.DefaultIdempotencyCoordinator().ClaimInferenceOperation(c.Request.Context(), service.IdempotencyExecuteOptions{
			Scope: scope, ActorScope: scope, Method: c.Request.Method,
			Route: c.Request.URL.Path, IdempotencyKey: operationID,
			Payload: struct {
				BodySHA256    string
				ContentType   string
				Query         string
				CatalogOption string
				GroupID       *int64
				Subscription  int64
			}{fmt.Sprintf("%x", sha256.Sum256(body)), c.GetHeader("Content-Type"), c.Request.URL.RawQuery, c.GetString(desktopOperationModelKey), key.GroupID, subscriptionID},
		})
		if err != nil {
			AbortWithError(c, infraerrors.Code(err), infraerrors.Reason(err), infraerrors.Message(err))
			return
		}
		c.Next()
	}
}
