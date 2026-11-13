//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type teamFundingHandlerRepo struct {
	service.TeamFundingRepository
	actor       int64
	deletedTeam int64
	deleteCalls int
	deleteError error
}

func (r *teamFundingHandlerRepo) DeleteTeam(_ context.Context, u, team int64) error {
	r.actor, r.deletedTeam = u, team
	r.deleteCalls++
	return r.deleteError
}

func (r *teamFundingHandlerRepo) GetFunding(_ context.Context, u int64) (*service.FundingView, error) {
	r.actor = u
	return &service.FundingView{FundingPreferences: service.FundingPreferences{Configured: true, Mode: "personal_only"}, PersonalBalance: "10.00000000", PersonalSubscriptions: []service.FundingSubscription{}, Teams: []service.TeamSummary{}}, nil
}
func TestTeamFundingHandlerUsesAuthenticatedSubjectAndEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &teamFundingHandlerRepo{}
	h := NewTeamFundingHandler(service.NewTeamFundingService(repo))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/funding?user_id=99", nil)
	h.GetFunding(c)
	require.Equal(t, 401, w.Code)
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/funding?user_id=99", nil)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
	h.GetFunding(c)
	require.Equal(t, 200, w.Code)
	require.Equal(t, int64(7), repo.actor)
	require.Contains(t, w.Body.String(), `"code":0`)
	require.Contains(t, w.Body.String(), `"personal_balance":"10.00000000"`)
}
func TestTeamFundingHandlerRejectsOwnerPersonalAuthorizationFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewTeamFundingHandler(service.NewTeamFundingService(&teamFundingHandlerRepo{}))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPatch, "/teams/1/members/2", strings.NewReader(`{"allow_personal_balance":true}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: "1"}, {Key: "user_id", Value: "2"}}
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
	h.UpdateMember(c)
	require.Equal(t, 400, w.Code)
}

func TestTeamDeletionHandlerUsesJWTActorAndValidatesTeamID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name       string
		team       string
		user       int64
		repoError  error
		statusCode int
		calls      int
	}{
		{name: "authentication required", team: "11", statusCode: 401},
		{name: "invalid ID", team: "invalid", user: 7, statusCode: 400},
		{name: "zero ID", team: "0", user: 7, statusCode: 400},
		{name: "negative ID", team: "-1", user: 7, statusCode: 400},
		{name: "overflow ID", team: "9223372036854775808", user: 7, statusCode: 400},
		{name: "JWT identity overrides request", team: "11", user: 7, statusCode: 200, calls: 1},
		{name: "member forbidden", team: "11", user: 7, repoError: service.ErrTeamFundingForbidden, statusCode: 403, calls: 1},
		{name: "team not found", team: "11", user: 7, repoError: service.ErrTeamFundingNotFound, statusCode: 404, calls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &teamFundingHandlerRepo{deleteError: test.repoError}
			h := NewTeamFundingHandler(service.NewTeamFundingService(repo))
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodDelete, "/teams/"+test.team+"?user_id=99", strings.NewReader(`{"owner_user_id":99}`))
			c.Params = gin.Params{{Key: "id", Value: test.team}}
			if test.user > 0 {
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: test.user})
			}
			h.DeleteTeam(c)
			require.Equal(t, test.statusCode, w.Code, w.Body.String())
			require.Equal(t, test.calls, repo.deleteCalls)
			if test.calls > 0 {
				require.Equal(t, test.user, repo.actor)
				require.Equal(t, int64(11), repo.deletedTeam)
			}
		})
	}
}
