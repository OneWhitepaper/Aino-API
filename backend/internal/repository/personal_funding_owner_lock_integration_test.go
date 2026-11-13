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

func TestPersonalFundingImplicitOwnerDoesNotWaitForTeamLock(t *testing.T) {
	repo, owner, _, team, _ := teamFundingFixture(t)
	key := mustCreateApiKey(t, testEntClient(t), &service.APIKey{UserID: owner, Key: "sk-" + uuid.NewString(), Name: "owner personal funding"})
	var preferences int
	require.NoError(t, integrationDB.QueryRow("SELECT count(*) FROM funding_preferences WHERE user_id=$1", owner).Scan(&preferences))
	require.Zero(t, preferences)

	// Shared reservations lock the team before its owner's wallet. A personal
	// reservation already owns the wallet lock, so its opt-in presence check must
	// not wait for this team lock and create the reverse dependency.
	tx, err := integrationDB.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	var lockedTeam int64
	require.NoError(t, tx.QueryRow("SELECT id FROM teams WHERE id=$1 FOR UPDATE", team).Scan(&lockedTeam))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hold, err := repo.(*teamFundingRepository).ReservePersonalFunding(ctx, personalFundingCommand(owner, key.ID, "10"))
	require.NoError(t, err, "personal escrow must not lock the team during implicit owner opt-in")
	require.Equal(t, "10.00000000", hold.Amount)
}
