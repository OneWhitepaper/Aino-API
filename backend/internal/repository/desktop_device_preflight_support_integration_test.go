//go:build integration && nativeconsumer

package repository_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type devicePreflight struct {
	r                           *ainoPlatformFixture
	dir, nonce, email, password string
	userID                      int64
	origins                     []string
	commands                    []*exec.Cmd
	keyPosts                    atomic.Int64
	completed                   chan struct{}
	completeOnce                sync.Once
	subText, subImage           *service.UserSubscription
	textGroup, imageGroup       int64
	protocol                    *ainoNativeProtocol
}

func preflightPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return port
}

func preflightSecret(t *testing.T) string {
	t.Helper()
	var raw [32]byte
	_, err := rand.Read(raw[:])
	require.NoError(t, err)
	return hex.EncodeToString(raw[:])
}

func newDevicePreflight(t *testing.T, dir string) *devicePreflight {
	t.Helper()
	r := newAinoPlatformFixture(t)
	u := r.user(t, service.StatusActive, false)
	u.Concurrency = 64
	require.NoError(t, r.userRepo.Update(r.ctx, u, service.UserUpdateFields{Concurrency: true}))
	f := &devicePreflight{r: r, dir: dir, nonce: preflightSecret(t), email: u.Email,
		password: "existing-account-password", userID: u.ID, completed: make(chan struct{})}
	fixturePath := filepath.Join(dir, "fixture-read.txt")
	content := "fixture-file-content-" + r.runID
	require.NoError(t, os.WriteFile(fixturePath, []byte(content+"\n"), 0600))
	p := &ainoNativeProtocol{rig: r, path: fixturePath, content: content, shutdown: make(chan struct{}), releaseTerminal: make(chan struct{})}
	f.protocol = p
	r.modelProvider = &devicePreflightUpstream{protocol: p}
	r.wireModel(t, u.ID)
	f.textGroup = r.modelGroupID
	textGroup, err := r.groups.GetByID(r.ctx, r.modelGroupID)
	require.NoError(t, err)
	textSub, imageSub := r.group(t, true), r.group(t, true)
	for _, g := range []*service.Group{textSub, imageSub} {
		g.ModelPricing = textGroup.ModelPricing
		g.ModelAllowlist = textGroup.ModelAllowlist
		g.AllowImageGeneration = true
		limit := 100.0
		g.DailyLimitUSD = &limit
		require.NoError(t, r.groups.Update(r.ctx, g))
	}
	f.imageGroup = imageSub.ID
	now := time.Now()
	for i, g := range []*service.Group{textSub, imageSub} {
		sub := &service.UserSubscription{UserID: u.ID, GroupID: g.ID, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour), Status: service.SubscriptionStatusActive, DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now}
		require.NoError(t, r.subs.Create(r.ctx, sub))
		if i == 0 {
			f.subText = sub
		} else {
			f.subImage = sub
		}
	}
	db := repository.GetIntegrationDB()
	var accountID int64
	require.NoError(t, db.QueryRowContext(r.ctx, "SELECT id FROM accounts WHERE name=$1", "fixture-"+r.runID).Scan(&accountID))
	accounts := repository.NewAccountRepository(r.client, db, nil)
	require.NoError(t, accounts.BindGroups(r.ctx, accountID, []int64{textGroup.ID, textSub.ID, imageSub.ID}))
	_, err = db.ExecContext(r.ctx, "UPDATE accounts SET concurrency=64 WHERE id=$1", accountID)
	require.NoError(t, err)
	chat := desktopEntry("fixture-tool-model", textGroup.ID)
	image := service.DesktopModelEntry{ID: nativeImageCatalogID, GroupID: textGroup.ID, Model: nativeImageUpstreamModel, DisplayName: "Fixture Image", ProviderLabel: "Fixture", Platform: service.PlatformOpenAI, APIMode: "images_generations", Capabilities: service.PlatformModelCapabilities{ImageGeneration: true, ImageQualityOptions: []string{"auto", "high"}}}
	subChat, subImageEntry := chat, image
	subChat.ID, subChat.GroupID = "fixture-text-subscription", textSub.ID
	subChat.DisplayName = "Fixture Text Subscription"
	subImageEntry.ID, subImageEntry.GroupID = nativeSubscriptionImageCatalogID, imageSub.ID
	subImageEntry.DisplayName = "Fixture Image Subscription"
	r.catalog(t, []service.DesktopModelEntry{chat, image, subChat, subImageEntry}, chat.ID)
	require.NoError(t, r.settingRepo.Set(r.ctx, "desktop.default_image_model_id", image.ID))
	return f
}

