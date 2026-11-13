//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func teamFundingUser(t *testing.T, balance float64) *service.User {
	t.Helper()
	return mustCreateUser(t, testEntClient(t), &service.User{Email: fmt.Sprintf("team-%s@example.com", uuid.NewString()), PasswordHash: "hash", Balance: balance})
}
func teamFundingFixture(t *testing.T) (service.TeamFundingRepository, int64, int64, int64, int64) {
	t.Helper()
	ctx := context.Background()
	owner := teamFundingUser(t, 500)
	member := teamFundingUser(t, 0)
	repo := NewTeamFundingRepository(testEntClient(t), integrationDB)
	team, e := repo.CreateTeam(ctx, owner.ID, &service.TeamCreateCommand{Name: "shared"})
	require.NoError(t, e)
	invite, e := repo.CreateInvitation(ctx, owner.ID, team.ID, &service.InvitationCreateCommand{UserID: member.ID})
	require.NoError(t, e)
	require.NoError(t, repo.AcceptInvitation(ctx, member.ID, invite.ID))
	key := mustCreateApiKey(t, testEntClient(t), &service.APIKey{UserID: member.ID, Key: "sk-" + uuid.NewString(), Name: "team"})
	_, e = repo.UpdateFunding(ctx, member.ID, &service.FundingUpdateCommand{Mode: "team_only", TeamID: &team.ID})
	require.NoError(t, e)
	return repo, owner.ID, member.ID, team.ID, key.ID
}
func teamFundingCap(t *testing.T, repo service.TeamFundingRepository, owner, team, member int64, cap string) {
	t.Helper()
	_, e := repo.UpdateMember(context.Background(), owner, team, member, &service.TeamMemberPatch{SpendLimit: service.OptionalFundingAmount{Set: true, Value: &cap}})
	require.NoError(t, e)
}
func teamFundingReserve(repo service.TeamFundingRepository, owner, member, team, key int64, amount string) (*service.TeamFundingReservation, error) {
	return repo.Reserve(context.Background(), &service.TeamFundingReserveCommand{RequestID: uuid.NewString(), APIKeyID: key, TeamID: team, MemberUserID: member, PayerUserID: owner, Source: "team_balance", Amount: amount})
}

