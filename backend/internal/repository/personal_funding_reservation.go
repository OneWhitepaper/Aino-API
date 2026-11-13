package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

const personalReservationSQL = `SELECT id,request_id,api_key_id,member_user_id,source,subscription_id,amount::text,settled_amount::text,unauthorized_amount::text,status,daily_window_start,weekly_window_start,monthly_window_start FROM personal_funding_reservations`

type personalReservation struct {
	ID                                                int64
	RequestID                                         string
	APIKeyID, MemberUserID                            int64
	Source                                            string
	SubscriptionID                                    *int64
	Amount, SettledAmount, UnauthorizedAmount, Status string
	Daily, Weekly, Monthly                            *time.Time
}

func scanPersonalReservation(row interface{ Scan(...any) error }) (*personalReservation, error) {
	v := &personalReservation{}
	e := row.Scan(&v.ID, &v.RequestID, &v.APIKeyID, &v.MemberUserID, &v.Source, &v.SubscriptionID, &v.Amount, &v.SettledAmount, &v.UnauthorizedAmount, &v.Status, &v.Daily, &v.Weekly, &v.Monthly)
	return v, e
}
func publicPersonalReservation(v *personalReservation) *service.PersonalFundingReservation {
	return &service.PersonalFundingReservation{ID: v.ID, RequestID: v.RequestID, APIKeyID: v.APIKeyID, MemberUserID: v.MemberUserID, Source: v.Source, SubscriptionID: v.SubscriptionID, Amount: v.Amount, Status: v.Status}
}

