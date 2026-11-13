package repository

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type sharedHTTPFundingRepo struct {
	service.TeamFundingRepository
	funding       *service.FundingContext
	admissions    map[string]string
	claimed       map[string]bool
	statuses      map[int64]string
	commands      []*service.TeamFundingReserveCommand
	personalCalls int
}

func (r *sharedHTTPFundingRepo) GetFundingContext(context.Context, int64) (*service.FundingContext, error) {
	return r.funding, nil
}
func (r *sharedHTTPFundingRepo) AcquireFundingAdmission(_ context.Context, id string, _, _ int64, token string) error {
	if r.admissions[id] != "" || r.claimed[id] {
		return service.ErrUsageBillingRequestConflict
	}
	r.admissions[id] = token
	return nil
}
func (r *sharedHTTPFundingRepo) ReleaseFundingAdmission(_ context.Context, id string, _, _ int64, token string) error {
	if r.admissions[id] == token && !r.claimed[id] {
		delete(r.admissions, id)
	}
	return nil
}
func (r *sharedHTTPFundingRepo) Reserve(_ context.Context, cmd *service.TeamFundingReserveCommand) (*service.TeamFundingReservation, error) {
	copy := *cmd
	r.commands = append(r.commands, &copy)
	id := int64(len(r.commands))
	r.statuses[id] = "reserved"
	return &service.TeamFundingReservation{ID: id, ReservedAmount: cmd.Amount}, nil
}
func (r *sharedHTTPFundingRepo) Release(_ context.Context, id, _ int64) error {
	if r.statuses[id] == "reserved" {
		r.statuses[id] = "released"
	}
	return nil
}
func (r *sharedHTTPFundingRepo) ClaimFundingDispatch(_ context.Context, id string, _, _ int64) error {
	if r.claimed[id] {
		return service.ErrUsageBillingRequestConflict
	}
	r.claimed[id] = true
	return nil
}
func (r *sharedHTTPFundingRepo) BeginFundingSnapshotDispatch(ctx context.Context, operation string, key, actor, id int64, shared bool) error {
	if !shared || r.statuses[id] != "reserved" {
		return service.ErrTeamFundingConflict
	}
	if err := r.ClaimFundingDispatch(ctx, operation, key, actor); err != nil {
		return err
	}
	r.statuses[id] = "dispatched"
	return nil
}
func (r *sharedHTTPFundingRepo) ReservePersonalFunding(context.Context, *service.PersonalFundingReserveCommand) (*service.PersonalFundingReservation, error) {
	r.personalCalls++
	return nil, service.ErrFundingUnavailable
}
func (r *sharedHTTPFundingRepo) ReleasePersonalFunding(context.Context, int64, int64) error {
	return nil
}
func (r *sharedHTTPFundingRepo) MarkPersonalFundingDispatched(context.Context, int64, int64) error {
	return nil
}
func (r *sharedHTTPFundingRepo) ReleaseDispatchedPersonalUnused(context.Context, int64, int64) error {
	return nil
}
func (r *sharedHTTPFundingRepo) SettlePersonalFundingReservationTx(context.Context, *sql.Tx, int64, string, int64, string) (*service.TeamFundingSettlement, error) {
	return nil, service.ErrFundingUnavailable
}

// The private personal repository also has a transactional settlement method;
// embedding the existing interface supplies it without exercising settlement in
// this dispatch integration test.
type sharedHTTPUserRepo struct {
	service.UserRepository
	user *service.User
}

func (r sharedHTTPUserRepo) GetByID(context.Context, int64) (*service.User, error) {
	return r.user, nil
}

type sharedHTTPSubscriptionRepo struct {
	service.UserSubscriptionRepository
	subs []service.UserSubscription
}

func (r sharedHTTPSubscriptionRepo) ListActiveByUserID(_ context.Context, user int64) ([]service.UserSubscription, error) {
	var result []service.UserSubscription
	for _, sub := range r.subs {
		if sub.UserID == user {
			result = append(result, sub)
		}
	}
	return result, nil
}

type sharedHTTPAccountRepo struct {
	service.AccountRepository
	account *service.Account
	lookups int
}

func (r *sharedHTTPAccountRepo) GetByID(context.Context, int64) (*service.Account, error) {
	r.lookups++
	return r.account, nil
}

type redirectedFundingHTTPUpstream struct {
	service.HTTPUpstream
	target *url.URL
}

func (u redirectedFundingHTTPUpstream) Do(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	copy := req.Clone(req.Context())
	copiedURL := *u.target
	copy.URL = &copiedURL
	copy.Host = copiedURL.Host
	return u.HTTPUpstream.Do(copy, proxy, id, concurrency)
}
func (u redirectedFundingHTTPUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, concurrency)
}