func TestTeamFundingInvitationsConsentAuthorizationAndRejoinHistory(t *testing.T) {
	ctx := context.Background()
	repo, owner, member, team, key := teamFundingFixture(t)
	outsider := teamFundingUser(t, 0)
	_, e := repo.CreateInvitation(ctx, member, team, &service.InvitationCreateCommand{UserID: outsider.ID})
	require.ErrorIs(t, e, service.ErrTeamFundingForbidden)
	invite, e := repo.CreateInvitation(ctx, owner, team, &service.InvitationCreateCommand{UserID: outsider.ID})
	require.NoError(t, e)
	require.WithinDuration(t, time.Now().Add(7*24*time.Hour), invite.ExpiresAt, time.Minute)
	require.ErrorIs(t, repo.AcceptInvitation(ctx, member, invite.ID), service.ErrTeamFundingForbidden)
	require.NoError(t, repo.AcceptInvitation(ctx, outsider.ID, invite.ID))
	require.NoError(t, repo.AcceptInvitation(ctx, outsider.ID, invite.ID))
	_, e = repo.GetTeam(ctx, teamFundingUser(t, 0).ID, team)
	require.Error(t, e)
	teamFundingCap(t, repo, owner, team, member, "100")
	r, e := teamFundingReserve(repo, owner, member, team, key, "40")
	require.NoError(t, e)
	tx, e := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, e)
	_, e = repo.SettleTeamFundingReservationTx(ctx, tx, r.ID, r.RequestID, key, "30")
	require.NoError(t, e)
	require.NoError(t, tx.Commit())
	require.NoError(t, repo.RemoveMember(ctx, owner, team, member))
	invite, e = repo.CreateInvitation(ctx, owner, team, &service.InvitationCreateCommand{UserID: member})
	require.NoError(t, e)
	require.NoError(t, repo.AcceptInvitation(ctx, member, invite.ID))
	detail, e := repo.GetTeam(ctx, owner, team)
	require.NoError(t, e)
	for _, m := range detail.Members {
		if m.UserID == member {
			require.Equal(t, "30.00000000", m.Spent)
			require.Equal(t, "100.00000000", *m.SpendLimit)
		}
	}
}
func TestTeamFundingStrictConcurrentBudgetPoolAndEscrow(t *testing.T) {
	ctx := context.Background()
	repo, owner, member, team, key := teamFundingFixture(t)
	teamFundingCap(t, repo, owner, team, member, "100")
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := teamFundingReserve(repo, owner, member, team, key, "10"); e == nil {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(10), accepted.Load())
	var balance, reserved string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance::text FROM users WHERE id=$1", owner).Scan(&balance))
	require.Equal(t, "400.00000000", balance)
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT reserved::text FROM team_members WHERE team_id=$1 AND user_id=$2", team, member).Scan(&reserved))
	require.Equal(t, "100.00000000", reserved)
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance::text FROM users WHERE id=$1", member).Scan(&balance))
	require.Equal(t, "0.00000000", balance)
}
func TestTeamFundingSettlementIdempotenceReleaseRollbackAndOverage(t *testing.T) {
	ctx := context.Background()
	repo, owner, member, team, key := teamFundingFixture(t)
	teamFundingCap(t, repo, owner, team, member, "100")
	r, e := teamFundingReserve(repo, owner, member, team, key, "50")
	require.NoError(t, e)
	tx, e := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, e)
	_, e = repo.SettleTeamFundingReservationTx(ctx, tx, r.ID, r.RequestID, key, "30")
	require.NoError(t, e)
	require.NoError(t, tx.Rollback())
	tx, e = integrationDB.BeginTx(ctx, nil)
	require.NoError(t, e)
	result, e := repo.SettleTeamFundingReservationTx(ctx, tx, r.ID, r.RequestID, key, "30")
	require.NoError(t, e)
	require.Equal(t, "30.00000000", result.ChargedAmount)
	require.NoError(t, tx.Commit())
	tx, e = integrationDB.BeginTx(ctx, nil)
	require.NoError(t, e)
	result, e = repo.SettleTeamFundingReservationTx(ctx, tx, r.ID, r.RequestID, key, "30")
	require.NoError(t, e)
	require.True(t, result.AlreadySettled)
	require.NoError(t, tx.Commit())
	page, e := repo.ListLedger(ctx, owner, team, 1, 20)
	require.NoError(t, e)
	require.Len(t, page.Items, 1)
	require.Equal(t, "30.00000000", page.Items[0].Amount)
	r, e = teamFundingReserve(repo, owner, member, team, key, "20")
	require.NoError(t, e)
	require.NoError(t, repo.Release(ctx, r.ID, member))
	require.NoError(t, repo.Release(ctx, r.ID, member))
	r, e = teamFundingReserve(repo, owner, member, team, key, "10")
	require.NoError(t, e)
	require.NoError(t, repo.MarkDispatched(ctx, r.ID, member))
	require.Error(t, repo.Release(ctx, r.ID, member))
	require.NoError(t, repo.RemoveMember(ctx, owner, team, member))
	tx, e = integrationDB.BeginTx(ctx, nil)
	require.NoError(t, e)
	result, e = repo.SettleTeamFundingReservationTx(ctx, tx, r.ID, r.RequestID, key, "15")
	require.NoError(t, e)
	require.Equal(t, "10.00000000", result.ChargedAmount)
	require.Equal(t, "5.00000000", result.UnauthorizedAmount)
	require.NoError(t, tx.Commit())
	invite, e := repo.CreateInvitation(ctx, owner, team, &service.InvitationCreateCommand{UserID: member})
	require.NoError(t, e)
	require.NoError(t, repo.AcceptInvitation(ctx, member, invite.ID))
	_, e = teamFundingReserve(repo, owner, member, team, key, "1")
	require.Error(t, e)
}

