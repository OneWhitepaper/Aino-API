package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync"
	"time"
	"unicode"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/iqtest"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/google/uuid"
)

const iqTestDuration = 10 * time.Minute

type iqTestActive struct {
	task   IQTest
	cancel context.CancelFunc
}
type IQTestService struct {
	repo       IQTestRepository
	runner     iqTestRunner
	keys       iqTestKeys
	pricing    *PricingService
	billing    *BillingService
	mu         sync.Mutex
	active     map[string]*iqTestActive
	ctx        context.Context
	cancel     context.CancelFunc
	stopped    bool
	wg         sync.WaitGroup
	modelSlots chan struct{}
}

func NewIQTestService(repo IQTestRepository, runner *iqtest.Runner, keys *APIKeyService, pricing *PricingService, billing *BillingService) *IQTestService {
	return newIQTestService(repo, runner, keys, pricing, billing)
}

func newIQTestService(repo IQTestRepository, runner iqTestRunner, keys iqTestKeys, pricing *PricingService, billing *BillingService) *IQTestService {
	ctx, cancel := context.WithCancel(context.Background())
	return &IQTestService{repo: repo, runner: runner, keys: keys, pricing: pricing, billing: billing, ctx: ctx, cancel: cancel, active: map[string]*iqTestActive{}, modelSlots: make(chan struct{}, 4)}
}

func invalidIQTest() error { return infraerrors.BadRequest("IQ_TEST_INVALID", "Invalid test settings") }

func normalizeIQConnection(request *IQTestConnectionRequest) error {
	request.APIKey = strings.TrimSpace(request.APIKey)
	switch request.Source {
	case "site":
		if request.KeyID <= 0 || request.BaseURL != "" || request.APIKey != "" {
			return invalidIQTest()
		}
	case "custom":
		base, err := iqtest.NormalizeBaseURL(request.BaseURL)
		if err != nil || request.KeyID != 0 || len(request.APIKey) < 8 || len(request.APIKey) > 8192 || strings.IndexFunc(request.APIKey, unicode.IsControl) >= 0 || strings.Contains(base, request.APIKey) {
			return invalidIQTest()
		}
		request.BaseURL = base
	default:
		return invalidIQTest()
	}
	return nil
}

func (s *IQTestService) connection(ctx context.Context, userID int64, request IQTestConnectionRequest, clientIP string) (iqtest.Connection, error) {
	if err := normalizeIQConnection(&request); err != nil {
		return iqtest.Connection{}, err
	}
	connection := iqtest.Connection{Source: request.Source, BaseURL: request.BaseURL, APIKey: request.APIKey, ClientIP: clientIP}
	if request.Source == "site" {
		if s.keys == nil {
			return connection, invalidIQTest()
		}
		key, err := s.keys.GetByID(ctx, request.KeyID)
		if err != nil || key == nil || key.UserID != userID {
			return connection, infraerrors.NotFound("IQ_TEST_KEY_NOT_FOUND", "API key not found")
		}
		if !key.IsActive() || key.IsExpired() || key.IsQuotaExhausted() {
			return connection, infraerrors.Forbidden("IQ_TEST_KEY_UNAVAILABLE", "API key is inactive, expired or exhausted")
		}
		connection.APIKey = key.Key
	}
	return connection, nil
}

