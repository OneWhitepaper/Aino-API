package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/iqtest"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type memoryIQTests struct {
	mu   sync.Mutex
	rows map[string]*IQTest
}

func cloneTestTask(task *IQTest) *IQTest {
	body, _ := json.Marshal(task)
	var out IQTest
	_ = json.Unmarshal(body, &out)
	out.UserID = task.UserID
	out.RequestID = task.RequestID
	out.Fingerprint = task.Fingerprint
	return &out
}
func (m *memoryIQTests) Create(_ context.Context, task *IQTest) (*IQTest, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, row := range m.rows {
		if row.UserID == task.UserID && row.RequestID == task.RequestID {
			return cloneTestTask(row), false, nil
		}
		if row.UserID == task.UserID && row.Status == "running" {
			return nil, false, ErrIQTestBusy
		}
	}
	m.rows[task.ID] = cloneTestTask(task)
	return cloneTestTask(task), true, nil
}
func (m *memoryIQTests) Get(_ context.Context, user int64, id string) (*IQTest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	row := m.rows[id]
	if row == nil || row.UserID != user {
		return nil, ErrIQTestNotFound
	}
	return cloneTestTask(row), nil
}
func (m *memoryIQTests) GetByRequest(_ context.Context, user int64, id string) (*IQTest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, row := range m.rows {
		if row.UserID == user && row.RequestID == id {
			return cloneTestTask(row), nil
		}
	}
	return nil, ErrIQTestNotFound
}
func (m *memoryIQTests) Finish(_ context.Context, task *IQTest) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	row := m.rows[task.ID]
	if row == nil || row.UserID != task.UserID || row.Status != "running" {
		return false, nil
	}
	m.rows[task.ID] = cloneTestTask(task)
	return true, nil
}
func (m *memoryIQTests) List(_ context.Context, user int64, offset, limit int) ([]*IQTest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rows := []*IQTest{}
	for _, row := range m.rows {
		if row.UserID == user {
			rows = append(rows, cloneTestTask(row))
		}
	}
	return rows, nil
}

type fakeIQRunner struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
	err     error
	output  string
}

func (r *fakeIQRunner) Run(ctx context.Context, input iqtest.RunInput, progress func(string)) (iqtest.Result, error) {
	r.calls.Add(1)
	if r.started != nil {
		r.started <- struct{}{}
	}
	progress("推理中")
	if r.release != nil {
		select {
		case <-ctx.Done():
			return iqtest.Result{Output: "推理中"}, ctx.Err()
		case <-r.release:
		}
	}
	return iqtest.Result{Output: r.output, DurationMS: 10}, r.err
}
func (r *fakeIQRunner) Models(context.Context, iqtest.Connection) ([]iqtest.Model, error) {
	return []iqtest.Model{{ID: "model", Efforts: []string{}}}, nil
}
func iqRequest() IQTestRequest {
	return IQTestRequest{RequestID: "6786c1bd-1997-4b96-b977-2a125400e570", Kind: "candy", IQTestConnectionRequest: IQTestConnectionRequest{Source: "custom", BaseURL: "https://example.com/v1", APIKey: "private-test-secret"}, Model: "model", Stream: true}
}
func iqService(runner *fakeIQRunner) (*IQTestService, *memoryIQTests) {
	repo := &memoryIQTests{rows: map[string]*IQTest{}}
	svc := newIQTestService(repo, runner, nil, nil, nil)
	return svc, repo
}

func TestIQTestPrivatePersistenceAndUnknownCost(t *testing.T) {
	runner := &fakeIQRunner{output: "最终答案：21"}
	svc, repo := iqService(runner)
	defer svc.Stop()
	task, err := svc.Start(context.Background(), 7, iqRequest(), "203.0.113.9")
	require.NoError(t, err)
	require.Eventually(t, func() bool { row, _ := repo.Get(context.Background(), 7, task.ID); return row.Status == "completed" }, time.Second, time.Millisecond)
	result, err := svc.Get(context.Background(), 7, task.ID)
	require.NoError(t, err)
	require.Equal(t, "passed", result.Verdict)
	require.Nil(t, result.ReferenceCostUSD)
	require.Nil(t, result.Result.Usage)
	_, err = svc.Get(context.Background(), 8, task.ID)
	require.ErrorIs(t, err, ErrIQTestNotFound)
	body, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(body), "private-test-secret")
	require.NotContains(t, string(body), "api_key\"")
}

