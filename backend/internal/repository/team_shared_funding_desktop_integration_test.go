//go:build integration

package repository_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTeamFundingManagementHTTPUsesJWTActor(t *testing.T) {
	r := newDesktopModelRig(t)
	owner, member, outsider := r.user(t, service.StatusActive, false), r.user(t, service.StatusActive, false), r.user(t, service.StatusActive, false)
	funding := service.NewTeamFundingService(repository.NewTeamFundingRepository(r.client, repository.GetIntegrationDB()))
	h := handler.NewTeamFundingHandler(funding)
	user := r.router.Group("/api/v1/user")
	user.Use(gin.HandlerFunc(middleware.NewJWTAuthMiddleware(r.auth, r.users, r.settings, nil)))
	user.POST("/teams", h.CreateTeam)
	user.GET("/teams/:id", h.GetTeam)
	user.POST("/teams/:id/invitations", h.CreateInvitation)
	user.POST("/team-invitations/:id/accept", h.AcceptInvitation)
	user.PATCH("/teams/:id/members/:user_id", h.UpdateMember)
	user.PUT("/funding", h.UpdateFunding)
	ownerToken, err := r.auth.GenerateToken(r.ctx, owner)
	require.NoError(t, err)
	memberToken, err := r.auth.GenerateToken(r.ctx, member)
	require.NoError(t, err)
	outsiderToken, err := r.auth.GenerateToken(r.ctx, outsider)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, r.request(http.MethodPost, "/api/v1/user/teams", `{"name":"http team"}`, "").Code)
	created := r.request(http.MethodPost, "/api/v1/user/teams", `{"name":"http team"}`, ownerToken)
	require.Equal(t, http.StatusOK, created.Code, created.Body.String())
	var team struct {
		Data service.TeamDetail `json:"data"`
	}
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &team))
	require.Equal(t, owner.ID, team.Data.OwnerUserID)
	invitePath := fmt.Sprintf("/api/v1/user/teams/%d/invitations", team.Data.ID)
	invited := r.request(http.MethodPost, invitePath, fmt.Sprintf(`{"user_id":%d}`, member.ID), ownerToken)
	require.Equal(t, http.StatusOK, invited.Code, invited.Body.String())
	var invitation struct {
		Data service.Invitation `json:"data"`
	}
	require.NoError(t, json.Unmarshal(invited.Body.Bytes(), &invitation))
	acceptPath := fmt.Sprintf("/api/v1/user/team-invitations/%d/accept", invitation.Data.ID)
	require.Equal(t, http.StatusForbidden, r.request(http.MethodPost, acceptPath, `{}`, outsiderToken).Code)
	accepted := r.request(http.MethodPost, acceptPath, `{}`, memberToken)
	require.Equal(t, http.StatusOK, accepted.Code, accepted.Body.String())
	memberPath := fmt.Sprintf("/api/v1/user/teams/%d/members/%d", team.Data.ID, member.ID)
	require.Equal(t, http.StatusForbidden, r.request(http.MethodPatch, memberPath, `{"spend_limit":"1"}`, memberToken).Code)
	patched := r.request(http.MethodPatch, memberPath, `{"spend_limit":"100.00000000"}`, ownerToken)
	require.Equal(t, http.StatusOK, patched.Code, patched.Body.String())
	configured := r.request(http.MethodPut, "/api/v1/user/funding", fmt.Sprintf(`{"mode":"team_only","team_id":%d,"allow_personal_balance":false,"allow_personal_subscription":false}`, team.Data.ID), memberToken)
	require.Equal(t, http.StatusOK, configured.Code, configured.Body.String())
	var view struct {
		Data service.FundingView `json:"data"`
	}
	require.NoError(t, json.Unmarshal(configured.Body.Bytes(), &view))
	require.Equal(t, service.FundingModeTeamOnly, view.Data.Mode)
	require.False(t, view.Data.AllowPersonalBalance)
	require.False(t, view.Data.AllowPersonalSubscription)
	require.Len(t, view.Data.Teams, 1)
	require.Equal(t, "100.00000000", *view.Data.Teams[0].SpendLimit)
}

