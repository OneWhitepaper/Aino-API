package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDesktopInferenceDispatchContextAllowsOnlyOneAttempt(t *testing.T) {
	ctx := WithDesktopInferenceOperation(context.Background(), "operation")
	var accepted atomic.Int32
	var workers sync.WaitGroup
	for range 24 {
		workers.Go(func() {
			if BeginDesktopInferenceDispatch(context.WithoutCancel(ctx)) == nil {
				accepted.Add(1)
			}
		})
	}
	workers.Wait()
	require.EqualValues(t, 1, accepted.Load())
	require.ErrorIs(t, BeginDesktopInferenceDispatch(ctx), ErrDesktopOperationUnknown)
	require.NoError(t, BeginDesktopInferenceDispatch(context.Background()))
	require.NoError(t, BeginDesktopInferenceDispatch(context.Background()))
	require.NoError(t, BeginDesktopInferenceDispatch(WithDesktopInferenceOperation(ctx, "new operation")))
}