func (r *teamFundingRepository) ReservePersonalFunding(ctx context.Context, c *service.PersonalFundingReserveCommand) (*service.PersonalFundingReservation, error) {
	if c == nil || c.RequestID == "" || c.APIKeyID <= 0 || c.MemberUserID <= 0 {
		return nil, service.ErrTeamFundingInvalid
	}
	amount, e := service.ParseTeamFundingAmount(c.Amount)
	if e != nil {
		return nil, e
	}
	copy := *c
	copy.Amount = amount.StringFixed(8)
	c = &copy
	var out *service.PersonalFundingReservation
	e = teamFundingTx(ctx, r.db, func(tx *sql.Tx) error {
		var keyUser int64
		if e := tx.QueryRowContext(ctx, "SELECT user_id FROM api_keys WHERE id=$1 AND deleted_at IS NULL", c.APIKeyID).Scan(&keyUser); e != nil || keyUser != c.MemberUserID {
			return service.ErrTeamFundingForbidden
		}
		var actor int64
		if e := tx.QueryRowContext(ctx, "SELECT id FROM users WHERE id=$1 AND deleted_at IS NULL AND status='active' FOR UPDATE", c.MemberUserID).Scan(&actor); e != nil {
			return service.ErrTeamFundingForbidden
		}
		var mode string
		var allowBalance, allowSub bool
		e := tx.QueryRowContext(ctx, "SELECT mode,allow_personal_balance,allow_personal_subscription FROM funding_preferences WHERE user_id=$1 FOR SHARE", c.MemberUserID).Scan(&mode, &allowBalance, &allowSub)
		if errors.Is(e, sql.ErrNoRows) {
			var team int64
			// This only detects implicit owner opt-in; it does not spend team
			// funds. Shared reservations lock team then owner, so acquiring a
			// team lock after the wallet lock would invert that order.
			if e = tx.QueryRowContext(ctx, "SELECT id FROM teams WHERE owner_user_id=$1 AND status='active' LIMIT 1", c.MemberUserID).Scan(&team); e != nil {
				return service.ErrTeamFundingForbidden
			}
			mode = service.FundingModePersonalFirst
			allowBalance, allowSub = true, true
		} else if e != nil {
			return e
		}
		if mode == service.FundingModeTeamOnly || c.Source == service.FundingSourcePersonalBalance && mode == service.FundingModeSubscriptionOnly || c.Source == service.FundingSourcePersonalSubscription && mode == service.FundingModePersonalOnly {
			return service.ErrTeamFundingForbidden
		}
		var unresolved bool
		if e = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM personal_funding_reservations WHERE member_user_id=$1 AND unauthorized_amount>0)", c.MemberUserID).Scan(&unresolved); e != nil {
			return e
		}
		if unresolved {
			return service.ErrFundingUnavailable
		}
		old, e := scanPersonalReservation(tx.QueryRowContext(ctx, personalReservationSQL+" WHERE request_id=$1 AND api_key_id=$2", c.RequestID, c.APIKeyID))
		if e == nil {
			sameSub := old.SubscriptionID == nil && c.SubscriptionID == nil || old.SubscriptionID != nil && c.SubscriptionID != nil && *old.SubscriptionID == *c.SubscriptionID
			if old.Status != "reserved" || old.MemberUserID != c.MemberUserID || old.Source != c.Source || old.Amount != c.Amount || !sameSub {
				return service.ErrUsageBillingRequestConflict
			}
			out = publicPersonalReservation(old)
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		var windows [3]*time.Time
		switch c.Source {
		case service.FundingSourcePersonalBalance:
			if !allowBalance || c.SubscriptionID != nil {
				return service.ErrTeamFundingForbidden
			}
			res, e := tx.ExecContext(ctx, "UPDATE users SET balance=balance-$2::numeric,updated_at=NOW() WHERE id=$1 AND balance >= $2::numeric", c.MemberUserID, c.Amount)
			if e != nil {
				return e
			}
			n, e := res.RowsAffected()
			if e != nil {
				return e
			}
			if n != 1 {
				return service.ErrTeamFundingConflict
			}
		case service.FundingSourcePersonalSubscription:
			if !allowSub || c.SubscriptionID == nil {
				return service.ErrTeamFundingForbidden
			}
			windows, e = reservePersonalSubscription(ctx, tx, c, amount)
			if e != nil {
				return e
			}
		default:
			return service.ErrTeamFundingInvalid
		}
		var id int64
		e = tx.QueryRowContext(ctx, `INSERT INTO personal_funding_reservations(request_id,api_key_id,member_user_id,source,subscription_id,amount,daily_window_start,weekly_window_start,monthly_window_start) VALUES($1,$2,$3,$4,$5,$6::numeric,$7,$8,$9) RETURNING id`, c.RequestID, c.APIKeyID, c.MemberUserID, c.Source, c.SubscriptionID, c.Amount, windows[0], windows[1], windows[2]).Scan(&id)
		if e != nil {
			return e
		}
		out = &service.PersonalFundingReservation{ID: id, RequestID: c.RequestID, APIKeyID: c.APIKeyID, MemberUserID: c.MemberUserID, Source: c.Source, SubscriptionID: c.SubscriptionID, Amount: c.Amount, Status: "reserved"}
		return nil
	})
	return out, e
}

