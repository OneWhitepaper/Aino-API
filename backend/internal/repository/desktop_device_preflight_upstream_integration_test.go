//go:build integration && nativeconsumer

package repository_test

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// Replaces only a controlled model fixture. The existing API processes and
// permanent device identity continue using their original ports and database.
func TestDevicePreflightUpstreamReplacement(t *testing.T) {
	dir := os.Getenv("AINO_DEVICE_PREFLIGHT_DIR")
	if dir == "" {
		t.Skip("explicit existing fixture required")
	}
	var manifest struct {
		Origin, Nonce, RunID, FixturePath, FixtureContent string
		Configs                                           []string `json:"instance_configs"`
	}
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)
	var values map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &values))
	for key, target := range map[string]*string{"origin": &manifest.Origin, "nonce": &manifest.Nonce, "run_id": &manifest.RunID, "fixture_path": &manifest.FixturePath, "fixture_content": &manifest.FixtureContent} {
		require.NoError(t, json.Unmarshal(values[key], target))
	}
	require.NoError(t, json.Unmarshal(values["instance_configs"], &manifest.Configs))
	require.Len(t, manifest.Configs, 2)
	var cfg struct {
		Database struct {
			Host, User, Password string
			Port                 int
			DBName               string `yaml:"dbname"`
		}
		Redis struct {
			Host string
			Port int
		}
	}
	raw, err = os.ReadFile(manifest.Configs[0])
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(raw, &cfg))
	require.Equal(t, "sub2api_test", cfg.Database.DBName)
	require.Equal(t, "postgres", cfg.Database.User)
	require.Contains(t, []string{"127.0.0.1", "localhost"}, cfg.Database.Host)
	require.Contains(t, []string{"127.0.0.1", "localhost"}, cfg.Redis.Host)
	containers, err := exec.Command("docker", "ps", "--filter", fmt.Sprintf("publish=%d", cfg.Database.Port), "--filter", "ancestor=postgres:18.1-alpine3.23", "--format", "{{.ID}}").Output()
	require.NoError(t, err)
	require.Len(t, strings.Fields(string(containers)), 1)
	ctx := context.Background()
	db, err := sql.Open("postgres", fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable", cfg.Database.Host, cfg.Database.Port, cfg.Database.User, cfg.Database.Password, cfg.Database.DBName))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	rdb := redis.NewClient(&redis.Options{Addr: fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port)})
	t.Cleanup(func() { _ = rdb.Close() })
	accounts := repository.NewAccountRepository(client, db, repository.NewSchedulerCache(rdb))
	credentials, ok := accounts.(interface {
		UpdateCredentials(context.Context, int64, map[string]any) error
	})
	require.True(t, ok)
	var accountID int64
	require.NoError(t, db.QueryRow("SELECT id FROM accounts WHERE name=$1", "fixture-"+manifest.RunID).Scan(&accountID))
	account, err := accounts.GetByID(ctx, accountID)
	require.NoError(t, err)
	previous, ok := account.Credentials["base_url"].(string)
	require.True(t, ok)
	restore := previous
	if audit, err := os.ReadFile(filepath.Join(dir, "upstream-id-compat-audit.json")); err == nil {
		var old struct {
			Old string `json:"old_loopback_upstream"`
			New string `json:"new_loopback_upstream"`
		}
		require.NoError(t, json.Unmarshal(audit, &old))
		if previous == old.New {
			restore = old.Old
		}
	}
	for _, value := range []string{previous, restore} {
		target, err := url.Parse(value)
		require.NoError(t, err)
		require.Equal(t, "http", target.Scheme)
		require.Contains(t, []string{"127.0.0.1", "localhost"}, target.Hostname())
		require.NotEmpty(t, target.Port())
	}
	require.True(t, filepath.IsAbs(*devicePreflightArtifactRoot))
	artifactRoot, err := filepath.EvalSymlinks(*devicePreflightArtifactRoot)
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(artifactRoot, "/cache/generated/images"))
	protocol := &ainoNativeProtocol{rig: &ainoPlatformFixture{runID: manifest.RunID}, path: manifest.FixturePath, content: manifest.FixtureContent, shutdown: make(chan struct{}), releaseTerminal: make(chan struct{})}
	provider := &devicePreflightUpstream{protocol: protocol}
	completed := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/fixture/control/") {
			if subtle.ConstantTimeCompare([]byte(req.Header.Get("X-Fixture-Nonce")), []byte(manifest.Nonce)) != 1 {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			if strings.HasSuffix(req.URL.Path, "/complete") {
				select {
				case completed <- struct{}{}:
				default:
				}
				_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"model_calls": protocol.rig.modelCalls.Load(), "tool_results": protocol.toolResults.Load(), "image_requests": protocol.images.snapshot(), "image_tool_results": protocol.images.toolResults.Load()})
			return
		}
		provider.ServeHTTP(w, req)
	}))
	t.Cleanup(server.Close)
	account.Credentials["base_url"] = server.URL
	require.NoError(t, credentials.UpdateCredentials(ctx, accountID, account.Credentials))
	t.Cleanup(func() {
		account.Credentials["base_url"] = restore
		require.NoError(t, credentials.UpdateCredentials(ctx, accountID, account.Credentials))
	})
	proof := map[string]any{"origin": server.URL, "previous_loopback_upstream": previous, "restore_loopback_upstream": restore, "switched_at": time.Now().UTC().Format(time.RFC3339Nano), "pid": os.Getpid(), "artifact_root": artifactRoot, "scope": "controlled model protocol only; API origin/device key/user data unchanged"}
	raw, err = json.MarshalIndent(proof, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "upstream-replacement.json"), raw, 0600))
	select {
	case <-completed:
	case <-time.After(time.Hour):
		t.Error("upstream consumer did not complete")
	}
}
