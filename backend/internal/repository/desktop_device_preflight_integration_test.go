//go:build integration && nativeconsumer

package repository_test

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

var devicePreflightNativeOnly = flag.Bool("aino-native-only", false, "Keep the isolated API fixture for native consumers without repeating preflight load/fault probes")
var devicePreflightServerBinary = flag.String("aino-preflight-server", filepath.Join(os.TempDir(), "aino-device-key-preflight-server"), "Absolute path to the isolated preflight server binary")
var devicePreflightArtifactRoot = flag.String("aino-native-artifact-root", "", "Explicit isolated native generated-image directory for fixture pixel verification")

func TestDevicePreflightAPI(t *testing.T) {
	dir := os.Getenv("AINO_DEVICE_PREFLIGHT_DIR")
	if dir == "" {
		t.Skip("explicit local preflight driver required")
	}
	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	tmp, err := filepath.EvalSymlinks(os.TempDir())
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(resolved, tmp+string(os.PathSeparator)))
	info, err := os.Stat(dir)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0700), info.Mode().Perm())
	f := newDevicePreflight(t, dir)
	f.startServers(t)
	manifest := map[string]any{"origin": f.origins[0], "origins": f.origins, "nonce": f.nonce, "email": f.email, "password": f.password, "user_id": f.userID, "run_id": f.r.runID, "fixture_path": f.protocol.path, "fixture_content": f.protocol.content, "text_model_id": "fixture-tool-model", "image_model_id": nativeImageCatalogID, "text_subscription_model_id": "fixture-text-subscription", "image_subscription_model_id": "fixture-image-subscription", "image_trigger": "fixture-image-generate", "edit_trigger": "fixture-image-edit", "instance_configs": []string{filepath.Join(dir, "instance-1", "config.yaml"), filepath.Join(dir, "instance-2", "config.yaml")}}
	blob, err := json.MarshalIndent(manifest, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"), blob, 0600))
	t.Log("Two real API processes ready; private manifest written; one hour bounded consumer lifetime")
	if *devicePreflightNativeOnly {
		f.awaitConsumer(t)
		return
	}
	// Every destructive probe gets a separate account from the Desktop consumer.
	u := f.r.user(t, service.StatusActive, false)
	u.Concurrency = 64
	require.NoError(t, f.r.userRepo.Update(f.r.ctx, u, service.UserUpdateFields{Concurrency: true}))
	require.NoError(t, f.r.userRepo.AddGroupToAllowedGroups(f.r.ctx, u.ID, f.textGroup))
	now := time.Now()
	probe := &devicePreflight{r: f.r, dir: f.dir, origins: f.origins, email: u.Email, password: f.password, userID: u.ID, textGroup: f.textGroup, imageGroup: f.imageGroup}
	for i, group := range []int64{f.subText.GroupID, f.subImage.GroupID} {
		sub := &service.UserSubscription{UserID: u.ID, GroupID: group, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Status: service.SubscriptionStatusActive, DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now}
		require.NoError(t, f.r.subs.Create(f.r.ctx, sub))
		if i == 0 {
			probe.subText = sub
		} else {
			probe.subImage = sub
		}
	}
	t.Run("real-http-routing-ledger-load-revocation", func(t *testing.T) { probe.verify(t) })
	require.NoError(t, os.WriteFile(filepath.Join(dir, "verification-complete"), []byte("complete\n"), 0600))
	f.awaitConsumer(t)
}

func (f *devicePreflight) awaitConsumer(t *testing.T) {
	t.Helper()
	select {
	case <-f.completed:
	case <-time.After(time.Hour):
		t.Error("Desktop consumer did not complete within bounded lifetime")
	}
}

func (f *devicePreflight) call(t *testing.T, instance int, method, path, token string, body any, headers map[string]string) (int, []byte, time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	status, raw, latency, err := preflightHTTP(ctx, f.origins[instance], method, path, token, body, headers)
	require.NoError(t, err)
	return status, raw, latency
}

func (f *devicePreflight) login(t *testing.T) string {
	t.Helper()
	status, raw, _ := f.call(t, 0, "POST", "/api/v1/auth/login", "", map[string]string{"email": f.email, "password": f.password}, nil)
	require.Equal(t, 200, status, string(raw))
	var result struct {
		Data struct {
			AccessToken string `json:"access_token"`
		}
	}
	require.NoError(t, json.Unmarshal(raw, &result))
	require.NotEmpty(t, result.Data.AccessToken)
	return result.Data.AccessToken
}

