//go:build integration

package repository

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type managedBalanceOnlyRig struct {
	repo                                *teamFundingRepository
	owner, actor, team, key, group, sub int64
}

func newManagedBalanceOnlyRig(t *testing.T, balance float64) *managedBalanceOnlyRig {
	t.Helper()
	ctx := t.Context()
	funding, owner, actor, team, _ := teamFundingFixture(t)
	_, err := integrationDB.ExecContext(ctx, "UPDATE users SET balance=$2::numeric WHERE id=$1", actor, balance)
	require.NoError(t, err)
	group := teamFundingGroup(t, &service.Group{Name: "managed-wallet-" + uuid.NewString(), Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard})
	limit := 100.0
	subGroup := teamFundingGroup(t, &service.Group{Name: "managed-unused-sub-" + uuid.NewString(), Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeSubscription, DailyLimitUSD: &limit, WeeklyLimitUSD: &limit, MonthlyLimitUSD: &limit})
	sub := mustCreateSubscription(t, testEntClient(t), &service.UserSubscription{UserID: actor, GroupID: subGroup.ID})
	key := &service.APIKey{UserID: actor, Key: "sk-managed-" + uuid.NewString(), Name: "restricted-wallet-fixture", Status: service.StatusActive, DesktopManaged: true, BillingPolicy: service.BillingPolicyBalanceOnly}
	require.NoError(t, NewAPIKeyRepository(testEntClient(t), integrationDB).Create(ctx, key))
	_, err = funding.UpdateFunding(ctx, actor, &service.FundingUpdateCommand{Mode: service.FundingModeTeamFirst, TeamID: &team, AllowPersonalSubscription: true})
	require.NoError(t, err)
	return &managedBalanceOnlyRig{repo: funding.(*teamFundingRepository), owner: owner, actor: actor, team: team, key: key.ID, group: group.ID, sub: sub.ID}
}

func (r *managedBalanceOnlyRig) command(amount string) *service.PersonalFundingReserveCommand {
	return &service.PersonalFundingReserveCommand{RequestID: uuid.NewString(), APIKeyID: r.key, MemberUserID: r.actor, GroupID: r.group, Source: service.FundingSourcePersonalBalance, Amount: amount}
}

func (r *managedBalanceOnlyRig) assertWallet(t *testing.T, expected string) {
	t.Helper()
	var actual string
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT balance::text FROM users WHERE id=$1", r.actor).Scan(&actual))
	require.Equal(t, expected, actual)
}

func (r *managedBalanceOnlyRig) assertOtherFundingUntouched(t *testing.T) {
	t.Helper()
	ctx := t.Context()
	var ownerBalance, reserved, spent, daily, weekly, monthly, mode string
	var allowBalance, allowSubscription bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance::text FROM users WHERE id=$1", r.owner).Scan(&ownerBalance))
	require.Equal(t, "500.00000000", ownerBalance)
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT reserved::text,spent::text FROM team_members WHERE team_id=$1 AND user_id=$2", r.team, r.actor).Scan(&reserved, &spent))
	require.Equal(t, "0.00000000", reserved)
	require.Equal(t, "0.00000000", spent)
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT daily_usage_usd::numeric(20,8)::text,weekly_usage_usd::numeric(20,8)::text,monthly_usage_usd::numeric(20,8)::text FROM user_subscriptions WHERE id=$1", r.sub).Scan(&daily, &weekly, &monthly))
	require.Equal(t, "0.00000000", daily)
	require.Equal(t, "0.00000000", weekly)
	require.Equal(t, "0.00000000", monthly)
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT mode,allow_personal_balance,allow_personal_subscription FROM funding_preferences WHERE user_id=$1", r.actor).Scan(&mode, &allowBalance, &allowSubscription))
	require.Equal(t, "team_first", mode)
	require.False(t, allowBalance)
	require.True(t, allowSubscription)
}

