//go:build unit

package service

import (
	"context"
	"github.com/shopspring/decimal"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestFundingPolicyOnlyModesNeverCrossConsent(t *testing.T) {
	prefs := FundingPreferences{Configured: true, Mode: FundingModeTeamOnly, AllowPersonalBalance: true, AllowPersonalSubscription: true}
	require.Equal(t, []string{"team"}, fundingSourceOrder(prefs))
	prefs.Mode = FundingModePersonalOnly
	require.Equal(t, []string{FundingSourcePersonalBalance}, fundingSourceOrder(prefs))
	prefs.AllowPersonalBalance = false
	require.Empty(t, fundingSourceOrder(prefs))
	prefs.Mode = FundingModeSubscriptionOnly
	prefs.AllowPersonalSubscription = false
	require.Empty(t, fundingSourceOrder(prefs))
}

func TestFundingTextBoundAllowsFunctionToolsAndEnforcesOutput(t *testing.T) {
	body := []byte(`{"model":"deepseek-chat","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"read_file","parameters":{"type":"object"}}}],"stream":true}`)
	prepared, model, input, output, count, err := prepareFundingTextRequest("/v1/chat/completions", body)
	require.NoError(t, err)
	require.Equal(t, "deepseek-chat", model)
	require.Contains(t, string(prepared), `"max_tokens":4096`)
	require.Greater(t, input, len(body))
	require.Equal(t, 4096, output)
	require.Equal(t, 1, count)
	_, _, _, _, _, err = prepareFundingTextRequest("/v1/responses", []byte(`{"model":"gpt-5","input":"x","tools":[{"type":"web_search"}]}`))
	require.ErrorIs(t, err, ErrTeamFundingCostUnbounded)
	_, _, _, _, _, err = prepareFundingTextRequest("/v1/responses", []byte(`{"model":"gpt-5","input":[{"type":"input_image","image_url":"https://example.com/x"}]}`))
	require.ErrorIs(t, err, ErrTeamFundingCostUnbounded)
	prepared, _, _, output, _, err = prepareFundingTextRequest("/v1/responses", []byte(`{"model":"gpt-5","input":"x","max_output_tokens":16384}`))
	require.NoError(t, err)
	require.Equal(t, 16384, output)
	require.Contains(t, string(prepared), `16384`)
}

func TestFundingSharedCommandPreservesActorAndCannotFallback(t *testing.T) {
	decision := &FundingDecision{Source: TeamFundingSourceBalance, PayerUserID: 11, TeamID: 2, ReservationID: 8, RequestID: "funding-operation", ReservedAmount: "1.00000000"}
	key := &APIKey{ID: 7, UserID: 22, Funding: decision}
	p := &postUsageBillingParams{APIKey: key, User: &User{ID: 22}, Account: &Account{ID: 3}, Cost: &CostBreakdown{ActualCost: 0.3, TotalCost: 0.3}}
	cmd := buildUsageBillingCommand("upstream-operation", nil, p)
	require.Equal(t, int64(22), cmd.UserID)
	require.Equal(t, int64(11), cmd.PayerUserID)
	require.Equal(t, int64(8), cmd.FundingReservationID)
	require.Equal(t, "funding-operation", cmd.RequestID)
	require.Equal(t, 0.0, cmd.BalanceCost)
	require.Equal(t, 0.3, cmd.FundingCost)
	applied, err := applyUsageBilling(context.Background(), "id", nil, p, &billingDeps{}, nil)
	require.False(t, applied)
	require.Error(t, err)
}

func TestFundingBudgetAndSubscriptionScope(t *testing.T) {
	limit := "0.00000000"
	team := &TeamFundingCandidate{Status: TeamMemberActive, SpendLimit: &limit, Spent: "0", Reserved: "0", AvailableBalance: "500"}
	require.False(t, teamFundingBudgetAvailable(team))
	limit = "100.00000000"
	team.SpendLimit = &limit
	require.True(t, teamFundingBudgetAvailable(team))
	now := time.Now()
	sub := UserSubscription{GroupID: 5, Status: SubscriptionStatusActive, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}
	require.True(t, fundingSubscriptionAvailable(&sub, &Group{ID: 5}, now))
	require.False(t, fundingSubscriptionAvailable(&sub, &Group{ID: 6}, now))
	sub.StartsAt = now.Add(time.Minute)
	require.False(t, fundingSubscriptionAvailable(&sub, &Group{ID: 5}, now))
}

func TestFundingDispatchGuardPersistsOnceAndRejectsReplay(t *testing.T) {
	var calls atomic.Int32
	ctx := WithFundingDispatchGuard(context.Background(), "funding-op", func(context.Context) error { calls.Add(1); return nil }, func(context.Context) error { return nil })
	require.Equal(t, "funding-op", FundingDispatchRequestID(ctx))
	require.NoError(t, BeginFundingDispatch(ctx))
	require.ErrorIs(t, BeginFundingDispatch(ctx), ErrTeamFundingConflict)
	require.Equal(t, int32(1), calls.Load())
}

func TestConfiguredFundingIsRejectedInSimpleMode(t *testing.T) {
	cfg := &config.Config{RunMode: config.RunModeSimple}
	require.True(t, fundingModeBlockedByRunMode(cfg))
	cfg.RunMode = config.RunModeStandard
	require.False(t, fundingModeBlockedByRunMode(cfg))
}

func TestFundingOperationIDIsStableAndScoped(t *testing.T) {
	key := &APIKey{ID: 7, UserID: 22}
	a := fundingOperationRequestID(key, "retry-token")
	require.Equal(t, a, fundingOperationRequestID(key, "retry-token"))
	require.NotEqual(t, a, fundingOperationRequestID(key, "different-token"))
	require.NotEqual(t, a, fundingOperationRequestID(&APIKey{ID: 8, UserID: 22}, "retry-token"))
	key.DesktopUsage = &DesktopUsage{CallID: "8b6b417d-5a94-44da-a9bb-0b9608193ba4"}
	require.Equal(t, fundingOperationRequestID(key, ""), fundingOperationRequestID(key, "different-token"))
}

func TestFundingSubscriptionCostAwareEligibility(t *testing.T) {
	now := time.Now()
	sub := UserSubscription{GroupID: 3, Status: SubscriptionStatusActive, StartsAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), DailyUsageUSD: 0.5}
	group := &Group{ID: 3, DailyLimitUSD: testPtrFloat64(1)}
	half := decimal.RequireFromString("0.5")
	slightlyMore := decimal.RequireFromString("0.50000001")
	require.True(t, fundingSubscriptionCovers(&sub, group, now, &half))
	require.False(t, fundingSubscriptionCovers(&sub, group, now, &slightlyMore))
}

