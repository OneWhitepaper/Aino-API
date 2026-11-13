//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestFundingPoolProtectsMemberEscrowFromLegacyOwnerSettlement(t *testing.T) {
	ctx := context.Background()
	funding, owner, member, team, key := teamFundingFixture(t)
	hold, err := teamFundingReserve(funding, owner, member, team, key, "500")
	require.NoError(t, err)
	ownerKey := mustCreateApiKey(t, testEntClient(t), &service.APIKey{UserID: owner, Name: "owner-inflight", Key: "sk-owner-inflight-" + uuid.NewString()})
	billing := NewUsageBillingRepository(testEntClient(t), integrationDB)
	cmd := &service.UsageBillingCommand{RequestID: uuid.NewString(), APIKeyID: ownerKey.ID, UserID: owner, BalanceCost: 1}
	result, err := billing.Apply(ctx, cmd)
	require.NoError(t, err)
	require.True(t, result.Applied)
	var balance string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance::text FROM users WHERE id=$1", owner).Scan(&balance))
	require.Equal(t, "0.00000000", balance, "an already-running owner request must not overdraw escrow held for a member")
	var requested, charged, unpaid string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT requested_amount::text,charged_amount::text,unpaid_amount::text FROM funding_billing_shortfalls WHERE request_id=$1", cmd.RequestID).Scan(&requested, &charged, &unpaid))
	require.Equal(t, "1.00000000", requested)
	require.Equal(t, "0.00000000", charged)
	require.Equal(t, "1.00000000", unpaid)
	replayed, err := billing.Apply(ctx, cmd)
	require.NoError(t, err)
	require.False(t, replayed.Applied)
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = funding.SettleTeamFundingReservationTx(ctx, tx, hold.ID, hold.RequestID, key, "500")
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance::text FROM users WHERE id=$1", owner).Scan(&balance))
	require.Equal(t, "0.00000000", balance)
}