func TestManagedBalanceOnlyConcurrentReservationsRespectOwnWallet(t *testing.T) {
	r := newManagedBalanceOnlyRig(t, 20)
	ctx := t.Context()
	start := make(chan struct{})
	errors := make(chan error, 30)
	var workers sync.WaitGroup
	var accepted atomic.Int32
	for range 30 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, err := r.repo.ReservePersonalFunding(ctx, r.command("3"))
			if err == nil {
				accepted.Add(1)
			}
			errors <- err
		}()
	}
	close(start)
	workers.Wait()
	close(errors)
	require.EqualValues(t, 6, accepted.Load())
	for err := range errors {
		if err != nil {
			require.ErrorIs(t, err, service.ErrTeamFundingConflict)
		}
	}
	r.assertWallet(t, "2.00000000")
	_, err := r.repo.Reserve(ctx, &service.TeamFundingReserveCommand{RequestID: uuid.NewString(), APIKeyID: r.key, TeamID: r.team, MemberUserID: r.actor, PayerUserID: r.owner, Source: service.TeamFundingSourceBalance, Amount: "1"})
	require.ErrorIs(t, err, service.ErrTeamFundingForbidden)
	r.assertOtherFundingUntouched(t)
}

func TestManagedBalanceOnlyConcurrentReservationReplayDebitsOnce(t *testing.T) {
	r := newManagedBalanceOnlyRig(t, 20)
	ctx := t.Context()
	command := r.command("3")
	start := make(chan struct{})
	type outcome struct {
		hold *service.PersonalFundingReservation
		err  error
	}
	results := make(chan outcome, 20)
	var workers sync.WaitGroup
	for range 20 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			copy := *command
			hold, err := r.repo.ReservePersonalFunding(ctx, &copy)
			results <- outcome{hold, err}
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	var reservationID int64
	for result := range results {
		require.NoError(t, result.err)
		if reservationID == 0 {
			reservationID = result.hold.ID
		}
		require.Equal(t, reservationID, result.hold.ID)
	}
	r.assertWallet(t, "17.00000000")
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM personal_funding_reservations WHERE api_key_id=$1 AND request_id=$2", r.key, command.RequestID).Scan(&count))
	require.Equal(t, 1, count)
	r.assertOtherFundingUntouched(t)
}

func TestManagedBalanceOnlySettlementRollbackAndConcurrentReplay(t *testing.T) {
	r := newManagedBalanceOnlyRig(t, 20)
	ctx := t.Context()
	command := r.command("8")
	hold, err := r.repo.ReservePersonalFunding(ctx, command)
	require.NoError(t, err)
	require.NoError(t, r.repo.BeginFundingSnapshotDispatch(ctx, command.RequestID, r.key, r.actor, hold.ID, false))
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = r.repo.SettlePersonalFundingReservationTx(ctx, tx, hold.ID, command.RequestID, r.key, "5.5")
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	r.assertWallet(t, "12.00000000")
	billing := NewUsageBillingRepository(testEntClient(t), integrationDB)
	start := make(chan struct{})
	type outcome struct {
		applied bool
		err     error
	}
	results := make(chan outcome, 24)
	var workers sync.WaitGroup
	for range 24 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			result, err := billing.Apply(ctx, &service.UsageBillingCommand{RequestID: command.RequestID, APIKeyID: r.key, UserID: r.actor, PayerUserID: r.actor, PersonalFundingReservationID: hold.ID, FundingSource: service.FundingSourcePersonalBalance, FundingCost: 5.5})
			results <- outcome{result != nil && result.Applied, err}
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	applied := 0
	for result := range results {
		require.NoError(t, result.err)
		if result.applied {
			applied++
		}
	}
	require.Equal(t, 1, applied)
	r.assertWallet(t, "14.50000000")
	var status, settled, unauthorized string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT status,settled_amount::text,unauthorized_amount::text FROM personal_funding_reservations WHERE id=$1", hold.ID).Scan(&status, &settled, &unauthorized))
	require.Equal(t, "settled", status)
	require.Equal(t, "5.50000000", settled)
	require.Equal(t, "0.00000000", unauthorized)
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_billing_dedup WHERE api_key_id=$1 AND request_id=$2", r.key, command.RequestID).Scan(&count))
	require.Equal(t, 1, count)
	r.assertOtherFundingUntouched(t)
}

