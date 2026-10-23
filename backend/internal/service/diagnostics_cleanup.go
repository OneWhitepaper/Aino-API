package service

import (
	"context"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// DiagnosticsCleanup uses the existing timing wheel, with no additional scheduler.
type DiagnosticsCleanup struct {
	service *DiagnosticsService
	wheel   *TimingWheelService
	mu      sync.Mutex
	stopped bool
	cancel  context.CancelFunc
	ctx     context.Context
}

func ProvideDiagnosticsCleanup(service *DiagnosticsService, wheel *TimingWheelService) *DiagnosticsCleanup {
	ctx, cancel := context.WithCancel(context.Background())
	cleanup := &DiagnosticsCleanup{service: service, wheel: wheel, ctx: ctx, cancel: cancel}
	cleanup.run()
	return cleanup
}

func (c *DiagnosticsCleanup) run() {
	ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer cancel()
	if _, err := c.service.PurgeExpired(ctx, time.Now().UTC()); err != nil {
		logger.LegacyPrintf("diagnostics.cleanup", "Diagnostic expiry purge failed: %v", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.stopped {
		c.wheel.Schedule("diagnostics_expiry", time.Hour, c.run)
	}
}

func (c *DiagnosticsCleanup) Stop() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopped = true
	c.cancel()
	c.wheel.Cancel("diagnostics_expiry")
}
