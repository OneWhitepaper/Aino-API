package service

import (
	"context"
	"encoding/json"
	"maps"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestDesktopImageQualityOptionsRoundTripAndRejectInvalid(t *testing.T) {
	for _, tc := range []struct {
		name    string
		options []string
		text    bool
		valid   bool
	}{
		{"valid image", []string{"auto", "low", "medium", "high"}, false, true},
		{"empty legacy", nil, false, true},
		{"unknown", []string{"ultra"}, false, false},
		{"duplicate", []string{"high", "high"}, false, false},
		{"text cannot declare", []string{"high"}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			repo := &desktopSettingsRepo{values: map[string]string{}}
			svc := NewSettingService(repo, &config.Config{})
			svc.SetDefaultSubscriptionGroupReader(&desktopGroupRepo{groups: []Group{{ID: 11, Status: StatusActive, Platform: PlatformOpenAI, AllowImageGeneration: true}}})
			baseline := desktopSeparateDefaultSettings()
			require.NoError(t, svc.UpdateSettings(ctx, &SystemSettings{Desktop: &baseline}))
			before := maps.Clone(repo.values)
			index := 1
			if tc.text {
				index = 0
			}
			baseline.Models[index].Capabilities.ImageQualityOptions = tc.options
			blob, err := json.Marshal(baseline)
			require.NoError(t, err)
			var changed DesktopSettings
			require.NoError(t, json.Unmarshal(blob, &changed))
			err = svc.UpdateSettings(ctx, &SystemSettings{Desktop: &changed})
			if !tc.valid {
				require.Error(t, err)
				require.Equal(t, before, repo.values)
				return
			}
			require.NoError(t, err)
			loaded, err := svc.GetDesktopSettings(ctx)
			require.NoError(t, err)
			if len(tc.options) > 0 {
				require.Equal(t, tc.options, loaded.Models[1].Capabilities.ImageQualityOptions)
			}

		})
	}
}
