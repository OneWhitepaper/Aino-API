//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDesktopUsageRecordedFromBothBillingPaths(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		for _, failed := range []bool{false, true} {
			t.Run(protocol+map[bool]string{false: "/settled", true: "/failed"}[failed], func(t *testing.T) {
				usageRepo := &openAIRecordUsageLogRepoStub{}
				billingRepo := &openAIRecordUsageBillingRepoStub{}
				if failed {
					billingRepo.err = errors.New("transaction unavailable")
				}
				correlation := &DesktopUsage{SessionID: uuid.NewString(), TurnID: uuid.NewString(), CallID: uuid.NewString(), Purpose: "compression"}
				key := &APIKey{ID: 1, DesktopManaged: true, DesktopUsage: correlation}
				user, account := &User{ID: 2}, &Account{ID: 3}
				var err error
				if protocol == "openai" {
					svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
					err = svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{APIKey: key, User: user, Account: account, Result: &OpenAIForwardResult{RequestID: uuid.NewString(), Model: "gpt-5.1", Usage: OpenAIUsage{InputTokens: 10, OutputTokens: 5}}})
				} else {
					svc := newGatewayRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
					err = svc.RecordUsage(context.Background(), &RecordUsageInput{APIKey: key, User: user, Account: account, Result: &ForwardResult{RequestID: uuid.NewString(), Model: "claude-sonnet-4", Usage: ClaudeUsage{InputTokens: 10, OutputTokens: 5}}})
				}
				log := usageRepo.lastLog
				require.NotNil(t, log)
				require.Equal(t, &correlation.SessionID, log.SessionID)
				require.Equal(t, &correlation.TurnID, log.DesktopTurnID)
				require.Equal(t, &correlation.CallID, log.DesktopCallID)
				require.Equal(t, &correlation.Purpose, log.DesktopPurpose)
				if failed {
					require.Error(t, err)
					require.Equal(t, "unknown", log.SettlementStatus)
					require.Nil(t, log.ActualCostDecimal, "no proven charge must not become a free call")
				} else {
					require.NoError(t, err)
					require.Equal(t, "settled", log.SettlementStatus)
					require.Positive(t, log.ActualCost)
					require.Equal(t, billingRepo.lastCmd.BalanceCost, log.ActualCost, "display must match the quantized ledger command")
				}
			})
		}
	}
}

func TestDesktopUsageWithoutAttributionUsesCommittedBilling(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		for _, failed := range []bool{false, true} {
			t.Run(protocol+map[bool]string{false: "/settled", true: "/failed"}[failed], func(t *testing.T) {
				usageRepo := &openAIRecordUsageLogRepoStub{}
				billingRepo := &openAIRecordUsageBillingRepoStub{}
				if failed {
					billingRepo.err = errors.New("transaction unavailable")
				}
				key := &APIKey{ID: 1, DesktopManaged: true, BillingPolicy: BillingPolicyBalanceOnly}
				user, account := &User{ID: 2}, &Account{ID: 3}
				var err error
				if protocol == "openai" {
					svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
					err = svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{APIKey: key, User: user, Account: account, Result: &OpenAIForwardResult{RequestID: uuid.NewString(), Model: "gpt-5.1", Usage: OpenAIUsage{InputTokens: 10, OutputTokens: 5}}})
				} else {
					svc := newGatewayRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
					err = svc.RecordUsage(context.Background(), &RecordUsageInput{APIKey: key, User: user, Account: account, Result: &ForwardResult{RequestID: uuid.NewString(), Model: "claude-sonnet-4", Usage: ClaudeUsage{InputTokens: 10, OutputTokens: 5}}})
				}
				log := usageRepo.lastLog
				require.NotNil(t, log)
				require.Nil(t, log.SessionID)
				require.Nil(t, log.DesktopTurnID)
				require.Nil(t, log.DesktopCallID)
				require.Nil(t, log.DesktopPurpose)
				if failed {
					require.Error(t, err)
					require.Equal(t, "unknown", log.SettlementStatus)
					require.Nil(t, log.ActualCostDecimal)
				} else {
					require.NoError(t, err)
					require.Equal(t, "settled", log.SettlementStatus)
					require.Equal(t, billingRepo.lastCmd.BalanceCost, log.ActualCost)
				}
			})
		}
	}
}

func TestDesktopUsageSettlementRequiresCommittedChargeReceipt(t *testing.T) {
	for _, tc := range []struct {
		name       string
		managed    bool
		command    *UsageBillingCommand
		result     *UsageBillingApplyResult
		wantStatus string
		wantCost   float64
	}{
		{name: "escrow shortfall", managed: true, command: &UsageBillingCommand{FundingCost: 3}, result: &UsageBillingApplyResult{Applied: true, FundingChargedAmount: "2.00000000", FundingUnauthorizedAmount: "1.00000000"}, wantStatus: "settled", wantCost: 2},
		{name: "deduplicated without receipt", managed: true, command: &UsageBillingCommand{FundingCost: 3}, result: &UsageBillingApplyResult{Applied: false}, wantStatus: "unknown"},
		{name: "failed without receipt", managed: true, command: &UsageBillingCommand{FundingCost: 3}, wantStatus: "unknown"},
		{name: "invalid charged amount", managed: true, command: &UsageBillingCommand{FundingCost: 3}, result: &UsageBillingApplyResult{Applied: true, FundingChargedAmount: "invalid"}, wantStatus: "unknown"},
		{name: "authorized free mode", managed: true, command: &UsageBillingCommand{}, result: &UsageBillingApplyResult{Applied: true}, wantStatus: "not_charged"},
		{name: "ordinary key", command: &UsageBillingCommand{BalanceCost: 3}, result: &UsageBillingApplyResult{Applied: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log := &UsageLog{}
			attachDesktopUsage(log, &APIKey{DesktopManaged: tc.managed})
			settleDesktopUsage(log, tc.command, tc.result)
			require.Equal(t, tc.wantStatus, log.SettlementStatus)
			require.Equal(t, tc.wantCost, log.ActualCost)
			require.Nil(t, log.ActualCostDecimal)
		})
	}
}