func TestIQTestIdempotencyBusyAndCancellation(t *testing.T) {
	runner := &fakeIQRunner{started: make(chan struct{}, 1), release: make(chan struct{})}
	svc, _ := iqService(runner)
	defer svc.Stop()
	request := iqRequest()
	first, err := svc.Start(context.Background(), 7, request, "203.0.113.9")
	require.NoError(t, err)
	<-runner.started
	second, err := svc.Start(context.Background(), 7, request, "203.0.113.9")
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	require.EqualValues(t, 1, runner.calls.Load())
	request.Model = "different"
	_, err = svc.Start(context.Background(), 7, request, "203.0.113.9")
	require.ErrorIs(t, err, ErrIQTestIdempotency)
	request = iqRequest()
	request.RequestID = "a786c1bd-1997-4b96-b977-2a125400e570"
	_, err = svc.Start(context.Background(), 7, request, "203.0.113.9")
	require.ErrorIs(t, err, ErrIQTestBusy)
	_, err = svc.Cancel(context.Background(), 8, first.ID)
	require.ErrorIs(t, err, ErrIQTestNotFound)
	result, err := svc.Cancel(context.Background(), 7, first.ID)
	require.NoError(t, err)
	require.Equal(t, "cancelled", result.Status)
	require.Empty(t, result.Verdict)
	require.Eventually(t, func() bool { svc.mu.Lock(); defer svc.mu.Unlock(); return len(svc.active) == 0 }, time.Second, time.Millisecond)
	result, err = svc.Get(context.Background(), 7, first.ID)
	require.NoError(t, err)
	require.Equal(t, "cancelled", result.Status)
}

func TestIQTestErrorsAreSafeAndNeverFailedIQVerdicts(t *testing.T) {
	runner := &fakeIQRunner{err: errors.New("upstream says private-test-secret"), output: "private-test-secret"}
	svc, repo := iqService(runner)
	defer svc.Stop()
	task, err := svc.Start(context.Background(), 7, iqRequest(), "203.0.113.9")
	require.NoError(t, err)
	require.Eventually(t, func() bool { row, _ := repo.Get(context.Background(), 7, task.ID); return row.Status == "failed" }, time.Second, time.Millisecond)
	result, err := svc.Get(context.Background(), 7, task.ID)
	require.NoError(t, err)
	require.Empty(t, result.Verdict)
	require.Equal(t, "connection_failed", result.ErrorCode)
	require.NotContains(t, result.Result.Output, "private-test-secret")
}

func TestIQTestRejectsUnsafeConfigBeforeDispatch(t *testing.T) {
	runner := &fakeIQRunner{}
	svc, _ := iqService(runner)
	defer svc.Stop()
	for _, change := range []func(*IQTestRequest){
		func(r *IQTestRequest) { r.BaseURL = "http://localhost:8080/v1" },
		func(r *IQTestRequest) { r.BaseURL = "https://example.com/private-test-secret/v1" },
		func(r *IQTestRequest) { r.Kind = "arbitrary" },
		func(r *IQTestRequest) { r.Effort = "infinite" },
		func(r *IQTestRequest) { r.RequestID = "" },
		func(r *IQTestRequest) { r.Source = "site"; r.KeyID = 1 },
	} {
		request := iqRequest()
		change(&request)
		_, err := svc.Start(context.Background(), 7, request, "203.0.113.9")
		require.Error(t, err)
		code, _ := infraerrors.ToHTTP(err)
		require.Less(t, code, 500)
	}
	require.Zero(t, runner.calls.Load())
}

type iqKeyFixture struct{ key *APIKey }

func (f iqKeyFixture) GetByID(context.Context, int64) (*APIKey, error) { return f.key, nil }
func (f iqKeyFixture) List(context.Context, int64, pagination.PaginationParams, APIKeyListFilters) ([]APIKey, *pagination.PaginationResult, error) {
	return []APIKey{*f.key}, nil, nil
}

