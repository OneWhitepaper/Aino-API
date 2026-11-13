//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func deleteEscrowTeam(t *testing.T, funding service.TeamFundingRepository, owner, team int64) {
	t.Helper()
	err := funding.DeleteTeam(context.Background(), owner, team)
	require.NoError(t, err)
}

func TestDeletedTeamPendingBalanceEscrowRetainsOwnerProtection(t *testing.T) {
	for _, state := range []string{"reserved", "queued", "dispatched"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			funding, owner, member, team, key := teamFundingFixture(t)
			hold, err := teamFundingReserve(funding, owner, member, team, key, "500")
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(ctx, "UPDATE team_funding_reservations SET status=$2 WHERE id=$1", hold.ID, state)
			require.NoError(t, err)
			deleteEscrowTeam(t, funding, owner, team)
			ownerKey := mustCreateApiKey(t, testEntClient(t), &service.APIKey{UserID: owner, Key: "sk-deleted-owner-" + uuid.NewString(), Name: "inflight"})
			billing := NewUsageBillingRepository(testEntClient(t), integrationDB)
			result, err := billing.Apply(ctx, &service.UsageBillingCommand{RequestID: uuid.NewString(), APIKeyID: ownerKey.ID, UserID: owner, BalanceCost: 1})
			require.NoError(t, err)
			require.Equal(t, "0.00000000", result.FundingChargedAmount)
			require.Equal(t, "1.00000000", result.FundingUnauthorizedAmount)
			var balance, reserved string
			require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance::text FROM users WHERE id=$1", owner).Scan(&balance))
			require.Equal(t, "0.00000000", balance)
			require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT reserved::text FROM team_members WHERE team_id=$1 AND user_id=$2", team, member).Scan(&reserved))
			require.Equal(t, "500.00000000", reserved)
			current, err := funding.GetFundingContext(ctx, owner)
			require.NoError(t, err)
			require.True(t, current.ImplicitOwner)
			tx, err := integrationDB.BeginTx(ctx, nil)
			require.NoError(t, err)
			_, err = funding.SettleTeamFundingReservationTx(ctx, tx, hold.ID, hold.RequestID, key, "500")
			require.NoError(t, err)
			require.NoError(t, tx.Commit())
			current, err = funding.GetFundingContext(ctx, owner)
			require.NoError(t, err)
			require.False(t, current.Preferences.Configured, "finished holds must not keep the implicit owner opt-in forever")
		})
	}
}

func TestDeletedTeamPendingSubscriptionEscrowRetainsWindowProtection(t *testing.T) {
	for _, state := range []string{"reserved", "queued", "dispatched"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			funding, owner, member, team, key := teamFundingFixture(t)
			limit := 100.0
			group := teamFundingGroup(t, &service.Group{Name: "deleted-sub-" + uuid.NewString(), Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeSubscription, DailyLimitUSD: &limit, WeeklyLimitUSD: &limit, MonthlyLimitUSD: &limit})
			sub := mustCreateSubscription(t, testEntClient(t), &service.UserSubscription{UserID: owner, GroupID: group.ID})
			ids := []int64{sub.ID}
			_, err := funding.UpdateTeam(ctx, owner, team, &service.TeamUpdateCommand{SharedSubscriptionIDs: &ids})
			require.NoError(t, err)
			hold, err := funding.Reserve(ctx, &service.TeamFundingReserveCommand{RequestID: uuid.NewString(), APIKeyID: key, GroupID: group.ID, TeamID: team, MemberUserID: member, PayerUserID: owner, Source: service.TeamFundingSourceSubscription, SubscriptionID: &sub.ID, Amount: "80"})
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(ctx, "UPDATE team_funding_reservations SET status=$2 WHERE id=$1", hold.ID, state)
			require.NoError(t, err)
			deleteEscrowTeam(t, funding, owner, team)
			ownerKey := mustCreateApiKey(t, testEntClient(t), &service.APIKey{UserID: owner, Key: "sk-deleted-sub-" + uuid.NewString(), Name: "inflight"})
			result, err := NewUsageBillingRepository(testEntClient(t), integrationDB).Apply(ctx, &service.UsageBillingCommand{RequestID: uuid.NewString(), APIKeyID: ownerKey.ID, UserID: owner, SubscriptionID: &sub.ID, SubscriptionCost: 50})
			require.NoError(t, err)
			require.Equal(t, "20.00000000", result.FundingChargedAmount)
			require.Equal(t, "30.00000000", result.FundingUnauthorizedAmount)
			var daily, weekly, monthly string
			require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT daily_usage_usd::numeric(20,8)::text,weekly_usage_usd::numeric(20,8)::text,monthly_usage_usd::numeric(20,8)::text FROM user_subscriptions WHERE id=$1", sub.ID).Scan(&daily, &weekly, &monthly))
			require.Equal(t, "100.00000000", daily)
			require.Equal(t, daily, weekly)
			require.Equal(t, daily, monthly)
		})
	}
}

