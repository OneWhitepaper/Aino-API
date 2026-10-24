package service

import (
	"context"
	"slices"
	"strings"

	"github.com/google/uuid"
)

// DesktopUsage belongs to one HTTP dispatch, never to the cached API key.
type DesktopUsage struct {
	SessionID string
	TurnID    string
	CallID    string
	Purpose   string
}

func ValidDesktopUsageID(value string) bool {
	if len(value) != 36 {
		return false
	}
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && strings.EqualFold(id.String(), value)
}

var desktopUsagePurposes = []string{"chat", "title", "compression", "vision", "delegation", "other_auxiliary"}
var desktopUsageTasks = []string{"session_summary", "approval", "mcp", "tts_audio_tags", "side_question"}

func ValidDesktopUsagePurpose(value string) bool {
	return slices.Contains(desktopUsagePurposes, value) || ValidDesktopUsageTask(value)
}

func ValidDesktopUsageTask(value string) bool {
	return slices.Contains(desktopUsageTasks, value)
}

// Older servers reject unknown purpose filters, so clients discover refinements from the list response.
func SupportedDesktopUsagePurposes() []string {
	return append(slices.Clone(desktopUsagePurposes), desktopUsageTasks...)
}

type desktopUsageContextKey struct{}

func WithDesktopUsageContext(ctx context.Context, usage *DesktopUsage) context.Context {
	return context.WithValue(ctx, desktopUsageContextKey{}, usage)
}

func attachDesktopUsage(log *UsageLog, key *APIKey) {
	if key == nil || !key.DesktopManaged || key.DesktopUsage == nil {
		return
	}
	u := key.DesktopUsage
	log.SessionID, log.DesktopTurnID, log.DesktopCallID, log.DesktopPurpose = &u.SessionID, &u.TurnID, &u.CallID, &u.Purpose
	log.SettlementStatus = "unknown"
}

// Only the transactional billing path can attest that a charge was settled.
func settleDesktopUsage(log *UsageLog, cmd *UsageBillingCommand) {
	if log == nil || log.DesktopCallID == nil {
		return
	}
	log.ActualCost = QuantizeUsageBillingAmount(cmd.BalanceCost + cmd.SubscriptionCost)
	log.SettlementStatus = "settled"
	if log.ActualCost == 0 {
		log.SettlementStatus = "not_charged"
	}
}