func (r *teamFundingRepository) ReleasePersonalFunding(ctx context.Context, id, user int64) error {
	return r.releasePersonalFunding(ctx, id, user, false)
}
func (r *teamFundingRepository) ReleaseDispatchedPersonalUnused(ctx context.Context, id, user int64) error {
	return r.releasePersonalFunding(ctx, id, user, true)
}
func (r *teamFundingRepository) releasePersonalFunding(ctx context.Context, id, user int64, dispatched bool) error {
	return teamFundingTx(ctx, r.db, func(tx *sql.Tx) error {
		v, e := scanPersonalReservation(tx.QueryRowContext(ctx, personalReservationSQL+" WHERE id=$1 FOR UPDATE", id))
		if e != nil {
			return e
		}
		if v.MemberUserID != user {
			return service.ErrTeamFundingForbidden
		}
		if v.Status == "released" {
			return nil
		}
		expected := "reserved"
		if dispatched {
			expected = "dispatched"
		}
		if v.Status != expected {
			return service.ErrTeamFundingConflict
		}
		if _, e = refundPersonalEscrow(ctx, tx, v, decimal.RequireFromString(v.Amount)); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, "UPDATE personal_funding_reservations SET status='released',updated_at=NOW() WHERE id=$1 AND status=$2", id, expected)
		return e
	})
}
func (r *teamFundingRepository) MarkPersonalFundingDispatched(ctx context.Context, id, user int64) error {
	res, e := r.db.ExecContext(ctx, "UPDATE personal_funding_reservations SET status='dispatched',updated_at=NOW() WHERE id=$1 AND member_user_id=$2 AND status='reserved'", id, user)
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return service.ErrTeamFundingConflict
	}
	return nil
}
func (r *teamFundingRepository) SettlePersonalFundingReservationTx(ctx context.Context, tx *sql.Tx, id int64, request string, key int64, amount string) (*service.TeamFundingSettlement, error) {
	return SettlePersonalFundingReservationTx(ctx, tx, id, request, key, amount)
}
func SettlePersonalFundingReservationTx(ctx context.Context, tx *sql.Tx, id int64, request string, key int64, amount string) (*service.TeamFundingSettlement, error) {
	actual, e := service.ParseTeamFundingAmount(amount)
	if e != nil {
		return nil, e
	}
	v, e := scanPersonalReservation(tx.QueryRowContext(ctx, personalReservationSQL+" WHERE id=$1 FOR UPDATE", id))
	if e != nil {
		return nil, e
	}
	if v.RequestID != request || v.APIKeyID != key || v.Status == "released" {
		return nil, service.ErrUsageBillingRequestConflict
	}
	if v.Status == "settled" {
		if !decimal.RequireFromString(v.SettledAmount).Add(decimal.RequireFromString(v.UnauthorizedAmount)).Equal(actual) {
			return nil, service.ErrUsageBillingRequestConflict
		}
		return &service.TeamFundingSettlement{ChargedAmount: v.SettledAmount, UnauthorizedAmount: v.UnauthorizedAmount, AlreadySettled: true}, nil
	}
	var actor int64
	if e = tx.QueryRowContext(ctx, "SELECT id FROM users WHERE id=$1 FOR UPDATE", v.MemberUserID).Scan(&actor); e != nil {
		return nil, e
	}
	held := decimal.RequireFromString(v.Amount)
	charged := decimal.Min(actual, held)
	unauthorized := decimal.Max(actual.Sub(held), decimal.Zero)
	balance, e := refundPersonalEscrow(ctx, tx, v, held.Sub(charged))
	if e != nil {
		return nil, e
	}
	_, e = tx.ExecContext(ctx, "UPDATE personal_funding_reservations SET status='settled',settled_amount=$2::numeric,unauthorized_amount=$3::numeric,updated_at=NOW() WHERE id=$1", id, charged.StringFixed(8), unauthorized.StringFixed(8))
	if e != nil {
		return nil, e
	}
	return &service.TeamFundingSettlement{ChargedAmount: charged.StringFixed(8), UnauthorizedAmount: unauthorized.StringFixed(8), NewBalance: balance}, nil
}
func refundPersonalEscrow(ctx context.Context, tx *sql.Tx, v *personalReservation, refund decimal.Decimal) (*float64, error) {
	if v.Source == service.FundingSourcePersonalBalance {
		var balance float64
		e := tx.QueryRowContext(ctx, "UPDATE users SET balance=balance+$2::numeric,updated_at=NOW() WHERE id=$1 RETURNING balance", v.MemberUserID, refund.StringFixed(8)).Scan(&balance)
		return &balance, e
	}
	_, e := tx.ExecContext(ctx, `UPDATE user_subscriptions s SET daily_usage_usd=CASE WHEN s.daily_window_start IS NOT DISTINCT FROM r.daily_window_start THEN GREATEST(s.daily_usage_usd-$2::numeric,0) ELSE s.daily_usage_usd END,weekly_usage_usd=CASE WHEN s.weekly_window_start IS NOT DISTINCT FROM r.weekly_window_start THEN GREATEST(s.weekly_usage_usd-$2::numeric,0) ELSE s.weekly_usage_usd END,monthly_usage_usd=CASE WHEN s.monthly_window_start IS NOT DISTINCT FROM r.monthly_window_start THEN GREATEST(s.monthly_usage_usd-$2::numeric,0) ELSE s.monthly_usage_usd END,updated_at=NOW() FROM personal_funding_reservations r WHERE r.id=$1 AND s.id=r.subscription_id`, v.ID, refund.StringFixed(8))
	return nil, e
}

