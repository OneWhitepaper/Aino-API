//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFundingMediaPlanImagesKeepsCountAndRejectsFinalExpansion(t *testing.T) {
	svc, key, _ := fundingPreparationFixture(TeamFundingSourceBalance)
	plan, err := svc.prepareFundingRequestPlan(t.Context(), key, "/v1/images/generations", "application/json", []byte(`{"model":"gpt-image-1","prompt":"tree","n":2,"size":"1024x1024"}`))
	require.NoError(t, err)
	require.Equal(t, FundingRequestImage, plan.Kind)
	require.Equal(t, 2, plan.Count)
	require.False(t, plan.NeedsModelBounds)
	request := httptest.NewRequest(http.MethodPost, "https://api.openai.com/v1/images/generations", bytes.NewReader(plan.Body))
	require.NoError(t, validateFundingRequestPlan(request, plan))
	request = httptest.NewRequest(http.MethodPost, "https://api.openai.com/v1/images/generations", bytes.NewBufferString(`{"model":"gpt-image-1","prompt":"tree","n":3}`))
	require.Error(t, validateFundingRequestPlan(request, plan))
}

func TestFundingMediaPlanMultipartImagesUsesFormCount(t *testing.T) {
	svc, key, _ := fundingPreparationFixture(TeamFundingSourceBalance)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gpt-image-1"))
	require.NoError(t, writer.WriteField("prompt", "tree"))
	require.NoError(t, writer.WriteField("n", "2"))
	part, err := writer.CreateFormFile("image", "input.png")
	require.NoError(t, err)
	_, err = part.Write([]byte("image bytes"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	plan, err := svc.prepareFundingRequestPlan(t.Context(), key, "/v1/images/edits", writer.FormDataContentType(), body.Bytes())
	require.NoError(t, err)
	require.Equal(t, 2, plan.Count)
	request := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(plan.Body))
	request.Header.Set("Content-Type", writer.FormDataContentType())
	require.NoError(t, validateFundingRequestPlan(request, plan))
}

func TestFundingMediaPlanTTSQuotesExactUnicodeCharacters(t *testing.T) {
	svc, key, _ := fundingPreparationFixture(TeamFundingSourceBalance)
	plan, err := svc.prepareFundingRequestPlan(t.Context(), key, "/v1/tts", "application/json", []byte(`{"input":"你好a"}`))
	require.NoError(t, err)
	require.InDelta(t, 0.000003, plan.AudioUnits, 1e-12)
	quote, err := svc.quoteFundingRequestPlan(t.Context(), key, plan)
	require.NoError(t, err)
	require.Equal(t, "0.00004500", quote)
	request := httptest.NewRequest(http.MethodPost, "/v1/tts", bytes.NewBufferString(`{"input":"你好ab"}`))
	require.Error(t, validateFundingRequestPlan(request, plan))
}

func TestFundingMediaPlanGeminiAndRemoteTextRequireRealModelBounds(t *testing.T) {
	svc, key, _ := fundingPreparationFixture(TeamFundingSourceBalance)
	for _, tt := range []struct{ path, body string }{
		{"/v1beta/models/gemini-3.8-flash:generateContent", `{"contents":[{"parts":[{"text":"hi"}]}],"generationConfig":{"maxOutputTokens":128,"candidateCount":2}}`},
		{"/v1/responses", `{"model":"gpt-4.1","previous_response_id":"resp_a","input":[{"type":"input_image","image_url":"https://example.com/a.png"}],"max_output_tokens":128}`},
	} {
		plan, err := svc.prepareFundingRequestPlan(t.Context(), key, tt.path, "application/json", []byte(tt.body))
		require.NoError(t, err)
		require.Equal(t, 128, plan.OutputTokens)
		if tt.path == "/v1/responses" {
			require.True(t, plan.NeedsModelBounds)
			require.True(t, plan.FullContext)
		}
		plan.ProviderBound = true
		request := httptest.NewRequest(http.MethodPost, tt.path, bytes.NewReader(plan.Body))
		require.NoError(t, validateFundingRequestPlan(request, plan))
	}
}

func TestFundingMediaPlanHostedSearchAndCacheKeepMeteredUnits(t *testing.T) {
	svc, key, _ := fundingPreparationFixture(TeamFundingSourceBalance)
	plan, err := svc.prepareFundingRequestPlan(t.Context(), key, "/v1/responses", "application/json", []byte(`{"model":"funding-oauth-model","input":"hi","max_output_tokens":64,"max_tool_calls":3,"tools":[{"type":"web_search"}]}`))
	require.NoError(t, err)
	require.Equal(t, 3, plan.SearchCalls)
	require.Equal(t, 3, plan.MaxToolCalls)
	require.True(t, plan.FullContext)
	cache, err := svc.prepareFundingRequestPlan(t.Context(), key, "/v1beta/cachedContents", "application/json", []byte(`{"model":"models/gemini-3.8-flash","ttl":"7200s","contents":[{"parts":[{"text":"hi"}]}]}`))
	require.NoError(t, err)
	require.Equal(t, int64(7200), cache.CacheTTLSeconds)
	require.Greater(t, cache.CacheTokenHours, 0.0)
}

func TestFundingMediaPlanVideoPreservesDurationAndPriceUnits(t *testing.T) {
	svc, key, _ := fundingPreparationFixture(TeamFundingSourceBalance)
	price := 0.2
	key.Group.VideoPrice720P = &price
	plan, err := svc.prepareFundingRequestPlan(t.Context(), key, "/v1/videos", "application/json", []byte(`{"model":"grok-imagine-video","prompt":"tree","duration":6,"resolution":"720p"}`))
	require.NoError(t, err)
	require.Equal(t, 6, plan.VideoDurationSeconds)
	quote, err := svc.quoteFundingRequestPlan(t.Context(), key, plan)
	require.NoError(t, err)
	require.Equal(t, "1.20000001", quote) // The float billing result is rounded upward for authorization.
	request := httptest.NewRequest(http.MethodPost, "/v1/videos/generations", bytes.NewBufferString(`{"model":"grok-imagine-video","prompt":"tree","duration":7,"resolution":"720p"}`))
	require.Error(t, validateFundingRequestPlan(request, plan))
}

func fundingTestWAV(seconds int) []byte {
	data := make([]byte, 44+seconds*8000*2)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], 8000)
	binary.LittleEndian.PutUint32(data[28:], 16000)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], uint32(len(data)-44))
	return data
}

