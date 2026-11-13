package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/shopspring/decimal"
)

func fundingRequestInvalid(message string) error {
	return infraerrors.New(http.StatusBadRequest, "FUNDING_REQUEST_INVALID", message)
}

func fundingPricingUnavailable(message string) *infraerrors.ApplicationError {
	return infraerrors.New(http.StatusServiceUnavailable, "FUNDING_PRICING_UNAVAILABLE", message)
}

func (s *APIKeyService) prepareFundingRequestPlan(ctx context.Context, key *APIKey, path, contentType string, body []byte) (*FundingRequestPlan, error) {
	path = strings.TrimRight(strings.SplitN(path, "?", 2)[0], "/")
	plan := &FundingRequestPlan{Body: body, ContentType: contentType, Endpoint: path, Count: 1, BodyBytes: 2*len(body) + 65536}
	if strings.Contains(path, "/custom-voices") || strings.HasSuffix(path, ":countTokens") || strings.HasSuffix(path, "/count_tokens") || strings.HasSuffix(path, "/input_tokens") {
		plan.Kind = FundingRequestControl
		return plan, nil
	}
	if len(body) == 0 && (strings.Contains(path, "/cachedContents") || strings.Contains(path, "/images/tasks/") || strings.Contains(path, "/images/batches") || strings.Contains(path, "/videos/")) {
		plan.Kind = FundingRequestControl
		return plan, nil
	}
	fields, uploads, err := fundingRequestFields(contentType, body)
	if err != nil {
		return nil, err
	}
	plan.Model, _ = fields["model"].(string)
	plan.Model = strings.TrimPrefix(strings.TrimSpace(plan.Model), "models/")
	if fields["type"] == "response.create" {
		// Earlier conversation.item.create events are server-side context even
		// when this response frame has no previous_response_id field.
		plan.FullContext, plan.NeedsModelBounds = true, true
	}
	if strings.Contains(path, "/models/") {
		modelAction := strings.SplitN(path, "/models/", 2)[1]
		plan.Model = strings.SplitN(modelAction, ":", 2)[0]
	}
	if strings.HasSuffix(path, "/alpha/search") {
		plan.Kind, plan.SearchCalls = FundingRequestAlphaSearch, 1
		return plan, nil
	}
	if strings.HasSuffix(path, "/web_search") || strings.HasSuffix(path, "/x_search") {
		plan.Kind, plan.SearchCalls = FundingRequestSearch, 1
		plan.Model = "grok-" + strings.ReplaceAll(path[strings.LastIndex(path, "/")+1:], "_", "-")
		return plan, nil
	}
	if strings.HasSuffix(path, "/tts") || strings.HasSuffix(path, "/audio/speech") {
		plan.Kind, plan.AudioMode = FundingRequestAudio, "tts"
		usage := estimateGrokVoiceAudioUsage("tts", body, contentType, nil, 0)
		if usage == nil {
			return nil, fundingRequestInvalid("speech input must contain text")
		}
		plan.AudioUnits = usage.DurationOrUnits
		if plan.Model == "" {
			plan.Model = "tts"
		}
		return plan, nil
	}
	if strings.HasSuffix(path, "/stt") || strings.HasSuffix(path, "/audio/transcriptions") || strings.HasSuffix(path, "/audio/translations") {
		media, err := fundingAudioUpload(contentType, body, fields, uploads)
		if err != nil {
			return nil, err
		}
		duration, err := probeFundingAudioDuration(ctx, media)
		if err != nil {
			return nil, err
		}
		plan.Kind, plan.AudioMode, plan.AudioUnits, plan.MaxDurationSeconds = FundingRequestAudio, "stt", duration/3600, duration
		sum := sha256.Sum256(media)
		plan.MediaSHA256 = hex.EncodeToString(sum[:])
		if plan.Model == "" {
			plan.Model = "stt"
		}
		return plan, nil
	}
	if strings.HasSuffix(path, "/realtime") {
		plan.Kind, plan.AudioMode = FundingRequestAudio, "realtime"
		if plan.Model == "" {
			plan.Model = "grok-voice-latest"
		}
		// The connection adapter sets its configured, enforced session deadline.
		seconds, err := fundingPlanOptionalCap(fields, "max_duration_seconds")
		if err != nil || seconds > 3600 {
			return nil, fundingRequestInvalid("realtime max_duration_seconds must be an integer between 1 and 3600")
		}
		plan.NeedsModelBounds = seconds == 0
		plan.MaxDurationSeconds, plan.AudioUnits = float64(seconds), float64(seconds)/60
		return plan, nil
	}
	if strings.Contains(path, "/contents/generations/tasks") {
		if !strings.HasSuffix(path, "/contents/generations/tasks") {
			plan.Kind = FundingRequestControl
			return plan, nil
		}
		info, err := ParseSeedanceRequest(body)
		if err != nil {
			return nil, fundingRequestInvalid(err.Error())
		}
		plan.Kind, plan.Model = FundingRequestText, info.Model
		plan.NeedsModelBounds, plan.FullContext = true, true
		plan.InputTokens = 2*len(body) + 65536
		if duration, exists := fields["duration"]; exists {
			seconds, valid := fundingPlanPositiveInt(duration)
			if !valid {
				return nil, fundingRequestInvalid("Seedance duration must be a positive integer")
			}
			plan.VideoDurationSeconds = seconds
		}
		plan.VideoResolution, _ = fields["resolution"].(string)
		// Seedance settlement bills completion_tokens; selected-model limits
		// cover native generation whose protocol has no client token cap.
		return plan, nil
	}
	if strings.HasSuffix(path, "/images/batches") {
		count, err := fundingBatchOutputCount(fields)
		if err != nil {
			return nil, err
		}
		if plan.Model == "" {
			return nil, fundingRequestInvalid("batch image model is required")
		}
		plan.Kind, plan.Count, plan.ImageEndpoint = FundingRequestBatchImage, count, true
		plan.ImageSize, _ = fields["image_size"].(string)
		if plan.ImageSize == "" {
			plan.ImageSize = defaultBatchImageImageSize
		}
		plan.ImageSize = NormalizeImageBillingTierOrDefault(plan.ImageSize)
		return plan, nil
	}
	if strings.Contains(path, "/images/") {
		parsed := &OpenAIImagesRequest{N: 1}
		mediaType, _, _ := mime.ParseMediaType(contentType)
		if mediaType == "multipart/form-data" {
			err = parseOpenAIImagesMultipartRequest(body, contentType, parsed)
		} else {
			err = parseOpenAIImagesJSONRequest(body, parsed)
		}
		if err != nil {
			return nil, fundingRequestInvalid(err.Error())
		}
		applyOpenAIImagesDefaults(parsed)
		plan.Kind, plan.Model, plan.Count = FundingRequestImage, parsed.Model, parsed.N
		plan.ImageEndpoint, plan.ImageSize = true, normalizeOpenAIImageSizeTier(parsed.Size)
		if n, ok := fields["n"]; ok {
			if value, valid := fundingPlanPositiveInt(n); !valid || value != parsed.N {
				return nil, fundingRequestInvalid("image n must be a positive integer")
			}
		}
		if s.fundingPricing != nil && key != nil {
			resolved := s.fundingPricing.Resolve(ctx, PricingInput{Model: plan.Model, Group: key.Group, GroupID: key.GroupID})
			plan.NeedsModelBounds = fundingImageTokenPricing(key, plan, resolved)
			plan.ImageTokenPricing = plan.NeedsModelBounds
			plan.FullContext = plan.NeedsModelBounds
		}
		return plan, nil
	}
	if strings.Contains(path, "/videos") {
		if !strings.HasSuffix(path, "/videos") && !strings.HasSuffix(path, "/generations") && !strings.HasSuffix(path, "/edits") && !strings.HasSuffix(path, "/extensions") {
			plan.Kind = FundingRequestControl // Payer is restored from the created task.
			return plan, nil
		}
		info := ParseGrokMediaRequest(contentType, body)
		if _, err := fundingPlanOptionalCap(fields, "duration", "seconds"); err != nil {
			return nil, err
		}
		if key != nil && key.Group != nil && key.Group.Platform == PlatformVideo {
			info, err = ParseVideoRequest(contentType, body)
			if err != nil {
				return nil, fundingRequestInvalid(err.Error())
			}
		}
		plan.Kind, plan.Model, plan.Count = FundingRequestVideo, info.Model, 1
		plan.VideoDurationSeconds, plan.VideoResolution = info.DurationSeconds, info.Resolution
		if plan.Model == "" || plan.VideoDurationSeconds <= 0 {
			return nil, fundingRequestInvalid("video model and a positive duration are required")
		}
		return plan, nil
	}
	if strings.Contains(path, "/cachedContents") {
		plan.Kind = FundingRequestCache
		if strings.HasSuffix(path, "/cachedContents") {
			parsed, err := ParseGeminiCachedContentCreateRequest(body, time.Now(), geminiCachedContentDefaultMaxTTL)
			if err != nil {
				return nil, fundingRequestInvalid(err.Error())
			}
			plan.Model, plan.CacheTTLSeconds = parsed.Model, parsed.TTLSeconds()
		} else {
			plan.Kind = FundingRequestCacheStorage
			plan.CachedContentName = "cachedContents/" + path[strings.LastIndex(path, "/")+1:]
			ttl, err := ParseGeminiCachedContentPatchRequest(body, "", time.Now(), geminiCachedContentDefaultMaxTTL)
			if err != nil {
				return nil, fundingRequestInvalid(err.Error())
			}
			plan.CacheTTLSeconds = int64(math.Ceil(ttl.Seconds()))
		}
		plan.InputTokens = 2*len(body) + 65536
		plan.CacheTokenHours = float64(plan.InputTokens) * float64(plan.CacheTTLSeconds) / 3600
		plan.NeedsModelBounds, plan.FullContext = true, true
		return plan, nil
	}
	plan.Kind = FundingRequestText
	gemini := strings.Contains(path, "/models/") && (strings.HasSuffix(path, ":generateContent") || strings.HasSuffix(path, ":streamGenerateContent"))
	if !gemini && !FundingTextEndpoint(path) && !strings.HasSuffix(path, "/embeddings") {
		return nil, fundingPricingUnavailable("no funding cost adapter is configured for this endpoint")
	}
	if !gemini && !plan.FullContext {
		if prepared, model, input, output, count, e := prepareFundingTextRequest(path, body); e == nil {
			plan.Body, plan.Model, plan.InputTokens, plan.OutputTokens, plan.Count = prepared, model, input, output, count
			return plan, nil
		}
	}
	if plan.Model == "" {
		return nil, fundingRequestInvalid("model is required")
	}
	plan.InputTokens = 2*len(body) + 65536
	if gemini {
		config := fundingObject(fields["generationConfig"])
		if len(config) == 0 {
			config = fundingObject(fields["generation_config"])
		}
		plan.OutputTokens, err = fundingPlanOptionalCap(config, "maxOutputTokens", "max_output_tokens")
		if err != nil {
			return nil, err
		}
		if count, e := fundingPlanOptionalCap(config, "candidateCount", "candidate_count"); e != nil {
			return nil, e
		} else if count > 0 {
			plan.Count = count
		}
		for _, raw := range []any{config["responseModalities"], config["response_modalities"]} {
			for _, modality := range fundingArray(raw) {
				if strings.EqualFold(fmt.Sprint(modality), "image") {
					plan.Kind, plan.NeedsModelBounds = FundingRequestImage, true
					plan.FullContext = true
				}
			}
		}
		imageConfig := fundingObject(config["imageConfig"])
		if len(imageConfig) == 0 {
			imageConfig = fundingObject(config["image_config"])
		}
		plan.ImageSize = NormalizeImageBillingTierOrDefault(fmt.Sprint(imageConfig["imageSize"]))
		// Native generateContent has no standard image-count request cap. An
		// arbitrary imageConfig.numberOfImages must not stand in for the
		// provider's declared MaxOutputImages limit.
		if isImageGenerationModel(plan.Model) {
			plan.Kind, plan.NeedsModelBounds, plan.FullContext = FundingRequestImage, true, true
		}
	} else {
		plan.OutputTokens, err = fundingPlanOptionalCap(fields, "max_tokens", "max_output_tokens", "max_completion_tokens")
		if err != nil {
			return nil, err
		}
		if count, e := fundingPlanOptionalCap(fields, "n", "best_of"); e != nil {
			return nil, e
		} else if count > 0 {
			plan.Count = count
		}
	}
	if ref, err := FindGeminiCachedContentReference(body); err != nil {
		return nil, fundingRequestInvalid(err.Error())
	} else if ref != nil {
		plan.CachedContentName, plan.FullContext = ref.Name, true
	}
	if fundingHasRemoteContent(fields) || fundingHasExpandedContext(fields) {
		plan.FullContext = true
	}
	if err := fundingPlanHostedTools(fields, plan); err != nil {
		return nil, err
	}
	plan.NeedsModelBounds = plan.NeedsModelBounds || plan.FullContext || plan.OutputTokens == 0 && !strings.HasSuffix(path, "/embeddings")
	s.classifyFundingImagePricing(ctx, key, plan, plan.Model)
	return plan, nil
}

