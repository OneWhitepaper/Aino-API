package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"sync/atomic"
)

const (
	FundingSourcePersonalBalance      = "personal_balance"
	FundingSourcePersonalSubscription = "personal_subscription"
	// This bounds memory before the gateway's own request limits run.
	FundingRequestBodyLimit        = 4 << 20
	ConfiguredFundingBodyReadLimit = 16 << 20
)

var ErrFundingUnavailable = infraerrors.Forbidden("FUNDING_UNAVAILABLE", "no authorized funding source is available for this model and group")

// FundingDecision belongs to one copied request key. It is never stored in the
// authentication cache. Identity, concurrency, key quotas and model permissions
// remain those of the caller; only the settlement destination changes.
type FundingDecision struct {
	Source          string
	PayerUserID     int64
	TeamID          int64
	SubscriptionID  *int64
	Subscription    *UserSubscription
	ReservationID   int64
	RequestID       string
	ReservedAmount  string
	dispatchContext context.Context
	fundingContext  *FundingContext
	AdmissionToken  string
}

func (d *FundingDecision) IsShared() bool {
	return d != nil && (d.Source == TeamFundingSourceBalance || d.Source == TeamFundingSourceSubscription)
}
func (d *FundingDecision) HasReservation() bool { return d != nil && d.ReservationID > 0 }
func (d *FundingDecision) IsSubscription() bool {
	return d != nil && (d.Source == FundingSourcePersonalSubscription || d.Source == TeamFundingSourceSubscription)
}
func (s *APIKeyService) SetTeamFundingService(funding *TeamFundingService) { s.teamFunding = funding }
func (s *APIKeyService) SetFundingPricing(billing *BillingService, pricing *ModelPricingResolver) {
	s.fundingBilling, s.fundingPricing = billing, pricing
}
func (s *BillingCacheService) SetTeamFundingService(funding *TeamFundingService) {
	s.teamFunding = funding
}

// FundingConfigured is DB authoritative: a newly paused/removed membership or
// changed personal consent must not live in the authentication cache.
func (s *APIKeyService) FundingConfigured(ctx context.Context, userID int64) (bool, error) {
	if s == nil || s.teamFunding == nil {
		return false, nil
	}
	f, err := s.teamFunding.GetFundingContext(ctx, userID)
	return f != nil && f.Preferences.Configured, err
}
func fundingSourceOrder(p FundingPreferences) []string {
	var order []string
	switch p.Mode {
	case FundingModePersonalFirst:
		order = []string{FundingSourcePersonalBalance, FundingSourcePersonalSubscription, "team"}
	case FundingModeSubscriptionFirst:
		order = []string{FundingSourcePersonalSubscription, FundingSourcePersonalBalance, "team"}
	case FundingModeTeamFirst:
		order = []string{"team", FundingSourcePersonalSubscription, FundingSourcePersonalBalance}
	case FundingModePersonalOnly:
		order = []string{FundingSourcePersonalBalance}
	case FundingModeSubscriptionOnly:
		order = []string{FundingSourcePersonalSubscription}
	case FundingModeTeamOnly:
		order = []string{"team"}
	}
	result := make([]string, 0, len(order))
	for _, source := range order {
		if source == FundingSourcePersonalBalance && !p.AllowPersonalBalance {
			continue
		}
		if source == FundingSourcePersonalSubscription && !p.AllowPersonalSubscription {
			continue
		}
		result = append(result, source)
	}
	return result
}
func fundingAllowsTeam(p FundingPreferences) bool {
	for _, s := range fundingSourceOrder(p) {
		if s == "team" {
			return true
		}
	}
	return false
}
func teamFundingBudgetAvailable(t *TeamFundingCandidate) bool {
	if t == nil || t.Status != TeamMemberActive {
		return false
	}
	if t.SpendLimit == nil {
		return true
	}
	limit, e := decimal.NewFromString(*t.SpendLimit)
	if e != nil {
		return false
	}
	spent, e := decimal.NewFromString(t.Spent)
	if e != nil {
		return false
	}
	reserved, e := decimal.NewFromString(t.Reserved)
	if e != nil {
		return false
	}
	return limit.GreaterThan(spent.Add(reserved))
}
func fundingSubscriptionAvailable(sub *UserSubscription, g *Group, now time.Time) bool {
	return fundingSubscriptionCovers(sub, g, now, nil)
}
func fundingSubscriptionCovers(sub *UserSubscription, g *Group, now time.Time, required *decimal.Decimal) bool {
	if sub == nil || g == nil || sub.GroupID != g.ID || sub.Status != SubscriptionStatusActive || sub.StartsAt.After(now) || !sub.ExpiresAt.After(now) || sub.DeletedAt != nil {
		return false
	}
	daily, weekly, monthly := sub.DailyUsageUSD, sub.WeeklyUsageUSD, sub.MonthlyUsageUSD
	if sub.NeedsDailyResetAt(now) {
		daily = 0
	}
	if sub.NeedsWeeklyResetAt(now) {
		weekly = 0
	}
	if sub.NeedsMonthlyResetAt(now) {
		monthly = 0
	}
	if required != nil {
		daily += required.InexactFloat64()
		weekly += required.InexactFloat64()
		monthly += required.InexactFloat64()
	}
	if required == nil {
		return !(g.HasDailyLimit() && daily >= *g.DailyLimitUSD || g.HasWeeklyLimit() && weekly >= *g.WeeklyLimitUSD || g.HasMonthlyLimit() && monthly >= *g.MonthlyLimitUSD)
	}
	return !(g.HasDailyLimit() && daily > *g.DailyLimitUSD || g.HasWeeklyLimit() && weekly > *g.WeeklyLimitUSD || g.HasMonthlyLimit() && monthly > *g.MonthlyLimitUSD)
}
func activeFundingSubscription(sub *UserSubscription, g *Group, now time.Time) bool {
	return sub != nil && g != nil && sub.GroupID == g.ID && sub.Status == SubscriptionStatusActive && !sub.StartsAt.After(now) && sub.ExpiresAt.After(now) && sub.DeletedAt == nil
}
func containsFundingID(ids []int64, id int64) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

