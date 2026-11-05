//go:build integration && nativeconsumer

package repository_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDeviceSubscriptionLifecycleWeeklyMonthlyExhaustionHTTP(t *testing.T) {
	for _, window := range []int{1, 2} {
		t.Run(subscriptionWindowName(window), func(t *testing.T) {
			started := time.Now()
			f := newDevicePreflight(t, t.TempDir())
			for i, sub := range []*service.UserSubscription{f.subText, f.subImage} {
				limit := 0.0001
				if i == 1 {
					limit = 0.16
				}
				subscriptionWindowLimit(t, f, sub.GroupID, window, limit)
			}
			f.startServers(t)
			now := subscriptionWindowSeedTime(t)
			for _, sub := range []*service.UserSubscription{f.subText, f.subImage} {
				sub.StartsAt, sub.ExpiresAt = now.Add(-45*24*time.Hour), now.Add(45*24*time.Hour)
				sub.DailyWindowStart, sub.WeeklyWindowStart, sub.MonthlyWindowStart = &now, &now, &now
				sub.DailyUsageUSD, sub.WeeklyUsageUSD, sub.MonthlyUsageUSD = 0, 0, 0
				require.NoError(t, f.r.subs.Update(f.r.ctx, sub))
			}
			identity := f.device(t, f.login(t), service.DesktopDeviceKeyRequest{DeviceID: uuid.NewString()})
			want := [2][3]float64{}
			subscriptionLifecycleAccept(t, f, 0, identity, "fixture-text-subscription", f.subText.GroupID, &f.subText.ID, 0.0001)
			want[0] = [3]float64{0.0001, 0.0001, 0.0001}
			subscriptionWindowAssertExact(t, f, want, 1)
			for instance := range 2 {
				subscriptionWindowReject(t, f, instance, identity.APIKey, "fixture-text-subscription", window)
			}
			for instance := range 2 {
				subscriptionLifecycleAccept(t, f, instance, identity, nativeSubscriptionImageCatalogID, f.subImage.GroupID, &f.subImage.ID, 0.08)
				for w := range 3 {
					want[1][w] += 0.08
				}
				subscriptionWindowAssertExact(t, f, want, instance+2)
			}
			for instance := range 2 {
				subscriptionWindowReject(t, f, instance, identity.APIKey, nativeSubscriptionImageCatalogID, window)
			}
			t.Logf("WINDOW_EXHAUSTION window=%s accepted=3 denied=4 api_processes=2 wallet=37.50000000 text_each_window=0.00010000 image_each_window=0.16000000 wallet_fallback=false cross_package_debit=false elapsed_ms=%d", subscriptionWindowName(window), time.Since(started).Milliseconds())
		})
	}
}

func TestDeviceSubscriptionLifecycleElapsedWindowResetHTTP(t *testing.T) {
	for window := range 3 {
		t.Run(subscriptionWindowName(window), func(t *testing.T) {
			started := time.Now()
			f := newDevicePreflight(t, t.TempDir())
			for i, sub := range []*service.UserSubscription{f.subText, f.subImage} {
				limit := 0.0002
				if i == 1 {
					limit = 0.16
				}
				subscriptionWindowLimit(t, f, sub.GroupID, window, limit)
			}
			f.startServers(t)
			now := subscriptionWindowSeedTime(t)
			period := []time.Duration{24 * time.Hour, 7 * 24 * time.Hour, 30 * 24 * time.Hour}[window]
			old := now.Add(-period - 24*time.Hour)
			expected := old.Add(period)
			if window == 0 {
				expected = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
			}
			want := [2][3]float64{{1, 2, 3}, {4, 5, 6}}
			for i, sub := range []*service.UserSubscription{f.subText, f.subImage} {
				limit := 0.0002
				if i == 1 {
					limit = 0.16
				}
				want[i][window] = limit
				sub.StartsAt, sub.ExpiresAt = now.Add(-45*24*time.Hour), now.Add(45*24*time.Hour)
				sub.DailyWindowStart, sub.WeeklyWindowStart, sub.MonthlyWindowStart = &now, &now, &now
				windows := []*(*time.Time){&sub.DailyWindowStart, &sub.WeeklyWindowStart, &sub.MonthlyWindowStart}
				*windows[window] = &old
				sub.DailyUsageUSD, sub.WeeklyUsageUSD, sub.MonthlyUsageUSD = want[i][0], want[i][1], want[i][2]
				require.NoError(t, f.r.subs.Update(f.r.ctx, sub))
			}
			identity := f.device(t, f.login(t), service.DesktopDeviceKeyRequest{DeviceID: uuid.NewString()})
			subscriptionWindowAssertExact(t, f, want, 0)
			rows := 0
			for i, sub := range []*service.UserSubscription{f.subText, f.subImage} {
				option, cost := "fixture-text-subscription", 0.0001
				if i == 1 {
					option, cost = nativeSubscriptionImageCatalogID, 0.08
				}
				for instance := range 2 {
					subscriptionLifecycleAccept(t, f, instance, identity, option, sub.GroupID, &sub.ID, cost)
					if instance == 0 {
						want[i][window] = 0
					}
					for w := range 3 {
						want[i][w] += cost
					}
					rows++
					subscriptionWindowAssertExact(t, f, want, rows)
					subscriptionWindowAssertAnchors(t, f, sub.ID, window, now, expected)
				}
				for instance := range 2 {
					subscriptionWindowReject(t, f, instance, identity.APIKey, option, window)
				}
			}
			t.Logf("WINDOW_RESET window=%s accepted=4 denied=4 api_processes=2 synthetic_old=%s expected_new=%s only_elapsed_counter_reset=true other_counters_retained=true wallet=37.50000000 wallet_fallback=false cross_package_debit=false real_elapsed_cycle_observation=false elapsed_ms=%d", subscriptionWindowName(window), old.Format(time.RFC3339), expected.Format(time.RFC3339), time.Since(started).Milliseconds())
		})
	}
}

