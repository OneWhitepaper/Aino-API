//go:build unit

package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIImagesBalanceExhaustionKeepsCauseInOps(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, streamStarted := range []bool{false, true} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", nil)
		(&OpenAIGatewayHandler{}).handleFailoverExhausted(c, &service.UpstreamFailoverError{
			StatusCode:       http.StatusBadGateway,
			Reason:           service.OpenAIImagesInsufficientBalanceReason,
			ClientStatusCode: http.StatusPaymentRequired,
			ClientMessage:    service.OpenAIImagesInsufficientBalanceMessage,
			ResponseBody:     []byte(`{"error":{"message":"Insufficient credit balance","code":"insufficient_balance"}}`),
			ResponseHeaders:  http.Header{"Retry-After": []string{"23"}, "X-Request-Id": []string{"req-images-balance"}},
		}, streamStarted)
		require.Equal(t, "23", recorder.Header().Get("Retry-After"))

		require.Contains(t, recorder.Body.String(), "当前模型服务暂不可用，请稍后重试或联系站点客服。")
		require.NotContains(t, recorder.Body.String(), "insufficient_balance")
		require.NotContains(t, recorder.Body.String(), "Insufficient credit balance")
		if !streamStarted {
			require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
			require.Equal(t, "service_unavailable", gjson.GetBytes(recorder.Body.Bytes(), "error.code").String())
		}
		require.Equal(t, http.StatusBadGateway, c.GetInt(service.OpsUpstreamStatusCodeKey))
		require.Contains(t, c.GetString(service.OpsUpstreamErrorMessageKey), "Insufficient credit balance")
	}
}

func TestUpstreamBalanceExhaustionUsesServiceUnavailableAcrossProtocols(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name  string
		path  string
		write func(*gin.Context, *service.UpstreamFailoverError)
	}{
		{"openai", "/v1/chat/completions", func(c *gin.Context, err *service.UpstreamFailoverError) {
			(&OpenAIGatewayHandler{}).handleFailoverExhausted(c, err, false)
		}},
		{"openai stream", "/v1/chat/completions", func(c *gin.Context, err *service.UpstreamFailoverError) {
			(&OpenAIGatewayHandler{}).handleFailoverExhausted(c, err, true)
		}},
		{"openai responses stream", "/v1/responses", func(c *gin.Context, err *service.UpstreamFailoverError) {
			(&OpenAIGatewayHandler{}).handleFailoverExhausted(c, err, true)
		}},
		{"anthropic", "/v1/messages", func(c *gin.Context, err *service.UpstreamFailoverError) {
			(&GatewayHandler{}).handleFailoverExhausted(c, err, service.PlatformAnthropic, false)
		}},
		{"anthropic stream", "/v1/messages", func(c *gin.Context, err *service.UpstreamFailoverError) {
			(&GatewayHandler{}).handleFailoverExhausted(c, err, service.PlatformAnthropic, true)
		}},
		{"compat chat", "/v1/chat/completions", func(c *gin.Context, err *service.UpstreamFailoverError) {
			(&GatewayHandler{}).handleCCFailoverExhausted(c, err, false)
		}},
		{"compat responses", "/v1/responses", func(c *gin.Context, err *service.UpstreamFailoverError) {
			(&GatewayHandler{}).handleResponsesFailoverExhausted(c, err, false)
		}},
		{"compat responses stream", "/v1/responses", func(c *gin.Context, err *service.UpstreamFailoverError) {
			(&GatewayHandler{}).handleResponsesFailoverExhausted(c, err, true)
		}},
		{"gemini", "/v1beta/models/gemini:generateContent", func(c *gin.Context, err *service.UpstreamFailoverError) {
			(&GatewayHandler{}).handleGeminiFailoverExhausted(c, err)
		}},
	} {
		for _, failure := range []struct {
			status int
			body   string
		}{
			{http.StatusPaymentRequired, `{"error":{"message":"Insufficient credit balance"}}`},
			{http.StatusBadGateway, `{"error":{"message":"upstream request failed","cause":{"errorKey":"insufficient_balance"}}}`},
			{http.StatusBadRequest, `{"error":{"message":"Your credit balance is too low to access the API."}}`},
			{http.StatusTooManyRequests, `{"error":{"type":"insufficient_quota","message":"You exceeded your current quota, please check your plan and billing details."}}`},
		} {
			t.Run(tc.name+"/"+http.StatusText(failure.status), func(t *testing.T) {
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, tc.path, nil)
				tc.write(c, &service.UpstreamFailoverError{StatusCode: failure.status, ResponseBody: []byte(failure.body)})
				require.Contains(t, rec.Body.String(), "当前模型服务暂不可用，请稍后重试或联系站点客服。")
				require.NotContains(t, rec.Body.String(), "insufficient")
				require.NotContains(t, rec.Body.String(), "credit balance")
				require.Equal(t, failure.status, c.GetInt(service.OpsUpstreamStatusCodeKey))
				require.NotEmpty(t, c.GetString(service.OpsUpstreamErrorMessageKey))
			})
		}
	}
}

func TestUpstreamAvailabilityMessageLeavesUserBillingErrorIntact(t *testing.T) {
	status, code, message, _ := billingErrorDetails(service.ErrInsufficientBalance)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	(&OpenAIGatewayHandler{}).errorResponse(c, status, code, message)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, "billing_error", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
	require.Equal(t, "insufficient balance", gjson.GetBytes(rec.Body.Bytes(), "error.message").String())
}

func TestNoAccountPoolUsesServiceUnavailableMessage(t *testing.T) {
	cls := classifyNoAccountErrorFromGin(newTestGinContextWithRequest(), nil, nil, "image-model", "image-model", service.PlatformOpenAI)
	require.Equal(t, http.StatusServiceUnavailable, cls.Status)
	require.Equal(t, "当前模型服务暂不可用，请稍后重试或联系站点客服。", cls.Message)
}
