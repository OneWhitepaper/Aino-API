package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *SettingHandler) UpdateSiteCacheRateVisibility(c *gin.Context) {
	var req struct {
		VisibleToUsers *bool `json:"visible_to_users" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "visible_to_users must be a boolean")
		return
	}
	if err := h.settingService.SetSiteCacheRateVisibility(c.Request.Context(), *req.VisibleToUsers); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, gin.H{"visible_to_users": *req.VisibleToUsers})
}