func TestFundingMediaDurationProbeReadsAudioAndRejectsMalformed(t *testing.T) {
	seconds, err := probeFundingAudioDuration(context.Background(), fundingTestWAV(2))
	require.NoError(t, err)
	require.InDelta(t, 2.0, seconds, 0.001)
	_, err = probeFundingAudioDuration(context.Background(), []byte("this is not audio"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "audio")
}

func TestFundingMediaPlanSTTBindsProbedUploadAndExactDuration(t *testing.T) {
	svc, key, _ := fundingPreparationFixture(TeamFundingSourceBalance)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", "speech.wav")
	require.NoError(t, err)
	_, err = file.Write(fundingTestWAV(2))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	plan, err := svc.prepareFundingRequestPlan(t.Context(), key, "/v1/stt", writer.FormDataContentType(), body.Bytes())
	require.NoError(t, err)
	require.InDelta(t, 2.0/3600, plan.AudioUnits, 1e-12)
	request := httptest.NewRequest(http.MethodPost, "/v1/stt", bytes.NewReader(body.Bytes()))
	request.Header.Set("Content-Type", writer.FormDataContentType())
	require.NoError(t, validateFundingRequestPlan(request, plan))
	request = httptest.NewRequest(http.MethodPost, "/v1/stt", bytes.NewReader(fundingTestWAV(3)))
	request.Header.Set("Content-Type", "audio/wav")
	require.Error(t, validateFundingRequestPlan(request, plan))
}

func TestFundingMediaPlanRejectsHostedSearchCapRemoval(t *testing.T) {
	svc, key, _ := fundingPreparationFixture(TeamFundingSourceBalance)
	plan, err := svc.prepareFundingRequestPlan(t.Context(), key, "/v1/responses", "application/json", []byte(`{"model":"funding-oauth-model","input":"hi","max_output_tokens":64,"max_tool_calls":3,"tools":[{"type":"web_search"}]}`))
	require.NoError(t, err)
	plan.ProviderBound = true
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"funding-oauth-model","input":"hi","max_output_tokens":64,"tools":[{"type":"web_search"}]}`))
	require.Error(t, validateFundingRequestPlan(request, plan), "a model output-token limit does not enforce a hosted-search call limit")
}

func TestFundingMediaPlanRejectsInvalidImageCountBeforeAdmission(t *testing.T) {
	svc, key, _ := fundingPreparationFixture(TeamFundingSourceBalance)
	_, err := svc.prepareFundingRequestPlan(t.Context(), key, "/v1/images/generations", "application/json", []byte(`{"model":"gpt-image-1","prompt":"tree","n":1.5}`))
	require.Error(t, err)
}

func TestFundingMediaPlanNativeImagesNeedARealCountOrTokenPrice(t *testing.T) {
	svc, key, _ := fundingPreparationFixture(TeamFundingSourceBalance)
	plan, err := svc.prepareFundingRequestPlan(t.Context(), key, "/v1beta/models/gemini-3-pro-image:generateContent", "application/json", []byte(`{"contents":[{"parts":[{"text":"tree"}]}],"generationConfig":{"responseModalities":["IMAGE"],"imageConfig":{"numberOfImages":1},"maxOutputTokens":512}}`))
	require.NoError(t, err)
	_, err = svc.quoteFundingRequestPlan(t.Context(), key, plan)
	require.Error(t, err, "per-image native generation must not guess image count from output tokens")
	plan.ImageCountLimit = 2
	quote, err := svc.quoteFundingRequestPlan(t.Context(), key, plan)
	require.NoError(t, err)
	require.Equal(t, "0.53600000", quote)
}

func TestFundingMediaPlanRealtimeQuotesEnforcedSessionDuration(t *testing.T) {
	svc, key, _ := fundingPreparationFixture(TeamFundingSourceBalance)
	plan, err := svc.prepareFundingRequestPlan(t.Context(), key, "/v1/realtime", "application/json", []byte(`{"model":"grok-voice-latest","max_duration_seconds":120}`))
	require.NoError(t, err)
	require.False(t, plan.NeedsModelBounds)
	require.Equal(t, 2.0, plan.AudioUnits)
	quote, err := svc.quoteFundingRequestPlan(t.Context(), key, plan)
	require.NoError(t, err)
	require.Equal(t, "0.10000000", quote)
}

func TestFundingMediaPlanRejectsAmbiguousJSONAndUnpricedFinalModel(t *testing.T) {
	svc, key, _ := fundingPreparationFixture(TeamFundingSourceBalance)
	_, err := svc.prepareFundingRequestPlan(t.Context(), key, "/v1/responses", "application/json", []byte(`{"model":"funding-oauth-model","input":"hi","max_output_tokens":1,"max_output_tokens":999}`))
	require.Error(t, err, "provider JSON parsers must not choose a different duplicate cap")
	plan, err := svc.prepareFundingRequestPlan(t.Context(), key, "/v1/responses", "application/json", []byte(`{"model":"funding-oauth-model","input":"hi","max_output_tokens":64}`))
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"unpriced-expensive-model","input":"hi","max_output_tokens":64}`))
	require.Error(t, validateFundingRequestPlan(request, plan))
}

