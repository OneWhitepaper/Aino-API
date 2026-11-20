//go:build unit

package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type desktopUsageDeduplicatingBilling struct {
	UsageBillingRepository
	calls atomic.Int32
}

func (r *desktopUsageDeduplicatingBilling) Apply(context.Context, *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	return &UsageBillingApplyResult{Applied: r.calls.Add(1) == 1}, nil
}

type desktopUsageOrderedInsert struct {
	UsageLogRepository
	mu                           sync.Mutex
	stored                       *UsageLog
	insertAttempts               int
	settledReady, releaseSettled chan struct{}
}

func (r *desktopUsageOrderedInsert) Create(_ context.Context, log *UsageLog) (bool, error) {
	if log.SettlementStatus == "settled" {
		close(r.settledReady)
		<-r.releaseSettled
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.insertAttempts++
	if r.stored != nil {
		return false, nil
	}
	copy := *log
	r.stored = &copy
	return true, nil
}

func TestDesktopUsageDuplicateCannotRaceCommittedReceipt(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			logs := &desktopUsageOrderedInsert{settledReady: make(chan struct{}), releaseSettled: make(chan struct{})}
			billing := &desktopUsageDeduplicatingBilling{}
			var once sync.Once
			release := func() { once.Do(func() { close(logs.releaseSettled) }) }
			defer release()
			var record func() error
			if protocol == "openai" {
				svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, billing, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
				record = func() error {
					return svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{APIKey: &APIKey{ID: 1, DesktopManaged: true, BillingPolicy: BillingPolicyBalanceOnly}, User: &User{ID: 2}, Account: &Account{ID: 3}, Result: &OpenAIForwardResult{RequestID: "same-provider-operation", Model: "gpt-5.1", Usage: OpenAIUsage{InputTokens: 10, OutputTokens: 5}}})
				}
			} else {
				svc := newGatewayRecordUsageServiceWithBillingRepoForTest(logs, billing, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
				record = func() error {
					return svc.RecordUsage(context.Background(), &RecordUsageInput{APIKey: &APIKey{ID: 1, DesktopManaged: true, BillingPolicy: BillingPolicyBalanceOnly}, User: &User{ID: 2}, Account: &Account{ID: 3}, Result: &ForwardResult{RequestID: "same-provider-operation", Model: "claude-sonnet-4", Usage: ClaudeUsage{InputTokens: 10, OutputTokens: 5}}})
				}
			}
			originalDone := make(chan error, 1)
			go func() { originalDone <- record() }()
			select {
			case <-logs.settledReady:
			case <-time.After(5 * time.Second):
				t.Fatal("committed receipt did not reach the usage repository")
			}
			require.NoError(t, record())
			release()
			select {
			case err := <-originalDone:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("committed receipt did not finish")
			}
			require.EqualValues(t, 2, billing.calls.Load())
			logs.mu.Lock()
			defer logs.mu.Unlock()
			require.NotNil(t, logs.stored)
			require.Equal(t, "settled", logs.stored.SettlementStatus)
			require.Positive(t, logs.stored.ActualCost)
			require.Equal(t, 1, logs.insertAttempts)
		})
	}
}
