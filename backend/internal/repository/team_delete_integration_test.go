//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type teamDeleter interface {
	DeleteTeam(context.Context, int64, int64) error
}

func requireTeamDeleter(t *testing.T, repo service.TeamFundingRepository) teamDeleter {
	t.Helper()
	d, ok := repo.(teamDeleter)
	require.True(t, ok, "team owners need a delete operation")
	return d
}

func TestTeamDeletionOwnerPermissionsVisibilityAndConsent(t *testing.T) {
	ctx := context.Background()
	repo, owner, member, team, key := teamFundingFixture(t)
	d := requireTeamDeleter(t, repo)
	outsider := teamFundingUser(t, 17)
	_, err := integrationDB.ExecContext(ctx, "UPDATE users SET balance=23 WHERE id=$1", member)
	require.NoError(t, err)
	_, err = repo.UpdateFunding(ctx, member, &service.FundingUpdateCommand{Mode: service.FundingModeTeamOnly, TeamID: &team, AllowPersonalBalance: true})
	require.NoError(t, err)
	pending, err := repo.CreateInvitation(ctx, owner, team, &service.InvitationCreateCommand{UserID: outsider.ID})
	require.NoError(t, err)
	other, err := repo.CreateTeam(ctx, owner, &service.TeamCreateCommand{Name: "keep this team"})
	require.NoError(t, err)
	otherInvite, err := repo.CreateInvitation(ctx, owner, other.ID, &service.InvitationCreateCommand{UserID: outsider.ID})
	require.NoError(t, err)

	require.ErrorIs(t, d.DeleteTeam(ctx, member, team), service.ErrTeamFundingForbidden)
	require.ErrorIs(t, d.DeleteTeam(ctx, outsider.ID, team), service.ErrTeamFundingForbidden)
	require.ErrorIs(t, d.DeleteTeam(ctx, owner, 9223372036854775807), service.ErrTeamFundingNotFound)
	require.NoError(t, d.DeleteTeam(ctx, owner, team))
	require.NoError(t, d.DeleteTeam(ctx, owner, team), "owner retries are idempotent")
	require.ErrorIs(t, d.DeleteTeam(ctx, outsider.ID, team), service.ErrTeamFundingForbidden)

	var state string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT status FROM teams WHERE id=$1", team).Scan(&state))
	require.Equal(t, "deleted", state)
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT status FROM team_members WHERE team_id=$1 AND user_id=$2", team, member).Scan(&state))
	require.Equal(t, "removed", state)
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT status FROM team_invitations WHERE id=$1", pending.ID).Scan(&state))
	require.Equal(t, "revoked", state)
	invites, err := repo.ListInvitations(ctx, outsider.ID)
	require.NoError(t, err)
	require.Len(t, invites, 1)
	require.Equal(t, otherInvite.ID, invites[0].ID)
	owned, err := repo.ListTeams(ctx, owner)
	require.NoError(t, err)
	require.Len(t, owned.Owned, 1)
	require.Equal(t, other.ID, owned.Owned[0].ID)
	joined, err := repo.ListTeams(ctx, member)
	require.NoError(t, err)
	require.Empty(t, joined.Joined)
	funding, err := repo.GetFunding(ctx, member)
	require.NoError(t, err)
	require.True(t, funding.Configured)
	require.Nil(t, funding.TeamID)
	require.Equal(t, service.FundingModeTeamOnly, funding.Mode)
	require.True(t, funding.AllowPersonalBalance)
	require.False(t, funding.AllowPersonalSubscription)
	require.Equal(t, "23.00000000", funding.PersonalBalance)
	contextData, err := repo.GetFundingContext(ctx, member)
	require.NoError(t, err)
	require.Nil(t, contextData.Team)

	_, err = repo.GetTeam(ctx, owner, team)
	require.ErrorIs(t, err, service.ErrTeamFundingNotFound)
	_, err = repo.GetTeam(ctx, member, team)
	require.ErrorIs(t, err, service.ErrTeamFundingNotFound)
	name := "cannot revive"
	_, err = repo.UpdateTeam(ctx, owner, team, &service.TeamUpdateCommand{Name: &name})
	require.ErrorIs(t, err, service.ErrTeamFundingNotFound)
	_, err = repo.CreateInvitation(ctx, owner, team, &service.InvitationCreateCommand{UserID: outsider.ID})
	require.ErrorIs(t, err, service.ErrTeamFundingNotFound)
	_, err = repo.UpdateMember(ctx, owner, team, member, &service.TeamMemberPatch{Status: service.TeamMemberActive})
	require.ErrorIs(t, err, service.ErrTeamFundingNotFound)
	require.ErrorIs(t, repo.RemoveMember(ctx, owner, team, member), service.ErrTeamFundingNotFound)
	require.ErrorIs(t, repo.RevokeInvitation(ctx, owner, team, pending.ID), service.ErrTeamFundingNotFound)
	require.ErrorIs(t, repo.AcceptInvitation(ctx, outsider.ID, pending.ID), service.ErrTeamFundingNotFound)
	var acceptedID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT id FROM team_invitations WHERE team_id=$1 AND user_id=$2 AND status='accepted'", team, member).Scan(&acceptedID))
	require.ErrorIs(t, repo.AcceptInvitation(ctx, member, acceptedID), service.ErrTeamFundingNotFound)
	_, err = repo.UpdateFunding(ctx, member, &service.FundingUpdateCommand{Mode: service.FundingModeTeamOnly, TeamID: &team})
	require.ErrorIs(t, err, service.ErrTeamFundingForbidden)
	_, err = teamFundingReserve(repo, owner, member, team, key, "1")
	require.ErrorIs(t, err, service.ErrTeamFundingForbidden)
}

