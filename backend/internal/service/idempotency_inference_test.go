package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func inferenceOptions() IdempotencyExecuteOptions {
	return IdempotencyExecuteOptions{
		Scope: "desktop_inference:user:4:key:223", ActorScope: "user:4:key:223",
		Method: "POST", Route: "/v1/chat/completions",
		IdempotencyKey: "4ee600b9-621b-4d63-b90c-23d8b3bd2957",
		Payload:        map[string]string{"body_sha256": "synthetic-body", "model_option": "wallet-text"},
	}
}

func TestInferenceOperationClaimOnlyOneDispatchAndNeverReclaims(t *testing.T) {
	repo := newInMemoryIdempotencyRepo()
	claimer := NewIdempotencyCoordinator(repo, DefaultIdempotencyConfig())
	opts := inferenceOptions()
	var winners atomic.Int64
	start := make(chan struct{})
	errorsSeen := make(chan error, 24)
	var workers sync.WaitGroup
	for range 24 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			err := claimer.ClaimInferenceOperation(context.Background(), opts)
			if err == nil {
				winners.Add(1)
			}
			errorsSeen <- err
		}()
	}
	close(start)
	workers.Wait()
	close(errorsSeen)
	require.EqualValues(t, 1, winners.Load())
	for err := range errorsSeen {
		if err != nil {
			require.Equal(t, 409, infraerrors.Code(err))
			require.Equal(t, "DESKTOP_OPERATION_UNKNOWN", infraerrors.Reason(err))
		}
	}

	hash := HashIdempotencyKey(opts.IdempotencyKey)
	record, err := repo.GetByScopeAndKeyHash(context.Background(), opts.Scope, hash)
	require.NoError(t, err)
	require.Equal(t, "inference_unknown", record.Status)
	require.Nil(t, record.ResponseBody)
	require.Nil(t, record.LockedUntil)
	for _, status := range []string{"inference_unknown", "processing", "succeeded", "failed_retryable"} {
		repo.mu.Lock()
		repo.data[repo.key(opts.Scope, hash)].ExpiresAt = time.Now().Add(-time.Hour)
		repo.data[repo.key(opts.Scope, hash)].Status = status
		repo.mu.Unlock()
		err = claimer.ClaimInferenceOperation(context.Background(), opts)
		require.Equal(t, "DESKTOP_OPERATION_UNKNOWN", infraerrors.Reason(err), "existing outcome %s must not dispatch", status)
	}
	changed := opts
	changed.Payload = map[string]string{"body_sha256": "changed-body", "model_option": "wallet-text"}
	require.Equal(t, "IDEMPOTENCY_KEY_CONFLICT", infraerrors.Reason(claimer.ClaimInferenceOperation(context.Background(), changed)))
	newOperation := opts
	newOperation.IdempotencyKey = "67f50829-40b0-454d-a69d-92f775ea2d88"
	require.NoError(t, claimer.ClaimInferenceOperation(context.Background(), newOperation))
}

type unavailableInferenceRepo struct {
	IdempotencyRepository
	createErr error
	created   bool
	readErr   error
}

func (r unavailableInferenceRepo) CreateProcessing(context.Context, *IdempotencyRecord) (bool, error) {
	return r.created, r.createErr
}

func (r unavailableInferenceRepo) GetByScopeAndKeyHash(context.Context, string, string) (*IdempotencyRecord, error) {
	return nil, r.readErr
}

func TestInferenceOperationClaimFailsClosed(t *testing.T) {
	opts := inferenceOptions()
	for _, repo := range []IdempotencyRepository{nil,
		unavailableInferenceRepo{createErr: errors.New("store unavailable")},
		unavailableInferenceRepo{readErr: errors.New("store unavailable")},
		unavailableInferenceRepo{},
		unavailableInferenceRepo{created: true},
	} {
		claimer := NewIdempotencyCoordinator(repo, DefaultIdempotencyConfig())
		require.Equal(t, 503, infraerrors.Code(claimer.ClaimInferenceOperation(context.Background(), opts)))
	}
	var claimer *IdempotencyCoordinator
	require.Equal(t, 503, infraerrors.Code(claimer.ClaimInferenceOperation(context.Background(), opts)))
	claimer = NewIdempotencyCoordinator(newInMemoryIdempotencyRepo(), DefaultIdempotencyConfig())
	missing := opts
	missing.IdempotencyKey = ""
	require.Equal(t, 400, infraerrors.Code(claimer.ClaimInferenceOperation(context.Background(), missing)))
	missing = opts
	missing.Scope = ""
	require.Equal(t, 400, infraerrors.Code(claimer.ClaimInferenceOperation(context.Background(), missing)))
}
