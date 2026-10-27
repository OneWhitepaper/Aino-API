package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/iqradar"
)

type radarSourceStub struct {
	calls  atomic.Int32
	fail   atomic.Bool
	cohort atomic.Int32
	block  <-chan struct{}
}

func (s *radarSourceStub) Catalog(ctx context.Context) (iqradar.Catalog, error) {
	s.calls.Add(1)
	if s.block != nil {
		select {
		case <-s.block:
		case <-ctx.Done():
			return iqradar.Catalog{}, ctx.Err()
		}
	}
	hash := "first"
	if s.cohort.Load() > 0 {
		hash = "second"
	}
	return iqradar.Catalog{Benchmark: "bench", ScoreVersion: "v150", TaskSetSHA256: hash, ScoreMax: 150, RequiredTasks: 64, Selections: []iqradar.Selection{{Model: "model", Effort: "low"}, {Model: "model", Effort: "high"}}}, nil
}
func (s *radarSourceStub) Score(_ context.Context, selection iqradar.Selection, _ iqradar.Catalog) (iqradar.Point, error) {
	if s.fail.Load() && selection.Effort == "high" {
		return iqradar.Point{}, errors.New("offline")
	}
	value := 100.0
	now := time.Now().UTC()
	return iqradar.Point{Model: selection.Model, Effort: selection.Effort, Score: &value, FetchedAt: &now, History: []iqradar.HistoryPoint{{Date: "2026-10-20", Score: value}}}, nil
}
func (s *radarSourceStub) Community(context.Context) (iqradar.Community, error) {
	if s.fail.Load() {
		return iqradar.Community{}, errors.New("offline")
	}
	now := time.Now().UTC()
	return iqradar.Community{WindowHours: 24, FetchedAt: &now, Models: []iqradar.CommunityModel{}}, nil
}
func waitRadarRefresh(t *testing.T, service *IQRadarService) {
	t.Helper()
	service.mu.Lock()
	done := service.refreshDone
	service.mu.Unlock()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("refresh did not complete")
	}
}

func TestIQRadarSharesOneNonblockingRefreshAndReturnsIsolatedSnapshots(t *testing.T) {
	unblock := make(chan struct{})
	source := &radarSourceStub{block: unblock}
	service := NewIQRadarService(source)
	first := service.Snapshot()
	if !first.Refreshing || len(first.Points) != 0 {
		t.Fatal("initial fetch must be nonblocking")
	}
	for range 20 {
		_ = service.Snapshot()
	}
	close(unblock)
	waitRadarRefresh(t, service)
	result := service.Snapshot()
	if source.calls.Load() != 1 || result.Refreshing || result.Stale || len(result.Points) != 2 {
		t.Fatalf("unexpected cache result: %+v", result)
	}
	*result.Points[0].Score = 1
	result.Points[0].History[0].Score = 2
	if *service.Snapshot().Points[0].Score != 100 || service.Snapshot().Points[0].History[0].Score != 100 {
		t.Fatal("caller mutated the shared cache")
	}
}

func TestIQRadarPartialFailuresPreserveOriginalFetchTimeAndDoNotMixCohorts(t *testing.T) {
	source := &radarSourceStub{}
	service := NewIQRadarService(source)
	now := time.Now()
	service.now = func() time.Time { return now }
	_ = service.Snapshot()
	waitRadarRefresh(t, service)
	original := service.Snapshot()
	source.fail.Store(true)
	now = now.Add(time.Hour)
	_ = service.Snapshot()
	waitRadarRefresh(t, service)
	partial := service.Snapshot()
	if !partial.Stale || partial.PartialFailures != 2 || !partial.Points[1].Stale || partial.Points[0].Stale || !partial.Community.Stale {
		t.Fatalf("partial failure not visible: %+v", partial)
	}
	if !partial.Points[1].FetchedAt.Equal(*original.Points[1].FetchedAt) || !partial.Community.FetchedAt.Equal(*original.Community.FetchedAt) {
		t.Fatal("stale data was labeled as newly fetched")
	}
	source.cohort.Store(1)
	now = now.Add(time.Hour)
	_ = service.Snapshot()
	waitRadarRefresh(t, service)
	changed := service.Snapshot()
	if changed.Points[1].Score != nil || len(changed.Points[1].History) != 0 {
		t.Fatal("retained a score from a different benchmark cohort")
	}
}