func (r *teamFundingRepository) ClaimFundingDispatch(ctx context.Context, request string, key, user int64) error {
	var id int64
	e := r.db.QueryRowContext(ctx, `INSERT INTO funding_dispatch_operations(request_id,api_key_id,user_id) VALUES($1,$2,$3) ON CONFLICT(request_id,api_key_id) DO NOTHING RETURNING id`, request, key, user).Scan(&id)
	if errors.Is(e, sql.ErrNoRows) {
		return service.ErrUsageBillingRequestConflict
	}
	return e
}
func (r *teamFundingRepository) AcquireFundingAdmission(ctx context.Context, request string, key, user int64, token string) error {
	var id int64
	e := r.db.QueryRowContext(ctx, `INSERT INTO funding_request_admissions(request_id,api_key_id,user_id,owner_token) VALUES($1,$2,$3,$4) ON CONFLICT(request_id,api_key_id) DO NOTHING RETURNING id`, request, key, user, token).Scan(&id)
	if errors.Is(e, sql.ErrNoRows) {
		return service.ErrUsageBillingRequestConflict
	}
	return e
}
func (r *teamFundingRepository) ReleaseFundingAdmission(ctx context.Context, request string, key, user int64, token string) error {
	_, e := r.db.ExecContext(ctx, `DELETE FROM funding_request_admissions a WHERE request_id=$1 AND api_key_id=$2 AND user_id=$3 AND owner_token=$4 AND NOT EXISTS(SELECT 1 FROM funding_dispatch_operations d WHERE d.request_id=a.request_id AND d.api_key_id=a.api_key_id)`, request, key, user, token)
	return e
}

// The global request claim and source CAS commit together. No failed local
// check can leave a global claim without a dispatched reservation.
func (r *teamFundingRepository) BeginFundingSnapshotDispatch(ctx context.Context, request string, key, user, id int64, shared bool) error {
	return teamFundingTx(ctx, r.db, func(tx *sql.Tx) error {
		var claim int64
		e := tx.QueryRowContext(ctx, `INSERT INTO funding_dispatch_operations(request_id,api_key_id,user_id) VALUES($1,$2,$3) ON CONFLICT(request_id,api_key_id) DO NOTHING RETURNING id`, request, key, user).Scan(&claim)
		if errors.Is(e, sql.ErrNoRows) {
			return service.ErrUsageBillingRequestConflict
		}
		if e != nil {
			return e
		}
		table := "personal_funding_reservations"
		if shared {
			table = "team_funding_reservations"
		}
		res, e := tx.ExecContext(ctx, "UPDATE "+table+" SET status='dispatched',updated_at=NOW() WHERE id=$1 AND member_user_id=$2 AND api_key_id=$3 AND request_id=$4 AND status='reserved'", id, user, key, request)
		if e != nil {
			return e
		}
		n, e := res.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return service.ErrTeamFundingConflict
		}
		return nil
	})
}

