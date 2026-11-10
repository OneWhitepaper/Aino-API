package handler

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type DesktopHandler struct {
	users           *service.UserService
	totp            *service.TotpService
	models          *service.DesktopModelService
	credentials     *service.DesktopCredentialService
	deviceKeys      *service.DesktopDeviceKeyService
	deviceKeyPolicy config.DesktopDeviceKeysConfig
	billing         *service.DesktopBillingService
	diagnostics     *service.DiagnosticsService
}

func NewDesktopHandler(models *service.DesktopModelService) *DesktopHandler {
	return &DesktopHandler{models: models}
}

func (h *DesktopHandler) SetCredentialService(credentials *service.DesktopCredentialService) {
	h.credentials = credentials
}

func ProvideDesktopHandler(models *service.DesktopModelService, credentials *service.DesktopCredentialService, users *service.UserService, totp *service.TotpService, billing *service.DesktopBillingService, diagnostics *service.DiagnosticsService, deviceKeys *service.DesktopDeviceKeyService, cfg *config.Config) *DesktopHandler {
	h := NewDesktopHandler(models)
	h.SetCredentialService(credentials)
	h.SetCredentialSecurity(users, totp)
	h.SetBillingService(billing)
	h.SetDiagnosticsService(diagnostics)
	h.SetDeviceKeyService(deviceKeys)
	h.SetDeviceKeyPolicy(cfg.DesktopDeviceKeys)
	return h
}

func (h *DesktopHandler) SetCredentialSecurity(users *service.UserService, totp *service.TotpService) {
	h.users, h.totp = users, totp
}

// GetBootstrap returns desktop capability metadata without provisioning a key
// or touching any upstream model service.
func (h *DesktopHandler) GetBootstrap(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	bootstrap, err := h.models.Bootstrap(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	deviceKeyEnabled := h.deviceKeyPolicy.AllowsMutation(subject.UserID, c.Query("device_id"))
	deviceKeyCapability := 0
	if deviceKeyEnabled {
		deviceKeyCapability = 1
	}
	response.Success(c, struct {
		*service.DesktopBootstrap
		Capabilities map[string]int `json:"capabilities"`
	}{bootstrap, map[string]int{"managed_device_key": deviceKeyCapability}})
}

// ListModels keeps the legacy chat catalog unless the client opts into images.
func (h *DesktopHandler) ListModels(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	includeImages, ok := parseBoolQueryWithDefault(c, "include_images", false)
	if !ok {
		return
	}
	models, err := h.models.ListForUser(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if !includeImages {
		chatModels := make([]service.PlatformModel, 0, len(models))
		for _, model := range models {
			if model.Purpose == service.DesktopPurposeChat {
				chatModels = append(chatModels, model)
			}
		}
		models = chatModels
	}
	response.Success(c, models)
}

// ProvisionCredential provisions a managed API key for the desktop client.
func (h *DesktopHandler) ProvisionCredential(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	var req struct {
		Purpose           string `json:"purpose"`
		DeviceID          string `json:"device_id" binding:"required"`
		ConnectionGrantID string `json:"connection_grant_id" binding:"required"`
		ModelID           string `json:"model_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	// Extract session_family_id from JWT claims stored in context
	sessionID := c.GetString(middleware.ContextKeySessionID)
	if sessionID == "" {
		response.Unauthorized(c, "Session ID not found")
		return
	}

	credReq := service.DesktopCredentialRequest{
		Purpose:           req.Purpose,
		UserID:            subject.UserID,
		DeviceID:          req.DeviceID,
		ConnectionGrantID: req.ConnectionGrantID,
		SessionFamilyID:   sessionID,
		ModelID:           req.ModelID,
	}

	cred, err := h.credentials.IssueOrReuseLease(c.Request.Context(), credReq)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, cred)
}

func (h *DesktopHandler) ListDevices(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	devices, err := h.credentials.ListDevices(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if h.deviceKeys != nil {
		devices, err = h.deviceKeys.MergeDevices(c.Request.Context(), subject.UserID, devices)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
	}
	devices, err = h.credentials.AttachDeviceInfo(c.Request.Context(), subject.UserID, devices)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, devices)
}

func (h *DesktopHandler) RevokeDevice(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if h.users == nil {
		response.ErrorFrom(c, service.ErrServiceUnavailable)
		return
	}
	user, err := h.users.GetByID(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if user.TotpEnabled {
		if h.totp == nil {
			response.ErrorFrom(c, service.ErrServiceUnavailable)
			return
		}
		if !middleware.EnforceStepUpAlways(c, h.totp, h.users) {
			return
		}
	} else if !service.IsPhoneBindingRecentAuth(subject.AuthTime, time.Now()) {
		response.ErrorFrom(c, service.ErrRecentAuthenticationRequired)
		return
	}
	if err := h.revokeDesktopDevice(c.Request.Context(), subject.UserID, c.Param("device_id")); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"revoked": true})
}
