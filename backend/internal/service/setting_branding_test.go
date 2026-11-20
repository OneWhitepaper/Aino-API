//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOmniFlowBrandingDefaults(t *testing.T) {
	ctx := context.Background()
	svc := NewSettingService(&settingPublicRepoStub{values: map[string]string{}}, &config.Config{})

	public, err := svc.GetPublicSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, "OmniFlow API", public.SiteName)
	require.Equal(t, "/omniflow-logo.png", public.SiteLogo)
	require.Equal(t, "AI Model Gateway", public.SiteSubtitle)

	admin := svc.parseSettings(map[string]string{})
	require.Equal(t, public.SiteName, admin.SiteName)
	require.Equal(t, public.SiteLogo, admin.SiteLogo)
	require.Equal(t, public.SiteSubtitle, admin.SiteSubtitle)

	nameService := NewSettingService(&settingAntigravityUARepoStub{values: map[string]string{}}, &config.Config{})
	require.Equal(t, public.SiteName, nameService.GetSiteName(ctx))
}

func TestOmniFlowBrandingInitializesNewInstall(t *testing.T) {
	repo := &forwardedIPMigrationRepoStub{values: map[string]string{}}
	svc := NewSettingService(repo, &config.Config{})
	require.NoError(t, svc.InitializeDefaultSettings(context.Background()))
	require.Equal(t, "OmniFlow API", repo.values[SettingKeySiteName])
	require.Equal(t, "/omniflow-logo.png", repo.values[SettingKeySiteLogo])
	require.Equal(t, "AI Model Gateway", repo.values[SettingKeySiteSubtitle])
}

func TestOmniFlowBrandingPreservesConfiguredBrand(t *testing.T) {
	values := map[string]string{
		SettingKeySiteName:     "Custom Gateway",
		SettingKeySiteLogo:     "https://example.com/custom.png",
		SettingKeySiteSubtitle: "Custom subtitle",
	}
	svc := NewSettingService(&settingPublicRepoStub{values: values}, &config.Config{})
	public, err := svc.GetPublicSettings(context.Background())
	require.NoError(t, err)
	admin := svc.parseSettings(values)
	require.Equal(t, values[SettingKeySiteName], public.SiteName)
	require.Equal(t, values[SettingKeySiteLogo], public.SiteLogo)
	require.Equal(t, values[SettingKeySiteSubtitle], public.SiteSubtitle)
	require.Equal(t, public.SiteName, admin.SiteName)
	require.Equal(t, public.SiteLogo, admin.SiteLogo)
	require.Equal(t, public.SiteSubtitle, admin.SiteSubtitle)

	nameService := NewSettingService(&settingAntigravityUARepoStub{values: values}, &config.Config{})
	require.Equal(t, public.SiteName, nameService.GetSiteName(context.Background()))
}