type fundingContextRepoStub struct {
	TeamFundingRepository
	context *FundingContext
}

func (r *fundingContextRepoStub) GetFundingContext(context.Context, int64) (*FundingContext, error) {
	return r.context, nil
}

type fundingUserRepoStub struct {
	UserRepository
	user *User
}

func (r *fundingUserRepoStub) GetByID(context.Context, int64) (*User, error) { return r.user, nil }

type fundingSubscriptionsStub struct {
	UserSubscriptionRepository
	subs []UserSubscription
}

func (r fundingSubscriptionsStub) ListActiveByUserID(_ context.Context, userID int64) ([]UserSubscription, error) {
	var out []UserSubscription
	for _, sub := range r.subs {
		if sub.UserID == userID {
			out = append(out, sub)
		}
	}
	return out, nil
}
func (r fundingSubscriptionsStub) GetActiveByUserIDAndGroupID(_ context.Context, userID, groupID int64) (*UserSubscription, error) {
	for _, sub := range r.subs {
		if sub.UserID == userID && sub.GroupID == groupID {
			copy := sub
			return &copy, nil
		}
	}
	return nil, ErrSubscriptionNotFound
}

func TestFundingRequiredAmountFallsThroughOnlyToAuthorizedSources(t *testing.T) {
	limit := "100.00000000"
	contextData := &FundingContext{Preferences: FundingPreferences{Configured: true, Mode: FundingModePersonalFirst, TeamID: ptrInt64ForFundingTest(4), AllowPersonalBalance: true}, Team: &TeamFundingCandidate{TeamID: 4, MemberUserID: 22, PayerUserID: 11, Status: TeamMemberActive, SharedMode: TeamSharedModeBalanceOnly, AvailableBalance: "10", SpendLimit: &limit, Spent: "0", Reserved: "0"}}
	svc := &APIKeyService{teamFunding: NewTeamFundingService(&fundingContextRepoStub{context: contextData}), userRepo: &fundingUserRepoStub{user: &User{ID: 22, Balance: 1}}, userSubRepo: fundingSubscriptionsStub{}}
	key := &APIKey{ID: 7, UserID: 22, User: &User{ID: 22}, Group: &Group{ID: 3}, GroupID: ptrInt64ForFundingTest(3)}
	required := decimal.RequireFromString("2.00000000")
	decision, err := svc.resolveFundingDecisionForCost(context.Background(), key, &required)
	require.NoError(t, err)
	require.Equal(t, TeamFundingSourceBalance, decision.Source, "personal_first must advance when the full quote exceeds the available personal wallet")
	contextData.Preferences.Mode = FundingModePersonalOnly
	decision, err = svc.resolveFundingDecisionForCost(context.Background(), key, &required)
	require.ErrorIs(t, err, ErrFundingUnavailable)
	require.Nil(t, decision, "personal_only cannot cross to a team source")
}