func fundingRequestFields(contentType string, body []byte) (map[string]any, map[string][]byte, error) {
	fields, uploads := map[string]any{}, map[string][]byte{}
	mediaType, parameters, _ := mime.ParseMediaType(contentType)
	if mediaType == "multipart/form-data" {
		if parameters["boundary"] == "" {
			return nil, nil, fundingRequestInvalid("multipart boundary is required")
		}
		reader := multipart.NewReader(bytes.NewReader(body), parameters["boundary"])
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, nil, fundingRequestInvalid("invalid multipart request")
			}
			data, err := io.ReadAll(part)
			_ = part.Close()
			if err != nil {
				return nil, nil, err
			}
			name := part.FormName()
			if part.FileName() != "" {
				if _, exists := uploads[name]; !exists {
					uploads[name] = data
				}
				continue
			}
			if _, duplicate := fields[name]; duplicate {
				return nil, nil, fundingRequestInvalid("duplicate multipart field: " + name)
			}
			fields[name] = string(data)
		}
		return fields, uploads, nil
	}
	if len(body) == 0 || strings.HasPrefix(mediaType, "audio/") || mediaType == "application/octet-stream" {
		return fields, uploads, nil
	}
	if err := fundingRejectDuplicateJSON(body); err != nil {
		return nil, nil, err
	}
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, nil, fundingRequestInvalid("request body must be a JSON object or multipart form")
	}
	return fields, uploads, nil
}