func TestManagedBalanceOnlyUnknownDispatchKeepsEscrowAndCannotRetry(t *testing.T) {
	r := newManagedBalanceOnlyRig(t, 10)
	ctx := t.Context()
	command := r.command("10")
	hold, err := r.repo.ReservePersonalFunding(ctx, command)
	require.NoError(t, err)
	require.NoError(t, r.repo.BeginFundingSnapshotDispatch(ctx, command.RequestID, r.key, r.actor, hold.ID, false))
	r.assertWallet(t, "0.00000000")
	interrupted, cancel := context.WithCancel(ctx)
	cancel()
	require.ErrorIs(t, r.repo.ReleasePersonalFunding(interrupted, hold.ID, r.actor), context.Canceled)
	restarted := &teamFundingRepository{db: integrationDB}
	require.ErrorIs(t, restarted.ReleasePersonalFunding(ctx, hold.ID, r.actor), service.ErrTeamFundingConflict)
	require.ErrorIs(t, restarted.BeginFundingSnapshotDispatch(ctx, command.RequestID, r.key, r.actor, hold.ID, false), service.ErrUsageBillingRequestConflict)
	_, err = restarted.ReservePersonalFunding(ctx, command)
	require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
	_, err = restarted.ReservePersonalFunding(ctx, r.command("1"))
	require.ErrorIs(t, err, service.ErrTeamFundingConflict)
	r.assertWallet(t, "0.00000000")
	var status string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT status FROM personal_funding_reservations WHERE id=$1", hold.ID).Scan(&status))
	require.Equal(t, "dispatched", status)
	r.assertOtherFundingUntouched(t)
}

func TestManagedBalanceOnlyOverageDoesNotDebitBeyondEscrow(t *testing.T) {
	r := newManagedBalanceOnlyRig(t, 2)
	ctx := t.Context()
	command := r.command("2")
	hold, err := r.repo.ReservePersonalFunding(ctx, command)
	require.NoError(t, err)
	billing := NewUsageBillingRepository(testEntClient(t), integrationDB)
	result, err := billing.Apply(ctx, &service.UsageBillingCommand{RequestID: command.RequestID, APIKeyID: r.key, UserID: r.actor, PayerUserID: r.actor, PersonalFundingReservationID: hold.ID, FundingSource: service.FundingSourcePersonalBalance, FundingCost: 3})
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.Equal(t, "2.00000000", result.FundingChargedAmount)
	require.Equal(t, "1.00000000", result.FundingUnauthorizedAmount)
	r.assertWallet(t, "0.00000000")
	_, err = r.repo.ReservePersonalFunding(ctx, r.command("1"))
	require.ErrorIs(t, err, service.ErrFundingUnavailable)
	r.assertOtherFundingUntouched(t)
}

func TestManagedBalanceOnlyReservationPreservesAccountPreferences(t *testing.T) {
	ctx := t.Context()
	user := teamFundingUser(t, 20)
	group := teamFundingGroup(t, &service.Group{Name: "balance-only-" + uuid.NewString(), Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard})
	key := &service.APIKey{UserID: user.ID, Key: "sk-" + uuid.NewString(), Name: "managed balance fixture", Status: service.StatusActive, DesktopManaged: true, BillingPolicy: "balance_only"}
	require.NoError(t, NewAPIKeyRepository(testEntClient(t), integrationDB).Create(ctx, key))
	repo := &teamFundingRepository{db: integrationDB}
	_, err := repo.UpdateFunding(ctx, user.ID, &service.FundingUpdateCommand{Mode: service.FundingModeTeamOnly})
	require.NoError(t, err)
	command := &service.PersonalFundingReserveCommand{RequestID: uuid.NewString(), APIKeyID: key.ID, MemberUserID: user.ID, GroupID: group.ID, Source: "personal_balance", Amount: "3"}
	hold, err := repo.ReservePersonalFunding(ctx, command)
	require.NoError(t, err)
	require.Equal(t, "personal_balance", hold.Source)
	var balance, mode string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance::text FROM users WHERE id=$1", user.ID).Scan(&balance))
	require.Equal(t, "17.00000000", balance)
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT mode FROM funding_preferences WHERE user_id=$1", user.ID).Scan(&mode))
	require.Equal(t, "team_only", mode)
	command.RequestID = uuid.NewString()
	command.Source = "personal_subscription"
	_, err = repo.ReservePersonalFunding(ctx, command)
	require.ErrorIs(t, err, service.ErrTeamFundingForbidden)
	require.NoError(t, repo.ReleasePersonalFunding(ctx, hold.ID, user.ID))
}
