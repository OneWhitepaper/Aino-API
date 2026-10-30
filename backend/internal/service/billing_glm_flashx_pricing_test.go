package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// Z.AI publishes FlashX separately from Flash. A missing remote catalog entry
// must not make the longer FlashX model name inherit the cheaper Flash rate.
// Source: https://docs.z.ai/guides/overview/pricing (checked 2026-10-30).
func TestGLM53FlashXFallbackAndPlazaReferencePricing(t *testing.T) {
	for _, catalog := range []*PricingService{nil, {pricingData: map[string]*LiteLLMModelPricing{}}} {
		billing := NewBillingService(&config.Config{}, catalog)
		plaza := NewModelPlazaService(nil, nil, catalog, billing, nil)
		for _, tc := range []struct {
			model  string
			input  float64
			output float64
			cache  float64
		}{
			{"GLM-5.3", 1.4e-6, 4.4e-6, 0.26e-6},
			{"GLM-5.3-Flash", 0.15e-6, 0.5e-6, 0.03e-6},
			{"GLM-5.3-FlashX", 0.37e-6, 1.25e-6, 0.075e-6},
			{"glm-5.3-flashx-legal", 0.37e-6, 1.25e-6, 0.075e-6},
			{"zhipu/GLM-5.3-FlashX", 0.37e-6, 1.25e-6, 0.075e-6},
		} {
			t.Run(tc.model, func(t *testing.T) {
				price, err := billing.GetModelPricing(tc.model)
				require.NoError(t, err)
				require.InDelta(t, tc.input, price.InputPricePerToken, 1e-15)
				require.InDelta(t, tc.output, price.OutputPricePerToken, 1e-15)
				require.InDelta(t, tc.cache, price.CacheReadPricePerToken, 1e-15)
				reference := plaza.lookupOfficialPricing(context.Background(), tc.model, make(map[string]*PlazaOfficialPricing))
				require.NotNil(t, reference)
				require.NotNil(t, reference.InputPrice)
				require.NotNil(t, reference.OutputPrice)
				require.NotNil(t, reference.CacheReadPrice)
				require.InDelta(t, tc.input, *reference.InputPrice, 1e-15)
				require.InDelta(t, tc.output, *reference.OutputPrice, 1e-15)
				require.InDelta(t, tc.cache, *reference.CacheReadPrice, 1e-15)
			})
		}
	}
}