// Authorization and the provider must observe the same fields even when their
// JSON libraries have different policies for repeated keys.
func fundingRejectDuplicateJSON(body []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	var walk func() error
	walk = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, container := token.(json.Delim)
		if !container {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				token, err := decoder.Token()
				if err != nil {
					return err
				}
				name, ok := token.(string)
				if !ok || seen[name] {
					return fundingRequestInvalid("duplicate or invalid JSON field in funding request")
				}
				seen[name] = true
				if err := walk(); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := walk(); err != nil {
					return err
				}
			}
		default:
			return fundingRequestInvalid("invalid JSON container in funding request")
		}
		_, err = decoder.Token()
		return err
	}
	if err := walk(); err != nil {
		return fundingRequestInvalid("invalid or ambiguous JSON in funding request")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fundingRequestInvalid("funding request must contain one JSON value")
	}
	return nil
}

func fundingAudioUpload(contentType string, body []byte, fields map[string]any, uploads map[string][]byte) ([]byte, error) {
	for _, name := range []string{"file", "audio", "audio_file"} {
		if data := uploads[name]; len(data) > 0 {
			return data, nil
		}
	}
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if strings.HasPrefix(mediaType, "audio/") || mediaType == "application/octet-stream" {
		return body, nil
	}
	for _, name := range []string{"audio", "file", "data", "input_audio"} {
		value := fields[name]
		if object := fundingObject(value); len(object) > 0 {
			value = object["data"]
		}
		if text, ok := value.(string); ok && text != "" {
			if strings.HasPrefix(text, "data:") {
				_, text, _ = strings.Cut(text, ",")
			}
			if data, e := base64.StdEncoding.DecodeString(text); e == nil && len(data) > 0 {
				return data, nil
			}
		}
	}
	return nil, fundingRequestInvalid("STT requires uploaded audio bytes with a verifiable duration")
}

