package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// GetDesktopSettings returns the catalog managed by the independent admin page.
func (h *SettingHandler) GetDesktopSettings(c *gin.Context) {
	settings, err := h.settingService.GetDesktopSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

func (h *SettingHandler) UpdateDesktopSettings(c *gin.Context) {
	// A full catalog document is required: an empty or partial object must not
	// silently clear a previously published catalog.
	var input struct {
		Enabled              *bool                        `json:"enabled" binding:"required"`
		Models               *[]service.DesktopModelEntry `json:"models" binding:"required"`
		CredentialTTLSeconds *int                         `json:"credential_ttl_seconds" binding:"required,gte=300,lte=3600"`
		DefaultModelID       *string                      `json:"default_model_id"`
		DefaultImageModelID  *string                      `json:"default_image_model_id"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "Invalid desktop settings document")
		return
	}
	req := service.DesktopSettings{
		Enabled: *input.Enabled, Models: *input.Models,
		CredentialTTLSeconds: *input.CredentialTTLSeconds,
		DefaultModelID:       input.DefaultModelID, DefaultImageModelID: input.DefaultImageModelID,
	}
	before, err := h.settingService.GetDesktopSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	settings, err := h.settingService.UpdateDesktopSettings(c.Request.Context(), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	h.auditSettingsUpdate(c, &service.SystemSettings{Desktop: before}, &service.SystemSettings{Desktop: settings}, nil, nil, UpdateSettingsRequest{Desktop: &req})
	response.Success(c, settings)
}