func TestDeletedTeamOwnerPersonalHoldKeepsControlledReservations(t *testing.T) {
	ctx := context.Background()
	funding, owner, _, team, _ := teamFundingFixture(t)
	personal := &teamFundingRepository{db: integrationDB}
	key := mustCreateApiKey(t, testEntClient(t), &service.APIKey{UserID: owner, Key: "sk-deleted-personal-" + uuid.NewString(), Name: "owner"})
	first, err := personal.ReservePersonalFunding(ctx, &service.PersonalFundingReserveCommand{RequestID: uuid.NewString(), APIKeyID: key.ID, MemberUserID: owner, Source: service.FundingSourcePersonalBalance, Amount: "400"})
	require.NoError(t, err)
	deleteEscrowTeam(t, funding, owner, team)
	current, err := funding.GetFundingContext(ctx, owner)
	require.NoError(t, err)
	require.True(t, current.ImplicitOwner)
	require.True(t, current.Preferences.Configured)
	second, err := personal.ReservePersonalFunding(ctx, &service.PersonalFundingReserveCommand{RequestID: uuid.NewString(), APIKeyID: key.ID, MemberUserID: owner, Source: service.FundingSourcePersonalBalance, Amount: "50"})
	require.NoError(t, err, "remaining accepted escrow keeps the owner in the protected reservation path")
	result, err := NewUsageBillingRepository(testEntClient(t), integrationDB).Apply(ctx, &service.UsageBillingCommand{RequestID: uuid.NewString(), APIKeyID: key.ID, UserID: owner, BalanceCost: 100})
	require.NoError(t, err)
	require.Equal(t, "50.00000000", result.FundingChargedAmount)
	require.Equal(t, "50.00000000", result.FundingUnauthorizedAmount)
	require.NoError(t, personal.ReleasePersonalFunding(ctx, first.ID, owner))
	require.NoError(t, personal.ReleasePersonalFunding(ctx, second.ID, owner))
	current, err = funding.GetFundingContext(ctx, owner)
	require.NoError(t, err)
	require.False(t, current.Preferences.Configured)
}

