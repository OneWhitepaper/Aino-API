package service

import (
	"context"
	"log/slog"
)

type dispatchedUnusedReleaser interface {
	ReleaseDispatchedUnused(context.Context, int64, int64) error
}

// ReleaseDispatchedUnused is reserved for an explicit provider response that
// proves the request was rejected before generation. Unknown outcomes never use
// this path; the repository requires the dispatched state and refunds escrow in
// the same transaction as the status change.
func (s *TeamFundingService) ReleaseDispatchedUnused(ctx context.Context, reservationID, memberID int64) error {
	if s == nil || s.repo == nil {
		return ErrFundingUnavailable
	}
	repo, ok := s.repo.(dispatchedUnusedReleaser)
	if !ok {
		return ErrFundingUnavailable
	}
	if err := repo.ReleaseDispatchedUnused(ctx, reservationID, memberID); err != nil {
		slog.Error("team_funding.proven_unused_release_failed", "reservation_id", reservationID, "member_user_id", memberID, "error", err)
		return err
	}
	return nil
}
