//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestVideoCreateAndLookupStayOnSubmittingAccount(t *testing.T) {
	h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false, "video")
	upstream.call = func(req *http.Request, id int64) (*http.Response, error) {
		require.Equal(t, int64(1), id)
		require.Equal(t, "Bearer test-key", req.Header.Get("Authorization"))
		body := `{"id":"job-1","status":"in_progress","model":"veo-3.1","progress":12}`
		if req.Method == http.MethodPost {
			body = `{"id":"job-1","status":"queued","model":"veo-3.1"}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
	c, w := grokMediaSlotContext(context.Background(), true)
	key, _ := middleware.GetAPIKeyFromContext(c)
	key.Group.Platform = "video"
	price := 0.1
	key.Group.VideoPrice720P = &price
	c.Request.Body = io.NopCloser(strings.NewReader(`{"model":"veo-3.1","prompt":"waves","seconds":4}`))
	h.VideoGeneration(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	id := gjson.Get(w.Body.String(), "id").String()
	require.True(t, strings.HasPrefix(id, "video_"))
	require.NotEmpty(t, bindings.pending)
	for _, payload := range bindings.pending {
		require.InDelta(t, price, gjson.GetBytes(payload, "video_billing_snapshot.price_per_second").Float(), 1e-12)
		require.True(t, gjson.GetBytes(payload, "video_billing_snapshot.rate_multiplier").Exists())
	}
	for _, wrongOwner := range []string{"", "key", "user"} {
		lookup, out := grokMediaSlotContext(context.Background(), false)
		lookup.Params[0].Value = id
		lookup.Request.URL.Path = "/v1/videos/" + id
		lookupKey, _ := middleware.GetAPIKeyFromContext(lookup)
		lookupKey.Group.Platform = "video"
		if wrongOwner == "key" {
			lookupKey.ID = 21
		}
		if wrongOwner == "user" {
			lookup.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 11, Concurrency: 5})
		}
		before := upstream.calls
		h.VideoStatus(lookup)
		if wrongOwner == "" {
			require.Equal(t, 200, out.Code, out.Body.String())
			require.Equal(t, id, gjson.Get(out.Body.String(), "id").String())
		} else {
			require.Equal(t, 404, out.Code)
			require.Equal(t, before, upstream.calls)
		}
	}
	require.Empty(t, slots.accounts)
	require.Empty(t, slots.users)
}

func TestVideoCreateRequiresExplicitPriceBeforeCallingUpstream(t *testing.T) {
	h, _, _, upstream := newGrokMediaSlotHandler(t, false, false, "video")
	c, w := grokMediaSlotContext(context.Background(), true)
	key, _ := middleware.GetAPIKeyFromContext(c)
	key.Group.Platform = "video"
	c.Request.Body = io.NopCloser(strings.NewReader(`{"model":"veo-3.1","prompt":"waves"}`))
	h.VideoGeneration(c)
	require.Equal(t, 400, w.Code, w.Body.String())
	require.Zero(t, upstream.calls)
}

func TestVideoCompletionUsesSnapshotAndClaimsOnlyOnce(t *testing.T) {
	h, _, _, _ := newGrokMediaSlotHandler(t, false, false, "video")
	key := &service.APIKey{ID: 20}
	owner := middleware.AuthSubject{UserID: 10}
	id := "video_test"
	require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(context.Background(), id, 10, 20, service.GrokVideoPendingBilling{Model: "public-video", BillingModel: "public-video", UpstreamModel: "veo-3.1-fast", VideoResolution: "720p", VideoDurationSeconds: 8, OriginalModel: "public-video", VideoBillingSnapshot: &service.VideoBillingSnapshot{PricePerSecond: 0.12, RateMultiplier: 0.5}, CreatedAt: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)}))
	status := &service.OpenAIForwardResult{ResponseID: id, VideoCount: 1, UpstreamModel: "veo-3.1-fast", VideoDurationSeconds: 6, VideoResolution: "1080p"}
	charged := prepareVideoCompletionBilling(context.Background(), h, key, owner, id, status)
	require.NotNil(t, charged)
	require.Equal(t, "public-video", charged.BillingModel)
	require.Equal(t, "720p", charged.VideoResolution)
	require.Equal(t, 0.12, charged.VideoBillingSnapshot.PricePerSecond)
	require.Equal(t, 0.5, charged.VideoBillingSnapshot.RateMultiplier)
	require.Equal(t, 6, charged.VideoDurationSeconds)
	require.Greater(t, charged.Duration, 50*time.Second)
	require.Nil(t, prepareVideoCompletionBilling(context.Background(), h, key, owner, id, status))
	require.Nil(t, prepareVideoCompletionBilling(context.Background(), h, key, owner, "missing", status))
	require.Nil(t, prepareVideoCompletionBilling(context.Background(), h, key, owner, id, &service.OpenAIForwardResult{}))
}
