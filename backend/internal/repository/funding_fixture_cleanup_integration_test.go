//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// CleanupFundingIntegrationUsers removes only this fixture's users and their
// funding dependencies. Funding tests need committed rows for concurrent
// transactions, so the shared integration database cannot roll them back.
func CleanupFundingIntegrationUsers(t *testing.T, userIDs ...int64) {
	t.Helper()
	ctx := context.Background()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	for _, query := range []string{
		`DELETE FROM team_funding_ledger WHERE user_id = ANY($1) OR payer_user_id = ANY($1) OR team_id IN (SELECT id FROM teams WHERE owner_user_id = ANY($1))`,
		`DELETE FROM team_funding_reservations WHERE member_user_id = ANY($1) OR payer_user_id = ANY($1) OR team_id IN (SELECT id FROM teams WHERE owner_user_id = ANY($1))`,
		`DELETE FROM personal_funding_reservations WHERE member_user_id = ANY($1)`,
		`DELETE FROM funding_billing_shortfalls WHERE payer_user_id = ANY($1) OR api_key_id IN (SELECT id FROM api_keys WHERE user_id = ANY($1))`,
		`DELETE FROM funding_dispatch_operations WHERE user_id = ANY($1)`,
		`DELETE FROM funding_request_admissions WHERE user_id = ANY($1)`,
		`DELETE FROM async_funding_tasks WHERE actor_user_id = ANY($1)`,
		`DELETE FROM gemini_cached_contents WHERE user_id = ANY($1)`,
		`DELETE FROM batch_image_jobs WHERE user_id = ANY($1)`,
		`DELETE FROM funding_preferences WHERE user_id = ANY($1) OR team_id IN (SELECT id FROM teams WHERE owner_user_id = ANY($1))`,
		`DELETE FROM team_shared_subscriptions WHERE team_id IN (SELECT id FROM teams WHERE owner_user_id = ANY($1)) OR subscription_id IN (SELECT id FROM user_subscriptions WHERE user_id = ANY($1))`,
		`DELETE FROM team_invitations WHERE user_id = ANY($1) OR team_id IN (SELECT id FROM teams WHERE owner_user_id = ANY($1))`,
		`DELETE FROM team_members WHERE user_id = ANY($1) OR team_id IN (SELECT id FROM teams WHERE owner_user_id = ANY($1))`,
		`DELETE FROM teams WHERE owner_user_id = ANY($1)`,
		`DELETE FROM usage_billing_dedup WHERE api_key_id IN (SELECT id FROM api_keys WHERE user_id = ANY($1))`,
		`DELETE FROM usage_billing_dedup_archive WHERE api_key_id IN (SELECT id FROM api_keys WHERE user_id = ANY($1))`,
		`DELETE FROM api_keys WHERE user_id = ANY($1)`,
		`DELETE FROM user_subscriptions WHERE user_id = ANY($1)`,
		`DELETE FROM users WHERE id = ANY($1)`,
	} {
		_, err = tx.ExecContext(ctx, query, pq.Array(userIDs))
		require.NoError(t, err, query)
	}
	require.NoError(t, tx.Commit())
}

func teamFundingGroup(t *testing.T, group *service.Group) *service.Group {
	t.Helper()
	created := mustCreateGroup(t, testEntClient(t), group)
	t.Cleanup(func() {
		ctx := context.Background()
		tx, err := integrationDB.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback() }()
		// Group deletion cascades to subscriptions; remove their non-cascading
		// funding references first, before the users' later cleanup callbacks.
		for _, table := range []string{"team_funding_ledger", "team_funding_reservations", "personal_funding_reservations", "funding_billing_shortfalls", "team_shared_subscriptions"} {
			_, err = tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE subscription_id IN (SELECT id FROM user_subscriptions WHERE group_id=$1)", created.ID)
			require.NoError(t, err, table)
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM groups WHERE id=$1", created.ID)
		require.NoError(t, err)
		require.NoError(t, tx.Commit())
	})
	return created
}
