package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	AsyncFundingProviderImage      = "image"
	AsyncFundingProviderGrokVideo  = "grok_video"
	AsyncFundingProviderVideo      = "video"
	AsyncFundingProviderSeedance   = "seedance"
	AsyncFundingProviderBatchImage = "batch_image"
)

// AsyncAPIKeyBillingSnapshot retains only the flags that decide which caller
// quota counters enter an immutable usage command. It contains no credential.
type AsyncAPIKeyBillingSnapshot struct {
	Quota       float64 `json:"quota"`
	RateLimit5h float64 `json:"rate_limit_5h"`
	RateLimit1d float64 `json:"rate_limit_1d"`
	RateLimit7d float64 `json:"rate_limit_7d"`
}

// AsyncFundingSnapshot is private billing metadata, serialized separately from
// APIKey so it cannot enter credential/authentication caches.
type AsyncFundingSnapshot struct {
	DesktopManaged  bool                        `json:"desktop_managed,omitempty"`
	APIKeyBilling   *AsyncAPIKeyBillingSnapshot `json:"api_key_billing,omitempty"`
	DesktopUsage    *DesktopUsage               `json:"desktop_usage,omitempty"`
	DesktopPurpose  string                      `json:"desktop_purpose,omitempty"`
	ActorUserID     int64                       `json:"actor_user_id"`
	APIKeyID        int64                       `json:"api_key_id"`
	Group           *Group                      `json:"group,omitempty"`
	Platform        string                      `json:"platform"`
	Funding         *FundingDecision            `json:"funding"`
	Subscription    *UserSubscription           `json:"subscription,omitempty"`
	AccountID       int64                       `json:"account_id,omitempty"`
	PricingSnapshot json.RawMessage             `json:"pricing_snapshot,omitempty"`
}
type AsyncFundingTask struct {
	OperationID   string
	Provider      string
	TaskID        string
	State         string
	Snapshot      *AsyncFundingSnapshot
	TerminalUsage json.RawMessage
}

// Optional repository capability keeps management-only repositories independent
// from asynchronous adapters.
type AsyncFundingRepository interface {
	QueueAsyncFunding(context.Context, *AsyncFundingTask) error
	BindAsyncFunding(context.Context, *AsyncFundingTask) error
	GetAsyncFunding(context.Context, string, string, int64, int64) (*AsyncFundingTask, error)
	BeginAsyncFundingDispatch(context.Context, string, int64, int64, int64, bool) error
	CancelQueuedAsyncFunding(context.Context, string, int64, int64) error
	StoreAsyncFundingUsage(context.Context, string, int64, int64, json.RawMessage) (json.RawMessage, error)
	MarkAsyncFundingSettled(context.Context, string, int64, int64) error
}

