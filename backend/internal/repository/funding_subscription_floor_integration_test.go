//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestFundingSubscriptionFloorProtectsEscrowAndRecordsShortfall(t *testing.T) {
	ctx := context.Background()
	funding, owner, member, team, key := teamFundingFixture(t)
	limit := 100.0
	group := mustCreateGroup(t, testEntClient(t), &service.Group{Name: "floor-sub-" + uuid.NewString(), Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeSubscription, DailyLimitUSD: &limit, WeeklyLimitUSD: &limit, MonthlyLimitUSD: &limit})
	sub := mustCreateSubscription(t, testEntClient(t), &service.UserSubscription{UserID: owner, GroupID: group.ID})
	ids := []int64{sub.ID}
	_, err := funding.UpdateTeam(ctx, owner, team, &service.TeamUpdateCommand{SharedSubscriptionIDs: &ids})
	require.NoError(t, err)
	hold, err := funding.Reserve(ctx, &service.TeamFundingReserveCommand{RequestID: uuid.NewString(), APIKeyID: key, GroupID: group.ID, TeamID: team, MemberUserID: member, PayerUserID: owner, Source: service.TeamFundingSourceSubscription, SubscriptionID: &sub.ID, Amount: "80"})
	require.NoError(t, err)
	ownerKey := mustCreateApiKey(t, testEntClient(t), &service.APIKey{UserID: owner, Key: "sk-floor-" + uuid.NewString(), Name: "owner-inflight"})
	cmd := &service.UsageBillingCommand{RequestID: uuid.NewString(), APIKeyID: ownerKey.ID, UserID: owner, SubscriptionID: &sub.ID, SubscriptionCost: 50}
	billing := NewUsageBillingRepository(testEntClient(t), integrationDB)
	result, err := billing.Apply(ctx, cmd)
	require.NoError(t, err)
	require.True(t, result.Applied)
	charged, unpaid := result.FundingChargedAmount, result.FundingUnauthorizedAmount
	require.Equal(t, "20.00000000", charged)
	require.Equal(t, "30.00000000", unpaid)
	var daily, weekly, monthly string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT daily_usage_usd::numeric(20,8)::text,weekly_usage_usd::numeric(20,8)::text,monthly_usage_usd::numeric(20,8)::text FROM user_subscriptions WHERE id=$1", sub.ID).Scan(&daily, &weekly, &monthly))
	require.Equal(t, "100.00000000", daily)
	require.Equal(t, daily, weekly)
	require.Equal(t, daily, monthly)
	var requested, storedCharged, storedUnpaid, source string
	var subscriptionID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT requested_amount::text,charged_amount::text,unpaid_amount::text,source,subscription_id FROM funding_billing_shortfalls WHERE request_id=$1 AND api_key_id=$2", cmd.RequestID, cmd.APIKeyID).Scan(&requested, &storedCharged, &storedUnpaid, &source, &subscriptionID))
	require.Equal(t, "50.00000000", requested)
	require.Equal(t, charged, storedCharged)
	require.Equal(t, unpaid, storedUnpaid)
	require.Equal(t, service.FundingSourcePersonalSubscription, source)
	require.Equal(t, sub.ID, subscriptionID)
	result, err = billing.Apply(ctx, cmd)
	require.NoError(t, err)
	require.False(t, result.Applied)
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = funding.SettleTeamFundingReservationTx(ctx, tx, hold.ID, hold.RequestID, key, "80")
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
}

func TestFundingSubscriptionFloorPreservesUnsharedLegacyBehavior(t *testing.T) {
	ctx := context.Background()
	owner := teamFundingUser(t, 0)
	limit := 10.0
	group := mustCreateGroup(t, testEntClient(t), &service.Group{Name: "unshared-floor-" + uuid.NewString(), Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeSubscription, DailyLimitUSD: &limit})
	sub := mustCreateSubscription(t, testEntClient(t), &service.UserSubscription{UserID: owner.ID, GroupID: group.ID})
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	charged, unpaid, err := incrementUsageBillingSubscriptionProtected(ctx, tx, &service.UsageBillingCommand{UserID: owner.ID, SubscriptionID: &sub.ID, SubscriptionCost: 20})
	require.NoError(t, err)
	require.Empty(t, charged)
	require.Empty(t, unpaid)
	require.NoError(t, tx.Commit())
	var daily string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT daily_usage_usd::numeric(20,8)::text FROM user_subscriptions WHERE id=$1", sub.ID).Scan(&daily))
	require.Equal(t, "20.00000000", daily)
}

