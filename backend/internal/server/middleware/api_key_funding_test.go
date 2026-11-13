//go:build unit

package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type readOnlyFundingRepo struct {
	service.TeamFundingRepository
	holds    int
	personal bool
}

func (r *readOnlyFundingRepo) GetFundingContext(context.Context, int64) (*service.FundingContext, error) {
	if r.personal {
		return &service.FundingContext{Preferences: service.FundingPreferences{Configured: true, Mode: service.FundingModePersonalOnly, AllowPersonalBalance: true}}, nil
	}
	teamID := int64(4)
	return &service.FundingContext{
		Preferences: service.FundingPreferences{Configured: true, Mode: service.FundingModeTeamOnly, TeamID: &teamID},
		Team:        &service.TeamFundingCandidate{TeamID: teamID, MemberUserID: 22, PayerUserID: 11, Status: service.TeamMemberActive, SharedMode: service.TeamSharedModeBalanceOnly, AvailableBalance: "100", Spent: "0", Reserved: "0"},
	}, nil
}

func (r *readOnlyFundingRepo) Reserve(context.Context, *service.TeamFundingReserveCommand) (*service.TeamFundingReservation, error) {
	r.holds++
	return nil, service.ErrTeamFundingConflict
}

type readOnlyFundingSubscriptions struct {
	service.UserSubscriptionRepository
}

func (readOnlyFundingSubscriptions) ListActiveByUserID(context.Context, int64) ([]service.UserSubscription, error) {
	return nil, nil
}

func readOnlyFundingAuth(t *testing.T, google bool, edit func(*service.APIKey)) (gin.HandlerFunc, *readOnlyFundingRepo) {
	return readOnlyFundingAuthSource(t, google, false, edit)
}

func readOnlyFundingAuthSource(t *testing.T, google, personal bool, edit func(*service.APIKey)) (gin.HandlerFunc, *readOnlyFundingRepo) {
	t.Helper()
	group := &service.Group{ID: 3, Status: service.StatusActive, Platform: service.PlatformOpenAI}
	key := &service.APIKey{ID: 7, UserID: 22, Status: service.StatusActive, GroupID: &group.ID, Group: group, User: &service.User{ID: 22, Status: service.StatusActive, Balance: 0}}
	if personal {
		key.User.Balance = 100
	}
	if edit != nil {
		edit(key)
	}
	repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { return key, nil }}
	cfg := &config.Config{}
	users := &stubUserRepo{getByID: func(context.Context, int64) (*service.User, error) { return key.User, nil }}
	keys := service.NewAPIKeyService(repo, users, nil, readOnlyFundingSubscriptions{}, nil, nil, cfg)
	fundingRepo := &readOnlyFundingRepo{personal: personal}
	keys.SetTeamFundingService(service.NewTeamFundingService(fundingRepo))
	if google {
		return APIKeyAuthWithSubscriptionGoogle(keys, nil, cfg), fundingRepo
	}
	return gin.HandlerFunc(NewAPIKeyAuthMiddleware(keys, nil, cfg)), fundingRepo
}

func TestAPIKeyAuthTeamOnlyCountTokensPreservesFreeRequest(t *testing.T) {
	for _, path := range []string{"/v1/messages/count_tokens", "/messages/count_tokens", "/antigravity/v1/messages/count_tokens"} {
		auth, funding := readOnlyFundingAuth(t, false, nil)
		r := gin.New()
		r.Use(auth)
		body := `{"model":"gpt-5","messages":[{"role":"user","content":"hello"}]}`
		r.POST(path, func(c *gin.Context) {
			key, ok := GetAPIKeyFromContext(c)
			require.True(t, ok)
			require.Equal(t, service.TeamFundingSourceBalance, key.Funding.Source)
			require.Zero(t, key.Funding.ReservationID)
			require.Empty(t, service.FundingDispatchRequestID(c.Request.Context()))
			actual, err := io.ReadAll(c.Request.Body)
			require.NoError(t, err)
			require.Equal(t, body, string(actual))
			c.Status(http.StatusOK)
		})
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("x-api-key", "sk-read-only-funding")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Zero(t, funding.holds)
	}
}

