//go:build integration

package repository_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestPhoneBindingAfterStepUpKeepsAccountAndBalance(t *testing.T) {
	r := newPhoneAuthFlowRig(t)
	u := r.user(t, service.StatusActive, true)
	token, err := r.auth.GenerateToken(r.ctx, u)
	require.NoError(t, err)
	before := r.request(http.MethodPost, "/send", `{"phone":"13900000009"}`, token)
	require.Equal(t, http.StatusForbidden, before.Code, before.Body.String())
	require.Contains(t, before.Body.String(), "STEP_UP_REQUIRED")
	require.Zero(t, r.sender.calls)
	claims, err := r.auth.ValidateToken(token)
	require.NoError(t, err)
	session := claims.SessionID
	if session == "" {
		session = fmt.Sprintf("u%d", u.ID)
	}
	// Simulate the grant persisted by successful TOTP verification; exercise
	// the real Redis cache, production-wired handler and identity transaction.
	require.NoError(t, repository.NewTotpCache(r.redis).SetStepUpGrant(r.ctx, u.ID, session, time.Minute))
	send := r.request(http.MethodPost, "/send", `{"phone":"13900000009"}`, token)
	require.Equal(t, http.StatusOK, send.Code, send.Body.String())
	bind := r.request(http.MethodPost, "/bind", fmt.Sprintf(`{"phone":"13900000009","challenge_id":%q,"code":%q}`, challengeID(t, send.Body.Bytes()), r.sender.latestCode()), token)
	require.Equal(t, http.StatusOK, bind.Code, bind.Body.String())
	require.Contains(t, bind.Body.String(), `"phone_bound":true`)
	stored, err := r.userRepo.GetByID(r.ctx, u.ID)
	require.NoError(t, err)
	require.Equal(t, u.Email, stored.Email)
	require.Equal(t, u.Balance, stored.Balance)
	require.True(t, stored.TotpEnabled)
}
