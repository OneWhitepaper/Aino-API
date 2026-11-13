package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

type wsFundingPreparerStub struct {
	operations map[string]bool
	payers     []int64
	calls      int
}

func (s *wsFundingPreparerStub) PrepareFundingRequestForTransport(ctx context.Context, key *service.APIKey, _, _ string, body []byte, operation string) (*service.APIKey, *service.UserSubscription, []byte, error) {
	if desktop := service.DesktopInferenceOperationID(ctx); desktop != "" {
		operation = desktop
	}
	if s.operations[operation] {
		return nil, nil, nil, service.ErrTeamFundingConflict
	}
	s.operations[operation] = true
	payer := s.payers[s.calls]
	s.calls++
	copy := *key
	subscription := &service.UserSubscription{ID: payer}
	copy.Funding = &service.FundingDecision{Source: service.TeamFundingSourceSubscription, PayerUserID: payer, Subscription: subscription, SubscriptionID: &subscription.ID, ReservationID: int64(s.calls), RequestID: operation}
	return &copy, subscription, body, nil
}

func (*wsFundingPreparerStub) ReleaseUndispatchedFunding(context.Context, *service.APIKey) {}

func TestWebSocketFundingFreezesPayerAndDesktopOperationPerTurn(t *testing.T) {
	preparer := &wsFundingPreparerStub{operations: map[string]bool{}, payers: []int64{101, 202}}
	funding := &openAIWSTurnFunding{keys: preparer, operation: "connection"}
	key := &service.APIKey{ID: 7, UserID: 9, Funding: &service.FundingDecision{Source: service.TeamFundingSourceBalance, PayerUserID: 101}}
	ctx := service.WithDesktopInferenceOperation(t.Context(), "desktop-connection")
	first := []byte(`{"type":"response.create","model":"m","event_id":"one"}`)
	second := []byte(`{"type":"response.create","model":"m","event_id":"two"}`)
	_, err := funding.prepare(ctx, 1, key, first, "m")
	require.NoError(t, err)
	snapshot1 := funding.snapshot(1)
	_, err = funding.prepare(ctx, 2, key, second, "m")
	require.NoError(t, err)
	snapshot2 := funding.snapshot(2)
	require.Equal(t, int64(101), snapshot1.key.Funding.PayerUserID)
	require.Equal(t, int64(101), snapshot1.subscription.ID)
	require.Equal(t, int64(202), snapshot2.key.Funding.PayerUserID)
	require.Equal(t, int64(202), snapshot2.subscription.ID)
	require.NotEqual(t, snapshot1.key.Funding.RequestID, snapshot2.key.Funding.RequestID, "a desktop operation must be scoped to each response turn")
	_, err = funding.prepare(ctx, 1, key, first, "m")
	require.NoError(t, err)
	require.Same(t, snapshot1, funding.snapshot(1))
	require.Equal(t, 2, preparer.calls, "a local turn retry must reuse the frozen source")
}

func TestWebSocketFundingRejectsRepeatedClientEventAcrossTurns(t *testing.T) {
	preparer := &wsFundingPreparerStub{operations: map[string]bool{}, payers: []int64{101}}
	funding := &openAIWSTurnFunding{keys: preparer, operation: "connection"}
	key := &service.APIKey{Funding: &service.FundingDecision{Source: service.TeamFundingSourceBalance}}
	body := []byte(`{"type":"response.create","model":"m","event_id":"same"}`)
	_, err := funding.prepare(context.Background(), 1, key, body, "m")
	require.NoError(t, err)
	_, err = funding.prepare(context.Background(), 2, key, body, "m")
	require.ErrorIs(t, err, service.ErrTeamFundingConflict)
	require.Equal(t, 1, preparer.calls)
}

func TestGrokRealtimeCleanUnusedSessionRefundsOnlyProvenUnusedFunding(t *testing.T) {
	for _, test := range []struct {
		name     string
		audio    bool
		err      error
		releases int
	}{
		{"clean-no-audio", false, coderws.CloseError{Code: coderws.StatusNormalClosure}, 1},
		{"going-away-no-audio", false, coderws.CloseError{Code: coderws.StatusGoingAway}, 1},
		{"observed-audio", true, coderws.CloseError{Code: coderws.StatusNormalClosure}, 0},
		{"abnormal-no-audio", false, coderws.CloseError{Code: coderws.StatusAbnormalClosure}, 0},
		{"unknown-no-audio", false, errors.New("upstream result unknown"), 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			releases := 0
			ctx := service.WithFundingDispatchGuard(context.Background(), "session", func(context.Context) error { return nil }, func(context.Context) error { releases++; return nil })
			require.NoError(t, service.BeginFundingDispatch(ctx))
			require.NoError(t, releaseUnusedGrokRealtimeFunding(ctx, test.audio, test.err))
			require.Equal(t, test.releases, releases)
		})
	}
}