func fundingBatchOutputCount(fields map[string]any) (int, error) {
	items := fundingArray(fields["items"])
	if len(items) == 0 {
		return 0, fundingRequestInvalid("batch image items must be a non-empty array")
	}
	count := 0
	for _, raw := range items {
		item := fundingObject(raw)
		if len(item) == 0 {
			return 0, fundingRequestInvalid("batch image item must be an object")
		}
		n := 1
		if output, exists := item["output_count"]; exists && output != float64(0) {
			var valid bool
			n, valid = fundingPlanPositiveInt(output)
			if !valid {
				return 0, fundingRequestInvalid("batch output_count must be a positive integer")
			}
		}
		if count > 10000000-n {
			return 0, fundingRequestInvalid("batch image output count exceeds the supported bound")
		}
		count += n
	}
	return count, nil
}

func fundingPlanPositiveInt(value any) (int, bool) {
	if text, ok := value.(string); ok {
		n, err := strconv.Atoi(text)
		return n, err == nil && n > 0 && n <= 10000000
	}
	return fundingPositiveInt(value)
}

func fundingPlanOptionalCap(fields map[string]any, names ...string) (int, error) {
	limit := 0
	for _, name := range names {
		if value, exists := fields[name]; exists {
			n, valid := fundingPlanPositiveInt(value)
			if !valid {
				return 0, fundingRequestInvalid(name + " must be a positive integer")
			}
			limit = max(limit, n)
		}
	}
	return limit, nil
}

func fundingObject(value any) map[string]any { object, _ := value.(map[string]any); return object }
func fundingArray(value any) []any           { array, _ := value.([]any); return array }

func fundingHasExpandedContext(value any) bool {
	switch object := value.(type) {
	case map[string]any:
		for key, child := range object {
			switch key {
			case "previous_response_id", "conversation", "cachedContent", "cached_content", "fileData", "file_data", "inlineData", "inline_data", "audio", "modalities", "context_management", "compaction", "encrypted_content":
				if child != nil && child != false {
					return true
				}
			}
			if fundingHasExpandedContext(child) {
				return true
			}
		}
	case []any:
		for _, child := range object {
			if fundingHasExpandedContext(child) {
				return true
			}
		}
	}
	return false
}

func fundingPlanHostedTools(fields map[string]any, plan *FundingRequestPlan) error {
	limit, err := fundingPlanOptionalCap(fields, "max_tool_calls")
	if err != nil {
		return err
	}
	plan.MaxToolCalls = limit
	for _, raw := range fundingArray(fields["tools"]) {
		tool := fundingObject(raw)
		kind, _ := tool["type"].(string)
		if kind == "function" || kind == "" && tool["name"] != nil && tool["input_schema"] != nil || tool["functionDeclarations"] != nil || tool["function_declarations"] != nil {
			continue
		}
		plan.FullContext, plan.NeedsModelBounds = true, true
		toolLimit, err := fundingPlanOptionalCap(tool, "max_uses")
		if err != nil {
			return err
		}
		if toolLimit == 0 {
			toolLimit = limit
		}
		if strings.Contains(kind, "search") || tool["googleSearch"] != nil || tool["google_search"] != nil {
			plan.HostedSearch = true
			plan.SearchCalls += toolLimit
		}
		if kind == "image_generation" {
			plan.HostedImages = true
			images, err := fundingPlanOptionalCap(tool, "n")
			if err != nil {
				return err
			}
			plan.ImageCountLimit += max(1, images) * toolLimit
		}
		plan.MaxToolCalls = max(plan.MaxToolCalls, toolLimit)
	}
	return nil
}

func fundingImageTokenPricing(key *APIKey, plan *FundingRequestPlan, resolved *ResolvedPricing) bool {
	return !imageCountBillingApplies(key, plan.ImageEndpoint) && resolved != nil && resolved.Mode == BillingModeToken &&
		(resolved.Source == PricingSourceGroup || resolved.Source == PricingSourceChannel)
}

// Resolve every reachable image billing model before deciding which provider
// bounds are necessary. A single token-priced candidate keeps the token bound.
func (s *APIKeyService) classifyFundingImagePricing(ctx context.Context, key *APIKey, plan *FundingRequestPlan, model string) {
	if plan == nil || key == nil || s.fundingPricing == nil || plan.Kind != FundingRequestImage && plan.Kind != FundingRequestText {
		return
	}
	resolved := s.fundingPricing.Resolve(ctx, PricingInput{Model: model, Group: key.Group, GroupID: key.GroupID})
	plan.ImageTokenPricing = plan.ImageTokenPricing || fundingImageTokenPricing(key, plan, resolved)
	if plan.Kind == FundingRequestImage && plan.ImageTokenPricing {
		// Earlier reachable providers may have cleared these after establishing
		// a per-image count bound. Token pricing needs the full envelope again.
		plan.NeedsModelBounds, plan.FullContext = true, true
	}
}

