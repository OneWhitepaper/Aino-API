package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// Keep the operator's cause separate from the client-facing availability
// message, including legacy image failovers with no response body.
func recordUpstreamBalanceFailure(c *gin.Context, failure *service.UpstreamFailoverError) bool {
	if failure == nil || (failure.Reason != service.OpenAIImagesInsufficientBalanceReason &&
		!service.IsUpstreamBalanceError(failure.StatusCode, failure.ResponseBody)) {
		return false
	}
	service.RecordUpstreamBalanceError(c, failure.StatusCode, failure.ResponseBody)
	return true
}