func TestDeletedTeamOwnerPersonalSubscriptionHoldProtectsWindow(t *testing.T) {
	for _, state := range []string{"reserved", "queued", "dispatched"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			funding, owner, _, team, _ := teamFundingFixture(t)
			personal := &teamFundingRepository{db: integrationDB}
			limit := 100.0
			group := teamFundingGroup(t, &service.Group{Name: "deleted-personal-sub-" + uuid.NewString(), Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeSubscription, DailyLimitUSD: &limit, WeeklyLimitUSD: &limit, MonthlyLimitUSD: &limit})
			sub := mustCreateSubscription(t, testEntClient(t), &service.UserSubscription{UserID: owner, GroupID: group.ID})
			key := mustCreateApiKey(t, testEntClient(t), &service.APIKey{UserID: owner, Key: "sk-deleted-private-sub-" + uuid.NewString(), Name: "owner"})
			hold, err := personal.ReservePersonalFunding(ctx, &service.PersonalFundingReserveCommand{RequestID: uuid.NewString(), APIKeyID: key.ID, MemberUserID: owner, GroupID: group.ID, SubscriptionID: &sub.ID, Source: service.FundingSourcePersonalSubscription, Amount: "80"})
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(ctx, "UPDATE personal_funding_reservations SET status=$2 WHERE id=$1", hold.ID, state)
			require.NoError(t, err)
			deleteEscrowTeam(t, funding, owner, team)
			billing := NewUsageBillingRepository(testEntClient(t), integrationDB)
			result, err := billing.Apply(ctx, &service.UsageBillingCommand{RequestID: uuid.NewString(), APIKeyID: key.ID, UserID: owner, SubscriptionID: &sub.ID, SubscriptionCost: 50})
			require.NoError(t, err)
			require.Equal(t, "20.00000000", result.FundingChargedAmount)
			require.Equal(t, "30.00000000", result.FundingUnauthorizedAmount)
			tx, err := integrationDB.BeginTx(ctx, nil)
			require.NoError(t, err)
			_, err = personal.SettlePersonalFundingReservationTx(ctx, tx, hold.ID, hold.RequestID, key.ID, "80")
			require.NoError(t, err)
			require.NoError(t, tx.Commit())
			current, err := funding.GetFundingContext(ctx, owner)
			require.NoError(t, err)
			require.False(t, current.Preferences.Configured)
			result, err = billing.Apply(ctx, &service.UsageBillingCommand{RequestID: uuid.NewString(), APIKeyID: key.ID, UserID: owner, SubscriptionID: &sub.ID, SubscriptionCost: 1})
			require.NoError(t, err)
			require.Empty(t, result.FundingChargedAmount, "a finished hold must not permanently change unshared legacy subscription accounting")
		})
	}
}

func TestDeletedTeamConcurrentPersonalEscrowCannotBeMissedByLegacySettlement(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	funding, owner, _, team, _ := teamFundingFixture(t)
	key := mustCreateApiKey(t, testEntClient(t), &service.APIKey{UserID: owner, Key: "sk-deleted-race-" + uuid.NewString(), Name: "owner"})
	// Hold the wallet as an in-flight owner reservation does, then delete the
	// team before that reservation commits. Its escrow is not yet MVCC-visible.
	held, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = held.Rollback() }()
	var holderPID int
	require.NoError(t, held.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&holderPID))
	_, err = held.ExecContext(ctx, "UPDATE users SET balance=balance-500 WHERE id=$1", owner)
	require.NoError(t, err)
	_, err = held.ExecContext(ctx, `INSERT INTO personal_funding_reservations(request_id,api_key_id,member_user_id,source,amount) VALUES($1,$2,$3,'personal_balance',500)`, uuid.NewString(), key.ID, owner)
	require.NoError(t, err)
	deleteEscrowTeam(t, funding, owner, team)
	type outcome struct {
		value *service.UsageBillingApplyResult
		err   error
	}
	done := make(chan outcome, 1)
	billing := NewUsageBillingRepository(testEntClient(t), integrationDB)
	go func() {
		value, err := billing.Apply(ctx, &service.UsageBillingCommand{RequestID: uuid.NewString(), APIKeyID: key.ID, UserID: owner, BalanceCost: 1})
		done <- outcome{value, err}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		e := integrationDB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))", holderPID).Scan(&waiting)
		return e == nil && waiting
	}, 3*time.Second, 10*time.Millisecond, "legacy settlement must reach the wallet lock while escrow is uncommitted")
	require.NoError(t, held.Commit())
	var result outcome
	select {
	case result = <-done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.NoError(t, result.err)
	require.Equal(t, "0.00000000", result.value.FundingChargedAmount)
	require.Equal(t, "1.00000000", result.value.FundingUnauthorizedAmount)
	var balance string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance::text FROM users WHERE id=$1", owner).Scan(&balance))
	require.Equal(t, "0.00000000", balance)
}