// EnrichFundingCachedContentPlan is called after the existing cache ownership
// lookup and before selected-account dispatch. The account envelope still covers
// the whole provider context; the durable token count prevents a cache reference
// from disappearing from that envelope when a protocol is rewritten.
func EnrichFundingCachedContentPlan(ctx context.Context, record *GeminiCachedContent) error {
	guard, _ := ctx.Value(fundingDispatchKey{}).(*fundingDispatchGuard)
	if guard == nil || guard.plan == nil {
		return nil
	}
	plan := guard.plan
	if record == nil || record.TotalTokenCount <= 0 || record.TotalTokenCount > math.MaxInt {
		return fundingRequestInvalid("owned cached content has invalid token metadata")
	}
	if plan.CachedContentName != "" && plan.CachedContentName != record.PublicName() {
		return fundingRequestInvalid("cached content differs from the resource authorized for this request")
	}
	if plan.Model != "" {
		model := strings.TrimPrefix(record.Model, "models/")
		allowed := strings.TrimPrefix(plan.Model, "models/") == model || strings.TrimPrefix(FundingDispatchModel(ctx), "models/") == model
		for _, candidate := range plan.BillingModels {
			allowed = allowed || strings.TrimPrefix(candidate, "models/") == model
		}
		if !allowed {
			return fundingRequestInvalid("cached content model differs from the authorized generation model")
		}
	}
	plan.CachedContentName, plan.CachedContentTokens = record.PublicName(), record.TotalTokenCount
	plan.InputTokens = max(plan.InputTokens, int(record.TotalTokenCount))
	plan.FullContext, plan.NeedsModelBounds, plan.ProviderBound = true, true, false
	return nil
}

func (s *APIKeyService) quoteFundingRequestPlan(ctx context.Context, key *APIKey, plan *FundingRequestPlan) (string, error) {
	if plan == nil || key == nil {
		return "", fundingRequestInvalid("funding request plan is missing")
	}
	if plan.Kind == FundingRequestControl {
		return "0.00000000", nil
	}
	if s.fundingBilling == nil || s.fundingPricing == nil {
		return "", fundingPricingUnavailable("funding pricing services are not configured")
	}
	if plan.HostedSearch && plan.SearchCalls <= 0 {
		return "", fundingPricingUnavailable("hosted search requires a provider-enforced max_tool_calls or max_uses limit")
	}
	if plan.HostedImages && (plan.MaxToolCalls <= 0 || plan.ImageCountLimit <= 0) {
		return "", fundingPricingUnavailable("hosted image generation requires an enforceable tool-call and image-count limit")
	}
	baseRate, imageRate, videoRate, err := s.fundingPlanRates(ctx, key)
	if err != nil {
		return "", err
	}
	if plan.Kind == FundingRequestAlphaSearch {
		return fundingPlanAmount(s.fundingBilling.CalculateWebSearchCost(max(1, plan.SearchCalls), webSearchPricePerCallFromAPIKey(key), baseRate).ActualCost)
	}
	models := []string{plan.Model}
	models = append(models, plan.BillingModels...)
	if key.GroupID != nil && s.fundingPricing.channelService != nil {
		mapping := s.fundingPricing.channelService.ResolveChannelMapping(ctx, *key.GroupID, plan.Model)
		if mapping.MappedModel != "" {
			models = append(models, mapping.MappedModel)
		}
	}
	storageCost := 0.0
	if plan.Kind == FundingRequestCache || plan.Kind == FundingRequestCacheStorage || plan.CacheTokenHours > 0 {
		storagePrice := 0.0
		for _, model := range models {
			storagePrice = math.Max(storagePrice, s.fundingBilling.CacheStoragePricePerTokenHour(strings.TrimPrefix(strings.TrimSpace(model), "models/")))
		}
		if storagePrice <= 0 {
			return "", fundingPricingUnavailable("cache storage price is not configured for the selected model or its concrete upstream")
		}
		hours := plan.CacheTokenHours
		if plan.CacheTTLSeconds > 0 {
			hours = math.Max(hours, float64(plan.InputTokens)*float64(plan.CacheTTLSeconds)/3600)
		}
		// Token billing may use a priced public alias while storage falls back
		// to the actual upstream. These independent maxima must be added.
		storageCost = hours * storagePrice * baseRate
	}
	best := 0.0
	seen := map[string]bool{}
	for _, model := range models {
		model = strings.TrimPrefix(strings.TrimSpace(model), "models/")
		if model == "" || seen[model] {
			continue
		}
		seen[model] = true
		resolved := s.fundingPricing.Resolve(ctx, PricingInput{Model: model, GroupID: key.GroupID, Group: key.Group})
		configured := resolved != nil && (resolved.Source == PricingSourceGroup || resolved.Source == PricingSourceChannel)
		factor := fundingPlanPricingFactor(resolved)
		perRequest := resolved != nil && (resolved.Mode == BillingModePerRequest || resolved.Mode == BillingModeImage || resolved.Mode == BillingModeVideo)
		cost := 0.0
		switch plan.Kind {
		case FundingRequestBatchImage:
			unit := -1.0
			if key.Group != nil {
				if configured := key.Group.GetImagePrice(plan.ImageSize); configured != nil {
					unit = *configured
				}
			}
			if unit < 0 {
				unit, err = (&BatchImageModelPricingResolver{Resolver: s.fundingPricing}).BatchImageUnitPrice(ctx, &BatchImageJob{Model: model})
				if err != nil {
					delete(seen, model)
					continue
				}
			}
			discount := defaultBatchImageDiscountMultiplier
			if key.Group != nil {
				discount = math.Max(0, key.Group.BatchImageDiscountMultiplier)
			}
			accountRate := 1.0
			if plan.BatchAccountMultiplier != nil {
				accountRate = *plan.BatchAccountMultiplier
			}
			if accountRate < 0 || math.IsNaN(accountRate) || math.IsInf(accountRate, 0) {
				return "", fundingPricingUnavailable("batch account multiplier must be finite and non-negative")
			}
			cost = unit * float64(plan.Count) * imageRate * discount * accountRate
		case FundingRequestImage:
			if fundingImageTokenPricing(key, plan, resolved) {
				cost, err = s.fundingPlanTokenQuote(ctx, key, plan, model)
				if err != nil {
					delete(seen, model)
					continue
				}
			} else {
				count := plan.Count
				if !plan.ImageEndpoint {
					if plan.ImageCountLimit <= 0 {
						return "", fundingPricingUnavailable("per-image pricing requires a documented provider image-count limit for native generation")
					}
					count = plan.ImageCountLimit
				}
				cost = s.fundingPlanPerImageQuote(key, model, count, imageRate, resolved)
			}
		case FundingRequestVideo:
			if plan.VideoDurationSeconds <= 0 {
				return "", fundingRequestInvalid("video duration must be positive")
			}
			if key.Group != nil && key.Group.Platform == PlatformVideo && key.Group.GetVideoPriceForModel(model, plan.VideoResolution) == nil {
				return "", fundingPricingUnavailable("video price is not configured for the selected model and resolution")
			}
			if configured && resolved.Mode == BillingModeToken {
				cost, err = s.fundingPlanTokenQuote(ctx, key, plan, model)
				if err != nil {
					delete(seen, model)
					continue
				}
			} else {
				cost = s.fundingBilling.CalculateVideoCost(model, plan.VideoResolution, max(1, plan.Count), plan.VideoDurationSeconds, videoPriceConfigFromAPIKey(key), videoRate).ActualCost
				if configured && perRequest {
					units := float64(max(1, plan.Count))
					if resolved.Mode == BillingModeVideo {
						units *= float64(plan.VideoDurationSeconds)
					}
					cost = math.Max(cost, maxPerRequestPrice(resolved)*units*videoRate*factor)
				}
			}
		case FundingRequestAudio:
			if plan.AudioUnits <= 0 || math.IsNaN(plan.AudioUnits) || math.IsInf(plan.AudioUnits, 0) {
				return "", fundingPricingUnavailable("audio duration or enforced session deadline is missing")
			}
			cost = s.fundingBilling.CalculateAudioCost(plan.AudioMode, plan.AudioUnits, groupAudioPriceConfigFromAPIKey(key), baseRate).ActualCost
			if configured && resolved.Mode == BillingModePerRequest {
				cost = math.Max(cost, maxPerRequestPrice(resolved)*plan.AudioUnits*baseRate*factor)
			}
		case FundingRequestSearch:
			if configured && perRequest {
				cost = maxPerRequestPrice(resolved) * float64(max(1, plan.Count)) * baseRate * factor
			}
			cost += s.fundingBilling.CalculateSearchCost(max(1, plan.SearchCalls), groupSearchPricePer1kFromAPIKey(key), baseRate).ActualCost
		case FundingRequestCacheStorage:
			// Extending storage is not an additional model request.
		case FundingRequestText, FundingRequestCache:
			cost, err = s.fundingPlanTokenQuote(ctx, key, plan, model)
			if err != nil {
				delete(seen, model)
				continue
			}
		default:
			return "", fundingPricingUnavailable("funding billing kind is not configured")
		}
		if plan.HostedImages && !fundingImageTokenPricing(key, plan, resolved) {
			// A mixed response can finish as text (token/request pricing), or
			// return images and take the existing per-image billing branch.
			cost = math.Max(cost, s.fundingPlanPerImageQuote(key, model, plan.ImageCountLimit, imageRate, resolved))
		}
		if plan.Kind != FundingRequestSearch && plan.SearchCalls > 0 {
			cost += s.fundingBilling.CalculateSearchCost(plan.SearchCalls, groupSearchPricePer1kFromAPIKey(key), baseRate).ActualCost
		}
		best = math.Max(best, cost)
	}
	if len(seen) == 0 {
		if err != nil {
			return "", err
		}
		return "", fundingPricingUnavailable("funding model is missing; resolve the owned resource before quoting")
	}
	return fundingPlanAmount(best + storageCost)
}

