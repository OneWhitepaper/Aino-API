package repository

import (
	"context"
	"database/sql"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ReleaseDispatchedUnused refunds only an already dispatched hold for which an
// upstream response definitively rejected the request. It cannot release a
// concurrent settlement or a still-running/unknown upstream operation.
func (r *teamFundingRepository) ReleaseDispatchedUnused(ctx context.Context, id, member int64) error {
	return teamFundingTx(ctx, r.db, func(tx *sql.Tx) error {
		v, err := scanTeamReservation(tx.QueryRowContext(ctx, teamReservationSQL+" WHERE id=$1 FOR UPDATE", id))
		if err != nil {
			return err
		}
		if v.MemberUserID != member {
			return service.ErrTeamFundingForbidden
		}
		if v.Status == "released" {
			return nil
		}
		if v.Status != "dispatched" {
			return service.ErrTeamFundingConflict
		}
		if _, err = tx.ExecContext(ctx, "UPDATE team_members SET reserved=reserved-$3::numeric,updated_at=NOW() WHERE team_id=$1 AND user_id=$2", v.TeamID, v.MemberUserID, v.Amount); err != nil {
			return err
		}
		if _, err = refundTeamEscrow(ctx, tx, v, v.Amount); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE team_funding_reservations SET status='released',updated_at=NOW() WHERE id=$1 AND status='dispatched'", id)
		return err
	})
}