// Reuses the native Agent tool protocol; basic load requests and pricing use bounded local responses.
type devicePreflightUpstream struct{ protocol *ainoNativeProtocol }

func (p *devicePreflightUpstream) configureNativeImageCatalog(t *testing.T, r *ainoPlatformFixture, g *service.Group) service.DesktopModelEntry {
	return p.protocol.configureNativeImageCatalog(t, r, g)
}
func (p *devicePreflightUpstream) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path == "/pricing" {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
		return
	}
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, "fixture read", 400)
		return
	}
	req.Body = io.NopCloser(bytes.NewReader(raw))
	if strings.HasPrefix(req.URL.Path, "/v1/images/") {
		digest, err := nativeImageBodyDigest(raw, req.Header.Get("Content-Type"))
		if err == nil {
			p.protocol.images.mu.Lock()
			if p.protocol.images.purposeByBody == nil {
				p.protocol.images.purposeByBody = make(map[[32]byte]string)
			}
			p.protocol.images.purposeByBody[digest] = "image"
			p.protocol.images.mu.Unlock()
		}
	}
	var body ainoNativeChatRequest
	if req.URL.Path == "/v1/chat/completions" && json.Unmarshal(raw, &body) == nil && len(body.Tools) == 0 {
		call := p.protocol.rig.modelCalls.Add(1)
		requestID := fmt.Sprintf("preflight-%s-%d", p.protocol.rig.runID, call)
		if bytes.Contains(raw, []byte("preflight-duplicate")) {
			requestID = "preflight-duplicate-" + p.protocol.rig.runID
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", requestID)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": requestID, "object": "chat.completion", "model": "fixture-tool-model", "choices": []any{map[string]any{"index": 0, "message": map[string]string{"role": "assistant", "content": "preflight verified"}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}})
		return
	}
	p.protocol.ServeHTTP(w, req)
}

func (f *devicePreflight) startServers(t *testing.T) {
	t.Helper()
	dsn, err := url.Parse(repository.GetIntegrationDSN())
	require.NoError(t, err)
	require.Contains(t, []string{"127.0.0.1", "localhost"}, dsn.Hostname(), "only disposable loopback storage")
	redisHost, redisPort, err := net.SplitHostPort(repository.GetIntegrationRedis().Options().Addr)
	require.NoError(t, err)
	pgPort, _ := strconv.Atoi(dsn.Port())
	rp, _ := strconv.Atoi(redisPort)
	jwt := preflightSecret(t)
	for i := range 2 {
		port := preflightPort(t)
		dataDir := filepath.Join(f.dir, fmt.Sprintf("instance-%d", i+1))
		require.NoError(t, os.MkdirAll(dataDir, 0700))
		password, _ := dsn.User.Password()
		cfg := map[string]any{"server": map[string]any{"host": "127.0.0.1", "port": port, "mode": "release"}, "run_mode": "standard", "database": map[string]any{"host": dsn.Hostname(), "port": pgPort, "user": dsn.User.Username(), "password": password, "dbname": strings.TrimPrefix(dsn.Path, "/"), "sslmode": "disable", "max_open_conns": 20, "max_idle_conns": 5}, "redis": map[string]any{"host": redisHost, "port": rp, "pool_size": 32, "min_idle_conns": 1}, "jwt": map[string]any{"secret": jwt, "expire_hour": 1}, "totp": map[string]any{"encryption_key": preflightSecret(t)}, "security": map[string]any{"url_allowlist": map[string]any{"allow_private_hosts": true, "allow_insecure_http": true}}, "log": map[string]any{"level": "warn", "format": "json"}, "pricing": map[string]any{"remote_url": "http://127.0.0.1:1/pricing", "hash_url": "", "data_dir": dataDir, "fallback_file": "./resources/model-pricing/model_prices_and_context_window.json"}, "api_key_auth_cache": map[string]any{"l1_ttl_seconds": 300, "l2_ttl_seconds": 300, "jitter_percent": 0}, "gateway": map[string]any{"scheduling": map[string]any{"outbox_poll_interval_seconds": 1}}}
		blob, err := yaml.Marshal(cfg)
		require.NoError(t, err)
		configPath := filepath.Join(dataDir, "config.yaml")
		require.NoError(t, os.WriteFile(configPath, blob, 0600))
		logfile, err := os.OpenFile(filepath.Join(dataDir, "server.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		require.NoError(t, err)
		cmd := exec.Command("/tmp/aino-device-key-preflight-server")
		cmd.Dir = "../.."
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dataDir, "DATA_DIR=" + dataDir, "CONFIG_FILE=" + configPath, "SKIP_SETUP=true", "TZ=UTC"}
		cmd.Stdout, cmd.Stderr = logfile, logfile
		require.NoError(t, cmd.Start())
		f.commands = append(f.commands, cmd)
		t.Cleanup(func() { _ = cmd.Process.Signal(os.Interrupt); _ = cmd.Wait(); _ = logfile.Close() })
		target, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
		proxy := httputil.NewSingleHostReverseProxy(target)
		front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if strings.HasPrefix(req.URL.Path, "/fixture/control/") {
				f.control(w, req)
				return
			}
			if req.Method == "POST" && req.URL.Path == "/api/v1/desktop/device-keys" {
				f.keyPosts.Add(1)
			}
			proxy.ServeHTTP(w, req)
		}))
		t.Cleanup(front.Close)
		f.origins = append(f.origins, front.URL)
		require.Eventually(t, func() bool {
			response, err := http.Get(target.String() + "/health")
			if err != nil {
				return false
			}
			defer response.Body.Close()
			return response.StatusCode == 200
		}, 45*time.Second, 100*time.Millisecond, "real server did not start; inspect private instance log")
	}
}

