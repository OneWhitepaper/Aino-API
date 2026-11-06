//go:build integration && nativeconsumer

package repository_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// The opt-in driver exercises the real native/Python coordinator against the
// ordinary authenticated routes and disposable PostgreSQL owned by TestMain.
func TestDesktopSyncNativeConsumer(t *testing.T) {
	node, script := os.Getenv("AINO_SYNC_CONSUMER_NODE"), os.Getenv("AINO_SYNC_CONSUMER_SCRIPT")
	if node == "" || script == "" {
		t.Skip("set AINO_SYNC_CONSUMER_NODE and AINO_SYNC_CONSUMER_SCRIPT to run the native consumer")
	}
	root := os.Getenv("AINO_SYNC_DESKTOP_ROOT")
	require.NotEmpty(t, root, "AINO_SYNC_DESKTOP_ROOT must identify the prepared desktop checkout")
	r := newDesktopModelRig(t)
	r.desktopHandler.SetSyncService(service.NewDesktopSyncService(repository.NewDesktopSyncRepository(repository.GetIntegrationDB())))
	profiles := r.router.Group("/api/v1/user", gin.HandlerFunc(middleware.NewJWTAuthMiddleware(r.auth, r.users, r.settings, nil)))
	userHandler := handler.NewUserHandler(r.users, r.auth, nil, nil, nil, nil, nil, r.settings)
	profiles.GET("/profile", userHandler.GetProfile)
	owner, other := r.user(t, service.StatusActive, false), r.user(t, service.StatusActive, false)
	token, err := r.auth.GenerateToken(r.ctx, owner)
	require.NoError(t, err)
	otherToken, err := r.auth.GenerateToken(r.ctx, other)
	require.NoError(t, err)
	server := httptest.NewServer(r.router)
	defer server.Close()
	input, err := json.Marshal(struct {
		Origin      string `json:"origin"`
		UserID      string `json:"user_id"`
		Token       string `json:"token"`
		OtherUserID string `json:"other_user_id"`
		OtherToken  string `json:"other_token"`
	}{server.URL, strconv.FormatInt(owner.ID, 10), token, strconv.FormatInt(other.ID, 10), otherToken})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, script)
	cmd.Dir = root
	cmd.Env = os.Environ()
	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	require.NoError(t, err, "native sync consumer failed: %s; stdout: %s", stderr.String(), string(output))
	var result struct {
		ContinuedMessages int  `json:"continued_messages"`
		ConflictCopies    int  `json:"conflict_copies"`
		AttachmentBytes   int  `json:"attachment_bytes"`
		AccountIsolation  bool `json:"account_isolation"`
	}
	require.NoError(t, json.Unmarshal(output, &result), "consumer must return JSON evidence")
	require.Positive(t, result.ContinuedMessages, "the second home must continue the imported conversation")
	require.Positive(t, result.ConflictCopies, "divergent edits must remain as a conflict copy")
	require.Positive(t, result.AttachmentBytes, "the private attachment must round trip")
	require.True(t, result.AccountIsolation, "the second account must not obtain the first account's history")
	t.Logf("native sync evidence: continued_messages=%d conflict_copies=%d attachment_bytes=%d account_isolation=%t", result.ContinuedMessages, result.ConflictCopies, result.AttachmentBytes, result.AccountIsolation)
}
