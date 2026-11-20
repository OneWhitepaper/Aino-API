package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type desktopAdminGroupReader struct{ groups map[int64]*Group }

func (r desktopAdminGroupReader) GetByID(_ context.Context, id int64) (*Group, error) {
	if group := r.groups[id]; group != nil {
		return group, nil
	}
	return nil, ErrGroupNotFound
}

func desktopAdminFixture() (*SettingService, *desktopSettingsRepo, DesktopSettings, *Group) {
	repo := &desktopSettingsRepo{values: map[string]string{SettingKeySiteName: "OmniFlow", SettingKeyOpsMonitoringEnabled: "true"}}
	svc := NewSettingService(repo, &config.Config{})
	group := &Group{ID: 11, Platform: PlatformOpenAI, Status: StatusActive, AllowImageGeneration: true}
	svc.SetDefaultSubscriptionGroupReader(desktopAdminGroupReader{map[int64]*Group{11: group}})
	id, imageID := "chat", "image"
	settings := DesktopSettings{
		Enabled: true, CredentialTTLSeconds: 900, DefaultModelID: &id, DefaultImageModelID: &imageID,
		Models: []DesktopModelEntry{
			{ID: id, GroupID: 11, Model: "fixture-chat", DisplayName: "Chat", ProviderLabel: "Fixture", Platform: PlatformOpenAI, APIMode: "responses", AgentVerified: true, Capabilities: PlatformModelCapabilities{Tools: true}},
			{ID: imageID, GroupID: 11, Model: "fixture-image", DisplayName: "Image", ProviderLabel: "Fixture", Platform: PlatformOpenAI, APIMode: "images_generations", Capabilities: PlatformModelCapabilities{ImageGeneration: true, ImageQualityOptions: []string{"auto", "high"}}},
		},
	}
	return svc, repo, settings, group
}

func TestAdminDesktopSettingsSaveIsScopedAndRoundTrips(t *testing.T) {
	svc, repo, settings, _ := desktopAdminFixture()
	result, err := svc.UpdateDesktopSettings(context.Background(), settings)
	require.NoError(t, err)
	require.Equal(t, settings, *result)
	require.Equal(t, "OmniFlow", repo.values[SettingKeySiteName])
	require.Equal(t, "true", repo.values[SettingKeyOpsMonitoringEnabled])
	require.Len(t, repo.values, len(desktopSettingKeys)+2, "only desktop settings may be written")
	loaded, err := svc.GetDesktopSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, result, loaded)
}

func TestAdminDesktopSettingsRejectInvalidCatalogBeforeWriting(t *testing.T) {
	for name, mutate := range map[string]func(*DesktopSettings, *Group){
		"unverified default":    func(s *DesktopSettings, _ *Group) { s.Models[0].AgentVerified = false },
		"default without tools": func(s *DesktopSettings, _ *Group) { s.Models[0].Capabilities.Tools = false },
		"duplicate IDs":         func(s *DesktopSettings, _ *Group) { s.Models[1].ID = s.Models[0].ID },
		"missing group":         func(s *DesktopSettings, _ *Group) { s.Models[0].GroupID = 12 },
		"platform mismatch":     func(s *DesktopSettings, _ *Group) { s.Models[0].Platform = PlatformAnthropic },
		"inactive default":      func(_ *DesktopSettings, g *Group) { g.Status = "inactive" },
		"image permission":      func(_ *DesktopSettings, g *Group) { g.AllowImageGeneration = false },
		"invalid TTL":           func(s *DesktopSettings, _ *Group) { s.CredentialTTLSeconds = 299 },
		"invalid image quality": func(s *DesktopSettings, _ *Group) {
			s.Models[1].Capabilities.ImageQualityOptions = []string{"unsupported"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			svc, repo, settings, group := desktopAdminFixture()
			mutate(&settings, group)
			_, err := svc.UpdateDesktopSettings(context.Background(), settings)
			require.Error(t, err)
			require.Equal(t, map[string]string{SettingKeySiteName: "OmniFlow", SettingKeyOpsMonitoringEnabled: "true"}, repo.values)
		})
	}
}

type desktopAdminFailingRepo struct{ *desktopSettingsRepo }

func (r desktopAdminFailingRepo) SetMultiple(context.Context, map[string]string) error {
	return errors.New("database unavailable")
}

func TestAdminDesktopSettingsReportsStorageFailure(t *testing.T) {
	svc := NewSettingService(desktopAdminFailingRepo{&desktopSettingsRepo{values: map[string]string{}}}, &config.Config{})
	_, err := svc.UpdateDesktopSettings(context.Background(), DesktopSettings{CredentialTTLSeconds: 900})
	require.ErrorContains(t, err, "database unavailable")
}
