package handler

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// Device usage aggregates one real key across its historical billing groups.
// A current wallet balance cannot represent remaining subscription capacity.
func writeDesktopDeviceUsage(c *gin.Context, key *service.APIKey, usage gin.H, dailyUsage, modelStats any) {
	response := gin.H{"mode": "device", "isValid": true, "api_key_id": key.ID, "billing_scope": "device", "unit": "USD"}
	if key.Quota > 0 {
		response["quota"] = gin.H{"limit": key.Quota, "used": key.QuotaUsed, "remaining": max(0, key.Quota-key.QuotaUsed)}
	}
	if usage != nil {
		response["usage"] = usage
	}
	if dailyUsage != nil {
		response["daily_usage"] = dailyUsage
	}
	if modelStats != nil {
		response["model_stats"] = modelStats
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, response)
}
