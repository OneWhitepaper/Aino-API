package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

// incrementUsageBillingSubscriptionProtected protects subscription escrow from
// personal requests that were admitted before sharing was enabled. It preserves
// the legacy increment for subscriptions not explicitly shared by an active team.
// The caller must invoke this inside its usage dedup transaction.
func incrementUsageBillingSubscriptionProtected(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) (string, string, error) {
	if cmd == nil || cmd.SubscriptionID == nil {
		return "", "", service.ErrTeamFundingInvalid
	}
	var owner int64
	var daily, weekly, monthly string
	var dayLimit, weekLimit, monthLimit *string
	err := tx.QueryRowContext(ctx, `SELECT s.user_id,s.daily_usage_usd::text,s.weekly_usage_usd::text,s.monthly_usage_usd::text,g.daily_limit_usd::text,g.weekly_limit_usd::text,g.monthly_limit_usd::text FROM user_subscriptions s JOIN groups g ON g.id=s.group_id WHERE s.id=$1 AND s.deleted_at IS NULL AND g.deleted_at IS NULL FOR UPDATE OF s FOR SHARE OF g`, *cmd.SubscriptionID).Scan(&owner, &daily, &weekly, &monthly, &dayLimit, &weekLimit, &monthLimit)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", service.ErrSubscriptionNotFound
	}
	if err != nil {
		return "", "", err
	}
	var shared bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM team_shared_subscriptions ts JOIN teams t ON t.id=ts.team_id WHERE ts.subscription_id=$1 AND t.owner_user_id=$2 AND t.status='active')`, *cmd.SubscriptionID, owner).Scan(&shared); err != nil {
		return "", "", err
	}
	if !shared {
		return "", "", incrementUsageBillingSubscription(ctx, tx, *cmd.SubscriptionID, cmd.SubscriptionCost)
	}
	requested := decimal.NewFromFloat(cmd.SubscriptionCost).Round(service.UsageBillingMonetaryScale)
	if requested.IsNegative() {
		return "", "", service.ErrTeamFundingInvalid
	}
	charged := requested
	usages := []string{daily, weekly, monthly}
	limits := []*string{dayLimit, weekLimit, monthLimit}
	for i, limitText := range limits {
		if limitText == nil {
			continue
		}
		limit, err := decimal.NewFromString(*limitText)
		if err != nil {
			return "", "", err
		}
		if !limit.IsPositive() {
			continue
		}
		usage, err := decimal.NewFromString(usages[i])
		if err != nil {
			return "", "", err
		}
		available := decimal.Max(limit.Sub(usage), decimal.Zero).Truncate(service.UsageBillingMonetaryScale)
		charged = decimal.Min(charged, available)
	}
	unpaid := requested.Sub(charged)
	if _, err = tx.ExecContext(ctx, `UPDATE user_subscriptions SET daily_usage_usd=daily_usage_usd+$2::numeric,weekly_usage_usd=weekly_usage_usd+$2::numeric,monthly_usage_usd=monthly_usage_usd+$2::numeric,updated_at=NOW() WHERE id=$1`, *cmd.SubscriptionID, charged.StringFixed(8)); err != nil {
		return "", "", err
	}
	if unpaid.IsPositive() {
		_, err = tx.ExecContext(ctx, `INSERT INTO funding_billing_shortfalls(request_id,api_key_id,payer_user_id,subscription_id,source,requested_amount,charged_amount,unpaid_amount) VALUES($1,$2,$3,$4,$5,$6::numeric,$7::numeric,$8::numeric)`, cmd.RequestID, cmd.APIKeyID, owner, *cmd.SubscriptionID, service.FundingSourcePersonalSubscription, requested.StringFixed(8), charged.StringFixed(8), unpaid.StringFixed(8))
		if err != nil {
			return "", "", err
		}
	}
	return charged.StringFixed(8), unpaid.StringFixed(8), nil
}
