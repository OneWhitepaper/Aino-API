//go:build unit

package service

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestVideoAccountGenerationReportsUpstreamFailureWithoutLeakingKey(t *testing.T) {
	account := videoTestAccount()
	body, err := json.Marshal(map[string]any{
		"error": map[string]string{
			"code":    "do_request_failed",
			"message": "upstream unavailable for " + account.GetCredential("api_key"),
		},
	})
	require.NoError(t, err)
	response := grokMediaContentStatusResponse(string(body))
	response.StatusCode = http.StatusInternalServerError
	response.Header.Set("X-Oneapi-Request-Id", "provider-request-123")
	upstream := &grokMediaContentUpstreamStub{response: response}
	svc := &AccountTestService{httpUpstream: upstream, cfg: &config.Config{}}
	c, output := grokMediaContentTestContext(http.MethodPost, "/api/v1/admin/accounts/91/test", nil)
	err = svc.testVideoAccountConnection(c, account, "public-video", "waves", "video", AccountTestOptions{})
	require.Error(t, err)
	require.Contains(t, output.Body.String(), "HTTP 500")
	require.Contains(t, output.Body.String(), "do_request_failed")
	require.Contains(t, output.Body.String(), "provider-request-123")
	require.NotContains(t, output.Body.String(), account.GetCredential("api_key"))
	require.Len(t, upstream.requests, 1, "failed creates must not be retried")
}

func TestVideoAccountGenerationReportsFailedTaskAndPreservesTaskID(t *testing.T) {
	upstream := &grokMediaContentUpstreamStub{responses: []*http.Response{
		grokMediaContentStatusResponse(`{"id":"job-failed","status":"queued"}`),
		grokMediaContentStatusResponse(`{"id":"job-failed","status":"failed","error":{"code":"upstream_unavailable","message":"Video provider is unavailable"}}`),
	}}
	svc := &AccountTestService{httpUpstream: upstream, cfg: &config.Config{}}
	c, output := grokMediaContentTestContext(http.MethodPost, "/api/v1/admin/accounts/91/test", nil)
	err := svc.testVideoAccountConnection(c, videoTestAccount(), "public-video", "waves", "video", AccountTestOptions{})
	require.Error(t, err)
	require.Contains(t, output.Body.String(), "job-failed")
	require.Contains(t, output.Body.String(), "upstream_unavailable")
	require.Contains(t, output.Body.String(), "Video provider is unavailable")
	require.NotContains(t, output.Body.String(), `"success":true`)
	require.Len(t, upstream.requests, 2, "failed tasks must not download or create again")
}