// resolveFundingDecision does not reserve money. Catalog and actual dispatch
// share this selection, while admission revalidates it in the reservation tx.
func (s *APIKeyService) resolveFundingDecision(ctx context.Context, key *APIKey) (*FundingDecision, error) {
	return s.resolveFundingDecisionForCost(ctx, key, nil)
}
func (s *APIKeyService) resolveFundingDecisionForCost(ctx context.Context, key *APIKey, required *decimal.Decimal) (*FundingDecision, error) {
	return s.resolveFundingDecisionExcluding(ctx, key, required, nil)
}
func (s *APIKeyService) resolveFundingDecisionExcluding(ctx context.Context, key *APIKey, required *decimal.Decimal, excluded map[string]bool) (*FundingDecision, error) {
	if s == nil || s.teamFunding == nil || key == nil || key.User == nil {
		return nil, nil
	}
	var f *FundingContext
	var err error
	if key.Funding != nil && key.Funding.fundingContext != nil {
		f = key.Funding.fundingContext
	} else {
		f, err = s.teamFunding.GetFundingContext(ctx, key.UserID)
		if err != nil {
			return nil, err
		}
	}
	if f == nil || !f.Preferences.Configured {
		return nil, nil
	}
	now := time.Now()
	own, err := s.userSubRepo.ListActiveByUserID(ctx, key.UserID)
	if err != nil {
		return nil, err
	}
	var shared []UserSubscription
	if f.Team != nil && f.Team.Status == TeamMemberActive && fundingAllowsTeam(f.Preferences) {
		shared, err = s.userSubRepo.ListActiveByUserID(ctx, f.Team.PayerUserID)
		if err != nil {
			return nil, err
		}
	}
	// Cash may pay a subscription group only while a real matching personal or
	// explicitly shared subscription still grants this caller access to the group.
	authorized := key.Group == nil || !key.Group.IsSubscriptionType() && key.User.CanBindGroup(key.Group.ID, key.Group.IsExclusive)
	for i := range own {
		if activeFundingSubscription(&own[i], key.Group, now) {
			authorized = true
		}
	}
	for i := range shared {
		if containsFundingID(f.Team.SubscriptionIDs, shared[i].ID) && activeFundingSubscription(&shared[i], key.Group, now) {
			authorized = true
		}
	}
	if !authorized {
		return nil, ErrFundingUnavailable
	}
	personalBalance := func() (*FundingDecision, error) {
		if excluded[FundingSourcePersonalBalance] {
			return nil, nil
		}
		user, err := s.userRepo.GetByID(ctx, key.UserID)
		if err != nil {
			return nil, err
		}
		threshold := 0.0
		if s.cfg != nil && s.cfg.Billing.MinimumBalanceReserve > 0 {
			threshold = s.cfg.Billing.MinimumBalanceReserve
		}
		if user.Balance <= 0 || user.Balance < threshold {
			return nil, nil
		}
		if required != nil && decimal.NewFromFloat(user.Balance).LessThan(*required) {
			return nil, nil
		}
		return &FundingDecision{Source: FundingSourcePersonalBalance, PayerUserID: key.UserID}, nil
	}
	personalSub := func() *FundingDecision {
		for i := range own {
			sub := own[i]
			if !excluded[fundingCandidateKey(&FundingDecision{Source: FundingSourcePersonalSubscription, SubscriptionID: &sub.ID})] && fundingSubscriptionCovers(&sub, key.Group, now, required) {
				return &FundingDecision{Source: FundingSourcePersonalSubscription, PayerUserID: key.UserID, SubscriptionID: &sub.ID, Subscription: &sub}
			}
		}
		return nil
	}
	team := func() *FundingDecision {
		t := f.Team
		if !teamFundingBudgetAvailable(t) {
			return nil
		}
		balance := func() *FundingDecision {
			if excluded[TeamFundingSourceBalance] || t.SharedMode == TeamSharedModeSubscriptionOnly {
				return nil
			}
			b, e := decimal.NewFromString(t.AvailableBalance)
			if e != nil || !b.IsPositive() || required != nil && b.LessThan(*required) {
				return nil
			}
			return &FundingDecision{Source: TeamFundingSourceBalance, PayerUserID: t.PayerUserID, TeamID: t.TeamID}
		}
		subscription := func() *FundingDecision {
			if t.SharedMode == TeamSharedModeBalanceOnly {
				return nil
			}
			for i := range shared {
				sub := shared[i]
				if !excluded[fundingCandidateKey(&FundingDecision{Source: TeamFundingSourceSubscription, SubscriptionID: &sub.ID})] && containsFundingID(t.SubscriptionIDs, sub.ID) && fundingSubscriptionCovers(&sub, key.Group, now, required) {
					return &FundingDecision{Source: TeamFundingSourceSubscription, PayerUserID: t.PayerUserID, TeamID: t.TeamID, SubscriptionID: &sub.ID, Subscription: &sub}
				}
			}
			return nil
		}
		if t.SharedMode == TeamSharedModeSubscriptionFirst || t.SharedMode == TeamSharedModeSubscriptionOnly {
			if d := subscription(); d != nil {
				return d
			}
			return balance()
		}
		if d := balance(); d != nil {
			return d
		}
		return subscription()
	}
	sourceOrder := fundingSourceOrder(f.Preferences)
	if f.ImplicitOwner {
		sourceOrder = []string{FundingSourcePersonalBalance}
		if key.Group != nil && key.Group.IsSubscriptionType() {
			sourceOrder = []string{FundingSourcePersonalSubscription}
		}
	}
	for _, source := range sourceOrder {
		var d *FundingDecision
		switch source {
		case FundingSourcePersonalBalance:
			d, err = personalBalance()
		case FundingSourcePersonalSubscription:
			d = personalSub()
		case "team":
			d = team()
		}
		if err != nil {
			return nil, err
		}
		if d != nil {
			d.fundingContext = f
			return d, nil
		}
	}
	return nil, ErrFundingUnavailable
}

