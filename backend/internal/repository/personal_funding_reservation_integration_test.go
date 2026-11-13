//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func personalFundingFixture(t *testing.T, balance float64) (*teamFundingRepository, int64, int64) {
	t.Helper()
	u := teamFundingUser(t, balance)
	key := mustCreateApiKey(t, testEntClient(t), &service.APIKey{UserID: u.ID, Key: "sk-" + uuid.NewString(), Name: "personal funding"})
	r := &teamFundingRepository{db: integrationDB}
	_, err := r.UpdateFunding(context.Background(), u.ID, &service.FundingUpdateCommand{Mode: service.FundingModePersonalOnly, AllowPersonalBalance: true})
	require.NoError(t, err)
	return r, u.ID, key.ID
}
func personalFundingCommand(user, key int64, amount string) *service.PersonalFundingReserveCommand {
	return &service.PersonalFundingReserveCommand{RequestID: uuid.NewString(), APIKeyID: key, MemberUserID: user, Source: service.FundingSourcePersonalBalance, Amount: amount}
}
func TestPersonalFundingConcurrentBalanceNeverExceedsPool(t *testing.T) {
	r, u, key := personalFundingFixture(t, 100)
	var success atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := r.ReservePersonalFunding(context.Background(), personalFundingCommand(u, key, "10")); err == nil {
				success.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 10, success.Load())
	var balance string
	require.NoError(t, integrationDB.QueryRow("SELECT balance::text FROM users WHERE id=$1", u).Scan(&balance))
	require.Equal(t, "0.00000000", balance)
}
func TestPersonalFundingSettlementIsAtomicAndDeduplicated(t *testing.T) {
	ctx := context.Background()
	r, u, key := personalFundingFixture(t, 100)
	c := personalFundingCommand(u, key, "40")
	hold, err := r.ReservePersonalFunding(ctx, c)
	require.NoError(t, err)
	again, err := r.ReservePersonalFunding(ctx, c)
	require.NoError(t, err)
	require.Equal(t, hold.ID, again.ID)
	billing := NewUsageBillingRepository(nil, integrationDB)
	cmd := &service.UsageBillingCommand{RequestID: c.RequestID, APIKeyID: key, UserID: u, PayerUserID: u, PersonalFundingReservationID: hold.ID, FundingSource: c.Source, FundingCost: 30}
	result, err := billing.Apply(ctx, cmd)
	require.NoError(t, err)
	require.True(t, result.Applied)
	result, err = billing.Apply(ctx, cmd)
	require.NoError(t, err)
	require.False(t, result.Applied)
	var balance string
	require.NoError(t, integrationDB.QueryRow("SELECT balance::text FROM users WHERE id=$1", u).Scan(&balance))
	require.Equal(t, "70.00000000", balance)
	_, err = r.ReservePersonalFunding(ctx, c)
	require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
}
func TestFundingProvenRejectionRefundsBothSources(t *testing.T) {
	ctx := context.Background()
	r, u, key := personalFundingFixture(t, 100)
	hold, err := r.ReservePersonalFunding(ctx, personalFundingCommand(u, key, "40"))
	require.NoError(t, err)
	require.NoError(t, r.MarkPersonalFundingDispatched(ctx, hold.ID, u))
	require.Error(t, r.ReleasePersonalFunding(ctx, hold.ID, u))
	require.NoError(t, r.ReleaseDispatchedPersonalUnused(ctx, hold.ID, u))
	var balance string
	require.NoError(t, integrationDB.QueryRow("SELECT balance::text FROM users WHERE id=$1", u).Scan(&balance))
	require.Equal(t, "100.00000000", balance)
	shared, owner, member, team, key := teamFundingFixture(t)
	sharedHold, err := teamFundingReserve(shared, owner, member, team, key, "40")
	require.NoError(t, err)
	require.NoError(t, shared.MarkDispatched(ctx, sharedHold.ID, member))
	require.NoError(t, shared.(*teamFundingRepository).ReleaseDispatchedUnused(ctx, sharedHold.ID, member))
	require.NoError(t, integrationDB.QueryRow("SELECT balance::text FROM users WHERE id=$1", owner).Scan(&balance))
	require.Equal(t, "500.00000000", balance)
}
func TestPersonalFundingSubscriptionEscrowRestoresMatchingWindows(t *testing.T) {
	ctx := context.Background()
	r, u, key := personalFundingFixture(t, 0)
	limit := 100.0
	group := mustCreateGroup(t, testEntClient(t), &service.Group{Name: "personal-sub-" + uuid.NewString(), Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeSubscription, DailyLimitUSD: &limit, WeeklyLimitUSD: &limit, MonthlyLimitUSD: &limit})
	sub := mustCreateSubscription(t, testEntClient(t), &service.UserSubscription{UserID: u, GroupID: group.ID})
	_, err := r.UpdateFunding(ctx, u, &service.FundingUpdateCommand{Mode: service.FundingModeSubscriptionOnly, AllowPersonalSubscription: true})
	require.NoError(t, err)
	cmd := &service.PersonalFundingReserveCommand{RequestID: uuid.NewString(), APIKeyID: key, MemberUserID: u, GroupID: group.ID, Source: service.FundingSourcePersonalSubscription, SubscriptionID: &sub.ID, Amount: "60"}
	hold, err := r.ReservePersonalFunding(ctx, cmd)
	require.NoError(t, err)
	var windows int
	require.NoError(t, integrationDB.QueryRow("SELECT ((daily_window_start IS NOT NULL)::int+(weekly_window_start IS NOT NULL)::int+(monthly_window_start IS NOT NULL)::int) FROM personal_funding_reservations WHERE id=$1", hold.ID).Scan(&windows))
	require.Equal(t, 3, windows)
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = r.SettlePersonalFundingReservationTx(ctx, tx, hold.ID, cmd.RequestID, key, "25")
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	var daily string
	require.NoError(t, integrationDB.QueryRow("SELECT daily_usage_usd::numeric(20,8)::text FROM user_subscriptions WHERE id=$1", sub.ID).Scan(&daily))
	require.Equal(t, "25.00000000", daily)
}
func TestFundingDispatchOperationClaimIsSingleAcrossSources(t *testing.T) {
	r, u, key := personalFundingFixture(t, 100)
	var count atomic.Int32
	var wg sync.WaitGroup
	request := "claim-" + uuid.NewString()
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r.ClaimFundingDispatch(context.Background(), request, key, u) == nil {
				count.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, count.Load(), fmt.Sprint("only one operation may dispatch across payer/source retries"))
}