func TestTeamFundingOwner500Member100And200RemainIndependent(t *testing.T) {
	ctx := context.Background()
	repo, owner, b, team, bkey := teamFundingFixture(t)
	c := teamFundingUser(t, 0)
	invite, e := repo.CreateInvitation(ctx, owner, team, &service.InvitationCreateCommand{UserID: c.ID})
	require.NoError(t, e)
	require.NoError(t, repo.AcceptInvitation(ctx, c.ID, invite.ID))
	ckey := mustCreateApiKey(t, testEntClient(t), &service.APIKey{UserID: c.ID, Key: "sk-" + uuid.NewString(), Name: "c"})
	_, e = repo.UpdateFunding(ctx, c.ID, &service.FundingUpdateCommand{Mode: "team_only", TeamID: &team})
	require.NoError(t, e)
	teamFundingCap(t, repo, owner, team, b, "100")
	teamFundingCap(t, repo, owner, team, c.ID, "200")
	for _, v := range []struct {
		u, key int64
		amount string
	}{{b, bkey, "100"}, {c.ID, ckey.ID, "200"}} {
		r, e := teamFundingReserve(repo, owner, v.u, team, v.key, v.amount)
		require.NoError(t, e)
		tx, e := integrationDB.BeginTx(ctx, nil)
		require.NoError(t, e)
		_, e = repo.SettleTeamFundingReservationTx(ctx, tx, r.ID, r.RequestID, v.key, v.amount)
		require.NoError(t, e)
		require.NoError(t, tx.Commit())
	}
	var balance string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance::text FROM users WHERE id=$1", owner).Scan(&balance))
	require.Equal(t, "200.00000000", balance)
	_, e = teamFundingReserve(repo, owner, b, team, bkey, "0.00000001")
	require.Error(t, e)
	ownerLedger, e := repo.ListLedger(ctx, owner, team, 1, 20)
	require.NoError(t, e)
	require.Len(t, ownerLedger.Items, 2)
	memberLedger, e := repo.ListLedger(ctx, b, team, 1, 20)
	require.NoError(t, e)
	require.Len(t, memberLedger.Items, 1)
	require.Equal(t, b, memberLedger.Items[0].UserID)
	detail, e := repo.GetTeam(ctx, b, team)
	require.NoError(t, e)
	require.Len(t, detail.Members, 1)
	require.Empty(t, detail.Invitations)
	require.NoError(t, repo.RemoveMember(ctx, b, team, b))
	memberLedger, e = repo.ListLedger(ctx, b, team, 1, 20)
	require.NoError(t, e)
	require.Len(t, memberLedger.Items, 1)
}
func TestTeamFundingUnlimitedDoesNotAllocateOrOverdrawPool(t *testing.T) {
	ctx := context.Background()
	repo, owner, member, team, key := teamFundingFixture(t)
	var count atomic.Int32
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := teamFundingReserve(repo, owner, member, team, key, "100"); e == nil {
				count.Add(1)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(5), count.Load())
	var b string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance::text FROM users WHERE id=$1", owner).Scan(&b))
	require.Equal(t, "0.00000000", b)
	teamFundingCap(t, repo, owner, team, member, "0")
	_, e := teamFundingReserve(repo, owner, member, team, key, "0.00000001")
	require.Error(t, e)
}
func TestTeamFundingRejectsExpiredOrDeclinedInvitationsAndPausedMember(t *testing.T) {
	ctx := context.Background()
	repo, owner, member, team, key := teamFundingFixture(t)
	target := teamFundingUser(t, 0)
	invite, e := repo.CreateInvitation(ctx, owner, team, &service.InvitationCreateCommand{UserID: target.ID})
	require.NoError(t, e)
	_, e = integrationDB.ExecContext(ctx, "UPDATE team_invitations SET expires_at=NOW()-INTERVAL '1 second' WHERE id=$1", invite.ID)
	require.NoError(t, e)
	require.ErrorIs(t, repo.AcceptInvitation(ctx, target.ID, invite.ID), service.ErrTeamFundingConflict)
	invite, e = repo.CreateInvitation(ctx, owner, team, &service.InvitationCreateCommand{UserID: target.ID})
	require.NoError(t, e)
	require.NoError(t, repo.DeclineInvitation(ctx, target.ID, invite.ID))
	require.ErrorIs(t, repo.AcceptInvitation(ctx, target.ID, invite.ID), service.ErrTeamFundingConflict)
	_, e = repo.UpdateMember(ctx, owner, team, member, &service.TeamMemberPatch{Status: "paused"})
	require.NoError(t, e)
	_, e = teamFundingReserve(repo, owner, member, team, key, "1")
	require.ErrorIs(t, e, service.ErrTeamFundingForbidden)
	funding, e := repo.GetFundingContext(ctx, member)
	require.NoError(t, e)
	require.Nil(t, funding.Team)
}

