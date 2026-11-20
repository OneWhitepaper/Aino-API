//go:build unit

package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestBalanceOnlyFundingUsesOwnBalanceRegardlessOfAccountPreference(t *testing.T) {
	for _, source := range []string{TeamFundingSourceBalance, FundingSourcePersonalSubscription, "unconfigured"} {
		t.Run(source, func(t *testing.T) {
			svc, key, repo := fundingPreparationFixture(source)
			key.Group.SubscriptionType = ""
			if source == "unconfigured" {
				repo.data.Preferences.Configured = false
			}
			require.NoError(t, json.Unmarshal([]byte(`{"desktop_managed":true,"billing_policy":"balance_only"}`), key))
			key.DesktopManaged = true
			original := repo.data.Preferences
			funded, sub, err := svc.ResolveRequestFunding(t.Context(), key)
			require.NoError(t, err)
			require.Nil(t, sub)
			require.NotNil(t, funded.Funding)
			require.Equal(t, "personal_balance", funded.Funding.Source)
			require.Equal(t, int64(22), funded.Funding.PayerUserID)
			require.Zero(t, funded.Funding.TeamID)
			require.Equal(t, original, repo.data.Preferences)
			key.User.Balance = 0
			_, _, err = svc.ResolveRequestFunding(t.Context(), key)
			require.ErrorIs(t, err, ErrFundingUnavailable)
		})
	}
}

func TestBalanceOnlyFundingRejectsSubscriptionGroupsAndFixedSharedSources(t *testing.T) {
	svc, key, _ := fundingPreparationFixture(FundingSourcePersonalSubscription)
	require.NoError(t, json.Unmarshal([]byte(`{"billing_policy":"balance_only"}`), key))
	key.DesktopManaged = true
	_, _, err := svc.ResolveRequestFunding(t.Context(), key)
	require.Error(t, err)
	key.Group.SubscriptionType = ""
	_, _, _, err = svc.prepareFundingRequestWithPlan(t.Context(), key, "/v1/responses", "application/json", nil, "fixture-operation", &FundingDecision{Source: TeamFundingSourceBalance, PayerUserID: 11}, &FundingRequestPlan{Kind: FundingRequestControl})
	require.Error(t, err)
}

func TestDeviceKeyBillingPolicySurvivesAuthSnapshot(t *testing.T) {
	key := &APIKey{ID: 7, UserID: 22, DesktopManaged: true, DesktopCredentialType: "device", User: &User{ID: 22}}
	require.NoError(t, json.Unmarshal([]byte(`{"billing_policy":"balance_only"}`), key))
	svc := &APIKeyService{}
	snapshot := svc.snapshotFromAPIKey(t.Context(), key)
	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"billing_policy":"balance_only"`)
	restored := svc.snapshotToAPIKey("fixture-secret", snapshot)
	encoded, err = json.Marshal(restored)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"billing_policy":"balance_only"`)
}

func TestBalanceOnlyAsyncSnapshotCannotRestoreAnotherPayer(t *testing.T) {
	_, key, _ := fundingPreparationFixture(TeamFundingSourceBalance)
	require.NoError(t, json.Unmarshal([]byte(`{"billing_policy":"balance_only"}`), key))
	key.DesktopManaged = true
	snapshot := &AsyncFundingSnapshot{ActorUserID: key.UserID, APIKeyID: key.ID, DesktopManaged: true, Group: key.Group, Funding: &FundingDecision{Source: TeamFundingSourceBalance, PayerUserID: 11, TeamID: 4}}
	_, _, err := RestoreAsyncFundingSnapshot(key, snapshot)
	require.ErrorIs(t, err, ErrFundingUnavailable)
	snapshot.Funding = &FundingDecision{Source: FundingSourcePersonalBalance, PayerUserID: key.UserID}
	key.User.AllowedGroups = []int64{}
	key.User.RestrictPublicGroups = true
	_, _, err = RestoreAsyncFundingSnapshot(key, snapshot)
	require.NoError(t, err)
}

func TestBalanceOnlyCatalogIgnoresTeamPreferenceAndExcludesSubscriptions(t *testing.T) {
	cfg := &config.Config{RunMode: config.RunModeStandard}
	users := &desktopUserRepo{user: &User{ID: 22, Status: StatusActive, Balance: 10}}
	input, output := 2e-6, 6e-6
	groups := &desktopGroupRepo{groups: []Group{
		{ID: 3, Status: StatusActive, Platform: PlatformOpenAI, SubscriptionType: SubscriptionTypeStandard, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{{Models: []string{"fixture-model"}, InputPrice: &input, OutputPrice: &output}}},
		{ID: 4, Status: StatusActive, Platform: PlatformOpenAI, SubscriptionType: SubscriptionTypeSubscription, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{{Models: []string{"fixture-model"}, InputPrice: &input, OutputPrice: &output}}},
	}}
	subs := fundingSubscriptionsStub{subs: []UserSubscription{{ID: 9, UserID: 22, GroupID: 4, Status: SubscriptionStatusActive, StartsAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour)}}}
	settings := NewSettingService(&desktopSettingsRepo{values: map[string]string{}}, cfg)
	updates, err := settings.buildDesktopSettingsUpdates(DesktopSettings{Enabled: true, CredentialTTLSeconds: 900, Models: []DesktopModelEntry{
		{ID: "balance-option", GroupID: 3, Model: "fixture-model", DisplayName: "Balance", ProviderLabel: "Fixture", Platform: PlatformOpenAI, APIMode: "responses", AgentVerified: true, Capabilities: PlatformModelCapabilities{Tools: true}},
		{ID: "subscription-option", GroupID: 4, Model: "fixture-model", DisplayName: "Subscription", ProviderLabel: "Fixture", Platform: PlatformOpenAI, APIMode: "responses", AgentVerified: true, Capabilities: PlatformModelCapabilities{Tools: true}},
	}})
	require.NoError(t, err)
	settings.settingRepo = &desktopSettingsRepo{values: updates}
	keys := NewAPIKeyService(nil, users, groups, subs, nil, nil, cfg)
	preferences := &FundingContext{Preferences: FundingPreferences{Configured: true, Mode: FundingModeTeamOnly}}
	keys.SetTeamFundingService(NewTeamFundingService(&fundingPreparationRepoStub{data: preferences}))
	billing := NewBillingService(cfg, nil)
	eligibility := NewBillingCacheService(nil, users, subs, nil, nil, nil, cfg, nil)
	t.Cleanup(eligibility.Stop)
	catalog := NewDesktopModelService(settings, keys, billing, NewModelPricingResolver(nil, billing), eligibility, nil)
	ctx := WithDesktopBillingPolicy(t.Context(), BillingPolicyBalanceOnly)
	models, err := catalog.ListForUser(ctx, 22)
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, "balance-option", models[0].ID)
	require.Equal(t, "available", models[0].State)
	require.Equal(t, "balance", models[0].BillingSource)
	key := &APIKey{ID: 7, UserID: 22, User: users.user, DesktopManaged: true, BillingPolicy: BillingPolicyBalanceOnly}
	_, err = catalog.ResolveRequest(t.Context(), key, "subscription-option", "responses", []string{"fixture-model"}, "", true)
	require.Error(t, err)
	_, err = catalog.ResolveRequest(t.Context(), key, "balance-option", "responses", []string{"fixture-model"}, "", true)
	require.NoError(t, err)
	users.user.Balance = 0
	models, err = catalog.ListForUser(ctx, 22)
	require.NoError(t, err)
	require.Empty(t, models)
	require.Equal(t, "team_only", preferences.Preferences.Mode)
}
