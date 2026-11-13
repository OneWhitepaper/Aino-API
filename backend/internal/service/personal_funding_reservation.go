package service

import (
	"context"
	"database/sql"
)

type PersonalFundingReserveCommand struct {
	RequestID      string
	APIKeyID       int64
	MemberUserID   int64
	GroupID        int64
	Source         string
	SubscriptionID *int64
	Amount         string
}
type PersonalFundingReservation struct {
	ID             int64
	RequestID      string
	APIKeyID       int64
	MemberUserID   int64
	Source         string
	SubscriptionID *int64
	Amount         string
	Status         string
}
type personalFundingReservationRepository interface {
	ClaimFundingDispatch(context.Context, string, int64, int64) error
	BeginFundingSnapshotDispatch(context.Context, string, int64, int64, int64, bool) error
	AcquireFundingAdmission(context.Context, string, int64, int64, string) error
	ReleaseFundingAdmission(context.Context, string, int64, int64, string) error
	ReservePersonalFunding(context.Context, *PersonalFundingReserveCommand) (*PersonalFundingReservation, error)
	ReleasePersonalFunding(context.Context, int64, int64) error
	MarkPersonalFundingDispatched(context.Context, int64, int64) error
	SettlePersonalFundingReservationTx(context.Context, *sql.Tx, int64, string, int64, string) (*TeamFundingSettlement, error)
	ReleaseDispatchedPersonalUnused(context.Context, int64, int64) error
}

func (s *TeamFundingService) ReservePersonal(ctx context.Context, c *PersonalFundingReserveCommand) (*PersonalFundingReservation, error) {
	if s == nil || s.repo == nil {
		return nil, ErrFundingUnavailable
	}
	r, ok := s.repo.(personalFundingReservationRepository)
	if !ok {
		return nil, ErrFundingUnavailable
	}
	return r.ReservePersonalFunding(ctx, c)
}
func (s *TeamFundingService) ReleasePersonal(ctx context.Context, id, user int64) error {
	if s == nil || s.repo == nil {
		return ErrFundingUnavailable
	}
	r, ok := s.repo.(personalFundingReservationRepository)
	if !ok {
		return ErrFundingUnavailable
	}
	return r.ReleasePersonalFunding(ctx, id, user)
}
func (s *TeamFundingService) MarkPersonalDispatched(ctx context.Context, id, user int64) error {
	if s == nil || s.repo == nil {
		return ErrFundingUnavailable
	}
	r, ok := s.repo.(personalFundingReservationRepository)
	if !ok {
		return ErrFundingUnavailable
	}
	return r.MarkPersonalFundingDispatched(ctx, id, user)
}

func (s *TeamFundingService) ReleasePersonalProvenUnused(ctx context.Context, id, user int64) error {
	if s == nil || s.repo == nil {
		return ErrFundingUnavailable
	}
	r, ok := s.repo.(personalFundingReservationRepository)
	if !ok {
		return ErrFundingUnavailable
	}
	return r.ReleaseDispatchedPersonalUnused(ctx, id, user)
}

func (s *TeamFundingService) ClaimDispatch(ctx context.Context, requestID string, keyID, userID int64) error {
	if s == nil || s.repo == nil {
		return ErrFundingUnavailable
	}
	r, ok := s.repo.(personalFundingReservationRepository)
	if !ok {
		return ErrFundingUnavailable
	}
	return r.ClaimFundingDispatch(ctx, requestID, keyID, userID)
}

func (s *TeamFundingService) AcquireAdmission(ctx context.Context, requestID string, key, user int64, token string) error {
	r, ok := s.repo.(personalFundingReservationRepository)
	if !ok {
		return ErrFundingUnavailable
	}
	return r.AcquireFundingAdmission(ctx, requestID, key, user, token)
}
func (s *TeamFundingService) ReleaseAdmission(ctx context.Context, requestID string, key, user int64, token string) error {
	r, ok := s.repo.(personalFundingReservationRepository)
	if !ok {
		return ErrFundingUnavailable
	}
	return r.ReleaseFundingAdmission(ctx, requestID, key, user, token)
}

func (s *TeamFundingService) BeginSnapshotDispatch(ctx context.Context, request string, key, user, id int64, shared bool) error {
	r, ok := s.repo.(personalFundingReservationRepository)
	if !ok {
		return ErrFundingUnavailable
	}
	return r.BeginFundingSnapshotDispatch(ctx, request, key, user, id, shared)
}