func TestTeamFundingSubscriptionSelectionWindowsAndAtomicRefund(t *testing.T) {
	ctx := context.Background()
	repo, owner, member, team, key := teamFundingFixture(t)
	limit := 100.0
	g := mustCreateGroup(t, testEntClient(t), &service.Group{Name: "team-sub-" + uuid.NewString(), Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeSubscription, DailyLimitUSD: &limit, WeeklyLimitUSD: &limit, MonthlyLimitUSD: &limit})
	sub := mustCreateSubscription(t, testEntClient(t), &service.UserSubscription{UserID: owner, GroupID: g.ID})
	ids := []int64{sub.ID}
	mode := "subscription_only"
	_, e := repo.UpdateTeam(ctx, owner, team, &service.TeamUpdateCommand{SharedMode: &mode, SharedSubscriptionIDs: &ids})
	require.NoError(t, e)
	reserve := func(amount string, group int64) (*service.TeamFundingReservation, error) {
		return repo.Reserve(ctx, &service.TeamFundingReserveCommand{RequestID: uuid.NewString(), APIKeyID: key, TeamID: team, MemberUserID: member, PayerUserID: owner, GroupID: group, Source: "team_subscription", SubscriptionID: &sub.ID, Amount: amount})
	}
	_, e = reserve("1", g.ID+100000)
	require.Error(t, e)
	r, e := reserve("100", g.ID)
	require.NoError(t, e)
	_, e = reserve("0.00000001", g.ID)
	require.Error(t, e)
	tx, e := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, e)
	_, e = repo.SettleTeamFundingReservationTx(ctx, tx, r.ID, r.RequestID, key, "40")
	require.NoError(t, e)
	require.NoError(t, tx.Commit())
	var daily string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT daily_usage_usd::numeric(20,8)::text FROM user_subscriptions WHERE id=$1", sub.ID).Scan(&daily))
	require.Equal(t, "40.00000000", daily)
	r, e = reserve("60", g.ID)
	require.NoError(t, e)
	require.NoError(t, repo.Release(ctx, r.ID, member))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT daily_usage_usd::numeric(20,8)::text FROM user_subscriptions WHERE id=$1", sub.ID).Scan(&daily))
	require.Equal(t, "40.00000000", daily)
	// A rolling window reset must not let a late refund erase newer usage.
	r, e = reserve("10", g.ID)
	require.NoError(t, e)
	_, e = integrationDB.ExecContext(ctx, "UPDATE user_subscriptions SET daily_window_start=NOW()+INTERVAL '1 second',daily_usage_usd=9 WHERE id=$1", sub.ID)
	require.NoError(t, e)
	require.NoError(t, repo.Release(ctx, r.ID, member))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT daily_usage_usd::numeric(20,8)::text FROM user_subscriptions WHERE id=$1", sub.ID).Scan(&daily))
	require.Equal(t, "9.00000000", daily)
	ids = []int64{}
	_, e = repo.UpdateTeam(ctx, owner, team, &service.TeamUpdateCommand{SharedSubscriptionIDs: &ids})
	require.NoError(t, e)
	_, e = reserve("1", g.ID)
	require.Error(t, e)
	var b string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance::text FROM users WHERE id=$1", owner).Scan(&b))
	require.Equal(t, "500.00000000", b)
}

func TestTeamFundingReservationIdempotenceAndCrossSourceMemberCap(t *testing.T) {
	ctx := context.Background()
	repo, owner, member, team, key := teamFundingFixture(t)
	teamFundingCap(t, repo, owner, team, member, "100")
	cmd := &service.TeamFundingReserveCommand{RequestID: uuid.NewString(), APIKeyID: key, TeamID: team, MemberUserID: member, PayerUserID: owner, Source: "team_balance", Amount: "60"}
	a, e := repo.Reserve(ctx, cmd)
	require.NoError(t, e)
	b, e := repo.Reserve(ctx, cmd)
	require.NoError(t, e)
	require.Equal(t, a.ID, b.ID)
	conflict := *cmd
	conflict.Amount = "61"
	_, e = repo.Reserve(ctx, &conflict)
	require.ErrorIs(t, e, service.ErrTeamFundingConflict)
	g := mustCreateGroup(t, testEntClient(t), &service.Group{Name: "mixed-sub-" + uuid.NewString(), Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeSubscription})
	sub := mustCreateSubscription(t, testEntClient(t), &service.UserSubscription{UserID: owner, GroupID: g.ID})
	ids := []int64{sub.ID}
	_, e = repo.UpdateTeam(ctx, owner, team, &service.TeamUpdateCommand{SharedSubscriptionIDs: &ids})
	require.NoError(t, e)
	cmd2 := &service.TeamFundingReserveCommand{RequestID: uuid.NewString(), APIKeyID: key, TeamID: team, MemberUserID: member, PayerUserID: owner, GroupID: g.ID, Source: "team_subscription", SubscriptionID: &sub.ID, Amount: "40"}
	_, e = repo.Reserve(ctx, cmd2)
	require.NoError(t, e)
	cmd2.RequestID = uuid.NewString()
	cmd2.Amount = "0.00000001"
	_, e = repo.Reserve(ctx, cmd2)
	require.ErrorIs(t, e, service.ErrTeamFundingConflict)
	var reserved string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT reserved::text FROM team_members WHERE team_id=$1 AND user_id=$2", team, member).Scan(&reserved))
	require.Equal(t, "100.00000000", reserved)
}

