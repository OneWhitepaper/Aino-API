package service

import (
	"context"
	"math"
	"strings"
)

func (s *APIKeyService) SetFundingAccounts(accounts AccountRepository) { s.fundingAccounts = accounts }

// Price source selection against every reachable real model before reserving.
// The selected account is rechecked immediately before network dispatch.
func (s *APIKeyService) prepareFundingReachableEnvelope(ctx context.Context, key *APIKey, plan *FundingRequestPlan) error {
	if plan == nil || plan.Kind == FundingRequestControl {
		return nil
	}
	mapped := plan.Model
	if s.fundingPricing != nil && s.fundingPricing.channelService != nil && key.GroupID != nil {
		m := s.fundingPricing.channelService.ResolveChannelMapping(ctx, *key.GroupID, plan.Model)
		if m.MappedModel != "" {
			mapped = m.MappedModel
			plan.BillingModels = append(plan.BillingModels, mapped)
		}
	}
	s.classifyFundingImagePricing(ctx, key, plan, mapped)
	needs := plan.NeedsModelBounds || plan.FullContext
	var accounts []Account
	var err error
	if s.fundingAccounts != nil {
		if key.GroupID != nil {
			accounts, err = s.fundingAccounts.ListSchedulableByGroupID(ctx, *key.GroupID)
		} else {
			accounts, err = s.fundingAccounts.ListSchedulableByPlatform(ctx, PlatformFromAPIKey(key))
		}
		if err != nil {
			return err
		}
	}
	bound := false
	for i := range accounts {
		account := &accounts[i]
		if !account.IsModelSupported(mapped) {
			continue
		}
		if plan.Kind == FundingRequestBatchImage {
			rate := account.BillingRateMultiplier()
			if plan.BatchAccountMultiplier == nil || rate > *plan.BatchAccountMultiplier {
				copyRate := rate
				plan.BatchAccountMultiplier = &copyRate
			}
		}
		upstream := strings.TrimSpace(account.GetMappedModel(mapped))
		if upstream != "" {
			plan.BillingModels = append(plan.BillingModels, upstream)
		}
		candidate := cloneFundingPlan(plan)
		candidate.Model = mapped
		candidate.ImageTokenPricing = false
		s.classifyFundingImagePricing(ctx, key, candidate, plan.Model)
		s.classifyFundingImagePricing(ctx, key, candidate, mapped)
		s.classifyFundingImagePricing(ctx, key, candidate, upstream)
		if err = applyFundingAccountEnvelope(ctx, account, candidate); err != nil {
			if !s.applyFundingExactPricingEnvelope(candidate, upstream) {
				return err
			}
		}
		plan.InputTokens = max(plan.InputTokens, candidate.InputTokens)
		plan.OutputTokens = max(plan.OutputTokens, candidate.OutputTokens)
		plan.ImageTokenPricing = plan.ImageTokenPricing || candidate.ImageTokenPricing
		plan.ImageCountLimit = max(plan.ImageCountLimit, candidate.ImageCountLimit)
		if candidate.Kind == FundingRequestImage {
			plan.Kind = candidate.Kind
			plan.NeedsModelBounds = candidate.NeedsModelBounds
			plan.FullContext = candidate.FullContext
		}
		plan.ProviderBound = plan.ProviderBound || candidate.ProviderBound
		plan.BillingModels = append(plan.BillingModels, candidate.BillingModels...)
		bound = true
	}
	if needs && !bound {
		// Exact pricing metadata is useful for API-key providers without a model
		// discovery endpoint. Family-name guesses and synthesized catalog defaults
		// are deliberately excluded.
		bound = s.applyFundingExactPricingEnvelope(plan, mapped)
		if !bound {
			return ErrFundingModelLimitsUnavailable.WithMetadata(map[string]string{"model": mapped})
		}
	}
	return nil
}

func (s *APIKeyService) applyFundingExactPricingEnvelope(plan *FundingRequestPlan, model string) bool {
	if s.fundingBilling == nil || s.fundingBilling.pricingService == nil {
		return false
	}
	p := s.fundingBilling.pricingService.GetIdentifiedModelPricing(model)
	passes, err := fundingProviderTokenPasses(plan)
	if err != nil {
		return false
	}
	inputOnly := plan.Kind == FundingRequestCache || plan.Kind == FundingRequestCacheStorage || strings.HasSuffix(plan.Endpoint, "/embeddings")
	if p == nil || p.MaxInputTokens <= 0 || p.MaxInputTokens > int64(math.MaxInt)/passes || !inputOnly && (p.MaxOutputTokens <= 0 || p.MaxOutputTokens > int64(math.MaxInt)/passes) {
		return false
	}
	plan.InputTokens = max(plan.InputTokens, int(p.MaxInputTokens*passes))
	if !inputOnly {
		plan.OutputTokens = max(plan.OutputTokens, int(p.MaxOutputTokens*passes))
	}
	plan.ProviderBound = true
	plan.BillingModels = append(plan.BillingModels, model)
	return true
}