// PrepareFundingRequest selects once, reserves in PostgreSQL, and returns a
// copied key for this request and the bounded body to dispatch. The durable hold
// is never released merely because a stream/client/worker times out.
func (s *APIKeyService) ResolveRequestFunding(ctx context.Context, key *APIKey) (*APIKey, *UserSubscription, error) {
	d, err := s.resolveFundingDecision(ctx, key)
	if err != nil {
		return nil, nil, err
	}
	if d == nil {
		return key, nil, nil
	}
	if err = s.CheckAPIKeyQuotaAndExpiry(key); err != nil {
		return nil, nil, err
	}
	if fundingModeBlockedByRunMode(s.cfg) {
		return nil, nil, ErrFundingUnavailable
	}
	copy := *key
	copy.Funding = d
	return &copy, d.Subscription, nil
}

func (s *APIKeyService) PrepareConfiguredFundingRequest(ctx context.Context, key *APIKey, path string, body []byte, idempotencyKey string) (*APIKey, *UserSubscription, []byte, error) {
	base := key.Funding
	var err error
	if base == nil {
		base, err = s.resolveFundingDecision(ctx, key)
		if err != nil {
			return nil, nil, nil, err
		}
		if base == nil {
			return key, nil, body, nil
		}
	}
	if fundingModeBlockedByRunMode(s.cfg) {
		return nil, nil, nil, ErrFundingUnavailable
	}
	prepared, model, input, output, count, parseErr := prepareFundingTextRequest(path, body)
	if parseErr == nil && key.Group != nil && !key.Group.ModelAllowlist.Allows(model) {
		return nil, nil, nil, infraerrors.Forbidden("MODEL_NOT_ALLOWED", "model is not allowed for this group")
	}
	var required *decimal.Decimal
	if parseErr == nil {
		if text, e := s.strictFundingTextCost(ctx, key, model, input, output, count); e == nil {
			v, _ := decimal.NewFromString(text)
			required = &v
		} else if base.IsShared() {
			return nil, nil, nil, e
		}
	}
	copy := *key
	copy.Funding = base
	// Existing personal image/audio and other unbounded adapters keep their
	// established path. They cannot change to a shared source after dispatch.
	if required == nil {
		if base.IsShared() {
			return nil, nil, nil, ErrTeamFundingCostUnbounded
		}
		return &copy, base.Subscription, body, nil
	}
	operationID := fundingOperationRequestIDForContext(ctx, key, idempotencyKey)
	admissionToken := uuid.NewString()
	if err := s.teamFunding.AcquireAdmission(ctx, operationID, key.ID, key.UserID, admissionToken); err != nil {
		return nil, nil, nil, err
	}
	excluded := map[string]bool{}
	var d *FundingDecision
	for attempts := 0; attempts < 32; attempts++ {
		d, err = s.resolveFundingDecisionExcluding(ctx, &copy, required, excluded)
		if err != nil {
			return nil, nil, nil, err
		}
		if d == nil {
			return nil, nil, nil, ErrFundingUnavailable
		}
		d.RequestID = operationID
		d.AdmissionToken = admissionToken
		groupID := int64(0)
		if key.GroupID != nil {
			groupID = *key.GroupID
		}
		if d.IsShared() {
			var res *TeamFundingReservation
			res, err = s.teamFunding.Reserve(ctx, &TeamFundingReserveCommand{RequestID: operationID, APIKeyID: key.ID, GroupID: groupID, MemberUserID: key.UserID, PayerUserID: d.PayerUserID, TeamID: d.TeamID, Source: d.Source, SubscriptionID: d.SubscriptionID, Amount: required.StringFixed(8)})
			if err == nil {
				d.ReservationID, d.ReservedAmount = res.ID, res.ReservedAmount
			}
		} else {
			var res *PersonalFundingReservation
			res, err = s.teamFunding.ReservePersonal(ctx, &PersonalFundingReserveCommand{RequestID: operationID, APIKeyID: key.ID, GroupID: groupID, MemberUserID: key.UserID, Source: d.Source, SubscriptionID: d.SubscriptionID, Amount: required.StringFixed(8)})
			if err == nil {
				d.ReservationID, d.ReservedAmount = res.ID, res.Amount
			}
		}
		if err == nil {
			break
		}
		if !errors.Is(err, ErrTeamFundingConflict) {
			return nil, nil, nil, err
		}
		excluded[fundingCandidateKey(d)] = true
	}
	if err != nil || d == nil || d.ReservationID <= 0 {
		return nil, nil, nil, ErrFundingUnavailable
	}
	guarded := WithFundingDispatchGuard(ctx, operationID, func(dispatchCtx context.Context) error {
		return s.teamFunding.BeginSnapshotDispatch(dispatchCtx, operationID, key.ID, key.UserID, d.ReservationID, d.IsShared())
	}, func(releaseCtx context.Context) error {
		if d.IsShared() {
			return s.teamFunding.ReleaseDispatchedUnused(releaseCtx, d.ReservationID, key.UserID)
		}
		return s.teamFunding.ReleasePersonalProvenUnused(releaseCtx, d.ReservationID, key.UserID)
	})
	WithFundingDispatchLimits(guarded, input, output, count)
	copy.Funding = cloneFundingDecision(d)
	copy.Funding.dispatchContext = guarded
	if key.Group != nil {
		group := *key.Group
		group.FallbackGroupID = nil
		group.FallbackGroupIDOnInvalidRequest = nil
		copy.Group = &group
	}
	return &copy, d.Subscription, prepared, nil
}
func fundingCandidateKey(d *FundingDecision) string {
	if d.SubscriptionID != nil {
		return fmt.Sprintf("%s:%d", d.Source, *d.SubscriptionID)
	}
	return d.Source
}