func reservePersonalSubscription(ctx context.Context, tx *sql.Tx, c *service.PersonalFundingReserveCommand, amount decimal.Decimal) ([3]*time.Time, error) {
	var windows [3]*time.Time
	var sub service.UserSubscription
	var daily, weekly, monthly string
	var dl, wl, ml *string
	e := tx.QueryRowContext(ctx, `SELECT s.id,s.user_id,s.group_id,s.starts_at,s.expires_at,s.status,s.daily_window_start,s.weekly_window_start,s.monthly_window_start,s.daily_usage_usd::text,s.weekly_usage_usd::text,s.monthly_usage_usd::text,g.daily_limit_usd::text,g.weekly_limit_usd::text,g.monthly_limit_usd::text FROM user_subscriptions s JOIN groups g ON g.id=s.group_id WHERE s.id=$1 AND s.user_id=$2 AND s.group_id=$3 AND s.deleted_at IS NULL AND g.deleted_at IS NULL AND g.status='active' FOR UPDATE OF s FOR SHARE OF g`, *c.SubscriptionID, c.MemberUserID, c.GroupID).Scan(&sub.ID, &sub.UserID, &sub.GroupID, &sub.StartsAt, &sub.ExpiresAt, &sub.Status, &sub.DailyWindowStart, &sub.WeeklyWindowStart, &sub.MonthlyWindowStart, &daily, &weekly, &monthly, &dl, &wl, &ml)
	if errors.Is(e, sql.ErrNoRows) {
		return windows, service.ErrTeamFundingForbidden
	}
	if e != nil {
		return windows, e
	}
	now := time.Now().UTC()
	if sub.Status != "active" || now.Before(sub.StartsAt) || !now.Before(sub.ExpiresAt) {
		return windows, service.ErrTeamFundingForbidden
	}
	windows = [3]*time.Time{sub.DailyWindowStart, sub.WeeklyWindowStart, sub.MonthlyWindowStart}
	usages := []decimal.Decimal{decimal.RequireFromString(daily), decimal.RequireFromString(weekly), decimal.RequireFromString(monthly)}
	limits := []*string{dl, wl, ml}
	dailyStart := timezone.StartOfDay(now)
	if windows[0] == nil || sub.NeedsDailyResetAt(now) {
		windows[0] = &dailyStart
		usages[0] = decimal.Zero
	}
	for i, period := range []time.Duration{7 * 24 * time.Hour, 30 * 24 * time.Hour} {
		j := i + 1
		if windows[j] == nil {
			start := now
			windows[j] = &start
			usages[j] = decimal.Zero
			continue
		}
		anchor := *windows[j]
		legacy := timezone.StartOfDay(sub.StartsAt)
		if legacy.Before(sub.StartsAt) && anchor.Equal(legacy) {
			anchor = sub.StartsAt
		}
		if !now.Before(anchor.Add(period)) && anchor.Add(period).Before(sub.ExpiresAt) {
			periods := now.Sub(anchor) / period
			last := (sub.ExpiresAt.Sub(anchor) - 1) / period
			if periods > last {
				periods = last
			}
			start := anchor.Add(periods * period)
			windows[j] = &start
			usages[j] = decimal.Zero
		}
	}
	for i := range 3 {
		usages[i] = usages[i].Add(amount)
		if limits[i] != nil {
			lim := decimal.RequireFromString(*limits[i])
			if lim.IsPositive() && usages[i].GreaterThan(lim) {
				return windows, service.ErrTeamFundingConflict
			}
		}
	}
	_, e = tx.ExecContext(ctx, "UPDATE user_subscriptions SET daily_window_start=$2,weekly_window_start=$3,monthly_window_start=$4,daily_usage_usd=$5::numeric,weekly_usage_usd=$6::numeric,monthly_usage_usd=$7::numeric,updated_at=NOW() WHERE id=$1", sub.ID, windows[0], windows[1], windows[2], usages[0].String(), usages[1].String(), usages[2].String())
	return windows, e
}