func (f *devicePreflight) control(w http.ResponseWriter, req *http.Request) {
	if subtle.ConstantTimeCompare([]byte(req.Header.Get("X-Fixture-Nonce")), []byte(f.nonce)) != 1 {
		http.Error(w, "forbidden", 403)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch strings.TrimPrefix(req.URL.Path, "/fixture/control/") {
	case "complete":
		f.completeOnce.Do(func() { close(f.completed) })
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	case "state":
		var balance, cost string
		var keys, calls int
		db := repository.GetIntegrationDB()
		_ = db.QueryRow("SELECT balance::text FROM users WHERE id=$1", f.userID).Scan(&balance)
		_ = db.QueryRow("SELECT count(*),coalesce(sum(actual_cost),0)::text FROM usage_logs WHERE user_id=$1 AND settlement_status='settled'", f.userID).Scan(&calls, &cost)
		_ = db.QueryRow("SELECT count(*) FROM api_keys WHERE user_id=$1 AND desktop_credential_type='device'", f.userID).Scan(&keys)
		rows, err := db.Query("SELECT api_key_id,coalesce(group_id,0),coalesce(subscription_id,0),coalesce(desktop_session_id,''),coalesce(desktop_turn_id,''),coalesce(desktop_call_id,''),coalesce(desktop_purpose,''),actual_cost::text FROM usage_logs WHERE user_id=$1 ORDER BY id", f.userID)
		ledger := []map[string]any{}
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var key, group, sub int64
				var session, turn, call, purpose, charge string
				if rows.Scan(&key, &group, &sub, &session, &turn, &call, &purpose, &charge) == nil {
					ledger = append(ledger, map[string]any{"api_key_id": key, "group_id": group, "subscription_id": sub, "desktop_session_id": session, "desktop_turn_id": turn, "desktop_call_id": call, "desktop_purpose": purpose, "actual_cost": charge})
				}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"user_id": f.userID, "balance": balance, "usage_cost": cost, "usage_calls": calls, "device_keys": keys, "key_posts": f.keyPosts.Load(), "usage_ledger": ledger, "model_calls": f.r.modelCalls.Load(), "tool_results": f.protocol.toolResults.Load(), "image_requests": f.protocol.images.snapshot()})
	default:
		http.NotFound(w, req)
	}
}

func preflightHTTP(ctx context.Context, origin, method, path, token string, body any, headers map[string]string) (int, []byte, time.Duration, error) {
	var reader io.Reader
	if body != nil {
		blob, err := json.Marshal(body)
		if err != nil {
			return 0, nil, 0, err
		}
		reader = bytes.NewReader(blob)
	}
	req, err := http.NewRequestWithContext(ctx, method, origin+path, reader)
	if err != nil {
		return 0, nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	start := time.Now()
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, time.Since(start), err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	return response.StatusCode, raw, time.Since(start), err
}
