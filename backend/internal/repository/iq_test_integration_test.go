//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/iqtest"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestIQTestSQLLifecycleAndGatewayACLIntegration(t *testing.T) {
	ctx := context.Background()
	user := mustCreateUser(t, integrationEntClient, &service.User{Balance: 10})
	t.Cleanup(func() { _, _ = integrationDB.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, user.ID) })
	users := NewUserRepository(integrationEntClient, integrationDB)
	keyRepo := NewAPIKeyRepository(integrationEntClient, integrationDB)
	key := &service.APIKey{UserID: user.ID, Key: "sk-iq-integration-" + uuid.NewString(), Name: "test", Status: service.StatusAPIKeyActive, IPWhitelist: []string{"203.0.113.8"}}
	require.NoError(t, keyRepo.Create(ctx, key))
	cfg := &config.Config{}
	keys := service.NewAPIKeyService(keyRepo, users, nil, nil, nil, nil, cfg)
	bridge := iqtest.NewGatewayTransport()
	router := gin.New()
	require.NoError(t, router.SetTrustedProxies(nil))
	var requests atomic.Int32
	router.POST("/v1/responses", gin.HandlerFunc(middleware.NewAPIKeyAuthMiddleware(keys, nil, cfg)), func(c *gin.Context) {
		requests.Add(1)
		require.Equal(t, "203.0.113.8", c.ClientIP())
		c.Header("Content-Type", "text/event-stream")
		_, _ = io.WriteString(c.Writer, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"最终答案：21\"}\n\n")
		c.Writer.Flush()
		_, _ = io.WriteString(c.Writer, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":40,\"output_tokens\":5}}}\n\n")
	})
	bridge.SetHandler(router)
	repo := NewIQTestRepository(integrationDB)
	runner := service.NewIQTestService(repo, iqtest.NewDefaultRunner(bridge), keys, nil, nil)
	defer runner.Stop()
	request := service.IQTestRequest{IQTestConnectionRequest: service.IQTestConnectionRequest{Source: "site", KeyID: key.ID}, RequestID: uuid.NewString(), Kind: "candy", Model: "fixture", Stream: true}
	denied, err := runner.Start(ctx, user.ID, request, "203.0.113.9")
	require.NoError(t, err)
	require.Eventually(t, func() bool { row, _ := runner.Get(ctx, user.ID, denied.ID); return row.Status != "running" }, 3*time.Second, 10*time.Millisecond)
	denied, err = runner.Get(ctx, user.ID, denied.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", denied.Status)
	require.Equal(t, "upstream_auth", denied.ErrorCode)
	require.Zero(t, requests.Load())
	request.RequestID = uuid.NewString()
	task, err := runner.Start(ctx, user.ID, request, "203.0.113.8")
	require.NoError(t, err)
	require.Eventually(t, func() bool { row, _ := runner.Get(ctx, user.ID, task.ID); return row.Status != "running" }, 3*time.Second, 10*time.Millisecond)
	task, err = runner.Get(ctx, user.ID, task.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", task.Status, fmt.Sprint(task.ErrorCode))
	require.Equal(t, "passed", task.Verdict)
	require.EqualValues(t, 40, task.Result.Usage.InputTokens)
	reused, err := runner.Start(ctx, user.ID, request, "203.0.113.8")
	require.NoError(t, err)
	require.Equal(t, task.ID, reused.ID)
	require.EqualValues(t, 1, requests.Load())
	_, err = runner.Get(ctx, user.ID+1, task.ID)
	require.ErrorIs(t, err, service.ErrIQTestNotFound)
	rows, more, err := runner.List(ctx, user.ID, 0)
	require.NoError(t, err)
	require.False(t, more)
	require.Len(t, rows, 2)
	require.Empty(t, rows[0].Result.Output)
	var stored string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT row_to_json(t)::text FROM user_iq_tests t WHERE id = $1`, task.ID).Scan(&stored))
	require.NotContains(t, stored, key.Key)
	require.NotContains(t, stored, "api_key")
	body, _ := json.Marshal(task)
	require.NotContains(t, string(body), key.Key)
	changed, err := repo.Finish(ctx, &service.IQTest{ID: task.ID, UserID: user.ID, Status: "cancelled"})
	require.NoError(t, err)
	require.False(t, changed)
}

func TestIQTestSQLIdempotencyAndAdmissionAcrossInstances(t *testing.T) {
	ctx := context.Background()
	user := mustCreateUser(t, integrationEntClient, &service.User{})
	t.Cleanup(func() { _, _ = integrationDB.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, user.ID) })
	repo := NewIQTestRepository(integrationDB)
	now := time.Now().UTC()
	first := &service.IQTest{ID: uuid.NewString(), UserID: user.ID, RequestID: uuid.NewString(), Fingerprint: strings.Repeat("a", 64), Config: service.IQTestConfig{Kind: "candy", Model: "fixture", Source: "custom"}, Status: "running", CreatedAt: now, DeadlineAt: now.Add(time.Minute)}
	_, created, err := repo.Create(ctx, first)
	require.NoError(t, err)
	require.True(t, created)
	reused, created, err := repo.Create(ctx, first)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, first.ID, reused.ID)
	second := *first
	second.ID = uuid.NewString()
	second.RequestID = uuid.NewString()
	_, _, err = NewIQTestRepository(integrationDB).Create(ctx, &second)
	require.ErrorIs(t, err, service.ErrIQTestBusy)
	_, err = integrationDB.ExecContext(ctx, `UPDATE user_iq_tests SET deadline_at = NOW() - INTERVAL '1 second' WHERE id = $1`, first.ID)
	require.NoError(t, err)
	_, created, err = repo.Create(ctx, &second)
	require.NoError(t, err)
	require.True(t, created)
	old, err := repo.Get(ctx, user.ID, first.ID)
	require.NoError(t, err)
	require.Equal(t, "interrupted", old.Status)
	second.Status = "cancelled"
	second.FinishedAt = &now
	changed, err := repo.Finish(ctx, &second)
	require.NoError(t, err)
	require.True(t, changed)
}
