package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type siteCacheRateRepoStub struct {
	UsageLogRepository
	stats   *usagestats.UsageStats
	err     error
	calls   atomic.Int64
	started chan struct{}
	release chan struct{}
}

func (r *siteCacheRateRepoStub) GetStatsWithFilters(ctx context.Context, _ usagestats.UsageLogFilters) (*usagestats.UsageStats, error) {
	r.calls.Add(1)
	if r.started != nil {
		close(r.started)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-r.release:
		}
	}
	return r.stats, r.err
}

func TestSiteCacheHitRateWeightsPromptTokensAndCachesByRange(t *testing.T) {
	repo := &siteCacheRateRepoStub{stats: &usagestats.UsageStats{TotalInputTokens: 200, TotalCacheReadTokens: 500, TotalCacheCreationTokens: 300, TotalOutputTokens: 100000}}
	s := NewUsageService(repo, nil, nil, nil)
	start := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	for range 2 {
		rate, err := s.GetSiteCacheHitRate(context.Background(), start, end)
		require.NoError(t, err)
		require.NotNil(t, rate)
		require.Equal(t, float64(50), *rate)
	}
	require.EqualValues(t, 1, repo.calls.Load())
	_, err := s.GetSiteCacheHitRate(context.Background(), start.AddDate(0, 0, -1), end)
	require.NoError(t, err)
	require.EqualValues(t, 2, repo.calls.Load())
	for key, entry := range s.siteCacheRate.entries {
		entry.expiresAt = time.Now().Add(-time.Second)
		s.siteCacheRate.entries[key] = entry
	}
	_, err = s.GetSiteCacheHitRate(context.Background(), start, end)
	require.NoError(t, err)
	require.EqualValues(t, 3, repo.calls.Load())
}

func TestSiteCacheHitRateDistinguishesNoUsageAndCacheMissAndRetriesErrors(t *testing.T) {
	for _, tc := range []struct {
		stats *usagestats.UsageStats
		rate  *float64
	}{
		{stats: &usagestats.UsageStats{}},
		{stats: &usagestats.UsageStats{TotalInputTokens: 500}, rate: new(float64)},
	} {
		repo := &siteCacheRateRepoStub{stats: tc.stats, err: errors.New("query failed")}
		s := NewUsageService(repo, nil, nil, nil)
		start := time.Now()
		_, err := s.GetSiteCacheHitRate(context.Background(), start, start.Add(time.Hour))
		require.Error(t, err)
		repo.err = nil
		rate, err := s.GetSiteCacheHitRate(context.Background(), start, start.Add(time.Hour))
		require.NoError(t, err)
		require.Equal(t, tc.rate, rate)
		require.EqualValues(t, 2, repo.calls.Load())
	}
}

func TestSiteCacheHitRateSharesQueriesAndSurvivesViewerCancellation(t *testing.T) {
	repo := &siteCacheRateRepoStub{stats: &usagestats.UsageStats{TotalCacheReadTokens: 500}, started: make(chan struct{}), release: make(chan struct{})}
	s := NewUsageService(repo, nil, nil, nil)
	start := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		_, err := s.GetSiteCacheHitRate(ctx, start, start.Add(time.Hour))
		firstDone <- err
	}()
	<-repo.started
	cancel()
	require.ErrorIs(t, <-firstDone, context.Canceled)
	var wg sync.WaitGroup
	errorsSeen := make(chan error, 10)
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rate, err := s.GetSiteCacheHitRate(context.Background(), start, start.Add(time.Hour))
			if err == nil && (rate == nil || *rate != 100) {
				err = errors.New("unexpected rate")
			}
			errorsSeen <- err
		}()
	}
	close(repo.release)
	wg.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		require.NoError(t, err)
	}
	require.EqualValues(t, 1, repo.calls.Load())
}
