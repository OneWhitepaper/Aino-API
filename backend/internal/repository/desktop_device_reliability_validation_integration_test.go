//go:build integration && nativeconsumer

package repository_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// This bounded entry point reuses the real two-process preflight without its
// native-consumer handoff. Each invocation gets TestMain's disposable stores.
func TestDeviceReliabilityValidationAPI(t *testing.T) {
	started := time.Now()
	f := newDevicePreflight(t, t.TempDir())
	f.startServers(t)
	dsn, err := url.Parse(repository.GetIntegrationDSN())
	require.NoError(t, err)
	t.Logf("ISOLATION postgres=%s redis=%s api_processes=%d pid1=%d pid2=%d", dsn.Host, repository.GetIntegrationRedis().Options().Addr, len(f.commands), f.commands[0].Process.Pid, f.commands[1].Process.Pid)
	t.Run("two-real-process-routing-load-revocation-hard-age", f.verify)
	t.Run("all-four-routes-concurrent-on-each-process", f.verifyReliabilityRouteMatrix)
	t.Run("same-logical-call-http-retry-charges-each-dispatch", f.verifyReliabilityHTTPLogicalReplay)
	for _, billing := range []string{"wallet", "subscription"} {
		t.Run("direct-ledger-concurrent-"+billing+"-and-archived-replay", func(t *testing.T) {
			f.verifyReliabilityLedgerReplay(t, billing)
		})
	}
	t.Logf("BOUNDED_COMPLETE elapsed_ms=%d native_consumer_handoff=false", time.Since(started).Milliseconds())
}

