package service

import (
	"context"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// A reservation is deliberately unknown before dispatch: a process can disappear
// after the provider accepts work but before any local completion is durable.
const IdempotencyStatusInferenceUnknown = "inference_unknown"

var ErrDesktopOperationUnknown = infraerrors.Conflict(
	"DESKTOP_OPERATION_UNKNOWN",
	"This operation was already accepted and its outcome is unknown; it will not be executed again",
)

// ClaimInferenceOperation returns nil only to the caller that durably reserves
// this operation. It never reclaims a prior claim or replays a stored result.
func (c *IdempotencyCoordinator) ClaimInferenceOperation(ctx context.Context, opts IdempotencyExecuteOptions) error {
	if c == nil || c.repo == nil {
		return ErrIdempotencyStoreUnavail
	}
	key, err := NormalizeIdempotencyKey(opts.IdempotencyKey)
	if err != nil {
		return err
	}
	if key == "" {
		return ErrIdempotencyKeyRequired
	}
	if opts.Scope == "" {
		return infraerrors.BadRequest("IDEMPOTENCY_SCOPE_REQUIRED", "idempotency scope is required")
	}
	fingerprint, err := BuildIdempotencyFingerprint(opts.Method, opts.Route, opts.ActorScope, opts.Payload)
	if err != nil {
		return err
	}
	record := &IdempotencyRecord{
		Scope: opts.Scope, IdempotencyKeyHash: HashIdempotencyKey(key),
		RequestFingerprint: fingerprint, Status: IdempotencyStatusInferenceUnknown,
		// Inference tombstones are excluded from expiry cleanup and reclaim.
		ExpiresAt: time.Now(),
	}
	created, err := c.repo.CreateProcessing(ctx, record)
	if err != nil {
		return ErrIdempotencyStoreUnavail
	}
	if created {
		if record.ID == 0 {
			return ErrIdempotencyStoreUnavail
		}
		return nil
	}
	existing, err := c.repo.GetByScopeAndKeyHash(ctx, record.Scope, record.IdempotencyKeyHash)
	if err != nil || existing == nil {
		return ErrIdempotencyStoreUnavail
	}
	if existing.RequestFingerprint != fingerprint {
		return ErrIdempotencyKeyConflict
	}
	return ErrDesktopOperationUnknown
}
