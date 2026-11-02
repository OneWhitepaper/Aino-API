//go:build unit

package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Exercise the production scheduler, HTTP transport and usage recorder together.
// Only identity/storage and the external provider are isolated test fixtures.
func TestVideoHTTPWorkflowBillsSnapshotOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var creates, polls, downloads, authorizationErrors atomic.Int32
	media := append([]byte{0, 0, 0, 24, 102, 116, 121, 112, 109, 112, 52, 50}, bytes.Repeat([]byte("fixture"), 200)...)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-provider-key" {
			authorizationErrors.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/videos":
			creates.Add(1)
			body, _ := io.ReadAll(r.Body)
			if gjson.GetBytes(body, "model").String() != "omni-flash" || gjson.GetBytes(body, "seconds").Int() != 3 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(w, `{"id":"provider-job","status":"queued","model":"omni-flash"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/videos/provider-job":
			if polls.Add(1) == 1 {
				_, _ = io.WriteString(w, `{"id":"provider-job","status":"in_progress","progress":50}`)
				return
			}
			_, _ = io.WriteString(w, `{"id":"provider-job","status":"completed","model":"omni-flash","seconds":"3","url":"https://provider.invalid/private-video"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/videos/provider-job/content":
			downloads.Add(1)
			w.Header().Set("Content-Type", "video/mp4")
			http.ServeContent(w, r, "fixture.mp4", time.Unix(1, 0), bytes.NewReader(media))
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true // loopback fixture only
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.Scheduling.StickySessionWaitTimeout = time.Second
	accounts := openAIImagesFailoverAccountRepo{accounts: []service.Account{{
		ID: 1, Platform: service.PlatformVideo, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 5, GroupIDs: []int64{24},
		Credentials: map[string]any{"base_url": provider.URL, "api_key": "private-provider-key", "model_mapping": map[string]any{"public-video": "omni-flash"}},
	}}}
	slots := &grokMediaSlotsCache{accounts: map[string]int64{}, users: map[string]int64{}}
	concurrency := service.NewConcurrencyService(slots)
	bindings := &grokMediaSlotBindings{}
	logs := &videoLiveUsageLogs{}
	transport := &videoLiveUpstream{HTTPUpstream: repository.NewHTTPUpstream(cfg)}
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	gateway := service.NewOpenAIGatewayService(accounts, logs, nil, nil, nil, nil, bindings, cfg, nil, concurrency,
		service.NewBillingService(cfg, nil), nil, billing, transport, service.NewDeferredService(accounts, nil, time.Minute),
		nil, nil, nil, nil, nil, nil, nil)
	h := NewOpenAIGatewayHandler(gateway, concurrency, billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		groupID := int64(24)
		price, rate := 0.02, 2.0
		if c.Request.Method == http.MethodGet {
			price, rate = 99, 9 // later price changes must not alter settlement
		}
		keyID := int64(20)
		if c.GetHeader("Authorization") != "Bearer customer-key" {
			keyID = 21
		}
		c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: keyID, UserID: 10, GroupID: &groupID,
			Group: &service.Group{ID: groupID, Platform: service.PlatformVideo, AllowImageGeneration: true, RateMultiplier: rate,
				VideoModelPrices: map[string]map[string]float64{"public-video": {"720p": price}}}, User: &service.User{ID: 10}})
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 10, Concurrency: 5})
		c.Next()
	})
	router.POST("/v1/videos", h.VideoGeneration)
	router.GET("/v1/videos/:request_id", h.VideoStatus)
	router.GET("/v1/videos/:request_id/content", h.VideoContent)
	public := httptest.NewServer(router)
	defer public.Close()
	request := func(method, path, key, byteRange string) (int, http.Header, []byte) {
		t.Helper()
		var body io.Reader
		if method == http.MethodPost {
			body = strings.NewReader(`{"model":"public-video","prompt":"paper boat","seconds":3}`)
		}
		req, err := http.NewRequestWithContext(context.Background(), method, public.URL+path, body)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		if byteRange != "" {
			req.Header.Set("Range", byteRange)
		}
		resp, err := public.Client().Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		data, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp.StatusCode, resp.Header, data
	}
	code, _, body := request(http.MethodPost, "/v1/videos", "customer-key", "")
	require.Equal(t, http.StatusOK, code, string(body))
	require.Empty(t, logs.rows, "acceptance must not charge")
	id := gjson.GetBytes(body, "id").String()
	require.True(t, strings.HasPrefix(id, "video_"))
	require.Equal(t, "public-video", gjson.GetBytes(body, "model").String())
	path := "/v1/videos/" + id
	code, _, _ = request(http.MethodGet, path, "wrong-key", "")
	require.Equal(t, http.StatusNotFound, code)
	require.Zero(t, polls.Load(), "other keys must not query the provider")
	code, _, body = request(http.MethodGet, path, "customer-key", "")
	require.Equal(t, http.StatusOK, code, string(body))
	require.Equal(t, "in_progress", gjson.GetBytes(body, "status").String())
	require.Empty(t, logs.rows)
	code, _, body = request(http.MethodGet, path, "customer-key", "")
	require.Equal(t, http.StatusOK, code, string(body))
	require.Equal(t, "completed", gjson.GetBytes(body, "status").String())
	require.NotContains(t, string(body), "provider.invalid")
	contentURL := gjson.GetBytes(body, "url").String()
	require.Contains(t, contentURL, path+"/content")
	code, headers, content := request(http.MethodGet, path+"/content", "customer-key", "")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, media, content)
	require.Equal(t, "video/mp4", headers.Get("Content-Type"))
	code, headers, content = request(http.MethodGet, path+"/content", "customer-key", "bytes=0-15")
	require.Equal(t, http.StatusPartialContent, code)
	require.Equal(t, media[:16], content)
	require.Contains(t, headers.Get("Content-Range"), "bytes 0-15/")
	_, _, _ = request(http.MethodGet, path, "customer-key", "")
	require.Len(t, logs.rows, 1, "polls and downloads must settle once")
	row := logs.rows[0]
	require.Equal(t, "public-video", row.Model)
	require.Equal(t, "omni-flash", *row.UpstreamModel)
	require.Equal(t, string(service.BillingModeVideo), *row.BillingMode)
	require.Equal(t, 3, *row.VideoDurationSeconds)
	require.InDelta(t, 0.06, row.TotalCost, 1e-12)
	require.InDelta(t, 0.12, row.ActualCost, 1e-12)
	require.Equal(t, int32(1), creates.Load())
	require.Equal(t, int32(2), downloads.Load())
	require.Zero(t, authorizationErrors.Load())
	for _, accountID := range transport.accountIDs {
		require.Equal(t, int64(1), accountID)
	}
	slots.assertReleased(t)
}