func (s *APIKeyService) fundingPlanPerImageQuote(key *APIKey, model string, count int, rate float64, resolved *ResolvedPricing) float64 {
	cost := 0.0
	for _, size := range []string{ImageBillingSize1K, ImageBillingSize2K, ImageBillingSize4K} {
		cost = math.Max(cost, s.fundingBilling.CalculateImageCost(model, size, count, imagePriceConfigFromAPIKey(key), rate).ActualCost)
	}
	if resolved != nil && (resolved.Source == PricingSourceGroup || resolved.Source == PricingSourceChannel) &&
		(resolved.Mode == BillingModePerRequest || resolved.Mode == BillingModeImage) {
		cost = math.Max(cost, maxPerRequestPrice(resolved)*float64(count)*rate*fundingPlanPricingFactor(resolved))
	}
	return cost
}

func fundingPlanAmount(cost float64) (string, error) {
	if cost < 0 || math.IsNaN(cost) || math.IsInf(cost, 0) {
		return "", fundingPricingUnavailable("funding price must be finite and non-negative")
	}
	return decimal.NewFromFloat(cost).RoundCeil(8).StringFixed(8), nil
}

func (s *APIKeyService) fundingPlanRates(ctx context.Context, key *APIKey) (float64, float64, float64, error) {
	rate := 1.0
	if s.cfg != nil {
		rate = s.cfg.Default.RateMultiplier
	}
	if key.Group != nil {
		rate = key.Group.RateMultiplier
	}
	rates, err := s.GetUserGroupRates(ctx, key.UserID)
	if err != nil {
		return 0, 0, 0, err
	}
	if key.GroupID != nil {
		if custom, found := rates[*key.GroupID]; found {
			rate = custom
		}
	}
	image, video := resolveImageRateMultiplier(key, rate), resolveVideoRateMultiplier(key, rate)
	// Gateway audio/search paths may include peak pricing. Taking the maximum
	// is safe for both that path and the OpenAI base-rate media path.
	if key.Group != nil && key.Group.PeakRateEnabled {
		rate *= math.Max(1, key.Group.PeakRateMultiplier)
	}
	return rate, image, video, nil
}

