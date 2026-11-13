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

func (r *teamFundingRepository) GetFundingContext(ctx context.Context, user int64) (*service.FundingContext, error) {
	out := &service.FundingContext{Preferences: service.FundingPreferences{Mode: "personal_first", AllowPersonalBalance: true, AllowPersonalSubscription: true}}
	p := &out.Preferences
	e := r.db.QueryRowContext(ctx, "SELECT mode,team_id,allow_personal_balance,allow_personal_subscription FROM funding_preferences WHERE user_id=$1", user).Scan(&p.Mode, &p.TeamID, &p.AllowPersonalBalance, &p.AllowPersonalSubscription)
	if errors.Is(e, sql.ErrNoRows) {
		e = r.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM teams WHERE owner_user_id=$1 AND status='active')", user).Scan(&p.Configured)
		if e != nil {
			return nil, e
		}
		out.ImplicitOwner = p.Configured
		return out, nil
	}
	if e != nil {
		return nil, e
	}
	p.Configured = true
	if p.TeamID == nil {
		return out, nil
	}
	v := &service.TeamFundingCandidate{SubscriptionIDs: []int64{}}
	e = r.db.QueryRowContext(ctx, `SELECT t.id,m.user_id,t.owner_user_id,COALESCE(m.shared_mode,t.shared_mode),m.spend_limit::text,m.spent::text,m.reserved::text,u.balance::text,m.status FROM teams t JOIN team_members m ON m.team_id=t.id JOIN users u ON u.id=t.owner_user_id WHERE t.id=$1 AND m.user_id=$2 AND t.owner_user_id<>$2 AND t.status='active' AND m.status='active' AND m.unauthorized_amount=0 AND u.deleted_at IS NULL AND u.status='active'`, *p.TeamID, user).Scan(&v.TeamID, &v.MemberUserID, &v.PayerUserID, &v.SharedMode, &v.SpendLimit, &v.Spent, &v.Reserved, &v.AvailableBalance, &v.Status)
	if errors.Is(e, sql.ErrNoRows) {
		return out, nil
	}
	if e != nil {
		return nil, e
	}
	subs, e := teamSubscriptions(ctx, r.db, v.PayerUserID, &v.TeamID)
	if e != nil {
		return nil, e
	}
	for _, s := range subs {
		v.SubscriptionIDs = append(v.SubscriptionIDs, s.ID)
	}
	out.Team = v
	return out, nil
}

const teamReservationSQL = `SELECT id,request_id,api_key_id,team_id,member_user_id,payer_user_id,source,subscription_id,amount::text,settled_amount::text,unauthorized_amount::text,status,created_at FROM team_funding_reservations`

