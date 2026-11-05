package service

import (
	"context"
	"sync/atomic"
)

type desktopInferenceOperationKey struct{}

type desktopInferenceOperation struct {
	id      string
	started atomic.Bool
}

// WithDesktopInferenceOperation binds one logical operation to all request-local
// forwarding attempts, including account failover and detached billing contexts.
func WithDesktopInferenceOperation(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, desktopInferenceOperationKey{}, &desktopInferenceOperation{id: id})
}

func DesktopInferenceOperationID(ctx context.Context) string {
	if operation, ok := ctx.Value(desktopInferenceOperationKey{}).(*desktopInferenceOperation); ok {
		return operation.id
	}
	return ""
}

// BeginDesktopInferenceDispatch conservatively permits one network dispatch.
// A timeout, partial response, or HTTP error cannot prove no provider work began.
func BeginDesktopInferenceDispatch(ctx context.Context) error {
	operation, ok := ctx.Value(desktopInferenceOperationKey{}).(*desktopInferenceOperation)
	if ok && !operation.started.CompareAndSwap(false, true) {
		return ErrDesktopOperationUnknown
	}
	return nil
}