func AsyncFundingSnapshotFromRequest(key *APIKey) *AsyncFundingSnapshot {
	if key == nil || !key.Funding.HasReservation() {
		return nil
	}
	snapshot := &AsyncFundingSnapshot{DesktopManaged: key.DesktopManaged, APIKeyBilling: &AsyncAPIKeyBillingSnapshot{Quota: key.Quota, RateLimit5h: key.RateLimit5h, RateLimit1d: key.RateLimit1d, RateLimit7d: key.RateLimit7d}, DesktopUsage: key.DesktopUsage, DesktopPurpose: key.DesktopPurpose, ActorUserID: key.UserID, APIKeyID: key.ID, Group: key.Group, Funding: cloneFundingDecision(key.Funding), Subscription: key.Funding.Subscription}
	if key.Group != nil {
		snapshot.Platform = key.Group.Platform
	}
	snapshot.Funding.AdmissionToken = ""
	snapshot.Funding.dispatchContext = nil
	snapshot.Funding.fundingContext = nil
	data, err := json.Marshal(snapshot)
	if err != nil {
		return nil
	}
	var copied AsyncFundingSnapshot
	if json.Unmarshal(data, &copied) != nil {
		return nil
	}
	return &copied
}
func (s *TeamFundingService) asyncRepository() (AsyncFundingRepository, error) {
	if s == nil {
		return nil, ErrFundingUnavailable
	}
	repo, ok := s.repo.(AsyncFundingRepository)
	if !ok {
		return nil, ErrFundingUnavailable
	}
	return repo, nil
}
func (s *TeamFundingService) HandoffFundingToAsync(ctx context.Context, key *APIKey, provider, taskID string) error {
	if key == nil || !key.Funding.HasReservation() {
		return nil
	}
	repo, err := s.asyncRepository()
	if err != nil {
		return err
	}
	snapshot := AsyncFundingSnapshotFromRequest(key)
	if snapshot == nil || strings.TrimSpace(provider) == "" || strings.TrimSpace(taskID) == "" {
		return ErrTeamFundingInvalid
	}
	task := &AsyncFundingTask{OperationID: key.Funding.RequestID, Provider: provider, TaskID: taskID, Snapshot: snapshot}
	if err = repo.QueueAsyncFunding(ctx, task); err != nil {
		return err
	}
	// The original HTTP cleanup only releases 'reserved'. A queued hold belongs
	// to the durable task until this guard atomically claims its network send.
	previousContext := FundingDispatchContext(ctx, key)
	key.Funding.dispatchContext = WithFundingDispatchGuard(ctx, key.Funding.RequestID, func(c context.Context) error {
		return repo.BeginAsyncFundingDispatch(c, key.Funding.RequestID, key.ID, key.UserID, key.Funding.ReservationID, key.Funding.IsShared())
	}, func(c context.Context) error {
		if key.Funding.IsShared() {
			return s.ReleaseDispatchedUnused(c, key.Funding.ReservationID, key.UserID)
		}
		return s.ReleasePersonalProvenUnused(c, key.Funding.ReservationID, key.UserID)
	})
	CopyFundingDispatchEnvelope(previousContext, key.Funding.dispatchContext)
	return nil
}
func (s *TeamFundingService) BindAsyncFundingTask(ctx context.Context, key *APIKey, provider, taskID string, accountID int64, pricing json.RawMessage) error {
	if key == nil || !key.Funding.HasReservation() {
		return nil
	}
	repo, err := s.asyncRepository()
	if err != nil {
		return err
	}
	snapshot := AsyncFundingSnapshotFromRequest(key)
	snapshot.AccountID = accountID
	snapshot.PricingSnapshot = pricing
	return repo.BindAsyncFunding(ctx, &AsyncFundingTask{OperationID: key.Funding.RequestID, Provider: provider, TaskID: taskID, Snapshot: snapshot})
}
func (s *TeamFundingService) RestoreAsyncFundingRequest(ctx context.Context, key *APIKey, provider, taskID string) (*APIKey, *UserSubscription, error) {
	if key == nil {
		return nil, nil, ErrTeamFundingNotFound
	}
	if provider == AsyncFundingProviderGeminiCache {
		resource, err := s.GetFundingCachedContent(ctx, key, taskID)
		if errors.Is(err, ErrGeminiCachedContentNotFound) {
			return nil, nil, ErrTeamFundingNotFound
		}
		if err != nil {
			return nil, nil, err
		}
		if resource.FundingSnapshot == nil {
			return nil, nil, ErrTeamFundingNotFound
		}
		return RestoreAsyncFundingSnapshot(key, resource.FundingSnapshot)
	}
	repo, err := s.asyncRepository()
	if err != nil {
		return nil, nil, err
	}
	task, err := repo.GetAsyncFunding(ctx, provider, taskID, key.UserID, key.ID)
	if err != nil {
		return nil, nil, err
	}
	return RestoreAsyncFundingSnapshot(key, task.Snapshot)
}
func RestoreAsyncFundingSnapshot(key *APIKey, snapshot *AsyncFundingSnapshot) (*APIKey, *UserSubscription, error) {
	if key == nil || snapshot == nil || snapshot.Funding == nil || key.UserID != snapshot.ActorUserID || key.ID != snapshot.APIKeyID {
		return nil, nil, ErrTeamFundingNotFound
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return nil, nil, err
	}
	var copied AsyncFundingSnapshot
	if err = json.Unmarshal(data, &copied); err != nil {
		return nil, nil, err
	}
	result := *key
	result.Group = copied.Group
	result.Funding = copied.Funding
	if billing := copied.APIKeyBilling; billing != nil {
		result.Quota = billing.Quota
		result.RateLimit5h = billing.RateLimit5h
		result.RateLimit1d = billing.RateLimit1d
		result.RateLimit7d = billing.RateLimit7d
	}
	result.DesktopManaged = copied.DesktopManaged
	result.DesktopUsage = copied.DesktopUsage
	result.DesktopPurpose = copied.DesktopPurpose
	if result.Group != nil {
		id := result.Group.ID
		result.GroupID = &id
	}
	result.Funding.Subscription = copied.Subscription
	return &result, copied.Subscription, nil
}
func (s *APIKeyService) HandoffFundingToAsync(ctx context.Context, key *APIKey, provider, taskID string) error {
	if s == nil {
		return ErrFundingUnavailable
	}
	return s.teamFunding.HandoffFundingToAsync(ctx, key, provider, taskID)
}
func (s *APIKeyService) BindAsyncFundingTask(ctx context.Context, key *APIKey, provider, taskID string, accountID int64, pricing json.RawMessage) error {
	if s == nil {
		return ErrFundingUnavailable
	}
	return s.teamFunding.BindAsyncFundingTask(ctx, key, provider, taskID, accountID, pricing)
}
func (s *APIKeyService) RestoreAsyncFundingRequest(ctx context.Context, key *APIKey, provider, taskID string) (*APIKey, *UserSubscription, error) {
	if s == nil {
		return nil, nil, ErrFundingUnavailable
	}
	return s.teamFunding.RestoreAsyncFundingRequest(ctx, key, provider, taskID)
}
func (s *APIKeyService) CancelQueuedAsyncFunding(ctx context.Context, key *APIKey) error {
	if key == nil || !key.Funding.HasReservation() {
		return nil
	}
	repo, err := s.teamFunding.asyncRepository()
	if err != nil {
		return err
	}
	return repo.CancelQueuedAsyncFunding(ctx, key.Funding.RequestID, key.ID, key.UserID)
}