func fundingOperationRequestID(key *APIKey, idempotencyKey string) string {
	return fundingOperationRequestIDForContext(context.Background(), key, idempotencyKey)
}
func fundingOperationRequestIDForContext(ctx context.Context, key *APIKey, idempotencyKey string) string {
	token := ""
	if operation := DesktopInferenceOperationID(ctx); operation != "" {
		token = "desktop-operation:" + operation
	}
	if token == "" && key != nil && key.DesktopUsage != nil && ValidDesktopUsageID(key.DesktopUsage.CallID) {
		token = "desktop:" + key.DesktopUsage.CallID
	}
	if token == "" {
		normalized := strings.TrimSpace(idempotencyKey)
		if len(normalized) > 0 && len(normalized) <= 256 && strings.IndexFunc(normalized, func(r rune) bool { return r < 0x21 || r > 0x7e }) < 0 {
			token = "http:" + normalized
		}
	}
	if token == "" {
		token = "single:" + uuid.NewString()
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%d|%s", key.UserID, key.ID, token)))
	return fmt.Sprintf("team:%d:%d:%x", key.UserID, key.ID, sum[:])
}

// Only self-contained text calls have an enforceable output bound here. Local
// function definitions/results are text; provider-hosted tools, remote media and
// server-side conversation state need dedicated cost/identity adapters.
func prepareFundingTextRequest(path string, body []byte) (prepared []byte, model string, input, output, count int, err error) {
	fail := func() ([]byte, string, int, int, int, error) { return nil, "", 0, 0, 0, ErrTeamFundingCostUnbounded }
	if !FundingTextEndpoint(path) || len(body) == 0 || len(body) > FundingRequestBodyLimit {
		return fail()
	}
	var req map[string]any
	if json.Unmarshal(body, &req) != nil {
		return fail()
	}
	model, _ = req["model"].(string)
	if strings.TrimSpace(model) == "" {
		return fail()
	}
	for _, name := range []string{"previous_response_id", "conversation", "background", "audio", "modalities", "prediction", "web_search_options", "file_ids", "context_management", "compaction", "encrypted_content"} {
		if v, ok := req[name]; ok && v != nil && v != false {
			return fail()
		}
	}
	if tools, ok := req["tools"].([]any); ok {
		for _, tool := range tools {
			t, ok := tool.(map[string]any)
			if !ok {
				return fail()
			}
			kind, _ := t["type"].(string)
			if kind != "function" && !(kind == "" && t["input_schema"] != nil && t["name"] != nil) {
				return fail()
			}
		}
	}
	if fundingHasRemoteContent(req) {
		return fail()
	}
	count = 1
	for _, name := range []string{"n", "best_of"} {
		if v, ok := req[name]; ok {
			n, ok := fundingPositiveInt(v)
			if !ok || n > 128 {
				return fail()
			}
			if n > count {
				count = n
			}
		}
	}
	output = 0
	for _, name := range []string{"max_tokens", "max_output_tokens", "max_completion_tokens"} {
		if v, ok := req[name]; ok {
			n, ok := fundingPositiveInt(v)
			if !ok || n > 10000000 {
				return fail()
			}
			if n > output {
				output = n
			}
		}
	}
	if output == 0 {
		output = 4096
		name := "max_tokens"
		if strings.HasSuffix(path, "/responses") {
			name = "max_output_tokens"
		}
		req[name] = output
	}
	prepared, e := json.Marshal(req)
	if e != nil {
		return fail()
	}
	// UTF-8 bytes upper-bound text token bytes. The additional full payload and
	// 64K framing allowance cover message/tool serializers and fixed gateway
	// prompts; remote data and server conversation expansion are rejected above.
	input = 2*len(prepared) + 65536
	return prepared, model, input, output, count, nil
}
func fundingPositiveInt(v any) (int, bool) {
	n, ok := v.(float64)
	if !ok {
		if i, yes := v.(int); yes {
			return i, i > 0
		}
		return 0, false
	}
	return int(n), n > 0 && n == math.Trunc(n) && n <= 10000000
}
func fundingHasRemoteContent(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, value := range x {
			switch k {
			case "image_url", "input_image", "input_audio", "file_id", "file_url", "video_url", "audio_url", "encrypted_content":
				return true
			}
			if k == "type" {
				if t, ok := value.(string); ok {
					switch t {
					case "image", "image_url", "input_image", "input_audio", "audio", "video", "document", "file", "input_file", "computer_use", "web_search", "web_search_preview", "code_interpreter", "image_generation":
						return true
					}
				}
			}
			if fundingHasRemoteContent(value) {
				return true
			}
		}
	case []any:
		for _, value := range x {
			if fundingHasRemoteContent(value) {
				return true
			}
		}
	}
	return false
}

