package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func desktopImageFixture(t *testing.T) (*DesktopModelService, *APIKeyService) {
	t.Helper()
	cfg := &config.Config{RunMode: config.RunModeSimple}
	users := &desktopUserRepo{user: &User{ID: 17, Status: StatusActive, Balance: 10, TokenVersionResolved: true}}
	price := 0.04
	groups := &desktopGroupRepo{groups: []Group{{ID: 11, Platform: PlatformOpenAI, Status: StatusActive, AllowImageGeneration: true, RateMultiplier: 2, ModelPricing: []ChannelModelPricing{{Models: []string{"fixture-image"}, BillingMode: BillingModeImage, PerRequestPrice: &price}}}}}
	settings := NewSettingService(&desktopSettingsRepo{values: map[string]string{"desktop.enabled": "true", "desktop.default_image_model_id": "image", "desktop.models": `[{"id":"image","group_id":11,"model":"fixture-image","display_name":"Image","provider_label":"Aino","platform":"openai","api_mode":"images_generations","capabilities":{"image_generation":true}}]`}}, cfg)
	keys := NewAPIKeyService(nil, users, groups, &desktopSubscriptionRepo{}, nil, nil, cfg)
	billing := NewBillingService(cfg, nil)
	eligibility := NewBillingCacheService(nil, users, &desktopSubscriptionRepo{}, nil, nil, nil, cfg, nil)
	t.Cleanup(eligibility.Stop)
	return NewDesktopModelService(settings, keys, billing, NewModelPricingResolver(nil, billing), eligibility, nil), keys
}

func TestDesktopImageCatalogUsesImagePermissionAndPricing(t *testing.T) {
	models, _ := desktopImageFixture(t)
	list, err := models.ListForUser(context.Background(), 17)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "available", list[0].State, "image models need image capability, not Agent tools")
	require.NotEqual(t, "per_million_tokens", list[0].Pricing.Unit)
	require.Nil(t, list[0].Pricing.Input)
	require.Empty(t, list[0].Pricing.Tiers)
	bootstrap, err := models.Bootstrap(context.Background(), 17)
	require.NoError(t, err)
	blob, err := json.Marshal(bootstrap)
	require.NoError(t, err)
	require.Contains(t, string(blob), `"default_image_model_id":"image"`)
	require.Nil(t, bootstrap.DefaultModelID)
	groupRepo, ok := models.keys.groupRepo.(*desktopGroupRepo)
	require.True(t, ok)
	groupRepo.groups[0].AllowImageGeneration = false
	list, err = models.ListForUser(context.Background(), 17)
	require.NoError(t, err)
	require.Equal(t, "unavailable", list[0].State)
}

type desktopImageLeaseRepo struct {
	DesktopCredentialRepository
	issued int
	lease  *DesktopCredential
	key    *APIKey
}

func (r *desktopImageLeaseRepo) IssueOrReuse(_ context.Context, lease *DesktopCredential, key *APIKey, _ time.Duration) (*DesktopCredential, *APIKey, error) {
	r.issued++
	lease.ID = 1
	key.ID = 2
	lease.APIKeyID = 2
	r.lease, r.key = lease, key
	return lease, key, nil
}
func (r *desktopImageLeaseRepo) GetByAPIKeyID(context.Context, int64) (*DesktopCredential, error) {
	return r.lease, nil
}

type desktopImageParentCache struct{ RefreshTokenCache }

func (desktopImageParentCache) ActiveFamilyDeadline(context.Context, int64, string) (time.Time, error) {
	return time.Now().Add(time.Hour), nil
}

func TestDesktopImageLeaseRequiresExplicitPurpose(t *testing.T) {
	models, keys := desktopImageFixture(t)
	repo := &desktopImageLeaseRepo{}
	svc := NewDesktopCredentialService(repo, keys, models, nil, desktopImageParentCache{})
	req := DesktopCredentialRequest{UserID: 17, DeviceID: uuid.NewString(), ConnectionGrantID: uuid.NewString(), SessionFamilyID: "family", ModelID: "image"}
	_, err := svc.IssueOrReuseLease(context.Background(), req)
	require.Error(t, err, "legacy chat request must not acquire an image credential")
	require.Zero(t, repo.issued)
	require.NoError(t, json.Unmarshal([]byte(`{"purpose":"image"}`), &req))
	response, err := svc.IssueOrReuseLease(context.Background(), req)
	require.NoError(t, err)
	blob, err := json.Marshal(response)
	require.NoError(t, err)
	require.Contains(t, string(blob), `"purpose":"image"`)
	require.Equal(t, 1, repo.issued)
}

type desktopImageAPIKeys struct {
	APIKeyRepository
	key *APIKey
}

func (r desktopImageAPIKeys) GetByID(context.Context, int64) (*APIKey, error) { return r.key, nil }

func TestDesktopImageLeaseRevalidatesImageOnlyModels(t *testing.T) {
	models, keys := desktopImageFixture(t)
	groupID := int64(11)
	expires := time.Now().Add(time.Hour)
	key := &APIKey{ID: 2, UserID: 17, GroupID: &groupID, Status: StatusActive, DesktopManaged: true, ExpiresAt: &expires}
	keys.apiKeyRepo = desktopImageAPIKeys{key: key}
	repo := &desktopImageLeaseRepo{lease: &DesktopCredential{Purpose: DesktopPurposeImage, APIKeyID: 2, UserID: 17, GroupID: groupID, SessionFamilyID: "family", ExpiresAt: expires}}
	keys.SetDesktopCredentialDependencies(repo, desktopImageParentCache{}, models.settings)
	require.NoError(t, keys.ValidateDesktopCredential(context.Background(), key), "verified image capability should authorize an image lease")
	require.Equal(t, []string{"fixture-image"}, key.Group.ModelAllowlist.Models)
	repo.lease.Purpose = DesktopPurposeChat
	require.ErrorIs(t, keys.ValidateDesktopCredential(context.Background(), key), ErrDesktopCredentialRevoked, "chat lease must not inherit image allowlist")
}