func TestTeamDeletionPreservesAcceptedReservationsAndLedger(t *testing.T) {
	for _, state := range []string{"reserved", "queued", "dispatched"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			repo, owner, member, team, key := teamFundingFixture(t)
			d := requireTeamDeleter(t, repo)
			historical, err := teamFundingReserve(repo, owner, member, team, key, "20")
			require.NoError(t, err)
			settle := func(reservation *service.TeamFundingReservation, amount string) {
				t.Helper()
				tx, err := integrationDB.BeginTx(ctx, nil)
				require.NoError(t, err)
				defer func() { _ = tx.Rollback() }()
				_, err = repo.SettleTeamFundingReservationTx(ctx, tx, reservation.ID, reservation.RequestID, key, amount)
				require.NoError(t, err)
				require.NoError(t, tx.Commit())
			}
			settle(historical, "15")
			pending, err := teamFundingReserve(repo, owner, member, team, key, "50")
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(ctx, "UPDATE team_funding_reservations SET status=$2 WHERE id=$1", pending.ID, state)
			require.NoError(t, err)
			require.NoError(t, d.DeleteTeam(ctx, owner, team))
			var got, balance, reserved string
			require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT status FROM team_funding_reservations WHERE id=$1", pending.ID).Scan(&got))
			require.Equal(t, state, got)
			require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance::text FROM users WHERE id=$1", owner).Scan(&balance))
			require.Equal(t, "435.00000000", balance, "deleting does not refund accepted work")
			require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT reserved::text FROM team_members WHERE team_id=$1 AND user_id=$2", team, member).Scan(&reserved))
			require.Equal(t, "50.00000000", reserved)
			settle(pending, "30")
			settle(pending, "30")
			require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance::text FROM users WHERE id=$1", owner).Scan(&balance))
			require.Equal(t, "455.00000000", balance)
			for _, actor := range []int64{owner, member} {
				ledger, err := repo.ListLedger(ctx, actor, team, 1, 20)
				require.NoError(t, err)
				require.Equal(t, 2, ledger.Total)
				for _, entry := range ledger.Items {
					require.Equal(t, owner, entry.PayerUserID)
					require.Equal(t, member, entry.UserID)
				}
			}
			outsider := teamFundingUser(t, 0)
			_, err = repo.ListLedger(ctx, outsider.ID, team, 1, 20)
			require.ErrorIs(t, err, service.ErrTeamFundingForbidden)
		})
	}
}

func TestTeamDeletionSerializesWithNewReservations(t *testing.T) {
	ctx := context.Background()
	repo, owner, member, team, key := teamFundingFixture(t)
	d := requireTeamDeleter(t, repo)
	start := make(chan struct{})
	errors := make(chan error, 11)
	var wg sync.WaitGroup
	var accepted atomic.Int64
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		errors <- d.DeleteTeam(ctx, owner, team)
	}()
	for i := range 10 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := repo.Reserve(ctx, &service.TeamFundingReserveCommand{RequestID: fmt.Sprintf("delete-race-%d-%d", team, i), APIKeyID: key, TeamID: team, MemberUserID: member, PayerUserID: owner, Source: service.TeamFundingSourceBalance, Amount: "1"})
			if err == nil {
				accepted.Add(1)
			} else if err != service.ErrTeamFundingForbidden {
				errors <- err
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	var balance, reserved int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance::bigint FROM users WHERE id=$1", owner).Scan(&balance))
	require.Equal(t, 500-accepted.Load(), balance)
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT reserved::bigint FROM team_members WHERE team_id=$1 AND user_id=$2", team, member).Scan(&reserved))
	require.Equal(t, accepted.Load(), reserved)
	_, err := teamFundingReserve(repo, owner, member, team, key, "1")
	require.ErrorIs(t, err, service.ErrTeamFundingForbidden)
}
