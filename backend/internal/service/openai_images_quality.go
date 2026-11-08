package service

import (
	"strings"
	"unicode"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const imageQualityMappingCredentialKey = "image_quality_mapping"

// NormalizeImageQualityMappingCredentials validates administrator-owned, exact
// upstream-model mappings. An empty object explicitly removes all overrides.
func NormalizeImageQualityMappingCredentials(credentials map[string]any) error {
	value, exists := credentials[imageQualityMappingCredentialKey]
	if !exists {
		return nil
	}
	mapping, ok := value.(map[string]any)
	if value == nil {
		mapping, ok = map[string]any{}, true
	}
	invalid := func() error {
		return infraerrors.BadRequest("INVALID_IMAGE_QUALITY_MAPPING", "image_quality_mapping requires exact upstream model names and supported quality values")
	}
	if !ok || len(mapping) > 64 {
		return invalid()
	}
	for model, raw := range mapping {
		if model == "" || len(model) > 256 || strings.TrimSpace(model) != model || strings.ContainsAny(model, "*?\r\n") || strings.IndexFunc(model, unicode.IsControl) >= 0 {
			return invalid()
		}
		qualities, ok := raw.(map[string]any)
		if !ok || len(qualities) == 0 || len(qualities) > 6 {
			return invalid()
		}
		for requested, rawTarget := range qualities {
			target, ok := rawTarget.(string)
			if !ok || !validImageQuality(requested, false) || !validImageQuality(target, true) {
				return invalid()
			}
		}
	}
	credentials[imageQualityMappingCredentialKey] = mapping
	return nil
}

func validImageQuality(value string, upstream bool) bool {
	switch value {
	case "auto", "low", "medium", "high", "xhigh", "max":
		return true
	case "1k", "2k", "4k":
		return upstream
	default:
		return false
	}
}

func imageQualityForUpstream(account *Account, model, requested string) (string, bool, error) {
	if account.Platform != PlatformOpenAI || account.Type != AccountTypeAPIKey {
		return requested, false, nil
	}
	if _, exists := account.Credentials[imageQualityMappingCredentialKey]; !exists {
		return requested, false, nil
	}
	// Revalidate persisted/imported configuration before forwarding as well.
	credentials := map[string]any{imageQualityMappingCredentialKey: account.Credentials[imageQualityMappingCredentialKey]}
	if err := NormalizeImageQualityMappingCredentials(credentials); err != nil {
		return "", false, err
	}
	mapping, _ := credentials[imageQualityMappingCredentialKey].(map[string]any)
	qualities, _ := mapping[model].(map[string]any)
	lookup := strings.TrimSpace(requested)
	if lookup == "" {
		lookup = "auto"
	}
	target, ok := qualities[lookup].(string)
	return target, ok, nil
}
