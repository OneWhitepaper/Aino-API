//go:build unit

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type videoLiveUsageLogs struct {
	service.UsageLogRepository
	rows []*service.UsageLog
}

func (s *videoLiveUsageLogs) Create(_ context.Context, row *service.UsageLog) (bool, error) {
	s.rows = append(s.rows, row)
	return true, nil
}

type videoLiveUpstream struct {
	service.HTTPUpstream
	accountIDs []int64
	creates    int
}

func (u *videoLiveUpstream) Do(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	u.accountIDs = append(u.accountIDs, id)
	if req.Method == http.MethodPost {
		u.creates++
	}
	return u.HTTPUpstream.Do(req, proxy, id, concurrency)
}

// Opt-in only: AINO_VIDEO_LIVE_CONFIG points to a private JSON file containing
// base_url and api_key. This creates ONE paid upstream task. A persistent guard
// in its directory prevents an accidental rerun from submitting a duplicate.
// AINO_VIDEO_LIVE_MODEL optionally selects a supported model; the test uses its
// shortest documented duration (3 seconds for Omni, 4 seconds for Veo).
// Scheduling, forwarding and settlement use production code; account/cache/log
// storage is isolated in memory and never changes a production user's balance.
func TestVideoProviderLiveScheduling(t *testing.T) {
	configPath := os.Getenv("AINO_VIDEO_LIVE_CONFIG")
	if configPath == "" {
		t.Skip("requires explicit credentials and authorization for one paid video")
	}
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	var credentials map[string]any
	require.NoError(t, json.Unmarshal(data, &credentials))
	require.NotEmpty(t, credentials["api_key"])
	require.NotEmpty(t, credentials["base_url"])
	model := strings.TrimSpace(os.Getenv("AINO_VIDEO_LIVE_MODEL"))
	if model == "" {
		model = "omni-flash"
	}
	require.Contains(t, service.DefaultVideoModelIDs(), model)
	seconds := 3
	if strings.HasPrefix(model, "veo-3.1") {
		seconds = 4
	}
	root := filepath.Dir(configPath)
	guard, err := os.OpenFile(filepath.Join(root, "creation-started"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	require.NoError(t, err, "a previous submission may already exist; do not automatically resubmit")
	require.NoError(t, guard.Close())

	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.Scheduling.StickySessionWaitTimeout = time.Second
	cfg.Gateway.Scheduling.StickySessionMaxWaiting = 3
	accountID, groupID := int64(1), int64(24)
	accounts := openAIImagesFailoverAccountRepo{accounts: []service.Account{{
		ID: accountID, Name: "video-live-verification", Platform: service.PlatformVideo, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 5, GroupIDs: []int64{groupID}, Credentials: credentials,
	}}}
	slots := &grokMediaSlotsCache{accounts: map[string]int64{}, users: map[string]int64{}}
	concurrency := service.NewConcurrencyService(slots)
	bindings := &grokMediaSlotBindings{}
	logs := &videoLiveUsageLogs{}
	upstream := &videoLiveUpstream{HTTPUpstream: repository.NewHTTPUpstream(cfg)}
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	gateway := service.NewOpenAIGatewayService(accounts, logs, nil, nil, nil, nil, bindings, cfg, nil, concurrency,
		service.NewBillingService(cfg, nil), nil, billing, upstream, service.NewDeferredService(accounts, nil, time.Minute),
		nil, nil, nil, nil, nil, nil, nil)
	h := NewOpenAIGatewayHandler(gateway, concurrency, billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	// A synthetic local price exercises settlement without altering live prices.
	price := 0.01
	configureKey := func(key *service.APIKey) {
		key.Group.Platform, key.Group.RateMultiplier = service.PlatformVideo, 1
		key.Group.VideoModelPrices = map[string]map[string]float64{model: {"720p": price}}
	}
	c, create := grokMediaSlotContext(ctx, true)
	key, _ := middleware.GetAPIKeyFromContext(c)
	configureKey(key)
	payload, err := json.Marshal(map[string]any{
		"model": model, "prompt": "A tiny paper boat drifting slowly on a calm sunlit lake, gentle natural motion, static camera, no text",
		"seconds": seconds, "resolution": "720p",
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "request.json"), payload, 0600))
	c.Request.Body = io.NopCloser(bytes.NewReader(payload))
	c.Request.URL.Path = "/v1/videos"
	t.Logf("Submitting one %d-second %s video through the video scheduler", seconds, model)
	h.VideoGeneration(c)
	createBody := strings.ReplaceAll(create.Body.String(), fmt.Sprint(credentials["api_key"]), "[REDACTED]")
	require.NoError(t, os.WriteFile(filepath.Join(root, "create-response.json"), []byte(createBody), 0600))
	require.True(t, create.Code >= 200 && create.Code < 300, "upstream HTTP %d: %s", create.Code, createBody)
	id := gjson.GetBytes(create.Body.Bytes(), "id").String()
	require.True(t, strings.HasPrefix(id, "video_"), create.Body.String())
	require.NoError(t, os.WriteFile(filepath.Join(root, "accepted.json"), create.Body.Bytes(), 0600))
	t.Logf("Accepted task %s", id)
	lookup := func(download, partial bool) (int, http.Header, []byte) {
		c, result := grokMediaSlotContext(ctx, false)
		key, _ := middleware.GetAPIKeyFromContext(c)
		configureKey(key)
		c.Params[0].Value = id
		c.Request.URL.Path = "/v1/videos/" + id
		if download {
			c.Request.URL.Path += "/content"
			if partial {
				c.Request.Header.Set("Range", "bytes=0-1023")
			}
			h.VideoContent(c)
		} else {
			h.VideoStatus(c)
		}
		return result.Code, result.Header(), result.Body.Bytes()
	}
	previous := ""
	for {
		code, _, body := lookup(false, false)
		require.Equal(t, http.StatusOK, code, string(body))
		require.NoError(t, os.WriteFile(filepath.Join(root, "status.json"), body, 0600))
		status := gjson.GetBytes(body, "status").String()
		progress := fmt.Sprintf("%s %s%%", status, gjson.GetBytes(body, "progress").String())
		if progress != previous {
			t.Log(progress)
			previous = progress
		}
		require.NotEqual(t, "failed", status, string(body))
		if status == "completed" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("video has not completed; preserve task ID %s without resubmitting", id)
		case <-time.After(8 * time.Second):
		}
	}
	code, headers, content := lookup(true, false)
	require.Equal(t, http.StatusOK, code, "content download failed")
	require.Contains(t, headers.Get("Content-Type"), "video/")
	require.Greater(t, len(content), 1024)
	require.True(t, bytes.Contains(content[:min(len(content), 64)], []byte("ftyp")), "MP4 header missing")
	artifact := filepath.Join(root, "video.mp4")
	require.NoError(t, os.WriteFile(artifact, content, 0600))
	code, headers, partial := lookup(true, true)
	require.Equal(t, http.StatusPartialContent, code)
	require.Len(t, partial, 1024)
	require.Contains(t, headers.Get("Content-Range"), "bytes 0-1023/")
	code, _, _ = lookup(false, false)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, logs.rows, 1, "repeated status/downloads must settle once")
	require.Equal(t, 1, logs.rows[0].VideoCount)
	require.Equal(t, seconds, *logs.rows[0].VideoDurationSeconds)
	require.InDelta(t, price*float64(seconds), logs.rows[0].ActualCost, 1e-12)
	require.Equal(t, 1, upstream.creates)
	for _, selectedID := range upstream.accountIDs {
		require.Equal(t, accountID, selectedID)
	}
	slots.assertReleased(t)
	t.Logf("Verified creation, owner-bound scheduling, completion, MP4 download, Range and single settlement. Artifact: %s (%d bytes)", artifact, len(content))
}
