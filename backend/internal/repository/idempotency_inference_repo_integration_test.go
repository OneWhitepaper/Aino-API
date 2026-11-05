//go:build integration

package repository

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func inferenceTombstone(t *testing.T, repo *idempotencyRepository) *service.IdempotencyRecord {
	t.Helper()
	record := &service.IdempotencyRecord{
		Scope:              uniqueTestValue(t, "inference-tombstone"),
		IdempotencyKeyHash: hashedTestValue(t, "inference-operation"),
		RequestFingerprint: hashedTestValue(t, "inference-payload"),
		Status:             "inference_unknown", ExpiresAt: time.Now().UTC().Add(-2 * time.Hour),
	}
	created, err := repo.CreateProcessing(context.Background(), record)
	require.NoError(t, err)
	require.True(t, created)
	return record
}

func TestIdempotencyInferenceTombstoneRejectsOrdinaryMutations(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	for _, action := range []string{"reclaim", "succeed", "retryable"} {
		t.Run(action, func(t *testing.T) {
			repo := &idempotencyRepository{sql: testTx(t)}
			unknown := inferenceTombstone(t, repo)
			switch action {
			case "reclaim":
				reclaimed, err := repo.TryReclaim(ctx, unknown.ID, "inference_unknown", now, now.Add(time.Minute), now.Add(time.Hour))
				require.NoError(t, err)
				require.False(t, reclaimed, "unknown provider outcome must never authorize a new execution")
			case "succeed":
				require.NoError(t, repo.MarkSucceeded(ctx, unknown.ID, 200, `{"ok":true}`, now.Add(time.Hour)))
			case "retryable":
				require.NoError(t, repo.MarkFailedRetryable(ctx, unknown.ID, "NETWORK_ERROR", now.Add(-time.Minute), now.Add(-time.Hour)))
			}
			got, err := repo.GetByScopeAndKeyHash(ctx, unknown.Scope, unknown.IdempotencyKeyHash)
			require.NoError(t, err)
			require.Equal(t, "inference_unknown", got.Status)
			require.Nil(t, got.ResponseBody)
			require.Nil(t, got.ErrorReason)
			require.Nil(t, got.LockedUntil)
		})
	}
}

func TestIdempotencyInferenceTombstoneSurvivesCleanupBeforeBatchLimit(t *testing.T) {
	ctx := context.Background()
	repo := &idempotencyRepository{sql: testTx(t)}
	now := time.Now().UTC()
	unknown := inferenceTombstone(t, repo)
	ordinary := &service.IdempotencyRecord{
		Scope:              uniqueTestValue(t, "ordinary-expired"),
		IdempotencyKeyHash: hashedTestValue(t, "ordinary-operation"),
		RequestFingerprint: hashedTestValue(t, "ordinary-payload"),
		Status:             service.IdempotencyStatusProcessing, ExpiresAt: now.Add(-time.Hour),
	}
	created, err := repo.CreateProcessing(ctx, ordinary)
	require.NoError(t, err)
	require.True(t, created)
	deleted, err := repo.DeleteExpired(ctx, now, 1)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted, "tombstone must not occupy the cleanup batch")
	got, err := repo.GetByScopeAndKeyHash(ctx, ordinary.Scope, ordinary.IdempotencyKeyHash)
	require.NoError(t, err)
	require.Nil(t, got, "ordinary expired operations still clean up")
	got, err = repo.GetByScopeAndKeyHash(ctx, unknown.Scope, unknown.IdempotencyKeyHash)
	require.NoError(t, err)
	require.NotNil(t, got, "cleanup must preserve the permanently claimed operation")
	created, err = repo.CreateProcessing(ctx, unknown)
	require.NoError(t, err)
	require.False(t, created, "a later operation replay cannot acquire the old claim")
}

func TestIdempotencyInferenceConcurrentClaimOnPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	repo := &idempotencyRepository{sql: integrationDB}
	coordinator := service.NewIdempotencyCoordinator(repo, service.DefaultIdempotencyConfig())
	claimer := coordinator
	opts := service.IdempotencyExecuteOptions{
		Scope: uniqueTestValue(t, "desktop_inference:user:4:key:223"), ActorScope: "user:4:key:223",
		IdempotencyKey: "4ee600b9-621b-4d63-b90c-23d8b3bd2957", Method: "POST", Route: "/v1/chat/completions",
		Payload: map[string]string{"body_sha256": "synthetic-body", "model_option": "wallet-text"},
	}
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(context.Background(), "DELETE FROM idempotency_records WHERE scope=$1", opts.Scope)
		require.NoError(t, err)
	})
	start := make(chan struct{})
	outcomes := make(chan error, 24)
	var workers sync.WaitGroup
	var winners atomic.Int64
	for range 24 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			err := claimer.ClaimInferenceOperation(ctx, opts)
			if err == nil {
				winners.Add(1)
			}
			outcomes <- err
		}()
	}
	close(start)
	workers.Wait()
	close(outcomes)
	require.EqualValues(t, 1, winners.Load())
	for err := range outcomes {
		if err != nil {
			require.Equal(t, "DESKTOP_OPERATION_UNKNOWN", infraerrors.Reason(err))
		}
	}
	changed := opts
	changed.Payload = map[string]string{"body_sha256": "different-body", "model_option": "wallet-text"}
	require.Equal(t, "IDEMPOTENCY_KEY_CONFLICT", infraerrors.Reason(claimer.ClaimInferenceOperation(ctx, changed)))
	_, err := integrationDB.ExecContext(ctx, "UPDATE idempotency_records SET expires_at=NOW()-INTERVAL '400 days' WHERE scope=$1", opts.Scope)
	require.NoError(t, err)
	_, err = repo.DeleteExpired(ctx, time.Now(), 500)
	require.NoError(t, err)
	require.Equal(t, "DESKTOP_OPERATION_UNKNOWN", infraerrors.Reason(claimer.ClaimInferenceOperation(ctx, opts)))
	var dispatched bool
	_, err = coordinator.Execute(ctx, opts, func(context.Context) (any, error) { dispatched = true; return nil, nil })
	require.Error(t, err)
	require.False(t, dispatched, "ordinary Execute cannot reopen the permanent inference claim")
}
