//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVideoBillingSnapshotPreservesAcceptedPriceAndPublicModel(t *testing.T) {
	for _, tc := range []struct {
		name          string
		price         float64
		currentPrices map[string]map[string]float64
		tokenChannel  bool
	}{
		{name: "changed price", price: 0.12, currentPrices: map[string]map[string]float64{"public-video": {"720p": 99}}},
		{name: "removed price", price: 0.12},
		{name: "token channel cannot override accepted video price", price: 0.12, tokenChannel: true},
		{name: "explicit free price", price: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			users := &openAIRecordUsageUserRepoStub{}
			svc := newOpenAIRecordUsageServiceForTest(logs, users, &openAIRecordUsageSubRepoStub{}, nil)
			groupID := int64(139)
			if tc.tokenChannel {
				svc.resolver = newOpenAITokenImageChannelPricingResolverForTest(t, groupID, "public-video")
			}
			err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
				Result: &OpenAIForwardResult{
					RequestID: "video-completed-job", ResponseID: "video_test", Model: "public-video", BillingModel: "public-video", UpstreamModel: "veo-3.1-fast",
					VideoCount: 1, VideoResolution: "720p", VideoDurationSeconds: 6,
					VideoBillingSnapshot: &VideoBillingSnapshot{PricePerSecond: tc.price, RateMultiplier: 0.5},
				},
				APIKey: &APIKey{ID: 20, GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformVideo, RateMultiplier: 5, VideoRateIndependent: true, VideoRateMultiplier: 8, VideoModelPrices: tc.currentPrices}},
				User:   &User{ID: 10}, Account: &Account{ID: 30, Platform: PlatformVideo},
			})
			require.NoError(t, err)
			require.NotNil(t, logs.lastLog)
			require.Equal(t, "public-video", logs.lastLog.Model)
			require.Equal(t, "public-video", logs.lastLog.RequestedModel)
			require.Equal(t, "veo-3.1-fast", *logs.lastLog.UpstreamModel)
			require.Equal(t, string(BillingModeVideo), *logs.lastLog.BillingMode)
			require.Equal(t, 0.5, logs.lastLog.RateMultiplier)
			require.InDelta(t, tc.price*6, logs.lastLog.TotalCost, 1e-12)
			require.InDelta(t, tc.price*6*0.5, logs.lastLog.ActualCost, 1e-12)
			require.InDelta(t, tc.price*6*0.5, users.lastAmount, 1e-12)
		})
	}
}

func TestVideoCompletionWithoutBillingSnapshotFailsClosed(t *testing.T) {
	logs := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newOpenAIRecordUsageServiceForTest(logs, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result:  &OpenAIForwardResult{VideoCount: 1},
		Account: &Account{ID: 30, Platform: PlatformVideo},
	})
	require.ErrorContains(t, err, "snapshot is required")
	require.Zero(t, logs.calls)
}
