package service

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// UpstreamServiceUnavailableMessage describes provider-side availability to
// callers without suggesting that they need to top up their own site balance.
const UpstreamServiceUnavailableMessage = "当前模型服务暂不可用，请稍后重试或联系站点客服。"
const UpstreamServiceUnavailableCode = "service_unavailable"

// RecordUpstreamBalanceError retains the provider's diagnosis in ops even if
// its outer message only says "upstream request failed".
func RecordUpstreamBalanceError(c *gin.Context, status int, body []byte) {
	message := "Upstream account has insufficient balance or quota"
	if original := sanitizeUpstreamErrorMessage(extractUpstreamErrorMessage(body)); original != "" {
		message += ": " + original
	}
	setOpsUpstreamError(c, status, message, "")
}

// IsUpstreamBalanceError is used only when presenting an upstream error. It
// does not change account cooldowns, failover decisions, or local billing.
func IsUpstreamBalanceError(status int, body []byte) bool {
	if status < http.StatusBadRequest {
		return false
	}
	if status == http.StatusPaymentRequired || isOpenAIImagesInsufficientBalance(body) {
		return true
	}
	for _, path := range []string{"error.code", "error.type", "response.error.code", "response.error.type", "code"} {
		if isUpstreamBalanceCode(gjson.GetBytes(body, path).String()) {
			return true
		}
	}
	// Read explicit error messages rather than scanning arbitrary JSON that
	// could include echoed request content or a prompt mentioning balance.
	for _, path := range []string{"error.message", "response.error.message", "error.errorMessage", "message"} {
		if isUpstreamBalanceMessage(gjson.GetBytes(body, path).String()) {
			return true
		}
	}
	return false
}

func isUpstreamBalanceCode(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "insufficient_balance", "insufficient_quota", "insufficient_credit":
		return true
	default:
		return false
	}
}

func isUpstreamBalanceMessage(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	for _, prefix := range []string{
		"insufficient balance", "insufficient credit", "your credit balance is too low",
		"balance is not enough", "no enough balance", "余额不足", "账户余额不足", "账号余额不足",
	} {
		if strings.HasPrefix(message, prefix) {
			return true
		}
	}
	return false
}
