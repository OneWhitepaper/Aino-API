package service

import (
	"math"
	"strings"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func TestPaymentQuoteDecimalBoundary(t *testing.T) {
	for _, raw := range []string{"", "-1", "0", "+20", " 20 ", "NaN", "Infinity", "1e1000000", "0.000000001", "999999999999.99999999", strings.Repeat("9", 1000)} {
		_, err := ParsePaymentAmountDecimal(raw)
		require.Error(t, err, raw)
	}
	for _, test := range []struct {
		raw  string
		want float64
	}{{"20", 20}, {"20.00", 20}, {"12.345", 12.345}, {"0.00000001", 1e-8}} {
		got, err := ParsePaymentAmountDecimal(test.raw)
		require.NoError(t, err)
		require.Equal(t, test.want, got)
	}
}

func TestPaymentQuoteOrderResolverPreservesCurrencyAndSubscriptionPolicy(t *testing.T) {
	for _, test := range []struct {
		currency, orderType, want string
		amount, fee, rate         float64
		plan                      *dbent.SubscriptionPlan
	}{
		{"JPY", "balance", "103", 100, 2.5, 0, nil},
		{"KWD", "balance", "12.469", 12.345, 1, 0, nil},
		{"CNY", "subscription", "73.22", 0, 2.5, 7.15, &dbent.SubscriptionPlan{Price: 9.99}},
		{"USD", "subscription", "10.24", 0, 2.5, 7.15, &dbent.SubscriptionPlan{Price: 9.99}},
		{"CNY", "balance", "50.00", 50, 0, 7.15, nil},
	} {
		got, err := resolvePaymentOrderAmounts(CreateOrderRequest{Amount: test.amount, OrderType: test.orderType}, &PaymentConfig{RechargeFeeRate: test.fee, SubscriptionUSDToCNYRate: test.rate, BalanceRechargeMultiplier: 0.14}, test.plan, test.currency)
		require.NoError(t, err)
		require.Equal(t, test.want, got.payText)
	}
}

func TestPaymentQuoteOrderResolverRejectsInvalidAmountsWithoutPanic(t *testing.T) {
	for _, test := range []struct {
		name                    string
		amount, fee, multiplier float64
	}{
		{"invalid fee", 20, math.NaN(), 1}, {"infinite fee", 20, math.Inf(1), 1},
		{"negative fee", 20, -1, 1}, {"excessive fee", 20, 101, 1},
		{"invalid amount", math.NaN(), 0, 1}, {"infinite amount", math.Inf(1), 0, 1},
		{"credit overflow", 20, 0, 1e300}, {"payment overflow", 1e308, 100, 1e-308},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NotPanics(t, func() {
				_, err := resolvePaymentOrderAmounts(CreateOrderRequest{Amount: test.amount, OrderType: "balance"}, &PaymentConfig{RechargeFeeRate: test.fee, BalanceRechargeMultiplier: test.multiplier}, nil, "USD")
				require.Error(t, err)
			})
		})
	}
}

func TestPaymentQuoteOrderResolverAppliesRechargePromotions(t *testing.T) {
	for _, test := range []struct {
		mode       string
		wantCredit float64
		wantLimit  float64
		wantPay    string
	}{
		{RechargeBonusModeBonus, 16.8, 100, "102.50"},
		{RechargeBonusModeDiscount, 14, 80, "82.00"},
	} {
		t.Run(test.mode, func(t *testing.T) {
			cfg := &PaymentConfig{
				BalanceRechargeMultiplier: 0.14,
				RechargeFeeRate:           2.5,
				RechargeBonusMode:         test.mode,
				RechargeBonusTiers:        []RechargeBonusTier{{MinAmount: 100, BonusPercent: 20}},
			}
			got, err := resolvePaymentOrderAmounts(CreateOrderRequest{Amount: 100, OrderType: "balance"}, cfg, nil, "CNY")
			require.NoError(t, err)
			require.Equal(t, test.wantCredit, got.credit)
			require.Equal(t, test.wantLimit, got.limit)
			require.Equal(t, test.wantPay, got.payText)
		})
	}
}
