package handler

import (
	"context"
	"time"

	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *OpenAIGatewayHandler) VideoGeneration(c *gin.Context) {
	h.handleGrokMedia(c, service.VideoEndpointCreate, "")
}

func (h *OpenAIGatewayHandler) VideoStatus(c *gin.Context) {
	h.handleGrokMedia(c, service.VideoEndpointStatus, c.Param("request_id"))
}

func (h *OpenAIGatewayHandler) VideoContent(c *gin.Context) {
	h.handleGrokMedia(c, service.VideoEndpointContent, c.Param("request_id"))
}

// Generic Videos status uses completed + url. Billing always retains the public
// model snapshot, and cannot silently fall back to Grok prices/defaults.
func prepareVideoCompletionBilling(ctx context.Context, h *OpenAIGatewayHandler, key *service.APIKey, owner middleware.AuthSubject, id string, result *service.OpenAIForwardResult) *service.OpenAIForwardResult {
	if result == nil || result.VideoCount <= 0 {
		return nil
	}
	pending, err := h.gatewayService.LoadGrokVideoPendingBilling(ctx, id, owner.UserID, key.ID)
	if err != nil || pending == nil || pending.VideoResolution == "" || pending.VideoDurationSeconds <= 0 || pending.BillingModel == "" || pending.VideoBillingSnapshot == nil {
		return nil
	}
	merged := *result
	merged.Model, merged.BillingModel = pending.Model, pending.BillingModel
	merged.UpstreamModel = firstNonEmptyString(pending.UpstreamModel, result.UpstreamModel)
	merged.VideoBillingSnapshot = pending.VideoBillingSnapshot
	// The accepted resolution determines the frozen unit price. The upstream
	// cannot switch the customer's price tier in a later status response.
	merged.VideoResolution = pending.VideoResolution
	if merged.VideoDurationSeconds <= 0 {
		merged.VideoDurationSeconds = pending.VideoDurationSeconds
	}
	if merged.VideoDurationSeconds > 15 || (merged.VideoResolution != "720p" && merged.VideoResolution != "1080p") {
		return nil
	}
	claimed, err := h.gatewayService.ClaimGrokVideoBilling(ctx, id, owner.UserID, key.ID)
	if err != nil || !claimed {
		return nil
	}
	merged.ResponseID = id
	merged.RequestID = service.StableGrokVideoBillingRequestID(id)
	merged.Duration = service.GrokVideoE2EDuration(pending.CreatedAt, time.Now())
	merged.ImageCount = 0
	merged.VideoCount = 1
	return &merged
}