func ptrInt64ForFundingTest(v int64) *int64 { return &v }

func TestDefinitiveFundingRejectionSet(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 413} {
		require.True(t, IsDefinitiveNoChargeResponse(status))
	}
	for _, status := range []int{0, 408, 409, 429, 500, 502, 503} {
		require.False(t, IsDefinitiveNoChargeResponse(status))
	}
}

func TestFundingBoundCoversDefaultFastTierAndAccountLongContext(t *testing.T) {
	cfg := &config.Config{}
	catalog := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{"funding-bound-model": {InputCostPerToken: 0.000001, OutputCostPerToken: 0.000002, LongContextInputTokenThreshold: 100000, LongContextInputCostMultiplier: 3, LongContextOutputCostMultiplier: 4}})
	billing := NewBillingService(cfg, catalog)
	resolver := NewModelPricingResolver(nil, billing)
	group := &Group{ID: 3, RateMultiplier: 1, LongContextPricingEnabled: false}
	svc := &APIKeyService{fundingBilling: billing, fundingPricing: resolver}
	key := &APIKey{UserID: 22, GroupID: &group.ID, Group: group}
	boundText, err := svc.strictFundingTextCostOne(context.Background(), key, "funding-bound-model", 150000, 500000, 1)
	require.NoError(t, err)
	bound := decimal.RequireFromString(boundText).InexactFloat64()
	enabled := true
	actual, err := billing.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "funding-bound-model", Group: group, GroupID: &group.ID, RateMultiplier: 1, Resolver: resolver, Tokens: UsageTokens{InputTokens: 150000, OutputTokens: 500000}, ServiceTier: "fast", LongContextBillingEnabled: &enabled})
	require.NoError(t, err)
	require.GreaterOrEqual(t, bound, actual.ActualCost)
	require.False(t, group.LongContextPricingEnabled, "quoting must not mutate the shared group")
}

func TestFundingPerRequestBoundCoversReasoningMultiplier(t *testing.T) {
	cfg := &config.Config{}
	billing := NewBillingService(cfg, nil)
	resolver := NewModelPricingResolver(nil, billing)
	price := 1.0
	group := &Group{ID: 3, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{{Models: []string{"funding-fixed"}, BillingMode: BillingModePerRequest, PerRequestPrice: &price, ReasoningEffortMultipliers: map[string]float64{"high": 3}}}}
	svc := &APIKeyService{fundingBilling: billing, fundingPricing: resolver}
	key := &APIKey{UserID: 22, GroupID: &group.ID, Group: group}
	boundText, err := svc.strictFundingTextCostOne(context.Background(), key, "funding-fixed", 100, 100, 1)
	require.NoError(t, err)
	require.GreaterOrEqual(t, decimal.RequireFromString(boundText).InexactFloat64(), 3.0)
}

func TestFundingDeepSeekBoundCoversPeakAndUngroupedDefault(t *testing.T) {
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 3
	billing := NewBillingService(cfg, nil)
	resolver := NewModelPricingResolver(nil, billing)
	svc := &APIKeyService{cfg: cfg, fundingBilling: billing, fundingPricing: resolver}
	key := &APIKey{UserID: 22}
	boundText, err := svc.strictFundingTextCostOne(context.Background(), key, "deepseek-v4-flash", 65536, 8192, 1)
	require.NoError(t, err)
	peak := time.Date(2026, 8, 24, 1, 0, 0, 0, time.UTC)
	actual, err := billing.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "deepseek-v4-flash", RateMultiplier: 3, Resolver: resolver, Tokens: UsageTokens{InputTokens: 65536, OutputTokens: 8192}, PricingAt: peak})
	require.NoError(t, err)
	require.GreaterOrEqual(t, decimal.RequireFromString(boundText).InexactFloat64(), actual.ActualCost)
}