func (f *devicePreflight) verifyReliabilityHTTPLogicalReplay(t *testing.T) {
	u := f.r.user(t, service.StatusActive, false)
	require.NoError(t, f.r.userRepo.AddGroupToAllowedGroups(f.r.ctx, u.ID, f.textGroup))
	p := &devicePreflight{r: f.r, origins: f.origins, email: u.Email, password: f.password, userID: u.ID}
	now := time.Now()
	for i, group := range []int64{f.subText.GroupID, f.subImage.GroupID} {
		sub := &service.UserSubscription{UserID: u.ID, GroupID: group, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Status: service.SubscriptionStatusActive, DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now}
		require.NoError(t, f.r.subs.Create(f.r.ctx, sub))
		if i == 0 {
			p.subText = sub
		} else {
			p.subImage = sub
		}
	}
	key := p.device(t, p.login(t), service.DesktopDeviceKeyRequest{DeviceID: uuid.NewString()})
	db := repository.GetIntegrationDB()
	for _, option := range []string{"fixture-tool-model", nativeImageCatalogID, "fixture-text-subscription", "fixture-image-subscription"} {
		t.Run(option, func(t *testing.T) {
			started := time.Now()
			path, purpose, wantCharge := "/v1/chat/completions", "chat", "0.00020000"
			body := map[string]any{"model": "fixture-tool-model", "messages": []any{map[string]string{"role": "user", "content": "preflight-duplicate same-logical-call"}}, "stream": false}
			upstreamCount := func() int64 { return f.r.modelCalls.Load() }
			if strings.Contains(option, "image") {
				path, purpose, wantCharge = "/v1/images/generations", "image", "0.16000000"
				body = map[string]any{"model": nativeImageUpstreamModel, "prompt": "fixture-image-generate same-logical-call", "quality": "auto", "size": "1024x1024", "n": 1}
				upstreamCount = func() int64 { return f.protocol.images.generationRequests.Load() }
			}
			raw, err := json.Marshal(body)
			require.NoError(t, err)
			session, turn, call, correlation := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
			headers := map[string]string{"X-Aino-Model-ID": option, "X-Aino-Session-Id": session, "X-Aino-Turn-Id": turn, "X-Aino-Call-Id": call, "X-Aino-Purpose": purpose, "X-Request-ID": correlation, "X-Client-Request-ID": correlation}
			before := upstreamCount()
			responseIDs := make([]string, 0, 2)
			for instance := range 2 {
				responseIDs = append(responseIDs, reliabilityLogicalReplayDispatch(t, p.origins[instance]+path, key.APIKey, raw, headers, correlation))
			}
			require.NotEqual(t, responseIDs[0], responseIDs[1])
			require.EqualValues(t, 2, upstreamCount()-before)
			var count, distinctIDs, attributed, claimed int
			var charge string
			require.Eventually(t, func() bool {
				err := db.QueryRowContext(f.r.ctx, "SELECT count(*),count(distinct request_id),coalesce(sum(actual_cost),0)::numeric(20,8)::text FROM usage_logs WHERE user_id=$1 AND desktop_call_id=$2 AND settlement_status='settled'", u.ID, call).Scan(&count, &distinctIDs, &charge)
				return err == nil && count == 2
			}, 15*time.Second, 50*time.Millisecond)
			require.Equal(t, 2, distinctIDs)
			require.Equal(t, wantCharge, charge)
			require.NoError(t, db.QueryRowContext(f.r.ctx, "SELECT count(*) FROM usage_logs WHERE user_id=$1 AND session_id=$2 AND desktop_turn_id=$3 AND desktop_call_id=$4 AND request_id IN ($5,$6) AND api_key_id=$7", u.ID, session, turn, call, "client:"+responseIDs[0], "client:"+responseIDs[1], key.KeyID).Scan(&attributed))
			require.Equal(t, 2, attributed)
			require.NoError(t, db.QueryRowContext(f.r.ctx, "SELECT count(*) FROM usage_billing_dedup WHERE api_key_id=$1 AND request_id IN ($2,$3)", key.KeyID, "client:"+responseIDs[0], "client:"+responseIDs[1]).Scan(&claimed))
			require.Equal(t, 2, claimed)
			t.Logf("LOGICAL_HTTP_RETRY route=%s http_successes=2 same_payload_bytes=true same_session_turn_call=true same_inbound_request_and_client_ids=true api_instances=2 upstream_executions=2 distinct_server_client_ids=2 settled_rows=2 dedup_rows=2 charge=%s elapsed_ms=%d", option, charge, time.Since(started).Milliseconds())
		})
	}
	var balance, text, image string
	require.NoError(t, db.QueryRowContext(f.r.ctx, "SELECT balance::numeric(20,8)::text FROM users WHERE id=$1", u.ID).Scan(&balance))
	require.NoError(t, db.QueryRowContext(f.r.ctx, "SELECT daily_usage_usd::numeric(20,8)::text FROM user_subscriptions WHERE id=$1", p.subText.ID).Scan(&text))
	require.NoError(t, db.QueryRowContext(f.r.ctx, "SELECT daily_usage_usd::numeric(20,8)::text FROM user_subscriptions WHERE id=$1", p.subImage.ID).Scan(&image))
	require.Equal(t, "37.33980000", balance)
	require.Equal(t, "0.00020000", text)
	require.Equal(t, "0.16000000", image)
	t.Logf("LOGICAL_HTTP_RETRY_TOTAL requests=8 wallet=%s text_subscription=%s image_subscription=%s", balance, text, image)
}

func reliabilityLogicalReplayDispatch(t *testing.T, target, key string, raw []byte, headers map[string]string, requestID string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", target, bytes.NewReader(raw))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	response, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	_, err = io.Copy(io.Discard, response.Body)
	require.NoError(t, err)
	require.Equal(t, 200, response.StatusCode)
	require.Equal(t, requestID, response.Header.Get("X-Request-ID"))
	clientID := response.Header.Get("X-Client-Request-ID")
	require.NotEmpty(t, clientID)
	require.NotEqual(t, requestID, clientID, "inbound correlation is not an HTTP settlement idempotency key")
	return clientID
}