func (s *APIKeyService) LoadAsyncFundingTask(ctx context.Context, provider, taskID string, user, key int64) (*AsyncFundingTask, error) {
	if s == nil {
		return nil, ErrFundingUnavailable
	}
	repo, err := s.teamFunding.asyncRepository()
	if err != nil {
		return nil, err
	}
	return repo.GetAsyncFunding(ctx, provider, taskID, user, key)
}

// PersistAsyncUsage captures the terminal billable result before handing it to
// the process-local usage queue. Only non-secret billing metadata is persisted.
// The first terminal envelope wins, making simultaneous content/status polls
// converge on the same durable billing fingerprint.
func (s *APIKeyService) PersistAsyncUsage(ctx context.Context, input *OpenAIRecordUsageInput) (*OpenAIRecordUsageInput, error) {
	persistCtx, cancelPersist := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancelPersist()
	if input == nil || input.APIKey == nil || !input.APIKey.Funding.HasReservation() {
		return input, nil
	}
	repo, err := s.teamFunding.asyncRepository()
	if err != nil {
		return nil, err
	}
	copy := *input
	copy.APIKeyService = nil
	copy.APIKey = nil
	copy.User = nil
	copy.Subscription = nil
	if input.Account != nil {
		account := *input.Account
		account.Credentials = nil
		account.Extra = asyncBillingAccountExtra(input.Account)
		account.Proxy = nil
		account.Groups = nil
		account.AccountGroups = nil
		copy.Account = &account
	}
	payload, err := json.Marshal(&copy)
	if err != nil {
		return nil, err
	}
	d := input.APIKey.Funding
	payload, err = repo.StoreAsyncFundingUsage(persistCtx, d.RequestID, input.APIKey.ID, input.APIKey.UserID, payload)
	if errors.Is(err, ErrTeamFundingNotFound) {
		return input, nil
	}
	if err != nil {
		return nil, err
	}
	var durable OpenAIRecordUsageInput
	if err = json.Unmarshal(payload, &durable); err != nil {
		return nil, err
	}
	durable.APIKey = input.APIKey
	durable.User = input.User
	durable.Subscription = input.Subscription
	durable.APIKeyService = input.APIKeyService
	return &durable, nil
}
func (s *APIKeyService) MarkAsyncFundingSettled(ctx context.Context, key *APIKey) error {
	if key == nil || !key.Funding.HasReservation() {
		return nil
	}
	repo, err := s.teamFunding.asyncRepository()
	if err != nil {
		return err
	}
	return repo.MarkAsyncFundingSettled(ctx, key.Funding.RequestID, key.ID, key.UserID)
}
func (s *APIKeyService) ReplayAsyncFundingUsage(ctx context.Context, key *APIKey, task *AsyncFundingTask, record func(context.Context, *OpenAIRecordUsageInput) error) error {
	if task == nil || len(task.TerminalUsage) == 0 || task.State == "settled" {
		return nil
	}
	var input OpenAIRecordUsageInput
	if err := json.Unmarshal(task.TerminalUsage, &input); err != nil {
		return err
	}
	restored, sub, err := RestoreAsyncFundingSnapshot(key, task.Snapshot)
	if err != nil {
		return err
	}
	input.APIKey = restored
	input.User = restored.User
	input.Subscription = sub
	input.APIKeyService = s
	if err = record(ctx, &input); err != nil {
		return err
	}
	return s.MarkAsyncFundingSettled(ctx, restored)
}