func TestFundingPersonalImageKeepsLegacyPathButTeamImageRejects(t *testing.T) {
	funding := &FundingContext{Preferences: FundingPreferences{Configured: true, Mode: FundingModePersonalOnly, AllowPersonalBalance: true}}
	svc := &APIKeyService{teamFunding: NewTeamFundingService(&fundingContextRepoStub{context: funding}), userRepo: &fundingUserRepoStub{user: &User{ID: 22, Balance: 1}}, userSubRepo: fundingSubscriptionsStub{}}
	key := &APIKey{ID: 7, UserID: 22, User: &User{ID: 22}, Group: &Group{ID: 3}, GroupID: ptrInt64ForFundingTest(3)}
	body := []byte(`{"model":"gpt-image-2","prompt":"test"}`)
	next, sub, prepared, err := svc.PrepareConfiguredFundingRequest(context.Background(), key, "/v1/images/generations", body, "")
	require.NoError(t, err)
	require.Nil(t, sub)
	require.Equal(t, body, prepared)
	require.Equal(t, FundingSourcePersonalBalance, next.Funding.Source)
	require.False(t, next.Funding.HasReservation())
	funding.Preferences.Mode = FundingModeTeamOnly
	funding.Preferences.TeamID = ptrInt64ForFundingTest(4)
	funding.Team = &TeamFundingCandidate{TeamID: 4, PayerUserID: 11, MemberUserID: 22, Status: TeamMemberActive, AvailableBalance: "500", Spent: "0", Reserved: "0"}
	_, _, _, err = svc.PrepareConfiguredFundingRequest(context.Background(), key, "/v1/images/generations", body, "")
	require.ErrorIs(t, err, ErrTeamFundingCostUnbounded)
}

func TestFundingSharedSubscriptionCanBindOnlyExplicitGrantedGroup(t *testing.T) {
	now := time.Now()
	funding := &FundingContext{Preferences: FundingPreferences{Configured: true, Mode: FundingModeTeamOnly, TeamID: ptrInt64ForFundingTest(4)}, Team: &TeamFundingCandidate{TeamID: 4, PayerUserID: 11, MemberUserID: 22, Status: TeamMemberActive, SubscriptionIDs: []int64{9}}}
	repo := &fundingContextRepoStub{context: funding}
	subs := fundingSubscriptionsStub{subs: []UserSubscription{{ID: 9, UserID: 11, GroupID: 3, Status: SubscriptionStatusActive, StartsAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}, {ID: 10, UserID: 11, GroupID: 5, Status: SubscriptionStatusActive, StartsAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}}}
	svc := &APIKeyService{teamFunding: NewTeamFundingService(repo), userSubRepo: subs}
	actor := &User{ID: 22}
	require.True(t, svc.canUserBindGroup(context.Background(), actor, &Group{ID: 3, SubscriptionType: SubscriptionTypeSubscription}))
	require.False(t, svc.canUserBindGroup(context.Background(), actor, &Group{ID: 5, SubscriptionType: SubscriptionTypeSubscription}), "another owner subscription never grants a member group")
	funding.Team = nil
	require.False(t, svc.canUserBindGroup(context.Background(), actor, &Group{ID: 3, SubscriptionType: SubscriptionTypeSubscription}), "revocation removes the key-binding grant immediately")
}

func TestFundingImplicitOwnerKeepsLegacySubscriptionSource(t *testing.T) {
	now := time.Now()
	data := &FundingContext{ImplicitOwner: true, Preferences: FundingPreferences{Configured: true, Mode: FundingModePersonalFirst, AllowPersonalBalance: true, AllowPersonalSubscription: true}}
	group := &Group{ID: 3, SubscriptionType: SubscriptionTypeSubscription}
	actor := &User{ID: 22, Balance: 500}
	svc := &APIKeyService{teamFunding: NewTeamFundingService(&fundingContextRepoStub{context: data}), userRepo: &fundingUserRepoStub{user: actor}, userSubRepo: fundingSubscriptionsStub{subs: []UserSubscription{{ID: 9, UserID: 22, GroupID: 3, Status: SubscriptionStatusActive, StartsAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}}}}
	decision, err := svc.resolveFundingDecision(context.Background(), &APIKey{ID: 7, UserID: 22, User: actor, Group: group, GroupID: &group.ID})
	require.NoError(t, err)
	require.Equal(t, FundingSourcePersonalSubscription, decision.Source)
	require.Equal(t, 500.0, actor.Balance)
	// Explicitly saved personal_first still honors that separate authorization.
	data.ImplicitOwner = false
	decision, err = svc.resolveFundingDecision(context.Background(), &APIKey{ID: 7, UserID: 22, User: actor, Group: group, GroupID: &group.ID})
	require.NoError(t, err)
	require.Equal(t, FundingSourcePersonalBalance, decision.Source)
}
