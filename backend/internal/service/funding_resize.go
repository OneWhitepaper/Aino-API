package service

import "context"

type FundingReservationResizer interface {
	ResizeFundingReservation(context.Context, int64, int64, int64, string, bool, string) error
}

// EnsureFundingReservation grows the original source before dispatch. It never
// changes payer or source, and cannot authorize a second send after dispatch.
func (s *APIKeyService) EnsureFundingReservation(ctx context.Context, key *APIKey, amount string) error {
	if key == nil || !key.Funding.HasReservation() {
		return ErrFundingUnavailable
	}
	target, err := ParseTeamFundingAmount(amount)
	if err != nil {
		return err
	}
	held, err := ParseTeamFundingAmount(key.Funding.ReservedAmount)
	if err != nil {
		return err
	}
	if !target.GreaterThan(held) {
		return nil
	}
	if s == nil || s.teamFunding == nil {
		return ErrFundingUnavailable
	}
	repo, ok := s.teamFunding.repo.(FundingReservationResizer)
	if !ok {
		return ErrFundingUnavailable
	}
	d := key.Funding
	if err = repo.ResizeFundingReservation(ctx, d.ReservationID, key.UserID, key.ID, d.RequestID, d.IsShared(), target.StringFixed(8)); err != nil {
		return err
	}
	d.ReservedAmount = target.StringFixed(8)
	return nil
}

type asyncFundingEnvelopeRefresher interface {
	RefreshAsyncFundingEnvelope(context.Context, int64, int64, *FundingDecision) error
}

func (s *APIKeyService) refreshAsyncFundingEnvelope(ctx context.Context, key *APIKey) error {
	if s == nil || s.teamFunding == nil || key == nil || key.Funding == nil {
		return nil
	}
	repo, ok := s.teamFunding.repo.(asyncFundingEnvelopeRefresher)
	if !ok {
		return nil
	}
	return repo.RefreshAsyncFundingEnvelope(ctx, key.UserID, key.ID, key.Funding)
}