func (f *devicePreflight) device(t *testing.T, token string, request service.DesktopDeviceKeyRequest) *service.DesktopDeviceKeyResponse {
	t.Helper()
	status, raw, _ := f.call(t, 0, "POST", "/api/v1/desktop/device-keys", token, request, map[string]string{"Idempotency-Key": uuid.NewString()})
	require.Equal(t, 200, status, string(raw))
	var result struct {
		Data service.DesktopDeviceKeyResponse
	}
	require.NoError(t, json.Unmarshal(raw, &result))
	return &result.Data
}

func (f *devicePreflight) infer(t *testing.T, instance int, key, option, prompt string) (int, []byte, time.Duration) {
	t.Helper()
	path := "/v1/chat/completions"
	body := map[string]any{"model": "fixture-tool-model", "messages": []any{map[string]string{"role": "user", "content": prompt}}, "stream": false}
	purpose := "chat"
	if strings.Contains(option, "image") {
		path = "/v1/images/generations"
		purpose = "image"
		body = map[string]any{"model": nativeImageUpstreamModel, "prompt": "fixture-image-generate " + prompt, "quality": "auto", "size": "1024x1024", "n": 1}
	}
	return f.call(t, instance, "POST", path, key, body, map[string]string{"X-Aino-Model-ID": option, "X-Aino-Session-Id": uuid.NewString(), "X-Aino-Turn-Id": uuid.NewString(), "X-Aino-Call-Id": uuid.NewString(), "X-Aino-Purpose": purpose})
}