func (s *APIKeyService) strictFundingTextCostOne(ctx context.Context, key *APIKey, model string, input, output, count int) (string, error) {
	if s.fundingBilling == nil || s.fundingPricing == nil {
		return "", ErrTeamFundingCostUnbounded
	}
	// Account-level long-context opt-ins may activate ladders even when the
	// group toggle is off. Authorization uses every reachable price tier.
	pricingGroup := key.Group
	if key.Group != nil {
		copy := *key.Group
		copy.LongContextPricingEnabled = true
		pricingGroup = &copy
	}
	schedule, err := s.fundingBilling.ResolveContextPricingSchedule(ctx, s.fundingPricing, ContextPricingScheduleInput{Model: model, Group: pricingGroup, Platform: PlatformFromAPIKey(key)})
	if err != nil {
		return "", ErrTeamFundingCostUnbounded.WithCause(err)
	}
	resolved := s.fundingPricing.Resolve(ctx, PricingInput{Model: model, Group: pricingGroup, GroupID: key.GroupID})
	if resolved == nil {
		return "", ErrTeamFundingCostUnbounded
	}
	rate := 1.0
	if s.cfg != nil {
		rate = s.cfg.Default.RateMultiplier
	}
	if key.Group != nil {
		rate = key.Group.RateMultiplier
	}
	rates, err := s.GetUserGroupRates(ctx, key.UserID)
	if err != nil {
		return "", err
	}
	if key.GroupID != nil {
		if r, ok := rates[*key.GroupID]; ok {
			rate = r
		}
	}
	if key.Group != nil && key.Group.PeakRateEnabled {
		rate *= math.Max(1, key.Group.PeakRateMultiplier)
	}
	extra := 1.0
	priceVariants := []*ModelPricing{resolved.BasePricing}
	for _, tier := range resolved.Intervals {
		priceVariants = append(priceVariants, s.fundingPricing.GetIntervalPricing(resolved, tier.MinTokens+1))
	}
	for _, pricing := range priceVariants {
		for _, tier := range []string{"priority", "fast", "flex", OpenAIFastTierUltrafast} {
			extra = math.Max(extra, configuredServiceTierMultiplier(tier, pricing))
		}
	}
	reasoning := 1.0
	for _, pricing := range priceVariants {
		if pricing != nil {
			for _, m := range pricing.ReasoningEffortMultipliers {
				reasoning = math.Max(reasoning, m)
			}
		}
	}
	extra *= reasoning
	// Default DeepSeek peak pricing is time dependent in the billing function.
	if resolved.Source == PricingSourceLiteLLM && isDeepSeekModel(model) {
		extra *= 2
	}

	if cp := resolved.channelPricing; cp != nil {
		if resolved.Mode == BillingModePerRequest {
			perRequestReasoning := 1.0
			for _, m := range cp.ReasoningEffortMultipliers {
				perRequestReasoning = math.Max(perRequestReasoning, m)
			}
			extra *= perRequestReasoning
		}
		if cp.TimePricing != nil {
			timeFactor := 1.0
			for _, p := range cp.TimePricing.Periods {
				timeFactor = math.Max(timeFactor, p.Multiplier)
			}
			extra *= timeFactor
		}
	}
	cost := 0.0
	if resolved.Mode == BillingModePerRequest {
		cost = maxPerRequestPrice(resolved) * float64(count)
	} else {
		if schedule == nil || len(schedule.Tiers) == 0 {
			return "", ErrTeamFundingCostUnbounded
		}
		inPrice, outPrice := 0.0, 0.0
		for _, tier := range schedule.Tiers {
			for _, p := range []*float64{tier.Input, tier.CacheRead, tier.CacheWrite, tier.CacheWrite1h} {
				if p != nil {
					inPrice = math.Max(inPrice, *p)
				}
			}
			if tier.Output != nil {
				outPrice = math.Max(outPrice, *tier.Output)
			}
		}
		// Priority prices can have independent overrides rather than a multiplier.
		if p := resolved.BasePricing; p != nil {
			for _, v := range []float64{p.InputPricePerTokenPriority, p.CacheReadPricePerTokenPriority, p.CacheCreationPricePerTokenPriority} {
				inPrice = math.Max(inPrice, v)
			}
			outPrice = math.Max(outPrice, p.OutputPricePerTokenPriority)
		}
		cost = (float64(input)*inPrice + float64(output)*outPrice) * float64(count)
	}
	cost *= rate * extra
	if math.IsNaN(cost) || math.IsInf(cost, 0) || cost < 0 {
		return "", ErrTeamFundingCostUnbounded
	}
	// Round upward, never down, when authorizing an upper bound.
	return decimal.NewFromFloat(cost).RoundCeil(8).StringFixed(8), nil
}