func fundingPlanPricingFactor(resolved *ResolvedPricing) float64 {
	factor := 1.0
	if resolved == nil {
		return factor
	}
	if cp := resolved.channelPricing; cp != nil {
		for _, multiplier := range cp.ReasoningEffortMultipliers {
			factor = math.Max(factor, multiplier)
		}
		if cp.TimePricing != nil {
			timeFactor := 1.0
			for _, period := range cp.TimePricing.Periods {
				timeFactor = math.Max(timeFactor, period.Multiplier)
			}
			factor *= timeFactor
		}
	}
	return factor
}

func (s *APIKeyService) fundingPlanTokenQuote(ctx context.Context, key *APIKey, plan *FundingRequestPlan, model string) (float64, error) {
	if plan.InputTokens <= 0 || plan.OutputTokens <= 0 && plan.Kind != FundingRequestCache && !strings.HasSuffix(plan.Endpoint, "/embeddings") {
		return 0, fundingPricingUnavailable("documented model input/output limits are required for this request")
	}
	text, err := s.strictFundingTextCostOne(ctx, key, model, plan.InputTokens, plan.OutputTokens, max(1, plan.Count))
	if err != nil {
		return 0, fundingPricingUnavailable("token pricing is not configured for model " + model).WithCause(err)
	}
	amount, _ := decimal.NewFromString(text)
	cost := amount.InexactFloat64()
	pricingGroup := key.Group
	if key.Group != nil {
		copy := *key.Group
		copy.LongContextPricingEnabled = true
		pricingGroup = &copy
	}
	resolved := s.fundingPricing.Resolve(ctx, PricingInput{Model: model, Group: pricingGroup, GroupID: key.GroupID})
	if resolved != nil && resolved.Mode == BillingModeToken {
		// Text pricing alone does not cover independently priced image tokens.
		// Add their maximum possible cost, including every context interval.
		variants := []*ModelPricing{resolved.BasePricing}
		for _, tier := range resolved.Intervals {
			variants = append(variants, s.fundingPricing.GetIntervalPricing(resolved, tier.MinTokens+1))
		}
		imageInput, imageOutput, tierFactor, reasoningFactor := 0.0, 0.0, 1.0, 1.0
		for _, pricing := range variants {
			if pricing == nil {
				continue
			}
			imageInput = math.Max(imageInput, math.Max(pricing.ImageInputPricePerToken, pricing.ImageCacheReadPricePerToken))
			imageOutput = math.Max(imageOutput, pricing.ImageOutputPricePerToken)
			for _, tier := range []string{"priority", "fast", "flex", OpenAIFastTierUltrafast} {
				tierFactor = math.Max(tierFactor, configuredServiceTierMultiplier(tier, pricing))
			}
			for _, multiplier := range pricing.ReasoningEffortMultipliers {
				reasoningFactor = math.Max(reasoningFactor, multiplier)
			}
		}
		rate, _, _, err := s.fundingPlanRates(ctx, key)
		if err != nil {
			return 0, err
		}
		cost += (float64(plan.InputTokens)*imageInput + float64(plan.OutputTokens)*imageOutput) * float64(max(1, plan.Count)) * rate * tierFactor * reasoningFactor * fundingPlanPricingFactor(resolved)
	}
	return cost, nil
}