func TestHTTPUpstreamSharedOAuthTransformAndAccountEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, source := range []string{service.TeamFundingSourceBalance, service.TeamFundingSourceSubscription} {
		t.Run(source, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.False(t, gjson.GetBytes(body, "max_output_tokens").Exists(), "the real OAuth transform must strip its unsupported cap")
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"provider-result\",\"status\":\"completed\",\"model\":\"funding-oauth-model\",\"output\":[],\"usage\":{\"input_tokens\":2,\"output_tokens\":1}}}\n\n")
			}))
			defer server.Close()
			cfg := &config.Config{}
			cfg.Security.URLAllowlist.Enabled = false
			cfg.Security.URLAllowlist.AllowInsecureHTTP = true
			cfg.Security.URLAllowlist.AllowPrivateHosts = true
			actor := &service.User{ID: 22, Balance: 0}
			price := 1.0
			group := &service.Group{ID: 3, Platform: service.PlatformOpenAI, RateMultiplier: 1, ModelPricing: []service.ChannelModelPricing{{Models: []string{"funding-oauth-model"}, BillingMode: service.BillingModePerRequest, PerRequestPrice: &price}}}
			team := &service.TeamFundingCandidate{TeamID: 4, MemberUserID: actor.ID, PayerUserID: 11, Status: service.TeamMemberActive, SharedMode: service.TeamSharedModeBalanceOnly, AvailableBalance: "100", Spent: "0", Reserved: "0"}
			var subscriptions []service.UserSubscription
			if source == service.TeamFundingSourceSubscription {
				group.SubscriptionType = service.SubscriptionTypeSubscription
				team.SharedMode, team.SubscriptionIDs = service.TeamSharedModeSubscriptionOnly, []int64{9}
				subscriptions = []service.UserSubscription{{ID: 9, UserID: 11, GroupID: group.ID, Status: service.SubscriptionStatusActive, StartsAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour)}}
			}
			repo := &sharedHTTPFundingRepo{funding: &service.FundingContext{Preferences: service.FundingPreferences{Configured: true, Mode: service.FundingModeTeamOnly}, Team: team}, admissions: map[string]string{}, claimed: map[string]bool{}, statuses: map[int64]string{}}
			keys := service.NewAPIKeyService(nil, sharedHTTPUserRepo{user: actor}, nil, sharedHTTPSubscriptionRepo{subs: subscriptions}, nil, nil, cfg)
			billing := service.NewBillingService(cfg, nil)
			pricing := service.NewModelPricingResolver(nil, billing)
			keys.SetTeamFundingService(service.NewTeamFundingService(repo))
			keys.SetFundingPricing(billing, pricing)
			key := &service.APIKey{ID: 7, UserID: actor.ID, User: actor, GroupID: &group.ID, Group: group}
			prepared, _, body, err := keys.PrepareConfiguredFundingRequest(t.Context(), key, "/v1/responses", []byte(`{"model":"funding-oauth-model","input":"hello","max_output_tokens":16}`), "stable-oauth")
			require.NoError(t, err)
			account := &service.Account{ID: 5, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Credentials: map[string]any{"access_token": "fixture"}}
			account.SetUpstreamModelMetadataSnapshot(service.UpstreamModelMetadataSnapshot{Source: "upstream", Models: map[string]service.UpstreamModelMetadata{"funding-oauth-model": {ContextWindow: 100000, MaxOutputTokens: 20000}}})
			accounts := &sharedHTTPAccountRepo{account: account}
			target, err := url.Parse(server.URL)
			require.NoError(t, err)
			transport := redirectedFundingHTTPUpstream{HTTPUpstream: ProvideHTTPUpstream(cfg, accounts), target: target}
			gateway := service.NewOpenAIGatewayService(accounts, nil, nil, nil, nil, nil, nil, cfg, nil, nil, billing, nil, nil, transport, nil, nil, nil, pricing, nil, nil, nil, nil)
			ctx := service.FundingDispatchContext(t.Context(), prepared)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
			_, err = gateway.Forward(ctx, c, account, body)
			require.NoError(t, err)
			require.Equal(t, int32(1), calls.Load())
			require.Equal(t, 1, accounts.lookups)
			require.Equal(t, source, prepared.Funding.Source)
			require.Equal(t, int64(11), prepared.Funding.PayerUserID)
			require.Equal(t, 20000, prepared.Funding.Plan.OutputTokens)
			require.True(t, prepared.Funding.Plan.ProviderBound)
			require.Equal(t, source, repo.commands[0].Source)
			require.Zero(t, repo.personalCalls)
			require.Zero(t, actor.Balance)
			_, err = gateway.Forward(ctx, c, account, body)
			require.Error(t, err)
			require.Equal(t, int32(1), calls.Load(), "the same funded operation cannot dispatch twice")
		})
	}
}
