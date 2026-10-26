package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// GetContactPage returns published contact details to signed-in users.
func (h *SettingHandler) GetContactPage(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	settings, err := h.settingService.GetContactPage(c.Request.Context(), false)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}