func validateFundingRequestPlan(req *http.Request, plan *FundingRequestPlan) error {
	if req == nil || plan == nil || plan.Kind == FundingRequestControl {
		return nil
	}
	if plan.NeedsModelBounds && !plan.ProviderBound {
		return fundingPricingUnavailable("final funding request is missing documented provider model limits")
	}
	limit := plan.BodyBytes
	if limit <= 0 {
		limit = ConfiguredFundingBodyReadLimit
	}
	var source io.ReadCloser
	var err error
	if req.GetBody != nil {
		source, err = req.GetBody()
	} else {
		source = req.Body
	}
	if err != nil || source == nil {
		return fundingRequestInvalid("final funding request body is unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(source, int64(limit)+1))
	if req.GetBody != nil {
		_ = source.Close()
	} else {
		req.Body = io.NopCloser(bytes.NewReader(data))
	}
	if err != nil || len(data) > limit {
		return fundingRequestInvalid("final request exceeds its authorized input bound")
	}
	contentType := req.Header.Get("Content-Type")
	if contentType == "" {
		contentType = plan.ContentType
	}
	fields, uploads, err := fundingRequestFields(contentType, data)
	if err != nil {
		return err
	}
	// Code Assist wraps native Gemini requests in a request envelope.
	if wrapped := fundingObject(fields["request"]); len(wrapped) > 0 {
		fields = wrapped
	}
	if finalModel, ok := fields["model"].(string); ok && finalModel != "" {
		finalModel = strings.TrimPrefix(finalModel, "models/")
		allowed := finalModel == plan.Model
		for _, model := range plan.BillingModels {
			allowed = allowed || finalModel == strings.TrimPrefix(model, "models/")
		}
		// The dedicated image bridge bills its image model, rather than its
		// text driver. The driver is selected by gateway configuration.
		allowed = allowed || plan.Kind == FundingRequestImage && plan.ImageEndpoint && finalModel == openAIImagesResponsesMainModelValue()
		if !allowed {
			return fundingPricingUnavailable("final model was not included in the funding price envelope")
		}
	}
	count, err := fundingPlanOptionalCap(fields, "n", "best_of")
	if err != nil || count > max(1, plan.Count) {
		return fundingRequestInvalid("final request exceeds its authorized completion count")
	}
	switch plan.Kind {
	case FundingRequestBatchImage:
		count, err := fundingBatchOutputCount(fields)
		if err != nil || count > plan.Count {
			return fundingRequestInvalid("final batch image count exceeds its authorization")
		}
		return nil
	case FundingRequestImage:
		if plan.ImageEndpoint && strings.Contains(req.URL.Path, "/images/") {
			return nil // All image sizes were priced; count and body remain bounded.
		}
		if plan.ImageEndpoint {
			tools := fundingArray(fields["tools"])
			calls, err := fundingPlanOptionalCap(fields, "max_tool_calls")
			if err != nil || calls != 1 || len(tools) != 1 || fundingObject(tools[0])["type"] != "image_generation" {
				return fundingRequestInvalid("the Images API driver must execute exactly one bounded image tool call")
			}
			tool := fundingObject(tools[0])
			images, err := fundingPlanOptionalCap(tool, "n")
			if err != nil || max(1, images) > max(1, plan.Count) {
				return fundingRequestInvalid("final image tool count exceeds its authorization")
			}
			model, _ := tool["model"].(string)
			allowed := model == plan.Model
			for _, candidate := range plan.BillingModels {
				allowed = allowed || model == candidate
			}
			if !allowed {
				return fundingPricingUnavailable("final image tool model was not included in funding pricing")
			}
			return nil // This bridge's billed unit is images, not driver text tokens.
		}
	case FundingRequestVideo:
		info := ParseGrokMediaRequest(contentType, data)
		if seconds, exists := fields["seconds"]; exists {
			if n, valid := fundingPlanPositiveInt(seconds); valid {
				info.DurationSeconds = n
			} else {
				return fundingRequestInvalid("final video duration is invalid")
			}
		}
		if info.DurationSeconds > plan.VideoDurationSeconds || info.Resolution != NormalizeVideoBillingResolutionOrDefault(plan.VideoResolution) {
			return fundingRequestInvalid("final video duration or resolution exceeds its authorization")
		}
		return nil
	case FundingRequestAudio:
		switch plan.AudioMode {
		case "tts":
			usage := estimateGrokVoiceAudioUsage("tts", data, contentType, nil, 0)
			if usage == nil || usage.DurationOrUnits > plan.AudioUnits+1e-12 {
				return fundingRequestInvalid("final speech text exceeds its authorized character count")
			}
		case "stt":
			media, err := fundingAudioUpload(contentType, data, fields, uploads)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(media)
			if hex.EncodeToString(sum[:]) != plan.MediaSHA256 {
				return fundingRequestInvalid("final audio differs from the duration-validated upload")
			}
		case "realtime":
			if plan.MaxDurationSeconds <= 0 || !plan.ProviderBound {
				return fundingPricingUnavailable("realtime requires an enforced session duration")
			}
		}
		return nil
	case FundingRequestSearch, FundingRequestAlphaSearch:
		return nil // Dedicated adapters execute exactly one search operation.
	case FundingRequestCache, FundingRequestCacheStorage:
		ttl, err := parseGeminiCachedContentExpiration(fields, time.Now())
		if err != nil || ttl.Seconds() > float64(plan.CacheTTLSeconds)+1 {
			return fundingRequestInvalid("final cache lifetime exceeds its authorization")
		}
		return nil
	}
	config := fundingObject(fields["generationConfig"])
	if len(config) == 0 {
		config = fundingObject(fields["generation_config"])
	}
	cap, err := fundingPlanOptionalCap(fields, "max_tokens", "max_output_tokens", "max_completion_tokens")
	if err != nil {
		return err
	}
	if len(config) > 0 {
		cap, err = fundingPlanOptionalCap(config, "maxOutputTokens", "max_output_tokens")
		if err != nil {
			return err
		}
		candidates, err := fundingPlanOptionalCap(config, "candidateCount", "candidate_count")
		if err != nil || candidates > max(1, plan.Count) {
			return fundingRequestInvalid("final Gemini candidate count exceeds its authorization")
		}
	}
	if cap > plan.OutputTokens || cap == 0 && plan.OutputTokens > 0 && !plan.ProviderBound {
		return fundingPricingUnavailable("final output limit exceeds its authorization or was removed without a provider bound")
	}
	finalTools := &FundingRequestPlan{}
	if err := fundingPlanHostedTools(fields, finalTools); err != nil {
		return err
	}
	if finalTools.MaxToolCalls > plan.MaxToolCalls || finalTools.SearchCalls > plan.SearchCalls {
		return fundingRequestInvalid("final hosted-tool calls exceed their authorization")
	}
	if finalTools.HostedSearch && finalTools.SearchCalls == 0 {
		return fundingRequestInvalid("final hosted search has no enforceable call limit")
	}
	if finalTools.HostedImages && (!plan.HostedImages || finalTools.ImageCountLimit <= 0 || finalTools.ImageCountLimit > plan.ImageCountLimit) {
		return fundingRequestInvalid("final hosted image generation exceeds its authorized image count")
	}
	if finalTools.FullContext && !plan.FullContext {
		return fundingRequestInvalid("final hosted tools were not included in funding authorization")
	}
	if (fundingHasRemoteContent(fields) || fundingHasExpandedContext(fields)) && !plan.FullContext {
		return fundingRequestInvalid("final remote or server-side context was not included in funding authorization")
	}
	return nil
}