func TestFundingMediaPlanEmbeddingsHaveNoGeneratedOutput(t *testing.T) {
	svc, key, _ := fundingPreparationFixture(TeamFundingSourceBalance)
	plan, err := svc.prepareFundingRequestPlan(t.Context(), key, "/v1/embeddings", "application/json", []byte(`{"model":"funding-oauth-model","input":["hello","world"]}`))
	require.NoError(t, err)
	require.Zero(t, plan.OutputTokens)
	require.False(t, plan.NeedsModelBounds)
	quote, err := svc.quoteFundingRequestPlan(t.Context(), key, plan)
	require.NoError(t, err)
	require.Equal(t, "2.00000000", quote) // Includes the reachable fast-service multiplier.
}

func TestFundingMediaPlanBatchTotalsOutputsAndUsesBatchDiscount(t *testing.T) {
	svc, key, _ := fundingPreparationFixture(TeamFundingSourceBalance)
	price := 0.5
	key.Group.ImagePrice1K, key.Group.BatchImageDiscountMultiplier = &price, 0.5
	plan, err := svc.prepareFundingRequestPlan(t.Context(), key, "/v1/images/batches", "application/json", []byte(`{"model":"gemini-3-pro-image","items":[{"custom_id":"a","prompt":"tree","output_count":2},{"custom_id":"b","prompt":"hill"}]}`))
	require.NoError(t, err)
	require.Equal(t, FundingRequestBatchImage, plan.Kind)
	require.Equal(t, 3, plan.Count)
	quote, err := svc.quoteFundingRequestPlan(t.Context(), key, plan)
	require.NoError(t, err)
	require.Equal(t, "0.75000000", quote)
	accountRate := 3.0
	plan.BatchAccountMultiplier = &accountRate
	quote, err = svc.quoteFundingRequestPlan(t.Context(), key, plan)
	require.NoError(t, err)
	require.Equal(t, "2.25000000", quote)
	accountRate = 0
	quote, err = svc.quoteFundingRequestPlan(t.Context(), key, plan)
	require.NoError(t, err)
	require.Equal(t, "0.00000000", quote, "an explicit zero account multiplier is free")
}