func scanTeamReservation(row interface{ Scan(...any) error }) (*service.TeamFundingReservation, error) {
	v := &service.TeamFundingReservation{}
	e := row.Scan(&v.ID, &v.RequestID, &v.APIKeyID, &v.TeamID, &v.MemberUserID, &v.PayerUserID, &v.Source, &v.SubscriptionID, &v.Amount, &v.SettledAmount, &v.UnauthorizedAmount, &v.Status, &v.CreatedAt)
	v.ReservedAmount = v.Amount
	return v, e
}
func sameReservation(v *service.TeamFundingReservation, c *service.TeamFundingReserveCommand) bool {
	d, e := service.ParseTeamFundingAmount(c.Amount)
	if e != nil {
		return false
	}
	sub := int64(0)
	if v.SubscriptionID != nil {
		sub = *v.SubscriptionID
	}
	want := int64(0)
	if c.SubscriptionID != nil {
		want = *c.SubscriptionID
	}
	return v.RequestID == c.RequestID && v.APIKeyID == c.APIKeyID && v.TeamID == c.TeamID && v.MemberUserID == c.MemberUserID && v.PayerUserID == c.PayerUserID && v.Source == c.Source && sub == want && v.Amount == d.StringFixed(8) && v.Status == "reserved"
}
func (r *teamFundingRepository) Reserve(ctx context.Context, c *service.TeamFundingReserveCommand) (*service.TeamFundingReservation, error) {
	if e := service.ValidateReserve(c); e != nil {
		return nil, e
	}
	amount, _ := service.ParseTeamFundingAmount(c.Amount)
	c2 := *c
	c2.Amount = amount.StringFixed(8)
	c = &c2
	var out *service.TeamFundingReservation
	e := teamFundingTx(ctx, r.db, func(tx *sql.Tx) error {
		var owner int64
		var teamState, teamMode string
		if e := tx.QueryRowContext(ctx, "SELECT owner_user_id,status,shared_mode FROM teams WHERE id=$1 FOR UPDATE", c.TeamID).Scan(&owner, &teamState, &teamMode); e != nil {
			return service.ErrTeamFundingNotFound
		}
		old, e := scanTeamReservation(tx.QueryRowContext(ctx, teamReservationSQL+" WHERE request_id=$1 AND api_key_id=$2", c.RequestID, c.APIKeyID))
		if e == nil {
			if !sameReservation(old, c) {
				return service.ErrUsageBillingRequestConflict
			}
			out = old
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if owner != c.PayerUserID || owner == c.MemberUserID || teamState != "active" {
			return service.ErrTeamFundingForbidden
		}
		var status, spent, reserved, unauthorized string
		var limit, override *string
		e = tx.QueryRowContext(ctx, "SELECT status,spend_limit::text,spent::text,reserved::text,unauthorized_amount::text,shared_mode FROM team_members WHERE team_id=$1 AND user_id=$2 FOR UPDATE", c.TeamID, c.MemberUserID).Scan(&status, &limit, &spent, &reserved, &unauthorized, &override)
		if e != nil || status != "active" || decimal.RequireFromString(unauthorized).IsPositive() {
			return service.ErrTeamFundingForbidden
		}
		if limit != nil {
			cap := decimal.RequireFromString(*limit)
			if !cap.IsPositive() || decimal.RequireFromString(spent).Add(decimal.RequireFromString(reserved)).Add(amount).GreaterThan(cap) {
				return service.ErrTeamFundingConflict
			}
		}
		if override != nil {
			teamMode = *override
		}
		if (teamMode == "subscription_only" && c.Source != "team_subscription") || (teamMode == "balance_only" && c.Source != "team_balance") {
			return service.ErrTeamFundingForbidden
		}
		var selected *int64
		var prefMode string
		e = tx.QueryRowContext(ctx, "SELECT mode,team_id FROM funding_preferences WHERE user_id=$1 FOR SHARE", c.MemberUserID).Scan(&prefMode, &selected)
		if e != nil || selected == nil || *selected != c.TeamID || prefMode == "personal_only" || prefMode == "subscription_only" {
			return service.ErrTeamFundingForbidden
		}
		var keyUser int64
		if e := tx.QueryRowContext(ctx, "SELECT user_id FROM api_keys WHERE id=$1 AND deleted_at IS NULL", c.APIKeyID).Scan(&keyUser); e != nil || keyUser != c.MemberUserID {
			return service.ErrTeamFundingForbidden
		}
		var payerState string
		if e := tx.QueryRowContext(ctx, "SELECT status FROM users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE", owner).Scan(&payerState); e != nil || payerState != "active" {
			return service.ErrTeamFundingForbidden
		}
		var windows [3]*time.Time
		if c.Source == "team_balance" {
			if c.SubscriptionID != nil {
				return service.ErrTeamFundingInvalid
			}
			res, e := tx.ExecContext(ctx, "UPDATE users SET balance=balance-$2::numeric,updated_at=NOW() WHERE id=$1 AND balance >= $2::numeric", owner, c.Amount)
			if e != nil {
				return e
			}
			n, e := res.RowsAffected()
			if e != nil {
				return e
			}
			if n == 0 {
				return service.ErrTeamFundingConflict
			}
		} else {
			var e error
			windows, e = reserveTeamSubscription(ctx, tx, c, amount)
			if e != nil {
				return e
			}
		}
		if _, e := tx.ExecContext(ctx, "UPDATE team_members SET reserved=reserved+$3::numeric,updated_at=NOW() WHERE team_id=$1 AND user_id=$2", c.TeamID, c.MemberUserID, c.Amount); e != nil {
			return e
		}
		var id int64
		if e := tx.QueryRowContext(ctx, `INSERT INTO team_funding_reservations(request_id,api_key_id,team_id,member_user_id,payer_user_id,source,subscription_id,amount,daily_window_start,weekly_window_start,monthly_window_start) VALUES($1,$2,$3,$4,$5,$6,$7,$8::numeric,$9,$10,$11) RETURNING id`, c.RequestID, c.APIKeyID, c.TeamID, c.MemberUserID, owner, c.Source, c.SubscriptionID, c.Amount, windows[0], windows[1], windows[2]).Scan(&id); e != nil {
			return e
		}
		out, e = scanTeamReservation(tx.QueryRowContext(ctx, teamReservationSQL+" WHERE id=$1", id))
		return e
	})
	return out, e
}

func reserveTeamSubscription(ctx context.Context, tx *sql.Tx, c *service.TeamFundingReserveCommand, amount decimal.Decimal) ([3]*time.Time, error) {
	var windows [3]*time.Time
	var sub service.UserSubscription
	var daily, weekly, monthly string
	var dl, wl, ml *string
	e := tx.QueryRowContext(ctx, `SELECT s.id,s.user_id,s.group_id,s.starts_at,s.expires_at,s.status,s.daily_window_start,s.weekly_window_start,s.monthly_window_start,s.daily_usage_usd::text,s.weekly_usage_usd::text,s.monthly_usage_usd::text,g.daily_limit_usd::text,g.weekly_limit_usd::text,g.monthly_limit_usd::text FROM user_subscriptions s JOIN groups g ON g.id=s.group_id JOIN team_shared_subscriptions ts ON ts.subscription_id=s.id WHERE s.id=$1 AND s.user_id=$2 AND s.group_id=$3 AND ts.team_id=$4 AND s.deleted_at IS NULL AND g.deleted_at IS NULL AND g.status='active' FOR UPDATE OF s FOR SHARE OF g`, *c.SubscriptionID, c.PayerUserID, c.GroupID, c.TeamID).Scan(&sub.ID, &sub.UserID, &sub.GroupID, &sub.StartsAt, &sub.ExpiresAt, &sub.Status, &sub.DailyWindowStart, &sub.WeeklyWindowStart, &sub.MonthlyWindowStart, &daily, &weekly, &monthly, &dl, &wl, &ml)
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
func (r *teamFundingRepository) MarkDispatched(ctx context.Context, id, user int64) error {
	res, e := r.db.ExecContext(ctx, "UPDATE team_funding_reservations SET status='dispatched',updated_at=NOW() WHERE id=$1 AND member_user_id=$2 AND status='reserved'", id, user)
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if n == 0 {
		return service.ErrTeamFundingConflict
	}
	return e
}
func (r *teamFundingRepository) Release(ctx context.Context, id, user int64) error {
	return teamFundingTx(ctx, r.db, func(tx *sql.Tx) error {
		v, e := scanTeamReservation(tx.QueryRowContext(ctx, teamReservationSQL+" WHERE id=$1 FOR UPDATE", id))
		if e != nil {
			return e
		}
		if v.MemberUserID != user {
			return service.ErrTeamFundingForbidden
		}
		if v.Status == "released" {
			return nil
		}
		if v.Status != "reserved" {
			return service.ErrTeamFundingConflict
		}
		if _, e := tx.ExecContext(ctx, "UPDATE team_members SET reserved=reserved-$3::numeric,updated_at=NOW() WHERE team_id=$1 AND user_id=$2", v.TeamID, v.MemberUserID, v.Amount); e != nil {
			return e
		}
		if _, e := refundTeamEscrow(ctx, tx, v, v.Amount); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, "UPDATE team_funding_reservations SET status='released',updated_at=NOW() WHERE id=$1", id)
		return e
	})
}
func (r *teamFundingRepository) SettleTeamFundingReservationTx(ctx context.Context, tx *sql.Tx, id int64, request string, key int64, amount string) (*service.TeamFundingSettlement, error) {
	return SettleTeamFundingReservationTx(ctx, tx, id, request, key, amount)
}

// SettleTeamFundingReservationTx joins the existing usage dedup transaction. It is
// the only shared-pool settlement path; callers must skip their ordinary payer debit.
func SettleTeamFundingReservationTx(ctx context.Context, tx *sql.Tx, id int64, request string, key int64, amount string) (*service.TeamFundingSettlement, error) {
	actual, e := service.ParseTeamFundingAmount(amount)
	if e != nil {
		return nil, e
	}
	v, e := scanTeamReservation(tx.QueryRowContext(ctx, teamReservationSQL+" WHERE id=$1 FOR UPDATE", id))
	if e != nil {
		return nil, e
	}
	if v.RequestID != request || v.APIKeyID != key {
		return nil, service.ErrTeamFundingConflict
	}
	if v.Status == "released" {
		return nil, service.ErrTeamFundingConflict
	}
	if v.Status == "settled" {
		if !decimal.RequireFromString(v.SettledAmount).Add(decimal.RequireFromString(v.UnauthorizedAmount)).Equal(actual) {
			return nil, service.ErrTeamFundingConflict
		}
		return &service.TeamFundingSettlement{ChargedAmount: v.SettledAmount, UnauthorizedAmount: v.UnauthorizedAmount, AlreadySettled: true}, nil
	}
	hold := decimal.RequireFromString(v.Amount)
	charged := decimal.Min(actual, hold)
	over := decimal.Max(actual.Sub(hold), decimal.Zero)
	refund := hold.Sub(charged)
	if _, e := tx.ExecContext(ctx, "UPDATE team_members SET reserved=reserved-$3::numeric,spent=spent+$4::numeric,unauthorized_amount=unauthorized_amount+$5::numeric,updated_at=NOW() WHERE team_id=$1 AND user_id=$2", v.TeamID, v.MemberUserID, v.Amount, charged.StringFixed(8), over.StringFixed(8)); e != nil {
		return nil, e
	}
	balance, e := refundTeamEscrow(ctx, tx, v, refund.StringFixed(8))
	if e != nil {
		return nil, e
	}
	if _, e := tx.ExecContext(ctx, `INSERT INTO team_funding_ledger(reservation_id,request_id,team_id,user_id,payer_user_id,source,subscription_id,amount,unauthorized_amount) VALUES($1,$2,$3,$4,$5,$6,$7,$8::numeric,$9::numeric)`, id, request, v.TeamID, v.MemberUserID, v.PayerUserID, v.Source, v.SubscriptionID, actual.StringFixed(8), over.StringFixed(8)); e != nil {
		return nil, e
	}
	if _, e := tx.ExecContext(ctx, "UPDATE team_funding_reservations SET status='settled',settled_amount=$2::numeric,unauthorized_amount=$3::numeric,updated_at=NOW() WHERE id=$1", id, charged.StringFixed(8), over.StringFixed(8)); e != nil {
		return nil, e
	}
	return &service.TeamFundingSettlement{ChargedAmount: charged.StringFixed(8), UnauthorizedAmount: over.StringFixed(8), NewBalance: balance}, nil
}
func refundTeamEscrow(ctx context.Context, tx *sql.Tx, v *service.TeamFundingReservation, amount string) (*float64, error) {
	if v.Source == "team_balance" {
		var balance float64
		e := tx.QueryRowContext(ctx, "UPDATE users SET balance=balance+$2::numeric,updated_at=NOW() WHERE id=$1 RETURNING balance", v.PayerUserID, amount).Scan(&balance)
		return &balance, e
	}
	_, e := tx.ExecContext(ctx, `UPDATE user_subscriptions s SET daily_usage_usd=CASE WHEN s.daily_window_start IS NOT DISTINCT FROM r.daily_window_start THEN GREATEST(s.daily_usage_usd-$2::numeric,0) ELSE s.daily_usage_usd END,weekly_usage_usd=CASE WHEN s.weekly_window_start IS NOT DISTINCT FROM r.weekly_window_start THEN GREATEST(s.weekly_usage_usd-$2::numeric,0) ELSE s.weekly_usage_usd END,monthly_usage_usd=CASE WHEN s.monthly_window_start IS NOT DISTINCT FROM r.monthly_window_start THEN GREATEST(s.monthly_usage_usd-$2::numeric,0) ELSE s.monthly_usage_usd END,updated_at=NOW() FROM team_funding_reservations r WHERE r.id=$1 AND s.id=r.subscription_id`, v.ID, amount)
	return nil, e
}
