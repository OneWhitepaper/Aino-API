//go:build unit

package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/collection"
	"github.com/zeromicro/go-zero/core/timex"
)

type diagnosticsPurgeProbe struct {
	DiagnosticsRepository
	calls chan context.Context
	count atomic.Int32
}

func (p *diagnosticsPurgeProbe) DeleteExpired(ctx context.Context, _ time.Time) (int64, error) {
	n := p.count.Add(1)
	p.calls <- ctx
	if n == 3 {
		<-ctx.Done()
		return 0, ctx.Err()
	}
	return 0, nil
}

func TestDiagnosticsCleanupStartsRepeatsAndCancelsInFlightWithoutRescheduling(t *testing.T) {
	ticker := timex.NewFakeTicker()
	completed := make(chan struct{}, 2)
	tw, err := collection.NewTimingWheelWithTicker(time.Hour, 4, func(_ any, task any) {
		fn, ok := task.(func())
		if !ok {
			t.Errorf("unexpected timing-wheel task: %T", task)
			return
		}
		fn()
		completed <- struct{}{}
	}, ticker)
	require.NoError(t, err)
	wheel := &TimingWheelService{tw: tw}
	t.Cleanup(wheel.Stop)
	probe := &diagnosticsPurgeProbe{calls: make(chan context.Context, 3)}
	cleanup := ProvideDiagnosticsCleanup(NewDiagnosticsService(probe), wheel)
	t.Cleanup(cleanup.Stop)
	require.Equal(t, int32(1), probe.count.Load(), "startup must purge without waiting an hour")
	<-probe.calls

	ticker.Tick()
	select {
	case <-completed:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduled expiry purge did not run")
	}
	require.Equal(t, int32(2), probe.count.Load())
	<-probe.calls

	ticker.Tick()
	var running context.Context
	select {
	case running = <-probe.calls:
	case <-time.After(2 * time.Second):
		t.Fatal("expiry purge did not recur")
	}
	cleanup.Stop()
	require.ErrorIs(t, running.Err(), context.Canceled)
	select {
	case <-completed:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not release the in-flight purge")
	}

	remaining := make(chan any, 4)
	require.NoError(t, tw.Drain(func(key, _ any) { remaining <- key }))
	// SetTimer is a rendezvous after Drain, so all drain callbacks finished first.
	require.NoError(t, tw.SetTimer("barrier", func() {}, time.Hour))
	require.Empty(t, remaining, "shutdown must not leave or recreate an expiry task")
}
