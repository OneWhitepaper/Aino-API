package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/iqradar"
)

type IQRadarSource interface {
	Catalog(context.Context) (iqradar.Catalog, error)
	Score(context.Context, iqradar.Selection, iqradar.Catalog) (iqradar.Point, error)
	Community(context.Context) (iqradar.Community, error)
}

type IQRadarSnapshot struct {
	iqradar.Catalog
	SourceName        string            `json:"source_name"`
	SourceURL         string            `json:"source_url"`
	FetchedAt         *time.Time        `json:"fetched_at"`
	Refreshing        bool              `json:"refreshing"`
	Stale             bool              `json:"stale"`
	PartialFailures   int               `json:"partial_failures"`
	CacheTTLSeconds   int               `json:"cache_ttl_seconds"`
	RetryAfterSeconds int               `json:"retry_after_seconds"`
	Points            []iqradar.Point   `json:"points"`
	Community         iqradar.Community `json:"community"`
}

// IQRadarService keeps one snapshot per server, shared by all authenticated users.
// Expired reads return immediately while a bounded background refresh runs.
type IQRadarService struct {
	source      IQRadarSource
	mu          sync.Mutex
	snapshot    IQRadarSnapshot
	refreshing  bool
	refreshDone chan struct{}
	nextRefresh time.Time
	now         func() time.Time
}

const radarCacheTTL = 30 * time.Minute

func NewIQRadarService(source IQRadarSource) *IQRadarService {
	return &IQRadarService{source: source, now: time.Now, snapshot: IQRadarSnapshot{SourceName: iqradar.SourceName, SourceURL: iqradar.SourceURL, CacheTTLSeconds: int(radarCacheTTL.Seconds()), Points: []iqradar.Point{}, Community: iqradar.Community{WindowHours: 24, Models: []iqradar.CommunityModel{}}}}
}

func (s *IQRadarService) Snapshot() IQRadarSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	expired := !now.Before(s.nextRefresh)
	if expired && !s.refreshing {
		s.refreshing = true
		s.refreshDone = make(chan struct{})
		previous := cloneRadarSnapshot(s.snapshot)
		go s.refresh(previous)
	}
	result := cloneRadarSnapshot(s.snapshot)
	result.Refreshing = s.refreshing
	result.Stale = result.Stale || expired
	result.RetryAfterSeconds = max(0, int(s.nextRefresh.Sub(now).Seconds()))
	return result
}

func (s *IQRadarService) refresh(previous IQRadarSnapshot) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var catalog iqradar.Catalog
	var community iqradar.Community
	var catalogErr, communityErr error
	var initial sync.WaitGroup
	initial.Add(2)
	go func() { defer initial.Done(); catalog, catalogErr = s.source.Catalog(ctx) }()
	go func() { defer initial.Done(); community, communityErr = s.source.Community(ctx) }()
	initial.Wait()
	result := previous
	result.PartialFailures = 0
	if communityErr != nil {
		slog.Warn("iq_radar.community_refresh_failed", "error", communityErr)
		result.Community.Stale = true
		result.PartialFailures++
	} else {
		result.Community = community
	}
	if catalogErr != nil {
		slog.Warn("iq_radar.catalog_refresh_failed", "error", catalogErr)
		result.PartialFailures++
		for index := range result.Points {
			result.Points[index].Stale = true
		}
	} else {
		result.Catalog = catalog
		result.Points = make([]iqradar.Point, len(catalog.Selections))
		errors := make([]error, len(catalog.Selections))
		// Catalog discovery determines the requested tiers; no per-user or hardcoded list.
		jobs := make(chan int)
		var workers sync.WaitGroup
		for range min(4, len(catalog.Selections)) {
			workers.Add(1)
			go func() {
				defer workers.Done()
				for index := range jobs {
					result.Points[index], errors[index] = s.source.Score(ctx, catalog.Selections[index], catalog)
				}
			}()
		}
		for index := range catalog.Selections {
			jobs <- index
		}
		close(jobs)
		workers.Wait()
		old := make(map[iqradar.Selection]iqradar.Point)
		// Never carry a stale score across task sets, score versions or scoring scales.
		if sameRadarCohort(previous.Catalog, catalog) {
			for _, point := range previous.Points {
				old[iqradar.Selection{Model: point.Model, Effort: point.Effort}] = point
			}
		}
		for index, err := range errors {
			if err == nil {
				continue
			}
			result.PartialFailures++
			selection := catalog.Selections[index]
			point, exists := old[selection]
			if !exists {
				point = iqradar.Point{Model: selection.Model, Effort: selection.Effort, RequiredTasks: catalog.RequiredTasks, ScoreStatus: "unavailable", History: []iqradar.HistoryPoint{}}
			}
			point.Stale = true
			result.Points[index] = point
		}
		if failures := result.PartialFailures; failures > 0 {
			slog.Warn("iq_radar.refresh_partial", "failed_requests", failures, "selections", len(catalog.Selections))
		}
		now := s.now().UTC()
		result.FetchedAt = &now
	}
	result.Stale = result.PartialFailures > 0
	delay := radarCacheTTL
	if result.Stale {
		delay = 5 * time.Minute
	}
	if catalogErr != nil && len(result.Points) == 0 {
		delay = time.Minute
	}
	s.mu.Lock()
	s.snapshot = result
	s.nextRefresh = s.now().Add(delay)
	s.refreshing = false
	close(s.refreshDone)
	s.mu.Unlock()
}

func sameRadarCohort(a, b iqradar.Catalog) bool {
	return a.Benchmark == b.Benchmark && a.ScoreVersion == b.ScoreVersion && a.TaskSetSHA256 == b.TaskSetSHA256 && a.ScoreMax == b.ScoreMax && a.RequiredTasks == b.RequiredTasks
}

func cloneRadarPointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneRadarSnapshot(snapshot IQRadarSnapshot) IQRadarSnapshot {
	snapshot.FetchedAt = cloneRadarPointer(snapshot.FetchedAt)
	snapshot.Selections = append([]iqradar.Selection{}, snapshot.Selections...)
	points := make([]iqradar.Point, len(snapshot.Points))
	for index, point := range snapshot.Points {
		point.Score = cloneRadarPointer(point.Score)
		point.AveragePriceUSD = cloneRadarPointer(point.AveragePriceUSD)
		point.AverageMinutes = cloneRadarPointer(point.AverageMinutes)
		point.LatestGradedAt = cloneRadarPointer(point.LatestGradedAt)
		point.FetchedAt = cloneRadarPointer(point.FetchedAt)
		point.History = append([]iqradar.HistoryPoint{}, point.History...)
		points[index] = point
	}
	snapshot.Points = points
	snapshot.Community.UpdatedAt = cloneRadarPointer(snapshot.Community.UpdatedAt)
	snapshot.Community.FetchedAt = cloneRadarPointer(snapshot.Community.FetchedAt)
	models := make([]iqradar.CommunityModel, len(snapshot.Community.Models))
	for index, model := range snapshot.Community.Models {
		model.Average = cloneRadarPointer(model.Average)
		models[index] = model
	}
	snapshot.Community.Models = models
	return snapshot
}