func (f *devicePreflight) verifyReliabilityRouteMatrix(t *testing.T) {
	u := f.r.user(t, service.StatusActive, false)
	u.Concurrency = 64
	require.NoError(t, f.r.userRepo.Update(f.r.ctx, u, service.UserUpdateFields{Concurrency: true}))
	require.NoError(t, f.r.userRepo.AddGroupToAllowedGroups(f.r.ctx, u.ID, f.textGroup))
	p := &devicePreflight{r: f.r, dir: f.dir, origins: f.origins, email: u.Email, password: f.password, userID: u.ID, textGroup: f.textGroup, imageGroup: f.imageGroup}
	now := time.Now()
	for i, group := range []int64{f.subText.GroupID, f.subImage.GroupID} {
		sub := &service.UserSubscription{UserID: u.ID, GroupID: group, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Status: service.SubscriptionStatusActive, DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now}
		require.NoError(t, f.r.subs.Create(f.r.ctx, sub))
		if i == 0 {
			p.subText = sub
		} else {
			p.subImage = sub
		}
	}
	key := p.device(t, p.login(t), service.DesktopDeviceKeyRequest{DeviceID: uuid.NewString()})
	type outcome struct {
		instance int
		option   string
		status   int
		latency  time.Duration
		err      error
	}
	const count = 16
	ready := make(chan struct{})
	results := make(chan outcome, count)
	var wg sync.WaitGroup
	started := time.Now()
	for instance := range 2 {
		for _, option := range []string{"fixture-tool-model", nativeImageCatalogID, "fixture-text-subscription", "fixture-image-subscription"} {
			for repeat := range 2 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-ready
					path := "/v1/chat/completions"
					purpose := "chat"
					body := map[string]any{"model": "fixture-tool-model", "messages": []any{map[string]string{"role": "user", "content": "route-matrix"}}, "stream": false}
					if strings.Contains(option, "image") {
						path = "/v1/images/generations"
						purpose = "image"
						body = map[string]any{"model": nativeImageUpstreamModel, "prompt": fmt.Sprintf("fixture-image-generate route-matrix-%d-%d-%s", instance, repeat, option), "quality": "auto", "size": "1024x1024", "n": 1}
					}
					ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
					defer cancel()
					status, _, latency, err := preflightHTTP(ctx, p.origins[instance], "POST", path, key.APIKey, body, map[string]string{"X-Aino-Model-ID": option, "X-Aino-Session-Id": uuid.NewString(), "X-Aino-Turn-Id": uuid.NewString(), "X-Aino-Call-Id": uuid.NewString(), "X-Aino-Purpose": purpose})
					results <- outcome{instance: instance, option: option, status: status, latency: latency, err: err}
				}()
			}
		}
	}
	close(ready)
	wg.Wait()
	close(results)
	counts := make(map[string]int)
	latencies := make([]time.Duration, 0, count)
	for result := range results {
		require.NoError(t, result.err)
		require.Equal(t, 200, result.status, "instance=%d option=%s", result.instance+1, result.option)
		counts[fmt.Sprintf("%d/%s", result.instance+1, result.option)]++
		latencies = append(latencies, result.latency)
	}
	require.Len(t, counts, 8)
	for route, n := range counts {
		require.Equal(t, 2, n, route)
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	db := repository.GetIntegrationDB()
	t.Cleanup(func() {
		if t.Failed() {
			var diagnostics string
			err := db.QueryRowContext(f.r.ctx, "SELECT coalesce(string_agg(status||':'||count,','),'no rows') FROM (SELECT coalesce(settlement_status,'NULL') AS status,count(*)::text AS count FROM usage_logs WHERE user_id=$1 GROUP BY settlement_status) states", u.ID).Scan(&diagnostics)
			t.Logf("ROUTE_MATRIX failure_settlement_counts=%s diagnostic_error=%v", diagnostics, err)
		}
	})
	var rows int
	require.Eventually(t, func() bool {
		err := db.QueryRowContext(f.r.ctx, "SELECT count(*) FROM usage_logs WHERE user_id=$1 AND settlement_status='settled'", u.ID).Scan(&rows)
		return err == nil && rows == count
	}, 15*time.Second, 50*time.Millisecond)
	var balance, text, image string
	require.NoError(t, db.QueryRowContext(f.r.ctx, "SELECT balance::numeric(20,8)::text FROM users WHERE id=$1", u.ID).Scan(&balance))
	require.NoError(t, db.QueryRowContext(f.r.ctx, "SELECT daily_usage_usd::numeric(20,8)::text FROM user_subscriptions WHERE id=$1", p.subText.ID).Scan(&text))
	require.NoError(t, db.QueryRowContext(f.r.ctx, "SELECT daily_usage_usd::numeric(20,8)::text FROM user_subscriptions WHERE id=$1", p.subImage.ID).Scan(&image))
	require.Equal(t, "37.17960000", balance)
	require.Equal(t, "0.00040000", text)
	require.Equal(t, "0.32000000", image)
	for _, route := range []struct {
		group, sub int64
		count      int
	}{{p.textGroup, 0, 8}, {p.subText.GroupID, p.subText.ID, 4}, {p.subImage.GroupID, p.subImage.ID, 4}} {
		var got int
		require.NoError(t, db.QueryRowContext(f.r.ctx, "SELECT count(*) FROM usage_logs WHERE user_id=$1 AND group_id=$2 AND coalesce(subscription_id,0)=$3 AND api_key_id=$4", u.ID, route.group, route.sub, key.KeyID).Scan(&got))
		require.Equal(t, route.count, got)
	}
	t.Logf("ROUTE_MATRIX requests=%d successes=%d errors=0 instances=2 routes_per_instance=4 requests_per_route_per_instance=2 settled_rows=%d elapsed_ms=%d p50_ms=%.3f p95_ms=%.3f max_ms=%.3f balance=%s text_subscription=%s image_subscription=%s", count, len(latencies), rows, time.Since(started).Milliseconds(), float64(latencies[len(latencies)/2].Microseconds())/1000, float64(latencies[len(latencies)*95/100].Microseconds())/1000, float64(latencies[len(latencies)-1].Microseconds())/1000, balance, text, image)
}

