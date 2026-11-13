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

func TestFundingResizeSharedConcurrentGrowthHonorsMemberCap(t *testing.T) {
	ctx := context.Background()
	repo, owner, member, team, key := teamFundingFixture(t)
	r := repo.(*teamFundingRepository)
	teamFundingCap(t, repo, owner, team, member, "100")
	holds := make([]*service.TeamFundingReservation, 10)
	for i := range holds {
		var err error
		holds[i], err = teamFundingReserve(repo, owner, member, team, key, "1")
		require.NoError(t, err)
	}
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for _, hold := range holds {
		wg.Add(1)
		go func(hold *service.TeamFundingReservation) {
			defer wg.Done()
			if r.ResizeFundingReservation(ctx, hold.ID, member, key, hold.RequestID, true, "11") == nil {
				accepted.Add(1)
			}
		}(hold)
	}
	wg.Wait()
	require.EqualValues(t, 9, accepted.Load(), "initial 10 plus nine growth deltas of 10 exhausts the cap")
	var balance, personal, reserved, sum string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance::text FROM users WHERE id=$1", owner).Scan(&balance))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance::text FROM users WHERE id=$1", member).Scan(&personal))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT reserved::text FROM team_members WHERE team_id=$1 AND user_id=$2", team, member).Scan(&reserved))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT SUM(amount)::numeric(20,8)::text FROM team_funding_reservations WHERE team_id=$1 AND member_user_id=$2", team, member).Scan(&sum))
	require.Equal(t, "400.00000000", balance)
	require.Equal(t, "0.00000000", personal)
	require.Equal(t, "100.00000000", reserved)
	require.Equal(t, reserved, sum)
}

func TestFundingResizePersonalGrowthIsAtomicAndIdempotent(t *testing.T) {
	ctx := context.Background()
	r, user, key := personalFundingFixture(t, 100)
	firstCmd, secondCmd := personalFundingCommand(user, key, "20"), personalFundingCommand(user, key, "20")
	first, err := r.ReservePersonalFunding(ctx, firstCmd)
	require.NoError(t, err)
	second, err := r.ReservePersonalFunding(ctx, secondCmd)
	require.NoError(t, err)
	var accepted atomic.Int32
	var winner atomic.Int64
	var wg sync.WaitGroup
	for _, hold := range []*service.PersonalFundingReservation{first, second} {
		wg.Add(1)
		go func(hold *service.PersonalFundingReservation) {
			defer wg.Done()
			if r.ResizeFundingReservation(ctx, hold.ID, user, key, hold.RequestID, false, "70") == nil {
				accepted.Add(1)
				winner.Store(hold.ID)
			}
		}(hold)
	}
	wg.Wait()
	require.EqualValues(t, 1, accepted.Load())
	winning := first
	if winner.Load() == second.ID {
		winning = second
	}
	require.NoError(t, r.ResizeFundingReservation(ctx, winning.ID, user, key, winning.RequestID, false, "70"))
	var balance, amount string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance::text FROM users WHERE id=$1", user).Scan(&balance))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT amount::text FROM personal_funding_reservations WHERE id=$1", winning.ID).Scan(&amount))
	require.Equal(t, "10.00000000", balance)
	require.Equal(t, "70.00000000", amount)
	require.ErrorIs(t, r.ResizeFundingReservation(ctx, winning.ID, user, key+1, winning.RequestID, false, "80"), service.ErrTeamFundingForbidden)
	require.ErrorIs(t, r.ResizeFundingReservation(ctx, winning.ID, user, key, "wrong-operation", false, "80"), service.ErrTeamFundingForbidden)
	require.NoError(t, r.MarkPersonalFundingDispatched(ctx, winning.ID, user))
	require.ErrorIs(t, r.ResizeFundingReservation(ctx, winning.ID, user, key, winning.RequestID, false, "80"), service.ErrTeamFundingConflict)
}

func TestFundingResizeSubscriptionFailureRollsBackRefundAndAnchors(t *testing.T) {
	ctx := context.Background()
	r, user, key := personalFundingFixture(t, 0)
	limit := 50.0
	group := teamFundingGroup(t, &service.Group{Name: "resize-sub-" + uuid.NewString(), Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeSubscription, DailyLimitUSD: &limit, WeeklyLimitUSD: &limit, MonthlyLimitUSD: &limit})
	sub := mustCreateSubscription(t, testEntClient(t), &service.UserSubscription{UserID: user, GroupID: group.ID})
	_, err := r.UpdateFunding(ctx, user, &service.FundingUpdateCommand{Mode: service.FundingModeSubscriptionOnly, AllowPersonalSubscription: true})
	require.NoError(t, err)
	command := &service.PersonalFundingReserveCommand{RequestID: uuid.NewString(), APIKeyID: key, MemberUserID: user, GroupID: group.ID, Source: service.FundingSourcePersonalSubscription, SubscriptionID: &sub.ID, Amount: "40"}
	hold, err := r.ReservePersonalFunding(ctx, command)
	require.NoError(t, err)
	require.ErrorIs(t, r.ResizeFundingReservation(ctx, hold.ID, user, key, hold.RequestID, false, "60"), service.ErrTeamFundingConflict)
	var amount, daily, weekly, monthly string
	var anchorsMatch bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT r.amount::text,s.daily_usage_usd::numeric(20,8)::text,s.weekly_usage_usd::numeric(20,8)::text,s.monthly_usage_usd::numeric(20,8)::text,(r.daily_window_start=s.daily_window_start AND r.weekly_window_start=s.weekly_window_start AND r.monthly_window_start=s.monthly_window_start) FROM personal_funding_reservations r JOIN user_subscriptions s ON s.id=r.subscription_id WHERE r.id=$1", hold.ID).Scan(&amount, &daily, &weekly, &monthly, &anchorsMatch))
	require.Equal(t, "40.00000000", amount)
	require.Equal(t, amount, daily)
	require.Equal(t, amount, weekly)
	require.Equal(t, amount, monthly)
	require.True(t, anchorsMatch)
	require.NoError(t, r.ResizeFundingReservation(ctx, hold.ID, user, key, hold.RequestID, false, "45"))
	require.NoError(t, r.ReleasePersonalFunding(ctx, hold.ID, user))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT daily_usage_usd::numeric(20,8)::text FROM user_subscriptions WHERE id=$1", sub.ID).Scan(&daily))
	require.Equal(t, "0.00000000", daily)
}
