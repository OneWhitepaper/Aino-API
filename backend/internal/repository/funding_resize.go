package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

// ResizeFundingReservation preserves the accepted source and atomically grows
// its escrow. Refund/re-reserve of subscriptions uses original window anchors,
// so a window rollover cannot carry old escrow into a new budget.
func (r *teamFundingRepository) ResizeFundingReservation(ctx context.Context, id, user, key int64, request string, shared bool, text string) error {
	target, err := service.ParseTeamFundingAmount(text)
	if err != nil {
		return err
	}
	return teamFundingTx(ctx, r.db, func(tx *sql.Tx) error {
		var queueState string
		err := tx.QueryRowContext(ctx, "SELECT state FROM async_funding_tasks WHERE operation_id=$1 AND api_key_id=$2 AND actor_user_id=$3 FOR UPDATE", request, key, user).Scan(&queueState)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && queueState != "queued" {
			return service.ErrTeamFundingConflict
		}

		if shared {
			v, e := scanTeamReservation(tx.QueryRowContext(ctx, teamReservationSQL+" WHERE id=$1 FOR UPDATE", id))
			if e != nil {
				return e
			}
			if v.MemberUserID != user || v.APIKeyID != key || v.RequestID != request {
				return service.ErrTeamFundingForbidden
			}
			if v.Status != "reserved" && v.Status != "queued" || v.Status == "queued" && queueState != "queued" {
				return service.ErrTeamFundingConflict
			}
			old := decimal.RequireFromString(v.Amount)
			if !target.GreaterThan(old) {
				return nil
			}
			delta := target.Sub(old)
			var owner int64
			var state, mode string
			if e = tx.QueryRowContext(ctx, "SELECT owner_user_id,status,shared_mode FROM teams WHERE id=$1 FOR UPDATE", v.TeamID).Scan(&owner, &state, &mode); e != nil {
				return e
			}
			if owner != v.PayerUserID || state != "active" {
				return service.ErrTeamFundingForbidden
			}
			var status, spent, reserved, unauthorized string
			var limit, override *string
			if e = tx.QueryRowContext(ctx, "SELECT status,spend_limit::text,spent::text,reserved::text,unauthorized_amount::text,shared_mode FROM team_members WHERE team_id=$1 AND user_id=$2 FOR UPDATE", v.TeamID, user).Scan(&status, &limit, &spent, &reserved, &unauthorized, &override); e != nil {
				return e
			}
			if status != "active" || decimal.RequireFromString(unauthorized).IsPositive() {
				return service.ErrTeamFundingForbidden
			}
			if limit != nil && decimal.RequireFromString(spent).Add(decimal.RequireFromString(reserved)).Add(delta).GreaterThan(decimal.RequireFromString(*limit)) {
				return service.ErrTeamFundingConflict
			}
			if override != nil {
				mode = *override
			}
			if mode == "balance_only" && v.Source != service.TeamFundingSourceBalance || mode == "subscription_only" && v.Source != service.TeamFundingSourceSubscription {
				return service.ErrTeamFundingForbidden
			}
			var payerStatus string
			if e = tx.QueryRowContext(ctx, "SELECT status FROM users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE", owner).Scan(&payerStatus); e != nil {
				return e
			}
			if payerStatus != "active" {
				return service.ErrTeamFundingForbidden
			}
			var windows [3]*time.Time
			if v.Source == service.TeamFundingSourceBalance {
				res, e := tx.ExecContext(ctx, "UPDATE users SET balance=balance-$2::numeric,updated_at=NOW() WHERE id=$1 AND balance >= $2::numeric", owner, delta.StringFixed(8))
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
			} else {
				if _, e = refundTeamEscrow(ctx, tx, v, old.StringFixed(8)); e != nil {
					return e
				}
				var groupID int64
				if e = tx.QueryRowContext(ctx, "SELECT group_id FROM user_subscriptions WHERE id=$1", v.SubscriptionID).Scan(&groupID); e != nil {
					return e
				}
				windows, e = reserveTeamSubscription(ctx, tx, &service.TeamFundingReserveCommand{TeamID: v.TeamID, PayerUserID: owner, MemberUserID: user, GroupID: groupID, Source: v.Source, SubscriptionID: v.SubscriptionID}, target)
				if e != nil {
					return e
				}
			}
			if _, e = tx.ExecContext(ctx, "UPDATE team_members SET reserved=reserved+$3::numeric,updated_at=NOW() WHERE team_id=$1 AND user_id=$2", v.TeamID, user, delta.StringFixed(8)); e != nil {
				return e
			}
			if v.Source == service.TeamFundingSourceBalance {
				_, e = tx.ExecContext(ctx, "UPDATE team_funding_reservations SET amount=$2::numeric,updated_at=NOW() WHERE id=$1", id, target.StringFixed(8))
			} else {
				_, e = tx.ExecContext(ctx, "UPDATE team_funding_reservations SET amount=$2::numeric,daily_window_start=$3,weekly_window_start=$4,monthly_window_start=$5,updated_at=NOW() WHERE id=$1", id, target.StringFixed(8), windows[0], windows[1], windows[2])
			}
			return e
		}
		v, e := scanPersonalReservation(tx.QueryRowContext(ctx, personalReservationSQL+" WHERE id=$1 FOR UPDATE", id))
		if e != nil {
			return e
		}
		if v.MemberUserID != user || v.APIKeyID != key || v.RequestID != request {
			return service.ErrTeamFundingForbidden
		}
		if v.Status != "reserved" && v.Status != "queued" || v.Status == "queued" && queueState != "queued" {
			return service.ErrTeamFundingConflict
		}
		old := decimal.RequireFromString(v.Amount)
		if !target.GreaterThan(old) {
			return nil
		}
		delta := target.Sub(old)
		var status string
		if e = tx.QueryRowContext(ctx, "SELECT status FROM users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE", user).Scan(&status); e != nil {
			return e
		}
		if status != "active" {
			return service.ErrTeamFundingForbidden
		}
		if v.Source == service.FundingSourcePersonalBalance {
			res, e := tx.ExecContext(ctx, "UPDATE users SET balance=balance-$2::numeric,updated_at=NOW() WHERE id=$1 AND balance >= $2::numeric", user, delta.StringFixed(8))
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
			_, e = tx.ExecContext(ctx, "UPDATE personal_funding_reservations SET amount=$2::numeric,updated_at=NOW() WHERE id=$1", id, target.StringFixed(8))
			return e
		}
		if _, e = refundPersonalEscrow(ctx, tx, v, old); e != nil {
			return e
		}
		var groupID int64
		if e = tx.QueryRowContext(ctx, "SELECT group_id FROM user_subscriptions WHERE id=$1", v.SubscriptionID).Scan(&groupID); e != nil {
			return e
		}
		windows, e := reservePersonalSubscription(ctx, tx, &service.PersonalFundingReserveCommand{MemberUserID: user, GroupID: groupID, Source: v.Source, SubscriptionID: v.SubscriptionID}, target)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, "UPDATE personal_funding_reservations SET amount=$2::numeric,daily_window_start=$3,weekly_window_start=$4,monthly_window_start=$5,updated_at=NOW() WHERE id=$1", id, target.StringFixed(8), windows[0], windows[1], windows[2])
		return e
	})
}

// RefreshAsyncFundingEnvelope stores the final account bound before the queue
// claim can change to dispatching. It cannot replace accepted payer identity.
func (r *teamFundingRepository) RefreshAsyncFundingEnvelope(ctx context.Context, user, key int64, decision *service.FundingDecision) error {
	payload, err := json.Marshal(decision)
	if err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, `UPDATE async_funding_tasks SET snapshot=jsonb_set(snapshot,'{funding}',$4::jsonb),updated_at=NOW() WHERE operation_id=$1 AND api_key_id=$2 AND actor_user_id=$3 AND state='queued' AND snapshot->'funding'->>'Source'=$5 AND (snapshot->'funding'->>'PayerUserID')::bigint=$6`, decision.RequestID, key, user, string(payload), decision.Source, decision.PayerUserID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	var exists bool
	if err = r.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM async_funding_tasks WHERE operation_id=$1 AND api_key_id=$2 AND actor_user_id=$3)", decision.RequestID, key, user).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return service.ErrTeamFundingConflict
	}
	return nil
}