func TestFundingMediaPlanSeedanceUsesCompletionTokenBilling(t *testing.T) {
	svc, key, _ := fundingPreparationFixture(TeamFundingSourceBalance)
	plan, err := svc.prepareFundingRequestPlan(t.Context(), key, "/api/v3/contents/generations/tasks", "application/json", []byte(`{"model":"seedance-model","content":[{"type":"text","text":"tree"}],"duration":6,"resolution":"720p"}`))
	require.NoError(t, err)
	require.Equal(t, FundingRequestText, plan.Kind)
	require.True(t, plan.NeedsModelBounds)
	require.Equal(t, 6, plan.VideoDurationSeconds)
	require.Equal(t, "720p", plan.VideoResolution)
}

func TestFundingMediaPlanCachedContentEnrichmentBindsOwnedTokens(t *testing.T) {
	plan := &FundingRequestPlan{Kind: FundingRequestText, Model: "gemini-3.8-flash", InputTokens: 100, CachedContentName: "cachedContents/cache1"}
	ctx := WithFundingDispatchGuard(t.Context(), "request", nil, nil)
	ctx.Value(fundingDispatchKey{}).(*fundingDispatchGuard).plan = plan
	record := &GeminiCachedContent{PublicID: "cache1", Model: "gemini-3.8-flash", TotalTokenCount: 300}
	require.NoError(t, EnrichFundingCachedContentPlan(ctx, record))
	require.Equal(t, int64(300), plan.CachedContentTokens)
	require.GreaterOrEqual(t, plan.InputTokens, 300)
	require.True(t, plan.FullContext)
	record.PublicID = "other-cache"
	require.Error(t, EnrichFundingCachedContentPlan(ctx, record))
}

func TestFundingMediaPlanImageTokensMultiplyServiceAndReasoningRates(t *testing.T) {
	svc, key, _ := fundingPreparationFixture(TeamFundingSourceBalance)
	fast := 2.0
	svc.fundingBilling.fallbackPrices["claude-sonnet-4"] = &ModelPricing{InputPricePerToken: 1, OutputPricePerToken: 1, ImageOutputPricePerToken: 10, FastMultiplier: &fast, ReasoningEffortMultipliers: map[string]float64{"high": 3}}
	plan := &FundingRequestPlan{Kind: FundingRequestText, Model: "claude-sonnet-4", InputTokens: 1, OutputTokens: 1, Count: 1}
	quote, err := svc.quoteFundingRequestPlan(t.Context(), key, plan)
	require.NoError(t, err)
	amount, err := strconv.ParseFloat(quote, 64)
	require.NoError(t, err)
	require.GreaterOrEqual(t, amount, 66.0, "one input token plus one image-output token, multiplied by fast 2 and reasoning 3, costs at least 66")
}