func (s *IQTestService) Start(ctx context.Context, userID int64, request IQTestRequest, clientIP string) (*IQTest, error) {
	request.Model = strings.TrimSpace(request.Model)
	if _, err := uuid.Parse(request.RequestID); err != nil {
		return nil, invalidIQTest()
	}
	if request.Kind != "candy" && request.Kind != "pelican" || request.Model == "" || len(request.Model) > 200 || strings.IndexFunc(request.Model, unicode.IsControl) >= 0 || !iqtest.ValidEffort(request.Effort) || (request.Tier != "" && request.Tier != "default" && request.Tier != "priority") {
		return nil, invalidIQTest()
	}
	if err := normalizeIQConnection(&request.IQTestConnectionRequest); err != nil {
		return nil, err
	}
	if request.Tier == "default" {
		request.Tier = ""
	}
	// Only a one-way request fingerprint is stored; the request contains credentials.
	body, _ := json.Marshal(request)
	digest := sha256.Sum256(body)
	fingerprint := hex.EncodeToString(digest[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, err := s.repo.GetByRequest(ctx, userID, request.RequestID)
	if err == nil {
		if existing.Fingerprint != fingerprint {
			return nil, ErrIQTestIdempotency
		}
		return existing, nil
	}
	if !errors.Is(err, ErrIQTestNotFound) {
		return nil, err
	}
	if s.stopped {
		return nil, infraerrors.ServiceUnavailable("IQ_TEST_UNAVAILABLE", "Test runner is stopping")
	}
	for _, run := range s.active {
		if run.task.UserID == userID {
			return nil, ErrIQTestBusy
		}
	}
	if len(s.active) >= 8 {
		return nil, infraerrors.TooManyRequests("IQ_TEST_CAPACITY", "Test runner is busy; try again later")
	}
	connection, err := s.connection(ctx, userID, request.IQTestConnectionRequest, clientIP)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	task := &IQTest{ID: uuid.NewString(), UserID: userID, RequestID: request.RequestID, Fingerprint: fingerprint, Config: IQTestConfig{Kind: request.Kind, Source: request.Source, KeyID: request.KeyID, BaseURL: request.BaseURL, Model: request.Model, Effort: request.Effort, Tier: request.Tier, Stream: request.Stream}, Status: "running", CreatedAt: now, DeadlineAt: now.Add(iqTestDuration)}
	task, created, err := s.repo.Create(ctx, task)
	if err != nil {
		return nil, err
	}
	if !created {
		if task.Fingerprint != fingerprint {
			return nil, ErrIQTestIdempotency
		}
		return task, nil
	}
	runCtx, cancel := context.WithDeadline(s.ctx, task.DeadlineAt)
	run := &iqTestActive{task: *task, cancel: cancel}
	s.active[task.ID] = run
	s.wg.Add(1)
	go s.execute(runCtx, run, iqtest.RunInput{Connection: connection, Kind: request.Kind, Model: request.Model, Effort: request.Effort, Tier: request.Tier, Stream: request.Stream})
	return task, nil
}

func (s *IQTestService) execute(ctx context.Context, run *iqTestActive, input iqtest.RunInput) {
	defer s.wg.Done()
	defer func() { run.cancel(); s.mu.Lock(); delete(s.active, run.task.ID); s.mu.Unlock() }()
	// A cancellation issued to another application instance is observed here.
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
				task, err := s.repo.Get(readCtx, run.task.UserID, run.task.ID)
				cancel()
				if errors.Is(err, ErrIQTestNotFound) || err == nil && task.Status != "running" {
					run.cancel()
					return
				}
			}
		}
	}()
	redact := func(text string) string {
		if input.APIKey != "" {
			return strings.ReplaceAll(text, input.APIKey, "[redacted]")
		}
		return text
	}
	result, err := s.runner.Run(ctx, input, func(output string) { s.mu.Lock(); run.task.Result.Output = redact(output); s.mu.Unlock() })
	result.Output = redact(result.Output)
	s.mu.Lock()
	task := run.task
	stopped := s.stopped
	s.mu.Unlock()
	result.DurationMS = time.Since(task.CreatedAt).Milliseconds()
	task.Result = result
	now := time.Now().UTC()
	task.FinishedAt = &now
	task.Status = "completed"
	if err != nil {
		task.Status = "failed"
		task.ErrorCode = safeIQTestError(err)
		if errors.Is(err, context.Canceled) {
			task.Status = "cancelled"
			task.ErrorCode = ""
			if stopped {
				task.Status = "interrupted"
				task.ErrorCode = "interrupted"
			}
		}
	} else {
		task.Verdict, task.Basis = iqtest.Grade(task.Config.Kind, result.Output)
	}
	if err == nil {
		task.ReferenceCostUSD = s.referenceCost(task)
	}
	// Persistence may be retried; a billable upstream generation is never retried.
	for range 3 {
		writeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, writeErr := s.repo.Finish(writeCtx, &task)
		cancel()
		if writeErr == nil {
			return
		}
	}
}

func safeIQTestError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	switch err.Error() {
	case "upstream_auth", "upstream_balance", "upstream_rate_limit", "upstream_unsupported", "upstream_error", "invalid_response", "incomplete_response", "empty_response", "response_too_large", "invalid_endpoint":
		return err.Error()
	default:
		return "connection_failed"
	}
}

