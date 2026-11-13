package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

type pendingAsyncFundingRepository interface {
	ListPendingAsyncFundingUsage(context.Context, int) ([]*AsyncFundingTask, error)
}

// AsyncFundingRecoveryService replays only durable terminal billing envelopes.
// It has no generation/forward callback: uncertain or merely queued upstream
// work cannot be resubmitted by process startup or a timer.
type AsyncFundingRecoveryService struct {
	funding *TeamFundingService
	keys    *APIKeyService
	users   UserRepository
	gateway *GatewayService
	openAI  *OpenAIGatewayService
	billing UsageBillingRepository
	batches BatchImageRepository
	logs    UsageLogRepository
	mu      sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
}

func NewAsyncFundingRecoveryService(funding *TeamFundingService, keys *APIKeyService, users UserRepository, gateway *GatewayService, openAI *OpenAIGatewayService, billing UsageBillingRepository, batches BatchImageRepository, logs UsageLogRepository) *AsyncFundingRecoveryService {
	return &AsyncFundingRecoveryService{funding: funding, keys: keys, users: users, gateway: gateway, openAI: openAI, billing: billing, batches: batches, logs: logs}
}
func (s *AsyncFundingRecoveryService) Start() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			runCtx, stop := context.WithTimeout(ctx, 45*time.Second)
			_, err := s.RunOnce(runCtx)
			stop()
			if err != nil && ctx.Err() == nil {
				logger.L().Warn("funding.async_usage_recovery_failed", zap.Error(err))
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
func (s *AsyncFundingRecoveryService) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
	s.mu.Lock()
	s.cancel = nil
	s.done = nil
	s.mu.Unlock()
}
func (s *AsyncFundingRecoveryService) RunOnce(ctx context.Context) (int, error) {
	if s == nil || s.funding == nil || s.keys == nil || s.users == nil {
		return 0, nil
	}
	repo, ok := s.funding.repo.(pendingAsyncFundingRepository)
	if !ok {
		return 0, nil
	}
	tasks, err := repo.ListPendingAsyncFundingUsage(ctx, 100)
	if err != nil {
		return 0, err
	}
	completed := 0
	var lastErr error
	for _, task := range tasks {
		if err = ctx.Err(); err != nil {
			return completed, err
		}
		if err = s.replay(ctx, task); err != nil {
			lastErr = err
			if failed, ok := s.funding.repo.(interface {
				TouchPendingAsyncFundingUsage(context.Context, string, int64, int64) error
			}); ok && task != nil && task.Snapshot != nil {
				_ = failed.TouchPendingAsyncFundingUsage(ctx, task.OperationID, task.Snapshot.APIKeyID, task.Snapshot.ActorUserID)
			}
			logger.L().Warn("funding.async_usage_replay_failed", zap.String("operation_id", task.OperationID), zap.String("provider", task.Provider), zap.Error(err))
			continue
		}
		completed++
	}
	return completed, lastErr
}
func (s *AsyncFundingRecoveryService) replay(ctx context.Context, task *AsyncFundingTask) error {
	if task == nil || task.Snapshot == nil || task.Snapshot.Funding == nil || len(task.TerminalUsage) == 0 {
		return ErrTeamFundingInvalid
	}
	var envelope struct {
		Gateway json.RawMessage      `json:"gateway_usage"`
		Batch   *UsageBillingCommand `json:"batch_command"`
	}
	if err := json.Unmarshal(task.TerminalUsage, &envelope); err != nil {
		return err
	}
	if envelope.Batch != nil {
		if s.batches == nil || s.billing == nil {
			return ErrFundingUnavailable
		}
		settlement := &BatchImageSettlementService{Repo: s.batches, BillingRepo: s.billing, UsageLogRepo: s.logs, Pricing: &BatchImageModelPricingResolver{}}
		_, err := settlement.Settle(ctx, task.TaskID)
		if err != nil && !errors.Is(err, ErrBatchImageAlreadySettled) {
			return err
		}
		return s.funding.markAsyncTaskSettled(ctx, task)
	}
	user, err := s.users.GetByIDIncludeDeleted(ctx, task.Snapshot.ActorUserID)
	if err != nil {
		return err
	}
	if s.keys.apiKeyRepo == nil {
		return ErrFundingUnavailable
	}
	key, err := s.keys.apiKeyRepo.GetByID(ctx, task.Snapshot.APIKeyID)
	if err != nil {
		if !errors.Is(err, ErrAPIKeyNotFound) {
			return err
		}
		key = &APIKey{ID: task.Snapshot.APIKeyID, UserID: task.Snapshot.ActorUserID}
	}
	if key == nil || key.UserID != task.Snapshot.ActorUserID {
		return ErrTeamFundingNotFound
	}
	key.User = user
	if len(envelope.Gateway) > 0 {
		if s.gateway == nil {
			return ErrFundingUnavailable
		}
		return s.keys.ReplayGatewayAsyncFundingUsage(ctx, key, task, s.gateway.RecordUsage)
	}
	if s.openAI == nil {
		return ErrFundingUnavailable
	}
	return s.keys.ReplayAsyncFundingUsage(ctx, key, task, s.openAI.RecordUsage)
}
func (s *TeamFundingService) markAsyncTaskSettled(ctx context.Context, task *AsyncFundingTask) error {
	repo, err := s.asyncRepository()
	if err != nil {
		return err
	}
	return repo.MarkAsyncFundingSettled(ctx, task.OperationID, task.Snapshot.APIKeyID, task.Snapshot.ActorUserID)
}
