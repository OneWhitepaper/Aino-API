package handler

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// SiteCacheRate exposes only a percentage, never fleet token counts or costs.
func (h *UsageHandler) SiteCacheRate(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if _, ok := middleware.GetAuthSubjectFromContext(c); !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	visible, err := h.settingService.GetSiteCacheRateVisibility(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	payload := struct {
		VisibleToUsers bool     `json:"visible_to_users"`
		CacheHitRate   *float64 `json:"cache_hit_rate,omitempty"`
	}{VisibleToUsers: visible}
	role, _ := middleware.GetUserRoleFromContext(c)
	if !visible && role != service.RoleAdmin {
		response.Success(c, payload)
		return
	}
	userTZ := c.Query("timezone")
	startTime, startErr := timezone.ParseInUserLocation("2006-01-02", strings.TrimSpace(c.Query("start_date")), userTZ)
	endDate, endErr := timezone.ParseInUserLocation("2006-01-02", strings.TrimSpace(c.Query("end_date")), userTZ)
	if startErr != nil || endErr != nil || endDate.Before(startTime) {
		response.BadRequest(c, "Invalid date range, use YYYY-MM-DD with end_date on or after start_date")
		return
	}
	payload.CacheHitRate, err = h.usageService.GetSiteCacheHitRate(c.Request.Context(), startTime, endDate.AddDate(0, 0, 1))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, payload)
}
