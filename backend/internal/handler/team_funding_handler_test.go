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
	actor int64
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