func (f *devicePreflight) verify(t *testing.T) {
	token := f.login(t)
	device := uuid.NewString()
	key := f.device(t, token, service.DesktopDeviceKeyRequest{DeviceID: device})
	require.NotEmpty(t, key.APIKey)
	repeat := f.device(t, token, service.DesktopDeviceKeyRequest{DeviceID: device})
	require.Equal(t, key.KeyID, repeat.KeyID)
	require.Empty(t, repeat.APIKey)
	db := repository.GetIntegrationDB()
	var migrationCount int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM information_schema.columns WHERE table_name='api_keys' AND column_name IN ('key_hash_type','desktop_device_id','desktop_key_version')").Scan(&migrationCount))
	require.Equal(t, 3, migrationCount)
	for _, option := range []string{"fixture-tool-model", nativeImageCatalogID, "fixture-text-subscription", "fixture-image-subscription"} {
		status, raw, _ := f.infer(t, 0, key.APIKey, option, "initial")
		require.Equal(t, 200, status, string(raw))
	}
	const load = 48
	var wg sync.WaitGroup
	latencies := make(chan time.Duration, load)
	failures := make(chan string, load)
	started := time.Now()
	for i := range load {
		wg.Add(1)
		go func() {
			defer wg.Done()
			option := "fixture-text-subscription"
			path := "/v1/chat/completions"
			body := map[string]any{"model": "fixture-tool-model", "messages": []any{map[string]string{"role": "user", "content": "load"}}, "stream": false}
			if i%4 == 0 {
				option = "fixture-tool-model"
			}
			if i%4 == 1 {
				option = "fixture-image-subscription"
				path = "/v1/images/generations"
				body = map[string]any{"model": nativeImageUpstreamModel, "prompt": "fixture-image-generate load", "quality": "auto", "size": "1024x1024", "n": 1}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			status, raw, d, err := preflightHTTP(ctx, f.origins[i%2], "POST", path, key.APIKey, body, map[string]string{"X-Aino-Model-ID": option})
			if err != nil || status != 200 {
				failures <- fmt.Sprintf("request %d status %d err %v body %s", i, status, err, raw)
			}
			latencies <- d
		}()
	}
	wg.Wait()
	close(failures)
	close(latencies)
	failureCount := len(failures)
	for err := range failures {
		t.Error(err)
	}
	values := []time.Duration{}
	for latency := range latencies {
		values = append(values, latency)
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	t.Logf("LOAD requests=%d errors=%d elapsed_ms=%d p50_ms=%.3f p95_ms=%.3f max_ms=%.3f", load, failureCount, time.Since(started).Milliseconds(), float64(values[len(values)/2].Microseconds())/1000, float64(values[len(values)*95/100].Microseconds())/1000, float64(values[len(values)-1].Microseconds())/1000)
	for i := range 8 {
		status, raw, _ := f.infer(t, i%2, key.APIKey, "fixture-text-subscription", "preflight-duplicate")
		require.Equal(t, 200, status, string(raw))
	}
	var logs int
	var balance, text, image float64
	require.Eventually(t, func() bool {
		_ = db.QueryRow("SELECT count(*) FROM usage_logs WHERE user_id=$1", f.userID).Scan(&logs)
		return logs == load+12
	}, 15*time.Second, 50*time.Millisecond)
	require.NoError(t, db.QueryRow("SELECT balance FROM users WHERE id=$1", f.userID).Scan(&balance))
	require.NoError(t, db.QueryRow("SELECT daily_usage_usd FROM user_subscriptions WHERE id=$1", f.subText.ID).Scan(&text))
	require.NoError(t, db.QueryRow("SELECT daily_usage_usd FROM user_subscriptions WHERE id=$1", f.subImage.ID).Scan(&image))
	require.InDelta(t, 37.5-0.08-13*0.0001, balance, 1e-8)
	require.InDelta(t, 33*0.0001, text, 1e-8)
	require.InDelta(t, 13*0.08, image, 1e-8)
	t.Logf("LEDGER rows=%d balance=%.8f text_subscription=%.8f image_subscription=%.8f repeated_http=8 physical_request_debits=8", logs, balance, text, image)
	f.verifyDuplicateSettlement(t, key, text)
	// Real per-user permission changes must converge through the existing 15s catalog projection.
	require.NoError(t, f.r.userRepo.RemoveGroupFromUserAllowedGroups(f.r.ctx, f.userID, f.textGroup))
	permissionStart := time.Now()
	require.Eventually(t, func() bool {
		status, _, _ := f.infer(t, 1, key.APIKey, "fixture-tool-model", "permission")
		return status == 403 || status == 404
	}, 18*time.Second, 250*time.Millisecond)
	t.Logf("PERMISSION denial_ms=%d", time.Since(permissionStart).Milliseconds())
	require.NoError(t, f.r.userRepo.AddGroupToAllowedGroups(f.r.ctx, f.userID, f.textGroup))
	require.Eventually(t, func() bool {
		status, _, _ := f.infer(t, 0, key.APIKey, "fixture-tool-model", "permission-restored")
		return status == 200
	}, 18*time.Second, 250*time.Millisecond)
	_, err := f.r.userRepo.SetBalance(f.r.ctx, f.userID, 0)
	require.NoError(t, err)
	require.NoError(t, repository.GetIntegrationRedis().Del(f.r.ctx, fmt.Sprintf("billing:balance:%d", f.userID)).Err())
	status, raw, _ := f.infer(t, 0, key.APIKey, "fixture-tool-model", "no-balance")
	require.NotEqual(t, 200, status, string(raw))
	t.Logf("BALANCE empty_status=%d", status)
	_, err = f.r.userRepo.SetBalance(f.r.ctx, f.userID, 37.5)
	require.NoError(t, err)
	require.NoError(t, repository.GetIntegrationRedis().Del(f.r.ctx, fmt.Sprintf("billing:balance:%d", f.userID)).Err())
	for instance := range 2 {
		status, raw, _ := f.call(t, instance, "GET", "/v1/models", key.APIKey, nil, nil)
		require.Equal(t, 200, status, string(raw))
	}
	start := time.Now()
	status, raw, _ = f.call(t, 0, "DELETE", "/api/v1/desktop/devices/"+device, token, nil, nil)
	require.Equal(t, 200, status, string(raw))
	for instance := range 2 {
		require.Eventually(t, func() bool {
			status, _, _ := f.call(t, instance, "GET", "/v1/models", key.APIKey, nil, nil)
			return status == 401 || status == 403
		}, 4*time.Second, 20*time.Millisecond)
		t.Logf("REVOKE instance=%d denial_ms=%d", instance+1, time.Since(start).Milliseconds())
	}
	stateStatus, stateRaw, _ := f.call(t, 0, "GET", "/api/v1/desktop/device-keys/"+device, token, nil, nil)
	require.Equal(t, 200, stateStatus, string(stateRaw))
	var state struct {
		Data service.DesktopDeviceKeyResponse
	}
	require.NoError(t, json.Unmarshal(stateRaw, &state))
	reauthorized := f.device(t, token, service.DesktopDeviceKeyRequest{DeviceID: device, ExpectedVersion: &state.Data.Version, Reauthorize: true})
	require.Equal(t, key.KeyID, reauthorized.KeyID)
	require.NotEqual(t, key.APIKey, reauthorized.APIKey)
	for instance := range 2 {
		status, raw, _ := f.infer(t, instance, reauthorized.APIKey, "fixture-text-subscription", "reauthorized")
		require.Equal(t, 200, status, string(raw))
	}
	t.Log("REAUTHORIZE same_key_id=true new_secret=true inference_success_instances=2")
	f.verifyHardAge(t, token, reauthorized)
}

func (f *devicePreflight) verifyHardAge(t *testing.T, token string, _ *service.DesktopDeviceKeyResponse) {
	key := f.device(t, token, service.DesktopDeviceKeyRequest{DeviceID: uuid.NewString()})
	db := repository.GetIntegrationDB()
	index := strings.TrimPrefix(service.HashDesktopDeviceSecret(key.APIKey), "sha256:")
	_, err := db.Exec("DELETE FROM auth_cache_invalidation_outbox WHERE cache_key=$1", index)
	require.NoError(t, err)
	for instance := range 2 {
		status, raw, _ := f.call(t, instance, "GET", "/v1/models", key.APIKey, nil, nil)
		require.Equal(t, 200, status, string(raw))
	}
	transaction, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	defer transaction.Rollback()
	_, err = transaction.Exec("ALTER TABLE api_keys DISABLE TRIGGER trg_api_keys_auth_cache_invalidation")
	require.NoError(t, err)
	_, err = transaction.Exec("UPDATE api_keys SET status='disabled',updated_at=now() WHERE id=$1", key.KeyID)
	require.NoError(t, err)
	_, err = transaction.Exec("ALTER TABLE api_keys ENABLE TRIGGER trg_api_keys_auth_cache_invalidation")
	require.NoError(t, err)
	require.NoError(t, transaction.Commit())
	start := time.Now()
	denied := [2]time.Duration{}
	accepted := 0
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for time.Since(start) < 57*time.Second {
		for instance := range 2 {
			if denied[instance] > 0 {
				continue
			}
			status, raw, _ := f.call(t, instance, "GET", "/v1/models", key.APIKey, nil, nil)
			if status == 401 || status == 403 {
				denied[instance] = time.Since(start)
			} else {
				require.Equal(t, 200, status, string(raw))
				accepted++
			}
		}
		if denied[0] > 0 && denied[1] > 0 {
			break
		}
		<-ticker.C
	}
	for instance, d := range denied {
		require.Positive(t, d, "hard age must eventually reject")
		require.LessOrEqual(t, d, 56*time.Second)
		t.Logf("HARD_AGE instance=%d denial_ms=%d cached_accepts=%d outbox_and_broadcast=omitted", instance+1, d.Milliseconds(), accepted)
	}
}

func (f *devicePreflight) verifyDuplicateSettlement(t *testing.T, key *service.DesktopDeviceKeyResponse, before float64) {
	db := repository.GetIntegrationDB()
	var accountID, keyID int64
	require.NoError(t, db.QueryRow("SELECT account_id,api_key_id FROM usage_logs WHERE user_id=$1 LIMIT 1", f.userID).Scan(&accountID, &keyID))
	repo := repository.NewUsageBillingRepository(f.r.client, db)
	requestID := uuid.NewString()
	const callers = 12
	results := make(chan bool, callers)
	errors := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := &service.UsageBillingCommand{RequestID: requestID, APIKeyID: keyID, UserID: f.userID, AccountID: accountID, AccountType: service.AccountTypeAPIKey, SubscriptionID: &f.subText.ID, Model: "fixture-tool-model", SubscriptionCost: 0.0001}
			result, err := repo.Apply(context.Background(), cmd)
			errors <- err
			if result != nil {
				results <- result.Applied
			}
		}()
	}
	wg.Wait()
	close(results)
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	applied := 0
	for result := range results {
		if result {
			applied++
		}
	}
	require.Equal(t, 1, applied)
	var after float64
	require.NoError(t, db.QueryRow("SELECT daily_usage_usd FROM user_subscriptions WHERE id=$1", f.subText.ID).Scan(&after))
	require.InDelta(t, 0.0001, after-before, 1e-8)
	t.Logf("DUPLICATE_SETTLEMENT callers=%d applied=%d debit=%.8f", callers, applied, after-before)
}
