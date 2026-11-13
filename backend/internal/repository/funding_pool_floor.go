package repository

import (
	"context"
	"database/sql"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

// deductUsageBillingBalanceProtected also covers requests admitted before the
// owner enabled sharing. Escrow has already left users.balance; a legacy
// overdraft may not spend it a second time. Retain any unpaid provider cost for
// reconciliation in the same transaction as the request's dedup claim.
func deductUsageBillingBalanceProtected(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) (float64, bool, string, string, error) {
	payer := usageBillingPayerID(cmd)
	var sharedOwner bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM teams WHERE owner_user_id=$1 AND status='active')", payer).Scan(&sharedOwner); err != nil {
		return 0, false, "", "", err
	}
	if !sharedOwner {
		balance, sufficient, err := deductUsageBillingBalance(ctx, tx, payer, cmd.BalanceCost)
		return balance, sufficient, "", "", err
	}
	var stored string
	if err := tx.QueryRowContext(ctx, "SELECT balance::text FROM users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE", payer).Scan(&stored); err != nil {
		return 0, false, "", "", err
	}
	balance, err := decimal.NewFromString(stored)
	if err != nil {
		return 0, false, "", "", err
	}
	requested := decimal.NewFromFloat(cmd.BalanceCost)
	charged := decimal.Min(requested, decimal.Max(balance, decimal.Zero))
	unpaid := requested.Sub(charged)
	next := balance.Sub(charged)
	if _, err = tx.ExecContext(ctx, "UPDATE users SET balance=$2::numeric,updated_at=NOW() WHERE id=$1", payer, next.StringFixed(8)); err != nil {
		return 0, false, "", "", err
	}
	if unpaid.IsPositive() {
		if _, err = tx.ExecContext(ctx, `INSERT INTO funding_billing_shortfalls(request_id,api_key_id,payer_user_id,requested_amount,charged_amount,unpaid_amount) VALUES($1,$2,$3,$4::numeric,$5::numeric,$6::numeric)`, cmd.RequestID, cmd.APIKeyID, payer, requested.StringFixed(8), charged.StringFixed(8), unpaid.StringFixed(8)); err != nil {
			return 0, false, "", "", err
		}
	}
	nextFloat, _ := next.Float64()
	return nextFloat, unpaid.IsZero(), charged.StringFixed(8), unpaid.StringFixed(8), nil
}
