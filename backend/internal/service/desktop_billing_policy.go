package service

import (
	"context"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const BillingPolicyBalanceOnly = "balance_only"

var ErrDesktopBillingPolicyConflict = infraerrors.Conflict("DESKTOP_BILLING_POLICY_CONFLICT", "existing credential has a different billing policy; authorize a new installation or connection grant")

type desktopBillingPolicyKey struct{}

func ValidateDesktopBillingPolicy(policy string) error {
	if policy != "" && policy != BillingPolicyBalanceOnly {
		return infraerrors.BadRequest("INVALID_BILLING_POLICY", "unsupported managed credential billing policy")
	}
	return nil
}

func WithDesktopBillingPolicy(ctx context.Context, policy string) context.Context {
	return context.WithValue(ctx, desktopBillingPolicyKey{}, policy)
}

func desktopBillingPolicy(ctx context.Context) string {
	policy, _ := ctx.Value(desktopBillingPolicyKey{}).(string)
	return policy
}

func validateBalanceOnlyFunding(key *APIKey, decision *FundingDecision) error {
	if err := validateBalanceOnlySource(key, decision); err != nil {
		return err
	}
	if key == nil || key.BillingPolicy == "" {
		return nil
	}
	if key.User == nil || key.Group == nil || key.Group.IsSubscriptionType() || !key.User.CanBindGroup(key.Group.ID, key.Group.IsExclusive) {
		return ErrFundingUnavailable
	}
	return nil
}

func validateBalanceOnlySource(key *APIKey, decision *FundingDecision) error {
	if key == nil || key.BillingPolicy == "" {
		return nil
	}
	if key.BillingPolicy != BillingPolicyBalanceOnly || !key.DesktopManaged {
		return ErrFundingUnavailable
	}
	if decision != nil && (decision.Source != FundingSourcePersonalBalance || decision.PayerUserID != key.UserID || decision.TeamID != 0 || decision.SubscriptionID != nil || decision.Subscription != nil) {
		return ErrFundingUnavailable
	}
	return nil
}