func AsyncVideoFundingProvider(taskID string) string {
	if strings.HasPrefix(taskID, "seedance:") {
		return AsyncFundingProviderSeedance
	}
	if strings.HasPrefix(taskID, "video_") {
		return AsyncFundingProviderVideo
	}
	return AsyncFundingProviderGrokVideo
}
func (s *OpenAIGatewayService) asyncFundingTask(ctx context.Context, taskID string, user, key int64) (*AsyncFundingTask, error) {
	if s == nil || s.billingCacheService == nil || s.billingCacheService.teamFunding == nil {
		return nil, ErrTeamFundingNotFound
	}
	repo, err := s.billingCacheService.teamFunding.asyncRepository()
	if err != nil {
		return nil, err
	}
	return repo.GetAsyncFunding(ctx, AsyncVideoFundingProvider(taskID), taskID, user, key)
}

type fundingFollowupKey struct{}

func WithFundingFollowup(ctx context.Context) context.Context {
	return context.WithValue(ctx, fundingFollowupKey{}, true)
}
func IsFundingFollowup(ctx context.Context) bool {
	value, _ := ctx.Value(fundingFollowupKey{}).(bool)
	return value
}

const AsyncFundingProviderGeminiCache = "gemini_cache"
const AsyncFundingProviderGeminiCacheExtension = "gemini_cache_extension"

type fundingCachedContentRepository interface {
	GetFundingCachedContent(context.Context, int64, int64, string) (*GeminiCachedContent, error)
}

func (s *TeamFundingService) GetFundingCachedContent(ctx context.Context, key *APIKey, name string) (*GeminiCachedContent, error) {
	if s == nil || key == nil {
		return nil, ErrGeminiCachedContentNotFound
	}
	repo, ok := s.repo.(fundingCachedContentRepository)
	if !ok {
		return nil, ErrGeminiCachedContentNotFound
	}
	return repo.GetFundingCachedContent(ctx, key.UserID, key.ID, name)
}
func (s *APIKeyService) GetFundingCachedContent(ctx context.Context, key *APIKey, name string) (*GeminiCachedContent, error) {
	if s == nil {
		return nil, ErrGeminiCachedContentNotFound
	}
	return s.teamFunding.GetFundingCachedContent(ctx, key, name)
}