func TestPersonalFundingOverageIsRecordedWithoutAdditionalDebit(t *testing.T) {
	ctx := context.Background()
	r, user, key := personalFundingFixture(t, 100)
	cmd := personalFundingCommand(user, key, "10")
	hold, err := r.ReservePersonalFunding(ctx, cmd)
	require.NoError(t, err)
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	settled, err := r.SettlePersonalFundingReservationTx(ctx, tx, hold.ID, cmd.RequestID, key, "15")
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	require.Equal(t, "10.00000000", settled.ChargedAmount)
	require.Equal(t, "5.00000000", settled.UnauthorizedAmount)
	var balance, unpaid string
	require.NoError(t, integrationDB.QueryRow("SELECT balance::numeric(20,8)::text FROM users WHERE id=$1", user).Scan(&balance))
	require.Equal(t, "90.00000000", balance)
	require.NoError(t, integrationDB.QueryRow("SELECT unauthorized_amount::text FROM personal_funding_reservations WHERE id=$1", hold.ID).Scan(&unpaid))
	require.Equal(t, "5.00000000", unpaid)
	_, err = r.ReservePersonalFunding(ctx, personalFundingCommand(user, key, "1"))
	require.ErrorIs(t, err, service.ErrFundingUnavailable)
}

func TestFundingAdmissionOwnershipAndAtomicDispatch(t *testing.T) {
	ctx := context.Background()
	r, user, key := personalFundingFixture(t, 100)
	cmd := personalFundingCommand(user, key, "10")
	original, duplicate := uuid.NewString(), uuid.NewString()
	require.NoError(t, r.AcquireFundingAdmission(ctx, cmd.RequestID, key, user, original))
	require.ErrorIs(t, r.AcquireFundingAdmission(ctx, cmd.RequestID, key, user, duplicate), service.ErrUsageBillingRequestConflict)
	require.NoError(t, r.ReleaseFundingAdmission(ctx, cmd.RequestID, key, user, duplicate))
	require.ErrorIs(t, r.AcquireFundingAdmission(ctx, cmd.RequestID, key, user, duplicate), service.ErrUsageBillingRequestConflict)
	hold, err := r.ReservePersonalFunding(ctx, cmd)
	require.NoError(t, err)
	// Failed source CAS rolls the global operation claim back too.
	require.Error(t, r.BeginFundingSnapshotDispatch(ctx, cmd.RequestID, key, user, hold.ID+10000000, false))
	require.NoError(t, r.BeginFundingSnapshotDispatch(ctx, cmd.RequestID, key, user, hold.ID, false))
	require.ErrorIs(t, r.BeginFundingSnapshotDispatch(ctx, cmd.RequestID, key, user, hold.ID, false), service.ErrUsageBillingRequestConflict)
	require.Error(t, r.ReleasePersonalFunding(ctx, hold.ID, user))
}
