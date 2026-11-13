package service

import (
	"context"
	"math"
	"net/http"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var ErrFundingModelLimitsUnavailable = infraerrors.New(http.StatusServiceUnavailable, "FUNDING_MODEL_LIMITS_UNAVAILABLE", "synchronize the selected account's exact model context and output limits before using this funding source")

func fundingProviderTokenPasses(plan *FundingRequestPlan) (int64, error) {
	if plan == nil || plan.MaxToolCalls < 0 || int64(plan.MaxToolCalls) == math.MaxInt64 {
		return 0, ErrFundingModelLimitsUnavailable
	}
	return 1 + int64(plan.MaxToolCalls), nil
}

// applyFundingAccountEnvelope uses the selected provider's declared limits. The
// client-facing Codex catalog contains synthesized defaults and is deliberately
// not a source of authorization limits.
func applyFundingAccountEnvelope(ctx context.Context, account *Account, plan *FundingRequestPlan) error {
	if plan == nil || account == nil {
		return ErrFundingModelLimitsUnavailable
	}
	if plan.Kind == FundingRequestAudio && plan.AudioMode == "realtime" && plan.MaxDurationSeconds > 0 {
		// Only the realtime adapter creates this plan, paired with a relay
		// deadline. Its billable unit is elapsed minutes, not model tokens.
		plan.ProviderBound = true
		return nil
	}
	model := strings.TrimSpace(plan.Model)
	if selected := strings.TrimSpace(FundingDispatchModel(ctx)); selected != "" && !plan.ImageEndpoint {
		model = selected
	} else {
		if resolved, ok := ResolvedUpstreamModelFromContext(ctx); ok && resolved != "" {
			model = resolved
		}
		model = strings.TrimSpace(account.GetMappedModel(model))
	}
	metadata, hasMetadata := account.GetUpstreamModelMetadata(model)
	imageOutput := isImageGenerationModel(model)
	for _, modality := range metadata.OutputModalities {
		imageOutput = imageOutput || strings.EqualFold(modality, "image")
	}
	if !plan.ImageEndpoint && plan.Kind == FundingRequestText && imageOutput {
		plan.Kind, plan.NeedsModelBounds, plan.FullContext = FundingRequestImage, true, true
	}
	passes, err := fundingProviderTokenPasses(plan)
	if err != nil {
		return err
	}
	if plan.Kind == FundingRequestImage && !plan.ImageTokenPricing && !plan.HostedImages {
		if plan.ImageEndpoint {
			return nil
		}
		if !hasMetadata || metadata.MaxOutputImages <= 0 || metadata.MaxOutputImages > int64(math.MaxInt/max(1, plan.Count))/passes {
			return ErrFundingModelLimitsUnavailable.WithMetadata(map[string]string{"model": model})
		}
		plan.ImageCountLimit = max(plan.ImageCountLimit, int(metadata.MaxOutputImages*passes)*max(1, plan.Count))
		plan.ProviderBound = true
		plan.BillingModels = appendFundingBillingModel(plan.BillingModels, model)
		return nil
	}
	inputOnly := plan.Kind == FundingRequestCache || plan.Kind == FundingRequestCacheStorage || strings.HasSuffix(plan.Endpoint, "/embeddings")
	tokenPlan := plan.Kind == FundingRequestText && !inputOnly
	needsOutput := !inputOnly && (plan.NeedsModelBounds || tokenPlan && account.UsesOpenAICodexProtocol())
	needsInput := plan.FullContext || plan.NeedsModelBounds
	if !needsInput && !needsOutput {
		return nil
	}
	if !hasMetadata {
		return ErrFundingModelLimitsUnavailable.WithMetadata(map[string]string{"model": model})
	}
	contextLimit := metadata.ContextWindow
	if metadata.MaxContextWindow > contextLimit {
		contextLimit = metadata.MaxContextWindow
	}
	// A hosted tool can resume the model with another full context. The
	// enforced tool-call count therefore bounds the number of model passes.
	if plan.Kind == FundingRequestImage && !plan.ImageEndpoint && metadata.MaxOutputImages > 0 {
		if metadata.MaxOutputImages > int64(math.MaxInt/max(1, plan.Count))/passes {
			return ErrFundingModelLimitsUnavailable.WithMetadata(map[string]string{"model": model})
		}
		plan.ImageCountLimit = max(plan.ImageCountLimit, int(metadata.MaxOutputImages*passes)*max(1, plan.Count))
	}
	if needsInput && (contextLimit <= 0 || contextLimit > int64(math.MaxInt)/passes) || needsOutput && (metadata.MaxOutputTokens <= 0 || metadata.MaxOutputTokens > int64(math.MaxInt)/passes) {
		return ErrFundingModelLimitsUnavailable.WithMetadata(map[string]string{"model": model})
	}
	if needsInput {
		plan.InputTokens = max(plan.InputTokens, int(contextLimit*passes))
	}
	if needsOutput {
		plan.OutputTokens = max(plan.OutputTokens, int(metadata.MaxOutputTokens*passes))
	}
	plan.ProviderBound = true
	plan.BillingModels = appendFundingBillingModel(plan.BillingModels, model)
	return nil
}

func appendFundingBillingModel(models []string, model string) []string {
	for _, existing := range models {
		if existing == model {
			return models
		}
	}
	return append(models, model)
}