func (s *APIKeyService) PersistGatewayAsyncUsage(ctx context.Context, input *RecordUsageInput) (*RecordUsageInput, error) {
	persistCtx, cancelPersist := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancelPersist()
	if input == nil || input.APIKey == nil || !input.APIKey.Funding.HasReservation() {
		return input, nil
	}
	repo, err := s.teamFunding.asyncRepository()
	if err != nil {
		return nil, err
	}
	copy := *input
	copy.APIKeyService = nil
	copy.APIKey = nil
	copy.User = nil
	copy.Subscription = nil
	if input.Account != nil {
		account := *input.Account
		account.Credentials = nil
		account.Extra = asyncBillingAccountExtra(input.Account)
		account.Proxy = nil
		account.Groups = nil
		account.AccountGroups = nil
		copy.Account = &account
	}
	payload, err := json.Marshal(map[string]any{"gateway_usage": &copy})
	if err != nil {
		return nil, err
	}
	d := input.APIKey.Funding
	payload, err = repo.StoreAsyncFundingUsage(persistCtx, d.RequestID, input.APIKey.ID, input.APIKey.UserID, payload)
	if errors.Is(err, ErrTeamFundingNotFound) {
		return input, nil
	}
	if err != nil {
		return nil, err
	}
	var durable struct {
		Usage *RecordUsageInput `json:"gateway_usage"`
	}
	if err = json.Unmarshal(payload, &durable); err != nil {
		return nil, err
	}
	if durable.Usage == nil {
		return nil, ErrTeamFundingConflict
	}
	durable.Usage.APIKey = input.APIKey
	durable.Usage.User = input.User
	durable.Usage.Subscription = input.Subscription
	durable.Usage.APIKeyService = input.APIKeyService
	return durable.Usage, nil
}
func (s *APIKeyService) ReplayGatewayAsyncFundingUsage(ctx context.Context, key *APIKey, task *AsyncFundingTask, record func(context.Context, *RecordUsageInput) error) error {
	if task == nil || len(task.TerminalUsage) == 0 || task.State == "settled" {
		return nil
	}
	var durable struct {
		Usage *RecordUsageInput `json:"gateway_usage"`
	}
	if err := json.Unmarshal(task.TerminalUsage, &durable); err != nil {
		return err
	}
	if durable.Usage == nil {
		return ErrTeamFundingConflict
	}
	restored, sub, err := RestoreAsyncFundingSnapshot(key, task.Snapshot)
	if err != nil {
		return err
	}
	input := durable.Usage
	input.APIKey = restored
	input.User = restored.User
	input.Subscription = sub
	input.APIKeyService = s
	if err = record(ctx, input); err != nil {
		return err
	}
	return s.MarkAsyncFundingSettled(ctx, restored)
}

func (s *APIKeyService) ReplayGatewayFundingResource(ctx context.Context, key *APIKey, provider, prefix string, record func(context.Context, *RecordUsageInput) error) error {
	if s == nil || s.teamFunding == nil {
		return nil
	}
	repo, ok := s.teamFunding.repo.(interface {
		ListAsyncFundingForResource(context.Context, string, string, int64, int64) ([]*AsyncFundingTask, error)
	})
	if !ok {
		return nil
	}
	tasks, err := repo.ListAsyncFundingForResource(ctx, provider, prefix, key.UserID, key.ID)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if err = s.ReplayGatewayAsyncFundingUsage(ctx, key, task, record); err != nil {
			return err
		}
	}
	return nil
}

// Paid mutations on an existing resource create a fresh funding operation; only
// their preceding ownership lookup is a free followup.
func WithoutFundingFollowup(ctx context.Context) context.Context {
	return context.WithValue(ctx, fundingFollowupKey{}, false)
}

func asyncBillingAccountExtra(account *Account) map[string]any {
	if account == nil {
		return nil
	}
	return map[string]any{"quota_limit": account.GetQuotaLimit(), "quota_daily_limit": account.GetQuotaDailyLimit(), "quota_weekly_limit": account.GetQuotaWeeklyLimit(), openAILongContextBillingEnabledKey: account.IsOpenAILongContextBillingEnabled()}
}
