package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func fundingEnvelopeAccount(model string, metadata UpstreamModelMetadata) *Account {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Source: "upstream", Models: map[string]UpstreamModelMetadata{model: metadata}})
	return account
}

func TestFundingOAuthEnvelopeUsesDeclaredOutputAfterCapStripping(t *testing.T) {
	account := fundingEnvelopeAccount("provider-model", UpstreamModelMetadata{ContextWindow: 100000, MaxContextWindow: 200000, MaxOutputTokens: 30000})
	account.Credentials = map[string]any{"model_mapping": map[string]any{"alias": "provider-model"}}
	plan := &FundingRequestPlan{Kind: FundingRequestText, Model: "alias", InputTokens: 1234, OutputTokens: 16, Count: 1, FullContext: true}
	require.NoError(t, applyFundingAccountEnvelope(context.Background(), account, plan))
	require.Equal(t, 200000, plan.InputTokens)
	require.Equal(t, 30000, plan.OutputTokens)
	require.True(t, plan.ProviderBound)
	require.Contains(t, plan.BillingModels, "provider-model")
	prepared, err := prepareOpenAIWSFundingRequest(account, &OpenAIWSIngressHooks{PrepareRequest: func(int, []byte, string) ([]byte, error) {
		return []byte(`{"type":"response.create","model":"provider-model","max_output_tokens":16}`), nil
	}}, 1, nil, "alias")
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(prepared, "max_output_tokens").Exists())
}

func TestFundingEnvelopeRequiresExactMetadataInsteadOfCatalogDefaults(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	plan := &FundingRequestPlan{Kind: FundingRequestText, Model: "gpt-5.4", OutputTokens: 16}
	require.ErrorIs(t, applyFundingAccountEnvelope(context.Background(), account, plan), ErrFundingModelLimitsUnavailable)
	account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{"gpt-5.4-other": {ContextWindow: 100000, MaxOutputTokens: 20000}}})
	require.ErrorIs(t, applyFundingAccountEnvelope(context.Background(), account, plan), ErrFundingModelLimitsUnavailable)
}

func TestFundingHostedToolEnvelopeCoversEachProviderPassOnce(t *testing.T) {
	account := fundingEnvelopeAccount("m", UpstreamModelMetadata{ContextWindow: 100000, MaxOutputTokens: 20000})
	plan := &FundingRequestPlan{Kind: FundingRequestText, Model: "m", NeedsModelBounds: true, FullContext: true, MaxToolCalls: 2}
	require.NoError(t, applyFundingAccountEnvelope(context.Background(), account, plan))
	require.Equal(t, 300000, plan.InputTokens)
	require.Equal(t, 60000, plan.OutputTokens)
	require.NoError(t, applyFundingAccountEnvelope(context.Background(), account, plan))
	require.Equal(t, 300000, plan.InputTokens, "rechecking a selected account must not multiply the hold again")
	require.Equal(t, 60000, plan.OutputTokens)
}

func TestFundingEnvelopeUsesFinalMappedHintWithoutRemapping(t *testing.T) {
	account := fundingEnvelopeAccount("final", UpstreamModelMetadata{ContextWindow: 100000, MaxOutputTokens: 20000})
	account.Credentials = map[string]any{"model_mapping": map[string]any{"alias": "final", "final": "different"}}
	ctx := WithFundingDispatchGuard(context.Background(), "op", func(context.Context) error { return nil }, nil)
	SetFundingDispatchModel(ctx, "final")
	plan := &FundingRequestPlan{Kind: FundingRequestText, Model: "alias", OutputTokens: 16}
	require.NoError(t, applyFundingAccountEnvelope(ctx, account, plan))
	require.Contains(t, plan.BillingModels, "final")
}

func TestFundingMappedImageUsesOnlyExplicitProviderCount(t *testing.T) {
	account := fundingEnvelopeAccount("private-image", UpstreamModelMetadata{ContextWindow: 100000, MaxOutputTokens: 20000, OutputModalities: []string{"image"}, MaxOutputImages: 3})
	account.Credentials = map[string]any{"model_mapping": map[string]any{"alias": "private-image"}}
	plan := &FundingRequestPlan{Kind: FundingRequestText, Model: "alias", Count: 2}
	require.NoError(t, applyFundingAccountEnvelope(context.Background(), account, plan))
	require.Equal(t, FundingRequestImage, plan.Kind)
	require.Equal(t, 6, plan.ImageCountLimit)
}

func TestFundingNativeImageCountDoesNotRequireTokenMetadata(t *testing.T) {
	account := fundingEnvelopeAccount("image-model", UpstreamModelMetadata{OutputModalities: []string{"image"}, MaxOutputImages: 2})
	plan := &FundingRequestPlan{Kind: FundingRequestImage, Model: "image-model", Count: 1, NeedsModelBounds: true, FullContext: true, MaxToolCalls: 1}
	require.NoError(t, applyFundingAccountEnvelope(context.Background(), account, plan))
	require.Equal(t, 4, plan.ImageCountLimit)
	require.True(t, plan.ProviderBound)
	require.Zero(t, plan.OutputTokens)
}

func TestFundingFixedMediaDoesNotRequireTokenMetadata(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	for _, plan := range []*FundingRequestPlan{
		{Kind: FundingRequestAudio, AudioMode: "tts", AudioUnits: 1},
		{Kind: FundingRequestVideo, VideoDurationSeconds: 10},
		{Kind: FundingRequestImage, ImageEndpoint: true, Count: 1},
		{Kind: FundingRequestAudio, AudioMode: "realtime", AudioUnits: 1, MaxDurationSeconds: 60},
	} {
		require.NoError(t, applyFundingAccountEnvelope(context.Background(), account, plan))
	}
}

func TestFundingPricingPreservesExactProviderLimits(t *testing.T) {
	service := &PricingService{}
	prices, err := service.parsePricingData([]byte(`{"exact":{"input_cost_per_token":1e-6,"output_cost_per_token":2e-6,"max_input_tokens":100000,"max_output_tokens":20000}}`))
	require.NoError(t, err)
	require.Equal(t, int64(100000), prices["exact"].MaxInputTokens)
	require.Equal(t, int64(20000), prices["exact"].MaxOutputTokens)
}

func TestGrokRealtimeFundingDurationUsesGatewayLimit(t *testing.T) {
	service := &OpenAIGatewayService{cfg: &config.Config{}}
	require.Equal(t, time.Hour, service.GrokRealtimeMaxSessionDuration())
	service.cfg.Gateway.Live.MaxSessionDurationSeconds = 20
	require.Equal(t, 20*time.Second, service.GrokRealtimeMaxSessionDuration())
	service.cfg.Gateway.Live.MaxSessionDurationSeconds = 7200
	require.Equal(t, time.Hour, service.GrokRealtimeMaxSessionDuration())
}