func (f *devicePreflight) verifyReliabilityLedgerReplay(t *testing.T, billing string) {
	t.Helper()
	u := f.r.user(t, service.StatusActive, false)
	identity, err := service.NewDesktopDeviceKeyService(repository.NewDesktopDeviceKeyRepository(f.r.client), f.r.keys).Ensure(f.r.ctx, u.ID, service.DesktopDeviceKeyRequest{DeviceID: uuid.NewString()})
	require.NoError(t, err)
	keyID, err := strconv.ParseInt(identity.KeyID, 10, 64)
	require.NoError(t, err)
	now := time.Now()
	sub := &service.UserSubscription{UserID: u.ID, GroupID: f.subText.GroupID, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Status: service.SubscriptionStatusActive, DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now}
	require.NoError(t, f.r.subs.Create(f.r.ctx, sub))
	cmd := service.UsageBillingCommand{RequestID: uuid.NewString(), APIKeyID: keyID, UserID: u.ID, Model: "fixture-tool-model", BalanceCost: 1.25, APIKeyQuotaCost: 1.25, APIKeyRateLimitCost: 1.25}
	wantBalance, wantUsage, wantCounter := "36.25000000", "0.00000000", "1.25000000"
	if billing == "subscription" {
		cmd.BalanceCost, cmd.SubscriptionCost, cmd.SubscriptionID = 0, 2.5, &sub.ID
		cmd.APIKeyQuotaCost, cmd.APIKeyRateLimitCost = 2.5, 2.5
		wantBalance, wantUsage, wantCounter = "37.50000000", "2.50000000", "2.50000000"
	}
	repo := repository.NewUsageBillingRepository(f.r.client, repository.GetIntegrationDB())
	const callers = 24
	started := time.Now()
	require.Equal(t, 1, reliabilityConcurrentApply(t, repo, cmd, callers))
	t.Logf("DIRECT_LEDGER billing=%s callers=%d applied=1 errors=0 elapsed_ms=%d", billing, callers, time.Since(started).Milliseconds())
	f.requireReliabilityLedgerState(t, cmd, sub.ID, wantBalance, wantUsage, wantCounter, 1, 0)

	db := repository.GetIntegrationDB()
	_, err = db.ExecContext(f.r.ctx, "UPDATE usage_billing_dedup SET created_at=$1 WHERE request_id=$2 AND api_key_id=$3", now.AddDate(0, 0, -400), cmd.RequestID, keyID)
	require.NoError(t, err)
	require.NoError(t, repository.NewDashboardAggregationRepository(db).CleanupUsageBillingDedup(f.r.ctx, now.AddDate(0, 0, -365)))
	started = time.Now()
	require.Zero(t, reliabilityConcurrentApply(t, repo, cmd, callers))
	f.requireReliabilityLedgerState(t, cmd, sub.ID, wantBalance, wantUsage, wantCounter, 0, 1)
	conflict := cmd
	conflict.Model = "conflicting-replay"
	_, err = repo.Apply(f.r.ctx, &conflict)
	require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
	f.requireReliabilityLedgerState(t, cmd, sub.ID, wantBalance, wantUsage, wantCounter, 0, 1)
	t.Logf("ARCHIVED_REPLAY billing=%s callers=%d applied=0 errors=0 fingerprint_conflicts=1 elapsed_ms=%d balance=%s subscription_each_window=%s key_quota_and_each_rate_window=%s live_dedup=0 archived_dedup=1 usage_logs=0", billing, callers, time.Since(started).Milliseconds(), wantBalance, wantUsage, wantCounter)
}