func subscriptionWindowName(window int) string { return []string{"daily", "weekly", "monthly"}[window] }

func subscriptionWindowSeedTime(t *testing.T) time.Time {
	t.Helper()
	deadline, bounded := t.Deadline()
	require.True(t, bounded, "window validation requires the existing go test timeout")
	for {
		now := time.Now().UTC()
		midnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
		if deadline.Before(midnight) {
			return now.Truncate(time.Second)
		}
		// Only seed when a successful test cannot cross another daily reset.
		// Near midnight the existing test deadline still bounds the wait.
		wait := min(time.Until(midnight)+time.Millisecond, 30*time.Second)
		t.Logf("WINDOW_SEED waiting_ms=%d utc_midnight=%s test_deadline=%s", wait.Milliseconds(), midnight.Format(time.RFC3339), deadline.UTC().Format(time.RFC3339))
		time.Sleep(wait)
	}
}

func subscriptionWindowLimit(t *testing.T, f *devicePreflight, groupID int64, window int, limit float64) {
	t.Helper()
	group, err := f.r.groups.GetByID(f.r.ctx, groupID)
	require.NoError(t, err)
	group.DailyLimitUSD, group.WeeklyLimitUSD, group.MonthlyLimitUSD = nil, nil, nil
	limits := []*(*float64){&group.DailyLimitUSD, &group.WeeklyLimitUSD, &group.MonthlyLimitUSD}
	*limits[window] = &limit
	group.FallbackGroupID = &f.textGroup
	require.NoError(t, f.r.groups.Update(f.r.ctx, group))
}

func subscriptionWindowReject(t *testing.T, f *devicePreflight, instance int, key, option string, window int) {
	t.Helper()
	before := subscriptionLifecycleSnapshot(t, f)
	_, status, raw := subscriptionLifecycleCall(t, f, instance, key, option)
	require.Equal(t, http.StatusForbidden, status, string(raw))
	require.Contains(t, string(raw), subscriptionWindowName(window)+" usage limit exceeded")
	require.Equal(t, before, subscriptionLifecycleSnapshot(t, f), "denial must preserve both packages, funded wallet, ledger and upstream counts")
	t.Logf("WINDOW_DENIED window=%s route=%s instance=%d status=%d ledger_and_upstream_unchanged=true", subscriptionWindowName(window), option, instance+1, status)
}

func subscriptionWindowAssertExact(t *testing.T, f *devicePreflight, want [2][3]float64, rows int) {
	t.Helper()
	db := repository.GetIntegrationDB()
	var balance string
	require.NoError(t, db.QueryRowContext(f.r.ctx, "SELECT balance::numeric(20,8)::text FROM users WHERE id=$1", f.userID).Scan(&balance))
	require.Equal(t, "37.50000000", balance)
	for i, sub := range []*service.UserSubscription{f.subText, f.subImage} {
		var got [3]string
		require.NoError(t, db.QueryRowContext(f.r.ctx, "SELECT daily_usage_usd::numeric(20,8)::text,weekly_usage_usd::numeric(20,8)::text,monthly_usage_usd::numeric(20,8)::text FROM user_subscriptions WHERE id=$1", sub.ID).Scan(&got[0], &got[1], &got[2]))
		for w := range 3 {
			require.Equal(t, fmt.Sprintf("%.8f", want[i][w]), got[w], "package=%d window=%s", i, subscriptionWindowName(w))
		}
	}
	state := subscriptionLifecycleSnapshot(t, f)
	require.Equal(t, rows, state.LedgerRows)
	require.Equal(t, rows, state.UpstreamCalls)
	var wrongLedger int
	require.NoError(t, db.QueryRowContext(f.r.ctx, `SELECT count(*) FROM usage_logs WHERE user_id=$1 AND (
		settlement_status IS DISTINCT FROM 'settled' OR
		CASE WHEN subscription_id=$2 THEN actual_cost <> 0.00010000 OR group_id IS DISTINCT FROM $3
		WHEN subscription_id=$4 THEN actual_cost <> 0.08000000 OR group_id IS DISTINCT FROM $5
		ELSE true END)`, f.userID, f.subText.ID, f.subText.GroupID, f.subImage.ID, f.subImage.GroupID).Scan(&wrongLedger))
	require.Zero(t, wrongLedger, "every row must contain the exact selected package, group and numeric charge")
}

func subscriptionWindowAssertAnchors(t *testing.T, f *devicePreflight, subID int64, window int, unchanged, advanced time.Time) {
	t.Helper()
	var got [3]time.Time
	require.NoError(t, repository.GetIntegrationDB().QueryRowContext(f.r.ctx, "SELECT daily_window_start,weekly_window_start,monthly_window_start FROM user_subscriptions WHERE id=$1", subID).Scan(&got[0], &got[1], &got[2]))
	for w := range 3 {
		want := unchanged
		if w == window {
			want = advanced
		}
		require.True(t, got[w].Equal(want), "window=%s got=%s want=%s", subscriptionWindowName(w), got[w], want)
	}
}