// ReleaseUndispatchedFunding cannot refund a dispatched or uncertain request:
// the repository only releases a row whose durable state is still reserved.
func (s *APIKeyService) ReleaseUndispatchedFunding(ctx context.Context, key *APIKey) {
	if s == nil || s.teamFunding == nil || key == nil || !key.Funding.HasReservation() {
		return
	}
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	var err error
	if key.Funding.IsShared() {
		err = s.teamFunding.Release(releaseCtx, key.Funding.ReservationID, key.UserID)
	} else {
		err = s.teamFunding.ReleasePersonal(releaseCtx, key.Funding.ReservationID, key.UserID)
	}
	if err == nil {
		_ = s.teamFunding.ReleaseAdmission(releaseCtx, key.Funding.RequestID, key.ID, key.UserID, key.Funding.AdmissionToken)
	}
}

// Only explicitly selected subscriptions grant visibility. No owner account
// groups, administrator role or other privileges enter the member projection.
func (s *APIKeyService) addSharedSubscriptionGroups(ctx context.Context, userID int64, groups map[int64]bool) error {
	if s.teamFunding == nil {
		return nil
	}
	f, err := s.teamFunding.GetFundingContext(ctx, userID)
	if err != nil {
		return err
	}
	if f == nil || !f.Preferences.Configured || !fundingAllowsTeam(f.Preferences) || f.Team == nil || f.Team.Status != TeamMemberActive {
		return nil
	}
	subs, err := s.userSubRepo.ListActiveByUserID(ctx, f.Team.PayerUserID)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, sub := range subs {
		if containsFundingID(f.Team.SubscriptionIDs, sub.ID) && sub.Status == SubscriptionStatusActive && !sub.StartsAt.After(now) && sub.ExpiresAt.After(now) && sub.DeletedAt == nil {
			groups[sub.GroupID] = true
		}
	}
	return nil
}

