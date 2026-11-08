//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestRawChatZeroUsageDiagnosticPreservesResponseAndRedactsContent(t *testing.T) {
	for _, tc := range []struct {
		name, usage string
		wantLog     bool
	}{
		{"missing", "", true},
		{"zero", `,"usage":{"prompt_tokens":0,"completion_tokens":0,"secret":"private-response-secret"}`, true},
		{"wrong counter type", `,"usage":{"prompt_tokens":"private-counter-secret","completion_tokens":0}`, true},
		{"total only", `,"usage":{"total_tokens":18}`, true},
		{"nested total only", `,"data":{"response":{"usage":{"total_tokens":18}}}`, true},
		{"normal", `,"usage":{"input_tokens":0,"output_tokens":0,"prompt_tokens":9,"completion_tokens":1}`, false},
		{"cache only", `,"usage":{"cache_read_input_tokens":5}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, rec, logs := usageDiagnosticContext(t)
			body := `{"model":"deepseek-test","choices":[{"message":{"content":"private-answer-secret"},"finish_reason":"stop"}]` + tc.usage + `}`
			resp := usageDiagnosticResponse(body, false)
			result, err := (&OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}).bufferRawChatCompletions(
				c, resp, &Account{ID: 17, Type: AccountTypeAPIKey}, "deepseek-test", "deepseek-test", "deepseek-test", nil, nil, time.Now())
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, body, rec.Body.String(), "diagnostics must not alter the response or billing parser")
			entries := logs.FilterMessage("openai_usage.success_missing_usage").All()
			if !tc.wantLog {
				require.Empty(t, entries)
				return
			}
			require.Len(t, entries, 1)
			fields := entries[0].ContextMap()
			require.Equal(t, int64(17), fields["account_id"])
			require.Equal(t, int64(4), fields["user_id"])
			require.Equal(t, int64(223), fields["api_key_id"])
			require.Equal(t, "req-usage-diagnostic", fields["request_id"])
			require.Equal(t, "deepseek-test", fields["upstream_model"])
			require.Equal(t, false, fields["stream"])
			require.Equal(t, "json", fields["terminal_event"])
			require.Contains(t, fields, "usage_shape")
			encoded, marshalErr := json.Marshal(fields)
			require.NoError(t, marshalErr)
			for _, secret := range []string{"private-answer-secret", "private-prompt-secret", "private-key-secret", "private-response-secret", "private-counter-secret", "private-query-secret", "Authorization", "refresh_token"} {
				require.NotContains(t, string(encoded), secret)
			}
			if tc.name == "nested total only" {
				require.Contains(t, string(encoded), `data.response.usage`)
				require.Contains(t, string(encoded), `"total_tokens":18`)
			}
		})
	}
}

func TestRawChatStreamZeroUsageDiagnosticRetainsOnlyBoundedShape(t *testing.T) {
	for _, tc := range []struct {
		name, frames, terminal string
		wantLog                bool
	}{
		{"missing with done", "", "data: [DONE]\n\n", true},
		{"finish reason without done", "", "", true},
		{"zero with done", "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":0,\"completion_tokens\":0}}\n\n", "data: [DONE]\n\n", true},
		{"positive overwritten by empty", "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":1}}\n\ndata: {\"choices\":[],\"usage\":{}}\n\n", "data: [DONE]\n\n", true},
		{"normal", "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":1}}\n\n", "data: [DONE]\n\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, rec, logs := usageDiagnosticContext(t)
			body := "data: {\"choices\":[{\"delta\":{\"content\":\"private-answer-secret\"},\"finish_reason\":\"stop\"}]}\n\n" + tc.frames + tc.terminal
			result, err := (&OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}).streamRawChatCompletions(
				c, usageDiagnosticResponse(body, true), &Account{ID: 17, Type: AccountTypeAPIKey}, "deepseek-test", "deepseek-test", "deepseek-test", nil, nil, time.Now(), 10)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Contains(t, rec.Body.String(), "private-answer-secret", "client output must remain intact")
			entries := logs.FilterMessage("openai_usage.success_missing_usage").All()
			if !tc.wantLog {
				require.Empty(t, entries)
				return
			}
			require.Len(t, entries, 1)
			fields := entries[0].ContextMap()
			require.Equal(t, true, fields["stream"])
			encoded, marshalErr := json.Marshal(fields)
			require.NoError(t, marshalErr)
			require.NotContains(t, string(encoded), "private-answer-secret")
			if tc.name == "positive overwritten by empty" {
				require.Contains(t, string(encoded), `"saw_nonzero_usage":true`)
				require.Contains(t, string(encoded), `"zero_after_nonzero":true`)
			}
		})
	}
}

func TestResponsesZeroUsageDiagnosticUsesSharedObservation(t *testing.T) {
	c, _, logs := usageDiagnosticContext(t)
	observer := beginUpstreamResponseModelObservation(c)
	observer.ObserveOpenAI([]byte(`{"type":"response.completed","response":{"model":"deepseek-test","status":"completed","output":[{"text":"private-answer-secret"}],"usage":{"input_tokens":0,"output_tokens":0}}}`), "response.completed")
	logOpenAISuccessMissingUsage(c.Request.Context(), c, &Account{ID: 17}, usageDiagnosticResponse("", false), &OpenAIUsage{}, "response.completed", false)
	entries := logs.FilterMessage("openai_usage.success_missing_usage").All()
	require.Len(t, entries, 1)
	encoded, err := json.Marshal(entries[0].ContextMap())
	require.NoError(t, err)
	require.Contains(t, string(encoded), "response.usage")
	require.NotContains(t, string(encoded), "private-answer-secret")
}

func TestZeroUsageDiagnosticCannotCopyProviderControlledText(t *testing.T) {
	c, _, logs := usageDiagnosticContext(t)
	c.Request.URL.Path = "/private-path-secret"
	SetOpsUpstreamModel(c, "https://upstream.invalid/private-model-secret?key=private-url-secret")
	group := int64(3)
	c.Set("api_key", &APIKey{ID: 223, UserID: 4, GroupID: &group, Key: "private-key-secret", DesktopUsage: &DesktopUsage{CallID: "bad-private-call-secret"}})
	o := beginUpstreamResponseModelObservation(c)
	o.ObserveOpenAI([]byte(`{"model":"Bearer private-model-secret","status":"private-status-secret","choices":[{"finish_reason":"private-finish-secret"}],"usage":{"input_tokens":"private-counter-secret","private-field-secret":33,"output_tokens":-5}}`), "")
	resp := usageDiagnosticResponse("", false)
	resp.Header.Set("X-Request-Id", "private-request-secret\nAuthorization: private-header-secret")
	logOpenAISuccessMissingUsage(c.Request.Context(), c, &Account{ID: 17}, resp, &OpenAIUsage{}, "json", false)
	entries := logs.FilterMessage("openai_usage.success_missing_usage").All()
	require.Len(t, entries, 1)
	encoded, err := json.Marshal(entries[0].ContextMap())
	require.NoError(t, err)
	for _, value := range []string{"private-model-secret", "private-url-secret", "private-key-secret", "private-call-secret", "private-status-secret", "private-finish-secret", "private-counter-secret", "private-field-secret", "private-request-secret", "private-header-secret"} {
		require.NotContains(t, string(encoded), value)
	}
	require.Contains(t, string(encoded), `"other":true`)
	require.NotContains(t, string(encoded), "private-path-secret")
	require.Contains(t, string(encoded), `"group_id":3`)
	require.Contains(t, string(encoded), `"invalid_numeric_fields":{"input_tokens":true,"output_tokens":true}`)
}

func usageDiagnosticContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder, *observer.ObservedLogs) {
	t.Helper()
	// These tests are serial, like the logger integration tests. Reset the existing
	// atomic sampler so each independent synthetic request can assert its receipt.
	openAIMissingUsageSampler.lastLog.Store(0)
	core, logs := observer.New(zap.WarnLevel)
	ctx := logger.IntoContext(context.Background(), zap.New(core).With(zap.String("request_id", "req-usage-diagnostic")))
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions?access_token=private-query-secret", strings.NewReader("private-prompt-secret")).WithContext(ctx)
	c.Request.Header.Set("Authorization", "Bearer private-key-secret")
	c.Set("api_key", &APIKey{ID: 223, UserID: 4, Key: "private-key-secret"})
	SetOpsUpstreamModel(c, "deepseek-test")
	return c, rec, logs
}

func usageDiagnosticResponse(body string, stream bool) *http.Response {
	contentType := "application/json"
	if stream {
		contentType = "text/event-stream"
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{contentType}, "X-Request-Id": []string{"upstream-usage-diagnostic"}, "Authorization": []string{"Bearer private-response-secret"}}, Body: io.NopCloser(strings.NewReader(body))}
}
