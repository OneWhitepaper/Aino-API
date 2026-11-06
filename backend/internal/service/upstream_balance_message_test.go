package service

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestUpstreamBalanceDirectErrorsKeepOperatorDetails(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(*OpenAIGatewayService, *gin.Context, *Account, int, string) error
	}{
		{"responses", func(s *OpenAIGatewayService, c *gin.Context, a *Account, status int, body string) error {
			_, err := s.handleErrorResponse(context.Background(), newOpenAIUpstreamErrorResponse(status, body), c, a, nil)
			return err
		}},
		{"chat", func(s *OpenAIGatewayService, c *gin.Context, a *Account, status int, body string) error {
			_, err := s.handleCompatErrorResponse(newOpenAIUpstreamErrorResponse(status, body), c, a, writeChatCompletionsError)
			return err
		}},
		{"images", func(s *OpenAIGatewayService, c *gin.Context, a *Account, status int, body string) error {
			_, err := s.handleOpenAIImagesErrorResponse(context.Background(), newOpenAIUpstreamErrorResponse(status, body), c, a)
			return err
		}},
		{"passthrough", func(s *OpenAIGatewayService, c *gin.Context, a *Account, status int, body string) error {
			return s.handleErrorResponsePassthrough(context.Background(), newOpenAIUpstreamErrorResponse(status, body), c, a, nil, []byte(body))
		}},
		{"anthropic", func(_ *OpenAIGatewayService, c *gin.Context, a *Account, status int, body string) error {
			_, err := (&GatewayService{cfg: &config.Config{}}).handleErrorResponse(context.Background(), newOpenAIUpstreamErrorResponse(status, body), c, a)
			return err
		}},
		{"gemini chat", func(_ *OpenAIGatewayService, c *gin.Context, a *Account, status int, body string) error {
			return (&GeminiMessagesCompatService{cfg: &config.Config{}}).writeGeminiChatCompletionsMappedError(c, a, status, "req-balance", []byte(body))
		}},
		{"gemini messages", func(_ *OpenAIGatewayService, c *gin.Context, a *Account, status int, body string) error {
			return (&GeminiMessagesCompatService{cfg: &config.Config{}}).writeGeminiMappedError(c, a, status, "req-balance", []byte(body))
		}},
		{"gemini native", func(_ *OpenAIGatewayService, c *gin.Context, a *Account, status int, body string) error {
			return (&GeminiMessagesCompatService{cfg: &config.Config{}}).writeGeminiNativeUpstreamError(c, a, newOpenAIUpstreamErrorResponse(status, body), []byte(body), "req-balance", false)
		}},
	} {
		for _, failure := range []struct {
			status int
			body   string
		}{
			{http.StatusPaymentRequired, `{"error":{"message":"Insufficient credit balance"}}`},
			{http.StatusBadRequest, `{"error":{"code":"insufficient_balance","message":"Insufficient credit balance"}}`},
		} {
			t.Run(tc.name+"/"+http.StatusText(failure.status), func(t *testing.T) {
				c, rec := newOpenAIUpstreamErrorTestContext(t)
				err := tc.write(&OpenAIGatewayService{cfg: &config.Config{}}, c, newOpenAIUpstreamErrorTestAccount(), failure.status, failure.body)
				require.Error(t, err)
				require.Equal(t, http.StatusServiceUnavailable, rec.Code)
				require.Equal(t, "当前模型服务暂不可用，请稍后重试或联系站点客服。", gjson.GetBytes(rec.Body.Bytes(), "error.message").String())
				require.NotContains(t, rec.Body.String(), "insufficient_balance")
				require.Equal(t, failure.status, c.GetInt(OpsUpstreamStatusCodeKey))
				require.Contains(t, c.GetString(OpsUpstreamErrorMessageKey), "Insufficient credit balance")
			})
		}
	}
}

func TestUpstreamBalancePresentationDoesNotClassifyRequestEchoesOrRateLimits(t *testing.T) {
	for _, body := range []string{
		`{"error":{"code":"invalid_request_error","message":"Invalid prompt: insufficient balance"},"request":{"code":"insufficient_balance"}}`,
		`{"error":{"type":"rate_limit_error","message":"Rate limit exceeded"}}`,
		`{"error":{"message":"Invalid value","details":[{"request":{"code":"insufficient_balance"}}]}}`,
		`{"error":{"message":"{\"errorKey\":\"insufficient_balance\"}"}}`,
	} {
		require.False(t, IsUpstreamBalanceError(http.StatusBadRequest, []byte(body)))
	}
	require.False(t, IsUpstreamBalanceError(http.StatusOK, []byte(`{"code":"insufficient_balance"}`)))
}

func TestImageBalanceStreamAndJSONOnlyExposeServiceUnavailable(t *testing.T) {
	upstreamErr := &OpenAIImagesUpstreamError{StatusCode: http.StatusPaymentRequired, Code: "insufficient_balance", Message: "Insufficient credit balance"}
	c, rec := newOpenAIUpstreamErrorTestContext(t)
	require.True(t, writeOpenAIImagesUpstreamErrorResponse(c, upstreamErr))
	for _, body := range [][]byte{rec.Body.Bytes(), buildOpenAIImagesStreamErrorBodyFromUpstream(upstreamErr)} {
		require.Equal(t, "当前模型服务暂不可用，请稍后重试或联系站点客服。", gjson.GetBytes(body, "error.message").String())
		require.Equal(t, "service_unavailable", gjson.GetBytes(body, "error.code").String())
		require.NotContains(t, string(body), "insufficient_balance")
	}
	require.Contains(t, upstreamErr.Error(), "Insufficient credit balance", "the internal error retains the provider cause")
	diagnostic := openAIImagesUpstreamErrorResponseBody(upstreamErr)
	require.Equal(t, "insufficient_balance", gjson.GetBytes(diagnostic, "error.code").String(), "failover classification must retain the provider cause")
}

func TestImageBalanceOAuthErrorRetainsOriginalOpsDiagnosis(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			c, rec := newOpenAIUpstreamErrorTestContext(t)
			body := "data: {\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"code\":\"insufficient_balance\",\"message\":\"Insufficient credit balance\"}}\n\n"
			resp := newOpenAIUpstreamErrorResponse(http.StatusOK, body)
			resp.Header.Set("Content-Type", "text/event-stream")
			svc := &OpenAIGatewayService{cfg: &config.Config{}}
			if stream {
				_, _, _, _, err := svc.handleOpenAIImagesOAuthStreamingResponse(resp, c, time.Now(), "b64_json", "image_generation", "gpt-image-1")
				require.Error(t, err)
			} else {
				_, _, _, err := svc.handleOpenAIImagesOAuthNonStreamingResponse(resp, c, "b64_json", "gpt-image-1")
				require.Error(t, err)
			}
			require.Contains(t, rec.Body.String(), "当前模型服务暂不可用，请稍后重试或联系站点客服。")
			require.NotContains(t, rec.Body.String(), "Insufficient credit balance")
			require.Equal(t, http.StatusBadRequest, c.GetInt(OpsUpstreamStatusCodeKey))
			require.Contains(t, c.GetString(OpsUpstreamErrorMessageKey), "Insufficient credit balance")
		})
	}
}
