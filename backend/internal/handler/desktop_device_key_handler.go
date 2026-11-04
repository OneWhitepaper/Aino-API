package handler

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *DesktopHandler) SetDeviceKeyService(keys *service.DesktopDeviceKeyService) {
	h.deviceKeys = keys
}

func (h *DesktopHandler) SetDeviceKeyPolicy(policy config.DesktopDeviceKeysConfig) {
	h.deviceKeyPolicy = policy
}

func (h *DesktopHandler) GetDeviceKey(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if h.deviceKeys == nil {
		response.ErrorFrom(c, service.ErrServiceUnavailable)
		return
	}
	result, err := h.deviceKeys.Get(c.Request.Context(), subject.UserID, c.Param("device_id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *DesktopHandler) ProvisionDeviceKey(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if h.deviceKeys == nil || h.users == nil {
		response.ErrorFrom(c, service.ErrServiceUnavailable)
		return
	}
	var req service.DesktopDeviceKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid device key request")
		return
	}
	_, stateErr := h.deviceKeys.Get(c.Request.Context(), subject.UserID, req.DeviceID)
	if stateErr != nil && !errors.Is(stateErr, service.ErrDesktopDeviceKeyNotFound) {
		response.ErrorFrom(c, stateErr)
		return
	}
	mutation := req.Rotate || req.Reauthorize || errors.Is(stateErr, service.ErrDesktopDeviceKeyNotFound)
	if mutation && !h.deviceKeyPolicy.AllowsMutation(subject.UserID, req.DeviceID) {
		response.ErrorFrom(c, infraerrors.Forbidden("DESKTOP_DEVICE_KEY_ROLLOUT_DENIED", "Device key provisioning is not enabled for this installation"))
		return
	}
	// auth_time comes from the completed login (including login MFA), survives
	// token refresh unchanged, and is independent of the permanent model key.
	if mutation && !service.IsPhoneBindingRecentAuth(subject.AuthTime, time.Now()) {
		response.ErrorFrom(c, service.ErrRecentAuthenticationRequired)
		return
	}
	h.executeDeviceKeyMutation(c, subject.UserID, req)
}

// Only metadata enters the coordinator. The secret exists solely in the first
// request's closure; a replay returns metadata and can recover by versioned rotation.
func (h *DesktopHandler) executeDeviceKeyMutation(c *gin.Context, userID int64, req service.DesktopDeviceKeyRequest) {
	var secret string
	execute := func(ctx context.Context) (any, error) {
		result, err := h.deviceKeys.Ensure(ctx, userID, req)
		if err != nil {
			return nil, err
		}
		secret = result.APIKey
		result.APIKey = ""
		return result, nil
	}
	coordinator := service.DefaultIdempotencyCoordinator()
	if coordinator == nil {
		data, err := h.deviceKeys.Ensure(c.Request.Context(), userID, req)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		response.Success(c, data)
		return
	}
	result, err := coordinator.Execute(c.Request.Context(), service.IdempotencyExecuteOptions{Scope: "desktop.device-key", ActorScope: "user:" + strconv.FormatInt(userID, 10), Method: c.Request.Method, Route: c.FullPath(), IdempotencyKey: c.GetHeader("Idempotency-Key"), Payload: req, RequireKey: true, TTL: 24 * time.Hour, ExecutionTimeout: 30 * time.Second}, execute)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if result.Replayed {
		c.Header("X-Idempotency-Replayed", "true")
		// Revocation after the first response must never replay stale active state.
		current, stateErr := h.deviceKeys.Get(c.Request.Context(), userID, req.DeviceID)
		if stateErr != nil {
			response.ErrorFrom(c, stateErr)
			return
		}
		response.Success(c, current)
		return
	}
	metadata, ok := result.Data.(*service.DesktopDeviceKeyResponse)
	if !ok {
		response.ErrorFrom(c, service.ErrServiceUnavailable)
		return
	}
	copy := *metadata
	copy.APIKey = secret
	response.Success(c, &copy)
}

func (h *DesktopHandler) revokeDesktopDevice(ctx context.Context, userID int64, deviceID string) error {
	var deviceErr error
	if h.deviceKeys != nil {
		deviceErr = h.deviceKeys.Revoke(ctx, userID, deviceID)
	} else {
		deviceErr = service.ErrDesktopDeviceKeyNotFound
	}
	legacyErr := h.credentials.RevokeDevice(ctx, userID, deviceID)
	if errors.Is(deviceErr, service.ErrDesktopDeviceKeyNotFound) {
		return legacyErr
	}
	if errors.Is(legacyErr, service.ErrDesktopCredentialNotFound) {
		return deviceErr
	}
	return errors.Join(deviceErr, legacyErr)
}
