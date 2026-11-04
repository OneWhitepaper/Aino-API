package service

import (
	"context"
	"maps"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func desktopSeparateDefaultSettings() DesktopSettings {
	chatID, imageID := "chat", "image"
	return DesktopSettings{
		Enabled: true, DefaultModelID: &chatID, DefaultImageModelID: &imageID, CredentialTTLSeconds: 900,
		Models: []DesktopModelEntry{
			{ID: chatID, GroupID: 11, Model: "chat-wire", DisplayName: "Text", ProviderLabel: "Aino", Platform: PlatformOpenAI, APIMode: "chat_completions", AgentVerified: true, Capabilities: PlatformModelCapabilities{Tools: true}},
			{ID: imageID, GroupID: 11, Model: "image-wire", DisplayName: "Image", ProviderLabel: "Aino", Platform: PlatformOpenAI, APIMode: "images_generations", Capabilities: PlatformModelCapabilities{ImageGeneration: true}},
		},
	}
}

func TestDesktopSeparateDefaultsRejectInvalidUpdatesWithoutWriting(t *testing.T) {
	tests := []struct {
		name string
		edit func(*DesktopSettings)
		want string
	}{
		{"image as text default", func(s *DesktopSettings) { s.DefaultModelID = s.DefaultImageModelID }, "default model must have verified Agent"},
		{"text as image default", func(s *DesktopSettings) { s.DefaultImageModelID = s.DefaultModelID }, "default image model must use the Images API"},
		{"image capability removed", func(s *DesktopSettings) { s.Models[1].Capabilities.ImageGeneration = false }, "image models require image generation capability"},
		{"text tools removed", func(s *DesktopSettings) { s.Models[0].Capabilities.Tools = false }, "default model must have verified Agent"},
		{"text default deleted", func(s *DesktopSettings) { s.Models = s.Models[1:] }, "default model is not in the catalog"},
		{"image default deleted", func(s *DesktopSettings) { s.Models = s.Models[:1] }, "default image model is not in the catalog"},
		{"image changed to text with old default", func(s *DesktopSettings) {
			s.Models[1].APIMode = "responses"
			s.Models[1].AgentVerified = true
			s.Models[1].Capabilities = PlatformModelCapabilities{Tools: true}
		}, "default image model must use the Images API"},
		{"text changed to image with old default", func(s *DesktopSettings) {
			s.Models[0].APIMode = "images_generations"
			s.Models[0].Capabilities = PlatformModelCapabilities{ImageGeneration: true}
		}, "default model must have verified Agent"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repo := &desktopSettingsRepo{values: map[string]string{}}
			settings := NewSettingService(repo, &config.Config{})
			settings.SetDefaultSubscriptionGroupReader(&desktopGroupRepo{groups: []Group{{ID: 11, Status: StatusActive, Platform: PlatformOpenAI, AllowImageGeneration: true}}})
			baseline := desktopSeparateDefaultSettings()
			require.NoError(t, settings.UpdateSettings(ctx, &SystemSettings{Desktop: &baseline}))
			before := maps.Clone(repo.values)
			changed := desktopSeparateDefaultSettings()
			test.edit(&changed)
			err := settings.UpdateSettings(ctx, &SystemSettings{Desktop: &changed})
			require.ErrorContains(t, err, test.want)
			require.Equal(t, before, repo.values, "an invalid model edit must not overwrite either model catalog or default")
			loaded, err := settings.GetDesktopSettings(ctx)
			require.NoError(t, err)
			require.Equal(t, baseline, *loaded)
		})
	}
}