func TestTeamFundingOwnerWithoutPreferencesUsesProtectedPersonalContext(t *testing.T) {
	ctx := context.Background()
	repo, owner, _, _, _ := teamFundingFixture(t)
	funding, err := repo.GetFundingContext(ctx, owner)
	require.NoError(t, err)
	require.True(t, funding.Preferences.Configured)
	require.True(t, funding.ImplicitOwner)
	var preferenceRows int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM funding_preferences WHERE user_id=$1", owner).Scan(&preferenceRows))
	require.Zero(t, preferenceRows)
	require.Equal(t, service.FundingModePersonalFirst, funding.Preferences.Mode)
	require.True(t, funding.Preferences.AllowPersonalBalance)
	require.True(t, funding.Preferences.AllowPersonalSubscription)
	require.Nil(t, funding.Team)
	legacy := teamFundingUser(t, 0)
	funding, err = repo.GetFundingContext(ctx, legacy.ID)
	require.NoError(t, err)
	require.False(t, funding.Preferences.Configured)
	require.False(t, funding.ImplicitOwner)
	_, err = repo.UpdateFunding(ctx, owner, &service.FundingUpdateCommand{Mode: service.FundingModePersonalFirst, AllowPersonalBalance: true, AllowPersonalSubscription: true})
	require.NoError(t, err)
	funding, err = repo.GetFundingContext(ctx, owner)
	require.NoError(t, err)
	require.False(t, funding.ImplicitOwner)
}

func TestFundingSubscriptionFloorUsesEachWindowAndPreservesFractionalUsage(t *testing.T) {
	ctx := context.Background()
	repo, owner, _, team, _ := teamFundingFixture(t)
	ownerKey := mustCreateApiKey(t, testEntClient(t), &service.APIKey{UserID: owner, Key: "sk-fraction-floor-" + uuid.NewString(), Name: "window-floors"})
	for _, window := range []string{"daily_usage_usd", "weekly_usage_usd", "monthly_usage_usd"} {
		t.Run(window, func(t *testing.T) {
			limit := 10.0
			group := mustCreateGroup(t, testEntClient(t), &service.Group{Name: "window-floor-" + uuid.NewString(), Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeSubscription, DailyLimitUSD: &limit, WeeklyLimitUSD: &limit, MonthlyLimitUSD: &limit})
			sub := mustCreateSubscription(t, testEntClient(t), &service.UserSubscription{UserID: owner, GroupID: group.ID})
			ids := []int64{sub.ID}
			_, err := repo.UpdateTeam(ctx, owner, team, &service.TeamUpdateCommand{SharedSubscriptionIDs: &ids})
			require.NoError(t, err)
			// The identifier comes from the fixed list above. Legacy usage retains 10 decimals.
			_, err = integrationDB.ExecContext(ctx, "UPDATE user_subscriptions SET "+window+"=9.9999999950 WHERE id=$1", sub.ID)
			require.NoError(t, err)
			tx, err := integrationDB.BeginTx(ctx, nil)
			require.NoError(t, err)
			cmd := &service.UsageBillingCommand{RequestID: uuid.NewString(), APIKeyID: ownerKey.ID, UserID: owner, SubscriptionID: &sub.ID, SubscriptionCost: 1}
			charged, unpaid, err := incrementUsageBillingSubscriptionProtected(ctx, tx, cmd)
			require.NoError(t, err)
			require.Equal(t, "0.00000000", charged)
			require.Equal(t, "1.00000000", unpaid)
			require.NoError(t, tx.Rollback())
			var stored string
			require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT "+window+"::text FROM user_subscriptions WHERE id=$1", sub.ID).Scan(&stored))
			require.Equal(t, "9.9999999950", stored)
			var count int
			require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM funding_billing_shortfalls WHERE request_id=$1", cmd.RequestID).Scan(&count))
			require.Zero(t, count, "rolled-back accounting must not leave a reconciliation row")
		})
	}
}