func (s *IQTestService) referenceCost(task IQTest) *float64 {
	u := task.Result.Usage
	if u == nil || s.pricing == nil || s.billing == nil {
		return nil
	}
	p := s.pricing.GetIdentifiedModelPricing(task.Config.Model)
	if p == nil || p.TokenPricingAbsent || u.CachedTokens == nil && u.InputTokens > 0 {
		return nil
	}
	tier := task.Result.ServiceTier
	if tier == "" {
		if task.Config.Tier == "priority" {
			return nil
		}
		tier = "default"
	}
	if tier != "default" && tier != "standard" && tier != "priority" {
		return nil
	}
	if tier == "priority" && (p.InputCostPerTokenPriority <= 0 || p.OutputCostPerTokenPriority <= 0) {
		return nil
	}
	cached := int64(0)
	if u.CachedTokens != nil {
		cached = *u.CachedTokens
	}
	cost, err := s.billing.CalculateCostWithServiceTier(task.Config.Model, UsageTokens{InputTokens: int(u.InputTokens - cached), OutputTokens: int(u.OutputTokens), CacheReadTokens: int(cached)}, 1, tier)
	if err != nil || cost == nil || math.IsNaN(cost.TotalCost) || math.IsInf(cost.TotalCost, 0) || cost.TotalCost < 0 {
		return nil
	}
	return &cost.TotalCost
}

func (s *IQTestService) Get(ctx context.Context, userID int64, id string) (*IQTest, error) {
	task, err := s.repo.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if task.Status == "running" {
		if time.Now().After(task.DeadlineAt) {
			task.Status = "interrupted"
			task.ErrorCode = "interrupted"
			now := time.Now().UTC()
			task.FinishedAt = &now
			if _, err = s.repo.Finish(ctx, task); err != nil {
				return nil, err
			}
			return s.repo.Get(ctx, userID, id)
		}
		s.mu.Lock()
		if run := s.active[id]; run != nil && run.task.UserID == userID {
			copy := run.task
			task = &copy
			task.Result.DurationMS = time.Since(task.CreatedAt).Milliseconds()
		}
		s.mu.Unlock()
	}
	return task, nil
}

func (s *IQTestService) List(ctx context.Context, userID int64, offset int) ([]*IQTest, bool, error) {
	if offset < 0 || offset > 1000 {
		return nil, false, invalidIQTest()
	}
	rows, err := s.repo.List(ctx, userID, offset, 21)
	if err != nil {
		return nil, false, err
	}
	more := len(rows) > 20
	if more {
		rows = rows[:20]
	}
	for i, row := range rows {
		if row.Status == "running" {
			rows[i], err = s.Get(ctx, userID, row.ID)
			if err != nil {
				return nil, false, err
			}
		}
	}
	return rows, more, nil
}

func (s *IQTestService) Cancel(ctx context.Context, userID int64, id string) (*IQTest, error) {
	task, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if task.Status != "running" {
		return task, nil
	}
	task.Status = "cancelled"
	task.Verdict = ""
	task.Basis = ""
	now := time.Now().UTC()
	task.FinishedAt = &now
	if _, err = s.repo.Finish(ctx, task); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if run := s.active[id]; run != nil && run.task.UserID == userID {
		run.cancel()
	}
	s.mu.Unlock()
	return s.repo.Get(ctx, userID, id)
}

func (s *IQTestService) Models(ctx context.Context, userID int64, request IQTestConnectionRequest, clientIP string) ([]iqtest.Model, error) {
	select {
	case s.modelSlots <- struct{}{}:
		defer func() { <-s.modelSlots }()
	default:
		return nil, infraerrors.TooManyRequests("IQ_MODELS_BUSY", "Model discovery is busy; try again later")
	}
	connection, err := s.connection(ctx, userID, request, clientIP)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	models, err := s.runner.Models(ctx, connection)
	if err != nil {
		return nil, infraerrors.BadRequest("IQ_MODELS_UNAVAILABLE", safeIQTestError(err))
	}
	return models, nil
}

func (s *IQTestService) Keys(ctx context.Context, userID int64) ([]IQTestKeyOption, error) {
	options := []IQTestKeyOption{}
	if s.keys == nil {
		return options, nil
	}
	keys, _, err := s.keys.List(ctx, userID, pagination.PaginationParams{Page: 1, PageSize: 100}, APIKeyListFilters{})
	if err != nil {
		return nil, err
	}
	for _, key := range keys {
		if key.IsActive() && !key.IsExpired() && !key.IsQuotaExhausted() {
			masked := "••••"
			if len(key.Key) >= 8 {
				masked = key.Key[:3] + "••••" + key.Key[len(key.Key)-4:]
			}
			options = append(options, IQTestKeyOption{ID: key.ID, Name: key.Name, Masked: masked})
		}
	}
	return options, nil
}

func (s *IQTestService) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.stopped = true
	s.cancel()
	s.mu.Unlock()
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(7 * time.Second):
	}
}