func TestFundingMediaPlanFixedImageOAuthDriverBoundsToolCount(t *testing.T) {
	body, err := buildOpenAIImagesResponsesRequest(&OpenAIImagesRequest{Endpoint: openAIImagesGenerationsEndpoint, Model: "gpt-image-2", Prompt: "tree", N: 2}, "gpt-image-2")
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(body, &fields))
	require.Equal(t, float64(1), fields["max_tool_calls"], "a driver must not invoke the image tool repeatedly")
	plan := &FundingRequestPlan{Kind: FundingRequestImage, Model: "gpt-image-2", Count: 2, ImageEndpoint: true, BodyBytes: 65536}
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	require.NoError(t, validateFundingRequestPlan(request, plan), "fixed per-image billing needs image/tool limits rather than driver token metadata")
	fields["max_tool_calls"] = float64(2)
	expanded, err := json.Marshal(fields)
	require.NoError(t, err)
	request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(expanded))
	require.Error(t, validateFundingRequestPlan(request, plan))
}

func TestFundingMediaPlanHostedImagesReserveEveryAuthorizedImage(t *testing.T) {
	svc, key, _ := fundingPreparationFixture(TeamFundingSourceBalance)
	plan, err := svc.prepareFundingRequestPlan(t.Context(), key, "/v1/responses", "application/json", []byte(`{"model":"funding-oauth-model","input":"tree","max_output_tokens":64,"max_tool_calls":3,"tools":[{"type":"image_generation","n":2}]}`))
	require.NoError(t, err)
	require.Equal(t, 6, plan.ImageCountLimit)
	quote, err := svc.quoteFundingRequestPlan(t.Context(), key, plan)
	require.NoError(t, err)
	amount, err := strconv.ParseFloat(quote, 64)
	require.NoError(t, err)
	require.GreaterOrEqual(t, amount, 6.0, "three tool calls producing two images each are billed six times")
	plan.ProviderBound = true
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"funding-oauth-model","input":"tree","max_output_tokens":64,"max_tool_calls":3,"tools":[{"type":"image_generation","n":3}]}`))
	require.Error(t, validateFundingRequestPlan(request, plan))
}

func TestFundingMediaPlanCachedContentAllowsAuthorizedChannelAlias(t *testing.T) {
	plan := &FundingRequestPlan{Kind: FundingRequestText, Model: "public-model", BillingModels: []string{"gemini-3.8-flash"}, CachedContentName: "cachedContents/cache1"}
	ctx := WithFundingDispatchGuard(t.Context(), "request", nil, nil)
	ctx.Value(fundingDispatchKey{}).(*fundingDispatchGuard).plan = plan
	require.NoError(t, EnrichFundingCachedContentPlan(ctx, &GeminiCachedContent{PublicID: "cache1", Model: "gemini-3.8-flash", TotalTokenCount: 300}))
}

func TestFundingMediaPlanCacheAliasKeepsTokenAndStorageFallbackPrices(t *testing.T) {
	svc, key, _ := fundingPreparationFixture(TeamFundingSourceBalance)
	plan := &FundingRequestPlan{Kind: FundingRequestCache, Model: "funding-oauth-model", BillingModels: []string{"gemini-3.8-flash"}, InputTokens: 1, Count: 1, CacheTokenHours: 2}
	quote, err := svc.quoteFundingRequestPlan(t.Context(), key, plan)
	require.NoError(t, err)
	require.Equal(t, "2.00000100", quote, "alias request price and concrete upstream storage price can apply together")
	plan.Kind = FundingRequestCacheStorage
	quote, err = svc.quoteFundingRequestPlan(t.Context(), key, plan)
	require.NoError(t, err)
	require.Equal(t, "0.00000100", quote, "storage-only mutation is not another model request")
}

func TestFundingMediaPlanTokenImageCandidateRestoresClearedTokenBounds(t *testing.T) {
	svc, key, _ := fundingPreparationFixture(TeamFundingSourceBalance)
	price := 1.0
	key.Group.ModelPricing = []ChannelModelPricing{{Models: []string{"token-image-model"}, BillingMode: BillingModeToken, InputPrice: &price, OutputPrice: &price}}
	// A preceding reachable provider needed only an image-count envelope.
	plan := &FundingRequestPlan{Kind: FundingRequestImage, Model: "public-image", ImageCountLimit: 2, ProviderBound: true}
	svc.classifyFundingImagePricing(t.Context(), key, plan, "token-image-model")
	require.True(t, plan.ImageTokenPricing)
	require.True(t, plan.NeedsModelBounds, "the next account must supply token limits even after a count-only candidate cleared this flag")
	require.True(t, plan.FullContext)
}