func (s *APIKeyService) strictFundingTextCost(ctx context.Context, key *APIKey, model string, input, output, count int) (string, error) {
	if key.Group != nil && key.Group.Platform == PlatformComposite {
		return "", ErrTeamFundingCostUnbounded
	}
	candidates := []string{model}
	if s.fundingPricing != nil && s.fundingPricing.channelService != nil && key.GroupID != nil {
		m := s.fundingPricing.channelService.ResolveChannelMapping(ctx, *key.GroupID, model)
		if m.BillingModelSource == BillingModelSourceUpstream {
			return "", ErrTeamFundingCostUnbounded
		}
		if m.MappedModel != "" && m.MappedModel != model && m.BillingModelSource != BillingModelSourceRequested {
			candidates = append(candidates, m.MappedModel)
		}
	}
	best := decimal.Zero
	for _, candidate := range candidates {
		v, err := s.strictFundingTextCostOne(ctx, key, candidate, input, output, count)
		if err != nil {
			return "", err
		}
		amount, err := decimal.NewFromString(v)
		if err != nil {
			return "", err
		}
		best = decimal.Max(best, amount)
	}
	return best.StringFixed(8), nil
}

type fundingDispatchKey struct{}
type fundingDispatchGuard struct {
	requestID       string
	begin           func(context.Context) error
	releaseUnused   func(context.Context) error
	count           uint32
	maxInputBytes   int
	maxOutputTokens int
	maxCompletions  int
}