func TestIQTestSiteKeyOwnershipAndMaskedOptions(t *testing.T) {
	runner := &fakeIQRunner{output: "21"}
	svc, _ := iqService(runner)
	defer svc.Stop()
	svc.keys = iqKeyFixture{key: &APIKey{ID: 4, UserID: 7, Key: "sk-private-site-secret", Name: "daily", Status: StatusAPIKeyActive}}
	request := iqRequest()
	request.Source = "site"
	request.KeyID = 4
	request.BaseURL = ""
	request.APIKey = ""
	_, err := svc.Start(context.Background(), 8, request, "203.0.113.8")
	require.Error(t, err)
	require.Zero(t, runner.calls.Load())
	options, err := svc.Keys(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, "sk-••••cret", options[0].Masked)
	task, err := svc.Start(context.Background(), 7, request, "203.0.113.8")
	require.NoError(t, err)
	require.Equal(t, "site", task.Config.Source)
	body, _ := json.Marshal(task)
	require.NotContains(t, string(body), "sk-private-site-secret")
}

func TestIQTestGlobalAdmissionAndShutdown(t *testing.T) {
	runner := &fakeIQRunner{release: make(chan struct{})}
	svc, repo := iqService(runner)
	for user := int64(1); user <= 8; user++ {
		_, err := svc.Start(context.Background(), user, iqRequest(), "203.0.113.8")
		require.NoError(t, err)
	}
	_, err := svc.Start(context.Background(), 9, iqRequest(), "203.0.113.8")
	require.Error(t, err)
	svc.Stop()
	repo.mu.Lock()
	defer repo.mu.Unlock()
	for _, row := range repo.rows {
		require.Equal(t, "interrupted", row.Status)
		require.Empty(t, row.Verdict)
	}
}

type blockedIQModels struct {
	*fakeIQRunner
	entered chan struct{}
	release chan struct{}
}

func (r *blockedIQModels) Models(ctx context.Context, _ iqtest.Connection) ([]iqtest.Model, error) {
	r.entered <- struct{}{}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.release:
		return []iqtest.Model{}, nil
	}
}
func TestIQModelsDiscoveryHasBoundedConcurrentConnections(t *testing.T) {
	runner := &blockedIQModels{fakeIQRunner: &fakeIQRunner{}, entered: make(chan struct{}, 4), release: make(chan struct{})}
	svc := newIQTestService(&memoryIQTests{rows: map[string]*IQTest{}}, runner, nil, nil, nil)
	defer svc.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 4)
	for range 4 {
		go func() { _, err := svc.Models(ctx, 7, iqRequest().IQTestConnectionRequest, "203.0.113.8"); done <- err }()
	}
	for range 4 {
		<-runner.entered
	}
	_, err := svc.Models(ctx, 7, iqRequest().IQTestConnectionRequest, "203.0.113.8")
	require.Error(t, err)
	status, _ := infraerrors.ToHTTP(err)
	require.Equal(t, 429, status)
	close(runner.release)
	for range 4 {
		require.NoError(t, <-done)
	}
}

func TestIQTestReferencePriceUsesKnownUsageAndNeverGuessesUnknownModel(t *testing.T) {
	pricing := &PricingService{pricingData: map[string]*LiteLLMModelPricing{"fixture": {InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6, CacheReadInputTokenCost: .25e-6}}}
	svc := newIQTestService(nil, &fakeIQRunner{}, nil, pricing, NewBillingService(&config.Config{}, pricing))
	defer svc.Stop()
	cached := int64(20)
	task := IQTest{Config: IQTestConfig{Model: "fixture"}, Result: iqtest.Result{Usage: &iqtest.Usage{InputTokens: 100, OutputTokens: 50, CachedTokens: &cached}}}
	cost := svc.referenceCost(task)
	require.NotNil(t, cost)
	require.InDelta(t, .000185, *cost, 1e-9)
	task.Config.Model = "unknown-gpt-model"
	require.Nil(t, svc.referenceCost(task))
	task.Config.Model = "fixture"
	task.Config.Tier = "priority"
	require.Nil(t, svc.referenceCost(task))
	task.Config.Tier = ""
	task.Result.Usage.CachedTokens = nil
	require.Nil(t, svc.referenceCost(task))
}