func TestAPIKeyAuthOversizedFundingPreReadPreservesPersonalBody(t *testing.T) {
	body := strings.Repeat("x", service.ConfiguredFundingBodyReadLimit+123)
	for _, personal := range []bool{true, false} {
		auth, funding := readOnlyFundingAuthSource(t, false, personal, nil)
		r := gin.New()
		r.Use(auth)
		called := false
		r.POST("/v1/messages", func(c *gin.Context) {
			called = true
			key, ok := GetAPIKeyFromContext(c)
			require.True(t, ok)
			require.Equal(t, service.FundingSourcePersonalBalance, key.Funding.Source)
			require.Zero(t, key.Funding.ReservationID)
			require.Equal(t, int64(len(body)), c.Request.ContentLength)
			actual, err := io.ReadAll(c.Request.Body)
			require.NoError(t, err, "funding pre-read must not impose its limit on the personal adapter")
			require.Equal(t, body, string(actual))
			c.Status(http.StatusOK)
		})
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
		req.Header.Set("x-api-key", "sk-read-only-funding")
		r.ServeHTTP(w, req)
		if personal {
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.True(t, called)
		} else {
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			require.False(t, called)
		}
		require.Zero(t, funding.holds)
	}
}

func TestAPIKeyAuthTeamOnlyModelReadsDoNotReserve(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		path, requestPath string
		google            bool
	}{
		{path: "/v1/models", requestPath: "/v1/models"},
		{path: "/v1/models/:model", requestPath: "/v1/models/gpt-5"},
		{path: "/models", requestPath: "/models"},
		{path: "/models/:model", requestPath: "/models/gpt-5"},
		{path: "/backend-api/codex/models", requestPath: "/backend-api/codex/models"},
		{path: "/antigravity/v1/models", requestPath: "/antigravity/v1/models"},
		{path: "/v1beta/models", requestPath: "/v1beta/models", google: true},
		{path: "/v1beta/models/:model", requestPath: "/v1beta/models/gemini-2.5-pro", google: true},
		{path: "/antigravity/v1beta/models", requestPath: "/antigravity/v1beta/models", google: true},
		{path: "/antigravity/v1beta/models/:model", requestPath: "/antigravity/v1beta/models/gemini-2.5-pro", google: true},
	} {
		t.Run(tc.requestPath, func(t *testing.T) {
			auth, funding := readOnlyFundingAuth(t, tc.google, nil)
			r := gin.New()
			r.Use(auth)
			r.GET(tc.path, func(c *gin.Context) {
				key, ok := GetAPIKeyFromContext(c)
				require.True(t, ok)
				require.Equal(t, service.TeamFundingSourceBalance, key.Funding.Source)
				require.Zero(t, key.Funding.ReservationID)
				require.Empty(t, service.FundingDispatchRequestID(c.Request.Context()))
				c.Status(http.StatusOK)
			})
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.requestPath, nil)
			req.Header.Set("x-api-key", "sk-read-only-funding")
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Zero(t, funding.holds)
		})
	}
}

func TestAPIKeyAuthTeamOnlyModelReadsRetainKeyAndGroupChecks(t *testing.T) {
	for _, edit := range []func(*service.APIKey){
		func(k *service.APIKey) { k.Status = service.StatusAPIKeyExpired },
		func(k *service.APIKey) { expired := time.Now().Add(-time.Minute); k.ExpiresAt = &expired },
		func(k *service.APIKey) { k.Quota = 1; k.QuotaUsed = 1 },
		func(k *service.APIKey) { k.Group.Status = service.StatusDisabled },
		func(k *service.APIKey) { k.Group.IsExclusive = true },
	} {
		for _, google := range []bool{false, true} {
			auth, funding := readOnlyFundingAuth(t, google, edit)
			r := gin.New()
			r.Use(auth)
			r.GET("/v1/models", func(c *gin.Context) { t.Fatal("invalid key or group reached handler") })
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			req.Header.Set("x-api-key", "sk-read-only-funding")
			r.ServeHTTP(w, req)
			require.NotEqual(t, http.StatusOK, w.Code)
			require.Zero(t, funding.holds)
		}
	}
}

func TestAPIKeyAuthSharedUnrecognizedReadStillFailsClosed(t *testing.T) {
	for _, path := range []string{"/v1/live/call-1", "/v1/models/gpt-5/result", "/v1beta/cachedContents/cache-1"} {
		auth, funding := readOnlyFundingAuth(t, false, nil)
		r := gin.New()
		r.Use(auth)
		r.GET(path, func(c *gin.Context) { t.Fatal("unbounded shared read reached handler") })
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("x-api-key", "sk-read-only-funding")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), "TEAM_FUNDING_COST_UNBOUNDED")
		require.Zero(t, funding.holds)
	}
}
