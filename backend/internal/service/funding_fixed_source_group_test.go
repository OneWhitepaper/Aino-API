//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fixedFundingGroupRepo struct {
	GroupRepository
	group *Group
	reads int
}

func (r *fixedFundingGroupRepo) GetByID(context.Context, int64) (*Group, error) {
	r.reads++
	return r.group, nil
}

func TestFundingFixedSourceSharedBalanceRequiresCurrentSelectedSubscriptionGroupGrant(t *testing.T) {
	for _, tc := range []struct {
		name       string
		expired    bool
		unselected bool
		removed    bool
		wantOK     bool
	}{
		{name: "active selected subscription", wantOK: true},
		{name: "expired selected subscription", expired: true},
		{name: "active unselected subscription", unselected: true},
		{name: "revoked shared subscription grant", removed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, key, repo := fundingPreparationFixture(TeamFundingSourceBalance)
			now := time.Now()
			currentGroup := *key.Group
			currentGroup.Status = StatusActive
			currentGroup.SubscriptionType = SubscriptionTypeSubscription
			currentGroup.IsExclusive = true
			groups := &fixedFundingGroupRepo{group: &currentGroup}
			svc.groupRepo = groups
			currentActor := &User{ID: key.UserID, Status: StatusActive, Balance: 0}
			svc.userRepo = &fundingUserRepoStub{user: currentActor}
			sub := UserSubscription{ID: 9, UserID: 11, GroupID: currentGroup.ID, Status: SubscriptionStatusActive,
				StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}
			if tc.expired {
				sub.ExpiresAt = now.Add(-time.Minute)
			}
			svc.userSubRepo = fundingSubscriptionsStub{subs: []UserSubscription{sub}}
			repo.data.Team.SubscriptionIDs = []int64{9}
			if tc.unselected {
				repo.data.Team.SubscriptionIDs = []int64{10}
			}
			if tc.removed {
				repo.data.Team.SubscriptionIDs = nil
			}
			// The historical operation had this grant. A paid extension must use
			// current team grants while retaining the original balance payer.
			staleContext := *repo.data
			staleTeam := *repo.data.Team
			staleTeam.SubscriptionIDs = []int64{9}
			staleContext.Team = &staleTeam
			source := &FundingDecision{Source: TeamFundingSourceBalance, PayerUserID: 11, TeamID: 4,
				RequestID: "original-cache", ReservationID: 99, ReservedAmount: "1.00000000", fundingContext: &staleContext}
			key.Funding = source
			body := []byte(`{"model":"funding-oauth-model","input":"hello","max_output_tokens":64}`)
			plan := &FundingRequestPlan{Body: body, ContentType: "application/json", Endpoint: "/v1/responses", Kind: FundingRequestText,
				Model: "funding-oauth-model", InputTokens: 32, OutputTokens: 64, Count: 1}

			next, subscription, _, err := svc.PrepareFundingRequestForFixedSource(t.Context(), key, plan, source, "cache-extension")
			require.Equal(t, 1, groups.reads, "current bound-group policy must be loaded")
			if !tc.wantOK {
				require.ErrorIs(t, err, ErrFundingUnavailable)
				require.Nil(t, next)
				require.Zero(t, repo.nextID, "revoked group access must fail before reserving shared funds")
				require.Empty(t, repo.reserved)
				require.Empty(t, repo.admissions)
			} else {
				require.NoError(t, err)
				require.Nil(t, subscription, "the subscription grants group access while the fixed source pays from shared balance")
				require.Same(t, currentActor, next.User)
				require.Equal(t, currentGroup, *next.Group)
				require.Equal(t, TeamFundingSourceBalance, next.Funding.Source)
				require.Equal(t, int64(11), next.Funding.PayerUserID)
				require.Equal(t, int64(4), next.Funding.TeamID)
				require.Nil(t, next.Funding.SubscriptionID)
				require.True(t, next.Funding.HasReservation())
				require.Equal(t, int64(1), repo.nextID)
				svc.ReleaseUndispatchedFunding(t.Context(), next)
				require.Empty(t, repo.reserved)
			}
			require.Equal(t, "original-cache", source.RequestID)
			require.Equal(t, int64(99), source.ReservationID)
			require.Equal(t, TeamFundingSourceBalance, source.Source)
		})
	}
}

func TestFundingFixedSourceRejectsInactiveCurrentActorBeforeReserve(t *testing.T) {
	for _, tc := range []struct {
		name  string
		actor *User
	}{
		{name: "deleted actor"},
		{name: "disabled actor", actor: &User{ID: 22, Status: StatusDisabled}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, key, repo := fundingPreparationFixture(TeamFundingSourceBalance)
			key.User.Status = StatusActive
			svc.userRepo = &fundingUserRepoStub{user: tc.actor}
			currentGroup := *key.Group
			currentGroup.Status = StatusActive
			svc.groupRepo = &fixedFundingGroupRepo{group: &currentGroup}
			source := &FundingDecision{Source: TeamFundingSourceBalance, PayerUserID: 11, TeamID: 4}
			key.Funding = source
			plan := &FundingRequestPlan{Kind: FundingRequestText, Endpoint: "/v1/responses", Model: "funding-oauth-model", Count: 1}

			next, _, _, err := svc.PrepareFundingRequestForFixedSource(t.Context(), key, plan, source, "inactive-actor-extension")
			require.ErrorIs(t, err, ErrTeamFundingForbidden)
			require.Nil(t, next)
			require.Zero(t, repo.nextID, "the stale active actor snapshot must not authorize a new hold")
			require.Empty(t, repo.reserved)
			require.Empty(t, repo.admissions)
		})
	}
}
