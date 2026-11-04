package routes

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	limitmiddleware "github.com/Wei-Shaw/sub2api/internal/middleware"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// RegisterDesktopRoutes registers account-scoped desktop APIs and bounded aggregate ingestion.
func RegisterDesktopRoutes(
	v1 *gin.RouterGroup,
	h *handler.Handlers,
	jwtAuth middleware.JWTAuthMiddleware,
	settingService *service.SettingService,
	panelRateLimiter *middleware.PanelRateLimiter,
	redisClient *redis.Client,
) {
	// Reuse the existing Redis limiter; a missing limiter must not expose ingestion.
	if redisClient != nil {
		limiter := limitmiddleware.NewRateLimiter(redisClient)
		v1.POST("/desktop/shared-metrics", limiter.LimitWithOptions("desktop-shared-metrics", 30, time.Minute,
			limitmiddleware.RateLimitOptions{FailureMode: limitmiddleware.RateLimitFailClose}), h.Desktop.UploadSharedMetrics)
	}
	desktop := v1.Group("/desktop")
	desktop.Use(gin.HandlerFunc(jwtAuth))
	desktop.Use(middleware.BackendModeUserGuard(settingService))
	desktop.Use(panelRateLimiter.Global())
	{
		desktop.GET("/bootstrap", h.Desktop.GetBootstrap)
		desktop.GET("/models", h.Desktop.ListModels)
		desktop.GET("/billing-summary", h.Desktop.BillingSummary)
		desktop.POST("/credentials", h.Desktop.ProvisionCredential)
		desktop.GET("/devices", h.Desktop.ListDevices)
		desktop.DELETE("/devices/:device_id", h.Desktop.RevokeDevice)
		desktop.POST("/diagnostics", h.Desktop.UploadDiagnostics)
		desktop.GET("/diagnostics/:id", h.Desktop.GetOwnDiagnostic)
		desktop.DELETE("/diagnostics/:id", h.Desktop.DeleteOwnDiagnostic)
	}
}