func TestTeamFundingManagementPermissionsAndMissingPreferencesKeepLegacy(t *testing.T) {
	ctx := context.Background()
	repo, owner, member, team, _ := teamFundingFixture(t)
	legacy := teamFundingUser(t, 12)
	funding, e := repo.GetFunding(ctx, legacy.ID)
	require.NoError(t, e)
	require.False(t, funding.Configured)
	require.Equal(t, "12.00000000", funding.PersonalBalance)
	mode := "balance_only"
	_, e = repo.UpdateTeam(ctx, member, team, &service.TeamUpdateCommand{SharedMode: &mode})
	require.ErrorIs(t, e, service.ErrTeamFundingForbidden)
	_, e = repo.UpdateMember(ctx, member, team, member, &service.TeamMemberPatch{Status: "active"})
	require.ErrorIs(t, e, service.ErrTeamFundingForbidden)
	require.ErrorIs(t, repo.RemoveMember(ctx, legacy.ID, team, member), service.ErrTeamFundingForbidden)
	_, e = repo.UpdateFunding(ctx, legacy.ID, &service.FundingUpdateCommand{Mode: "team_only", TeamID: &team})
	require.ErrorIs(t, e, service.ErrTeamFundingForbidden)
	_, e = repo.UpdateFunding(ctx, owner, &service.FundingUpdateCommand{Mode: "team_only", TeamID: &team})
	require.ErrorIs(t, e, service.ErrTeamFundingForbidden)
	other := teamFundingUser(t, 0)
	g := mustCreateGroup(t, testEntClient(t), &service.Group{Name: "foreign-sub-" + uuid.NewString(), Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeSubscription})
	sub := mustCreateSubscription(t, testEntClient(t), &service.UserSubscription{UserID: other.ID, GroupID: g.ID})
	ids := []int64{sub.ID}
	_, e = repo.UpdateTeam(ctx, owner, team, &service.TeamUpdateCommand{SharedSubscriptionIDs: &ids})
	require.ErrorIs(t, e, service.ErrTeamFundingForbidden)
}

func TestTeamFundingDispatchedAndSettledReservationsCannotBeReplayed(t *testing.T) {
	ctx := context.Background()
	repo, owner, member, team, key := teamFundingFixture(t)
	cmd := &service.TeamFundingReserveCommand{RequestID: uuid.NewString(), APIKeyID: key, TeamID: team, MemberUserID: member, PayerUserID: owner, Source: "team_balance", Amount: "2"}
	r, e := repo.Reserve(ctx, cmd)
	require.NoError(t, e)
	require.NoError(t, repo.MarkDispatched(ctx, r.ID, member))
	_, e = repo.Reserve(ctx, cmd)
	require.ErrorIs(t, e, service.ErrTeamFundingConflict)
	tx, e := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, e)
	_, e = repo.SettleTeamFundingReservationTx(ctx, tx, r.ID, r.RequestID, key, "1")
	require.NoError(t, e)
	require.NoError(t, tx.Commit())
	_, e = repo.Reserve(ctx, cmd)
	require.ErrorIs(t, e, service.ErrTeamFundingConflict)
}

func TestTeamFundingZeroCapRejectsEvenZeroCostSharedReservation(t *testing.T) {
	repo, owner, member, team, key := teamFundingFixture(t)
	teamFundingCap(t, repo, owner, team, member, "0")
	_, e := teamFundingReserve(repo, owner, member, team, key, "0")
	require.ErrorIs(t, e, service.ErrTeamFundingConflict)
}