func TestDesktopTeamSharedBalanceZeroPersonalWallet(t *testing.T) {
	r := newDesktopModelRig(t)
	owner, member := r.user(t, service.StatusActive, false), r.user(t, service.StatusActive, false)
	_, err := r.userRepo.SetBalance(r.ctx, owner.ID, 500)
	require.NoError(t, err)
	_, err = r.userRepo.SetBalance(r.ctx, member.ID, 0)
	require.NoError(t, err)

	db := repository.GetIntegrationDB()
	funding := service.NewTeamFundingService(repository.NewTeamFundingRepository(r.client, db))
	r.keys.SetTeamFundingService(funding)
	team, err := funding.CreateTeam(r.ctx, owner.ID, &service.TeamCreateCommand{Name: "desktop shared billing"})
	require.NoError(t, err)
	invitation, err := funding.CreateInvitation(r.ctx, owner.ID, team.ID, &service.InvitationCreateCommand{UserID: member.ID})
	require.NoError(t, err)
	require.NoError(t, funding.AcceptInvitation(r.ctx, member.ID, invitation.ID))
	limit := "100.00000000"
	_, err = funding.UpdateMember(r.ctx, owner.ID, team.ID, member.ID, &service.TeamMemberPatch{SpendLimit: service.OptionalFundingAmount{Set: true, Value: &limit}})
	require.NoError(t, err)
	_, err = funding.UpdateFunding(r.ctx, member.ID, &service.FundingUpdateCommand{Mode: service.FundingModeTeamOnly, TeamID: &team.ID})
	require.NoError(t, err)

	group := r.group(t, false)
	inputPrice, outputPrice := 2e-6, 6e-6
	group.ModelPricing = []service.ChannelModelPricing{{Models: []string{"fixture-team-billing"}, InputPrice: &inputPrice, OutputPrice: &outputPrice}}
	require.NoError(t, r.groups.Update(r.ctx, group))
	require.NoError(t, r.userRepo.AddGroupToAllowedGroups(r.ctx, member.ID, group.ID))
	entry := desktopEntry("team-shared-device-model", group.ID)
	entry.Model, entry.APIMode = "fixture-team-billing", "responses"
	r.catalog(t, []service.DesktopModelEntry{entry}, entry.ID)

	device := service.NewDesktopDeviceKeyService(repository.NewDesktopDeviceKeyRepository(r.client), r.keys)
	identity, err := device.Ensure(r.ctx, member.ID, service.DesktopDeviceKeyRequest{DeviceID: uuid.NewString()})
	require.NoError(t, err)
	accountRow, err := r.client.Account.Create().SetName("team-funding-desktop-fixture").SetPlatform(service.PlatformOpenAI).SetType(service.AccountTypeAPIKey).SetCredentials(map[string]any{}).Save(r.ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.client.Account.DeleteOneID(accountRow.ID).Exec(r.ctx) })

	var upstreamCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp_team_fixture","status":"completed","model":"fixture-team-billing","output":[],"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`)
	}))
	t.Cleanup(upstream.Close)
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.AllowPrivateHosts = true
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	billingCache := service.NewBillingCacheService(nil, r.userRepo, r.subs, nil, nil, r.rates, cfg, nil)
	billingCache.SetTeamFundingService(funding)
	t.Cleanup(billingCache.Stop)
	logs := repository.NewUsageLogRepository(r.client, db)
	gateway := service.NewOpenAIGatewayService(nil, logs, repository.NewUsageBillingRepository(r.client, db), r.userRepo, r.subs, r.rates, nil, cfg, nil, nil, r.billing, nil, billingCache, repository.NewHTTPUpstream(cfg), &service.DeferredService{}, nil, nil, r.pricing, nil, nil, nil, nil)
	subscriptions := service.NewSubscriptionService(r.groups, r.subs, billingCache, r.client, cfg)
	t.Cleanup(subscriptions.Stop)
	router := gin.New()
	router.POST("/v1/responses", gin.HandlerFunc(middleware.ProvideAPIKeyAuthMiddleware(r.keys, subscriptions, cfg, r.models, nil)), func(c *gin.Context) {
		key, ok := middleware.GetAPIKeyFromContext(c)
		require.True(t, ok)
		require.NotNil(t, key.Funding)
		require.Equal(t, service.TeamFundingSourceBalance, key.Funding.Source)
		require.Equal(t, owner.ID, key.Funding.PayerUserID)
		subscription, _ := middleware.GetSubscriptionFromContext(c)
		body, readErr := io.ReadAll(c.Request.Body)
		require.NoError(t, readErr)
		account := &service.Account{ID: accountRow.ID, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"base_url": upstream.URL, "api_key": "fixture"}}
		result, forwardErr := gateway.Forward(c.Request.Context(), c, account, body)
		require.NoError(t, forwardErr)
		require.NotNil(t, result)
		result.RequestID = "desktop-team-shared-request"
		usage := &service.OpenAIRecordUsageInput{Result: result, APIKey: key, User: key.User, Account: account, Subscription: subscription}
		require.NoError(t, gateway.RecordUsage(context.Background(), usage))
		// A duplicate billing task for one dispatched request must not settle twice.
		require.NoError(t, gateway.RecordUsage(context.Background(), usage))
		c.Status(http.StatusOK)
	})
	operation := uuid.NewString()
	dispatch := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"fixture-team-billing","input":"hello"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+identity.APIKey)
		request.Header.Set("X-Aino-Model-ID", entry.ID)
		request.Header.Set("Idempotency-Key", operation)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	response := dispatch()
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	replay := dispatch()
	require.Equal(t, http.StatusConflict, replay.Code, replay.Body.String())
	require.EqualValues(t, 1, upstreamCalls.Load(), "replaying a paid operation must not call the provider twice")

	var ownerBalance, memberBalance string
	require.NoError(t, db.QueryRowContext(r.ctx, "SELECT balance::numeric(20,8)::text FROM users WHERE id=$1", owner.ID).Scan(&ownerBalance))
	require.NoError(t, db.QueryRowContext(r.ctx, "SELECT balance::numeric(20,8)::text FROM users WHERE id=$1", member.ID).Scan(&memberBalance))
	require.Equal(t, "0.00000000", memberBalance)
	require.Less(t, ownerBalance, "500.00000000")
	ledger, err := funding.ListLedger(r.ctx, owner.ID, team.ID, 1, 20)
	require.NoError(t, err)
	require.Len(t, ledger.Items, 1)
	require.Equal(t, member.ID, ledger.Items[0].UserID)
	require.Equal(t, owner.ID, ledger.Items[0].PayerUserID)
	require.Equal(t, service.TeamFundingSourceBalance, ledger.Items[0].Source)
	require.NotEmpty(t, ledger.Items[0].RequestID)
	var spent, reserved string
	require.NoError(t, db.QueryRowContext(r.ctx, "SELECT spent::numeric(20,8)::text,reserved::numeric(20,8)::text FROM team_members WHERE team_id=$1 AND user_id=$2", team.ID, member.ID).Scan(&spent, &reserved))
	require.Equal(t, "0.00000000", reserved)
	require.NotEqual(t, "0.00000000", spent)
	var debit string
	require.NoError(t, db.QueryRowContext(r.ctx, "SELECT (500::numeric-balance)::numeric(20,8)::text FROM users WHERE id=$1", owner.ID).Scan(&debit))
	require.Equal(t, spent, debit)
	var usageCount int
	require.NoError(t, db.QueryRowContext(r.ctx, "SELECT COUNT(*) FROM usage_logs WHERE request_id=$1", "desktop-team-shared-request").Scan(&usageCount))
	require.Equal(t, 1, usageCount)
}