func WithFundingDispatchGuard(ctx context.Context, id string, begin func(context.Context) error, releaseUnused func(context.Context) error) context.Context {
	return context.WithValue(ctx, fundingDispatchKey{}, &fundingDispatchGuard{requestID: id, begin: begin, releaseUnused: releaseUnused})
}
func FundingDispatchRequestID(ctx context.Context) string {
	g, _ := ctx.Value(fundingDispatchKey{}).(*fundingDispatchGuard)
	if g == nil {
		return ""
	}
	return g.requestID
}
func BeginFundingDispatch(ctx context.Context) error {
	g, _ := ctx.Value(fundingDispatchKey{}).(*fundingDispatchGuard)
	if g == nil {
		return nil
	}
	if !atomic.CompareAndSwapUint32(&g.count, 0, 1) {
		return ErrTeamFundingConflict
	}
	if err := g.begin(ctx); err != nil {
		atomic.StoreUint32(&g.count, 0)
		return err
	}
	return nil
}
func cloneFundingDecision(d *FundingDecision) *FundingDecision {
	if d == nil {
		return nil
	}
	copy := *d
	if d.SubscriptionID != nil {
		id := *d.SubscriptionID
		copy.SubscriptionID = &id
	}
	if d.Subscription != nil {
		sub := *d.Subscription
		copy.Subscription = &sub
	}
	return &copy
}

func FundingDispatchContext(ctx context.Context, key *APIKey) context.Context {
	if key != nil && key.Funding != nil && key.Funding.dispatchContext != nil {
		return key.Funding.dispatchContext
	}
	return ctx
}

func fundingModeBlockedByRunMode(cfg *config.Config) bool {
	return cfg != nil && cfg.RunMode == config.RunModeSimple
}

func ReleaseFundingAfterDefinitiveRejection(ctx context.Context) error {
	g, _ := ctx.Value(fundingDispatchKey{}).(*fundingDispatchGuard)
	if g == nil || g.releaseUnused == nil {
		return nil
	}
	if err := g.releaseUnused(ctx); err != nil {
		return err
	}
	return nil
}
func IsDefinitiveNoChargeResponse(status int) bool {
	switch status {
	case 400, 401, 403, 404, 413:
		return true
	}
	return false
}

func WithFundingDispatchLimits(ctx context.Context, inputBytes, outputTokens, completions int) {
	g, _ := ctx.Value(fundingDispatchKey{}).(*fundingDispatchGuard)
	if g != nil {
		g.maxInputBytes = inputBytes
		g.maxOutputTokens = outputTokens
		g.maxCompletions = completions
	}
}

// ValidateFundingDispatchRequest rechecks the final transformed payload. In
// particular ChatGPT OAuth strips output limits, so those paths cannot consume
// a bounded reservation until their upstream protocol supports a real cap.
func ValidateFundingDispatchRequest(req *http.Request) error {
	if req == nil {
		return nil
	}
	g, _ := req.Context().Value(fundingDispatchKey{}).(*fundingDispatchGuard)
	if g == nil || g.maxOutputTokens == 0 {
		return nil
	}
	if req.GetBody == nil {
		return ErrTeamFundingCostUnbounded
	}
	body, err := req.GetBody()
	if err != nil {
		return ErrTeamFundingCostUnbounded.WithCause(err)
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, int64(g.maxInputBytes)+1))
	if err != nil || len(data) > g.maxInputBytes {
		return ErrTeamFundingCostUnbounded
	}
	var fields map[string]any
	if json.Unmarshal(data, &fields) != nil {
		return ErrTeamFundingCostUnbounded
	}
	found := false
	for _, name := range []string{"max_tokens", "max_output_tokens", "max_completion_tokens"} {
		if v, ok := fields[name]; ok {
			limit, valid := fundingPositiveInt(v)
			if !valid || limit > g.maxOutputTokens {
				return ErrTeamFundingCostUnbounded
			}
			found = true
		}
	}
	if !found {
		return ErrTeamFundingCostUnbounded
	}
	for _, name := range []string{"n", "best_of"} {
		if v, ok := fields[name]; ok {
			n, valid := fundingPositiveInt(v)
			if !valid || n > g.maxCompletions {
				return ErrTeamFundingCostUnbounded
			}
		}
	}
	if fundingHasRemoteContent(fields) {
		return ErrTeamFundingCostUnbounded
	}
	if tools, ok := fields["tools"].([]any); ok {
		for _, raw := range tools {
			tool, ok := raw.(map[string]any)
			if !ok {
				return ErrTeamFundingCostUnbounded
			}
			kind, _ := tool["type"].(string)
			if kind != "function" && !(kind == "" && tool["input_schema"] != nil && tool["name"] != nil) {
				return ErrTeamFundingCostUnbounded
			}
		}
	}
	return nil
}

func FundingTextEndpoint(path string) bool {
	return strings.HasSuffix(path, "/chat/completions") || strings.HasSuffix(path, "/responses") || strings.HasSuffix(path, "/messages")
}
