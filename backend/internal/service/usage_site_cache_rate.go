package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"golang.org/x/sync/singleflight"
)

type siteCacheRateEntry struct {
	rate      *float64
	expiresAt time.Time
}

type siteCacheRateCache struct {
	mu      sync.Mutex
	entries map[string]siteCacheRateEntry
	group   singleflight.Group
}

func (cache *siteCacheRateCache) get(key string) (siteCacheRateEntry, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry, ok := cache.entries[key]
	return entry, ok && time.Now().Before(entry.expiresAt)
}

// GetSiteCacheHitRate only scopes by time, regardless of the viewer's filters.
// Cache the small aggregate across users; never cache the visibility decision.
func (s *UsageService) GetSiteCacheHitRate(ctx context.Context, startTime, endTime time.Time) (*float64, error) {
	key := fmt.Sprintf("%d:%d", startTime.Unix(), endTime.Unix())
	cache := &s.siteCacheRate
	if entry, ok := cache.get(key); ok {
		return entry.rate, nil
	}
	result := cache.group.DoChan(key, func() (any, error) {
		if entry, ok := cache.get(key); ok {
			return entry, nil
		}
		// One disconnected viewer must not cancel a shared aggregate for others.
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		stats, err := s.GetStatsWithFilters(loadCtx, usagestats.UsageLogFilters{StartTime: &startTime, EndTime: &endTime})
		if err != nil {
			return nil, err
		}
		entry := siteCacheRateEntry{expiresAt: time.Now().Add(30 * time.Second)}
		if stats != nil {
			promptTokens := stats.TotalInputTokens + stats.TotalCacheReadTokens + stats.TotalCacheCreationTokens
			if promptTokens > 0 {
				rate := float64(stats.TotalCacheReadTokens) / float64(promptTokens) * 100
				entry.rate = &rate
			}
		}
		cache.mu.Lock()
		defer cache.mu.Unlock()
		if cache.entries == nil {
			cache.entries = make(map[string]siteCacheRateEntry)
		}
		if len(cache.entries) >= 128 {
			oldestKey := ""
			var oldest time.Time
			for candidate, existing := range cache.entries {
				if oldest.IsZero() || existing.expiresAt.Before(oldest) {
					oldestKey, oldest = candidate, existing.expiresAt
				}
			}
			delete(cache.entries, oldestKey)
		}
		cache.entries[key] = entry
		return entry, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case loaded := <-result:
		if loaded.Err != nil {
			return nil, loaded.Err
		}
		entry, ok := loaded.Val.(siteCacheRateEntry)
		if !ok {
			return nil, fmt.Errorf("unexpected site cache rate result type %T", loaded.Val)
		}
		return entry.rate, nil
	}
}
