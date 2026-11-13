package service

import (
	"context"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

const (
	batchImageHoldRequestPrefix    = "batch_image_hold:"
	batchImageCaptureRequestPrefix = "batch_image_capture:"
	batchImageReleaseRequestPrefix = "batch_image_release:"
)

func BatchImageHoldRequestID(batchID string) string {
	return batchImageHoldRequestPrefix + strings.TrimSpace(batchID)
}

func BatchImageCaptureRequestID(batchID string) string {
	return batchImageCaptureRequestPrefix + strings.TrimSpace(batchID)
}

func BatchImageReleaseRequestID(batchID string) string {
	return batchImageReleaseRequestPrefix + strings.TrimSpace(batchID)
}

func buildBatchImageHoldCommand(job *BatchImageJob, requestID string, actualAmount float64, payloadHash string) (*BatchImageBalanceHoldCommand, error) {
	if job == nil {
		return nil, ErrBatchImageBillingHoldFailed
	}
	if job.APIKeyID == nil || *job.APIKeyID <= 0 {
		return nil, ErrBatchImageSettlementMissingAPIKeyID
	}
	holdAmount := job.EstimatedCost
	if job.HoldAmount != nil {
		holdAmount = *job.HoldAmount
	}
	if holdAmount < 0 {
		holdAmount = 0
	}
	if actualAmount < 0 {
		actualAmount = 0
	}
	return &BatchImageBalanceHoldCommand{
		RequestID:          requestID,
		APIKeyID:           *job.APIKeyID,
		UserID:             job.UserID,
		BatchID:            job.BatchID,
		HoldAmount:         holdAmount,
		ActualAmount:       actualAmount,
		RequestPayloadHash: strings.TrimSpace(payloadHash),
	}, nil
}

func reserveBatchImageBalanceHold(ctx context.Context, repo UsageBillingRepository, job *BatchImageJob, payloadHash string) error {
	if job != nil && job.FundingSnapshot != nil {
		return nil
	}
	if repo == nil {
		return ErrBatchImageBillingHoldFailed.WithCause(errors.New("batch image billing repository is not configured"))
	}
	cmd, err := buildBatchImageHoldCommand(job, BatchImageHoldRequestID(job.BatchID), 0, payloadHash)
	if err != nil {
		return err
	}
	if cmd.HoldAmount <= 0 {
		return nil
	}
	if _, err := repo.ReserveBatchImageBalance(ctx, cmd); err != nil {
		if errors.Is(err, ErrBatchImageInsufficientBalance) {
			return ErrBatchImageInsufficientBalance
		}
		return ErrBatchImageBillingHoldFailed.WithCause(err)
	}
	return nil
}

func captureBatchImageBalanceHold(ctx context.Context, repo UsageBillingRepository, job *BatchImageJob, actualAmount float64, payloadHash string) error {
	if job != nil && job.FundingSnapshot != nil {
		d := job.FundingSnapshot.Funding
		if d == nil || job.APIKeyID == nil || job.AccountID == nil {
			return ErrFundingUnavailable
		}
		cmd := &UsageBillingCommand{RequestID: d.RequestID, APIKeyID: *job.APIKeyID, UserID: job.UserID, PayerUserID: d.PayerUserID, FundingSource: d.Source, FundingCost: actualAmount, SubscriptionID: d.SubscriptionID, AccountID: *job.AccountID, Model: job.Model, MediaType: "image", ImageCount: job.SuccessCount, APIKeyQuotaCost: actualAmount, APIKeyRateLimitCost: actualAmount, AccountQuotaCost: actualAmount * job.AccountRateMultiplier, RequestPayloadHash: payloadHash}
		if d.IsShared() {
			cmd.FundingReservationID = d.ReservationID
		} else {
			cmd.PersonalFundingReservationID = d.ReservationID
		}
		if durable, ok := repo.(interface {
			ApplyBatchImageFunding(context.Context, *UsageBillingCommand) error
		}); ok {
			return durable.ApplyBatchImageFunding(ctx, cmd)
		}
		_, err := repo.Apply(ctx, cmd)
		return err
	}
	if repo == nil {
		return ErrBatchImageSettlementBillingFailed.WithCause(errors.New("batch image billing repository is not configured"))
	}
	cmd, err := buildBatchImageHoldCommand(job, BatchImageCaptureRequestID(job.BatchID), actualAmount, payloadHash)
	if err != nil {
		return err
	}
	if _, err := repo.CaptureBatchImageBalance(ctx, cmd); err != nil {
		return ErrBatchImageSettlementBillingFailed.WithCause(err)
	}
	return nil
}

func releaseBatchImageBalanceHold(ctx context.Context, repo UsageBillingRepository, job *BatchImageJob, payloadHash string) error {
	if job != nil && job.FundingSnapshot != nil {
		releaser, ok := repo.(batchImageFundingReleaser)
		if !ok {
			return ErrFundingUnavailable
		}
		err := releaser.ReleaseQueuedBatchImageFunding(ctx, job.FundingSnapshot)
		if errors.Is(err, ErrTeamFundingConflict) {
			return nil
		}
		return err
	}
	if repo == nil || job == nil {
		return nil
	}
	cmd, err := buildBatchImageHoldCommand(job, BatchImageReleaseRequestID(job.BatchID), 0, payloadHash)
	if err != nil {
		return err
	}
	if cmd.HoldAmount <= 0 {
		return nil
	}
	if _, err := repo.ReleaseBatchImageBalance(ctx, cmd); err != nil {
		// 同一 release request id 出现指纹冲突，说明此前已有一次携带不同
		// payloadHash 的释放成功提交（资金已归还）。视为幂等成功，
		// 避免历史指纹不一致的 job 永远卡在释放失败的毒消息循环里。
		if errors.Is(err, ErrUsageBillingRequestConflict) {
			logger.L().Warn("batch_image.release_fingerprint_conflict_treated_as_released",
				zap.String("batch_id", job.BatchID),
			)
			return nil
		}
		return ErrBatchImageBillingHoldFailed.WithCause(err)
	}
	return nil
}

// Configured asynchronous funding uses escrow rather than the legacy personal
// frozen wallet. An ambiguous provider failure must retain dispatched escrow.
type batchImageFundingReleaser interface {
	ReleaseQueuedBatchImageFunding(context.Context, *AsyncFundingSnapshot) error
}