func reliabilityConcurrentApply(t *testing.T, repo service.UsageBillingRepository, cmd service.UsageBillingCommand, callers int) int {
	t.Helper()
	type outcome struct {
		result *service.UsageBillingApplyResult
		err    error
	}
	ready := make(chan struct{})
	results := make(chan outcome, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-ready
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			copy := cmd // Normalize mutates a command; callers own separate copies.
			result, err := repo.Apply(ctx, &copy)
			results <- outcome{result: result, err: err}
		}()
	}
	close(ready)
	wg.Wait()
	close(results)
	applied := 0
	for result := range results {
		require.NoError(t, result.err)
		require.NotNil(t, result.result)
		if result.result.Applied {
			applied++
		}
	}
	return applied
}

func (f *devicePreflight) requireReliabilityLedgerState(t *testing.T, cmd service.UsageBillingCommand, subID int64, balance, usage, counter string, live, archived int) {
	t.Helper()
	db := repository.GetIntegrationDB()
	var gotBalance, daily, weekly, monthly, quota, h5, h1, d7 string
	require.NoError(t, db.QueryRowContext(f.r.ctx, "SELECT balance::numeric(20,8)::text FROM users WHERE id=$1", cmd.UserID).Scan(&gotBalance))
	require.NoError(t, db.QueryRowContext(f.r.ctx, "SELECT daily_usage_usd::numeric(20,8)::text,weekly_usage_usd::numeric(20,8)::text,monthly_usage_usd::numeric(20,8)::text FROM user_subscriptions WHERE id=$1", subID).Scan(&daily, &weekly, &monthly))
	require.NoError(t, db.QueryRowContext(f.r.ctx, "SELECT quota_used::numeric(20,8)::text,usage_5h::numeric(20,8)::text,usage_1d::numeric(20,8)::text,usage_7d::numeric(20,8)::text FROM api_keys WHERE id=$1", cmd.APIKeyID).Scan(&quota, &h5, &h1, &d7))
	require.Equal(t, balance, gotBalance)
	for _, got := range []string{daily, weekly, monthly} {
		require.Equal(t, usage, got)
	}
	for _, got := range []string{quota, h5, h1, d7} {
		require.Equal(t, counter, got)
	}
	var liveCount, archiveCount, logs int
	require.NoError(t, db.QueryRowContext(f.r.ctx, "SELECT count(*) FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2", cmd.RequestID, cmd.APIKeyID).Scan(&liveCount))
	require.NoError(t, db.QueryRowContext(f.r.ctx, "SELECT count(*) FROM usage_billing_dedup_archive WHERE request_id=$1 AND api_key_id=$2", cmd.RequestID, cmd.APIKeyID).Scan(&archiveCount))
	require.NoError(t, db.QueryRowContext(f.r.ctx, "SELECT count(*) FROM usage_logs WHERE user_id=$1", cmd.UserID).Scan(&logs))
	require.Equal(t, live, liveCount)
	require.Equal(t, archived, archiveCount)
	require.Zero(t, logs, "direct Apply validates settlement effects, not upstream execution or HTTP usage logging")
}
