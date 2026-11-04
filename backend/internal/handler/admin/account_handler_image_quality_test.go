package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type imageTestAccountRepo struct {
	service.AccountRepository
	account *service.Account
}

func (r *imageTestAccountRepo) GetByID(context.Context, int64) (*service.Account, error) {
	return r.account, nil
}

type imageTestHTTPUpstream struct {
	request *http.Request
}

func (u *imageTestHTTPUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.request = req
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"data":[{"b64_json":"aGVsbG8="}]}`)),
	}, nil
}

func (u *imageTestHTTPUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}

// Exercises the full admin JSON -> account/model routing -> Images API path.
// Dropping image_quality at any layer would recreate the upstream pricing error.
func TestAccountHandlerImageTestForwardsSelectedQuality(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, quality := range []string{"1k", "2k", "4k", "auto", "low", "medium", "high", " 1k ", "", "   "} {
		t.Run("quality="+quality, func(t *testing.T) {
			upstream := &imageTestHTTPUpstream{}
			repo := &imageTestAccountRepo{account: &service.Account{
				ID:       54,
				Platform: service.PlatformOpenAI,
				Type:     service.AccountTypeAPIKey,
				Credentials: map[string]any{
					"api_key":       "test-image-key",
					"base_url":      "https://image-upstream.example/v1",
					"model_mapping": map[string]any{"paint": "gpt-image-2.5-sunburst"},
				},
			}}
			testService := service.NewAccountTestService(repo, nil, nil, nil, nil, upstream, &config.Config{}, nil)
			handler := &AccountHandler{accountTestService: testService}
			router := gin.New()
			router.POST("/api/v1/admin/accounts/:id/test", handler.Test)
			payload, err := json.Marshal(map[string]any{
				"model_id":      "paint",
				"prompt":        "a small yellow flower",
				"image_quality": quality,
			})
			require.NoError(t, err)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/54/test", strings.NewReader(string(payload)))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code)
			require.Contains(t, rec.Body.String(), `"success":true`)
			require.NotNil(t, upstream.request)
			require.Equal(t, "https://image-upstream.example/v1/images/generations", upstream.request.URL.String())
			var sent map[string]any
			require.NoError(t, json.NewDecoder(upstream.request.Body).Decode(&sent))
			require.Equal(t, "gpt-image-2.5-sunburst", sent["model"])
			require.Equal(t, "a small yellow flower", sent["prompt"])
			if want := strings.TrimSpace(quality); want != "" {
				require.Equal(t, want, sent["quality"])
			} else {
				require.NotContains(t, sent, "quality", "unspecified quality must retain upstream defaults")
			}
		})
	}
}
