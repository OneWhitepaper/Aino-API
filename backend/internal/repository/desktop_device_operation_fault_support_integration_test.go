//go:build integration && nativeconsumer

package repository_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

var operationFaultOptions = []string{"fixture-tool-model", nativeImageCatalogID, "fixture-text-subscription", nativeSubscriptionImageCatalogID}

func operationFaultSeedWindows(t *testing.T, f *devicePreflight) {
	t.Helper()
	now := subscriptionWindowSeedTime(t)
	for _, sub := range []*service.UserSubscription{f.subText, f.subImage} {
		sub.StartsAt, sub.ExpiresAt = now.Add(-time.Hour), now.Add(24*time.Hour)
		sub.DailyWindowStart, sub.WeeklyWindowStart, sub.MonthlyWindowStart = &now, &now, &now
		require.NoError(t, f.r.subs.Update(f.r.ctx, sub))
	}
}

type operationFaultRequest struct {
	path, operation, call string
	body                  []byte
	headers               map[string]string
}

type operationFaultResponse struct {
	status int
	body   []byte
	err    error
}

func newOperationFaultRequest(t *testing.T, option string) operationFaultRequest {
	t.Helper()
	r := operationFaultRequest{path: "/v1/chat/completions", operation: uuid.NewString(), call: uuid.NewString()}
	purpose := "chat"
	body := map[string]any{"model": "fixture-tool-model", "messages": []any{map[string]string{"role": "user", "content": "operation-fault-" + r.operation}}, "stream": false}
	if strings.Contains(option, "image") {
		r.path, purpose = "/v1/images/generations", "image"
		body = map[string]any{"model": nativeImageUpstreamModel, "prompt": "fixture-image-generate operation-fault-" + r.operation, "quality": "auto", "size": "1024x1024", "n": 1}
	}
	var err error
	r.body, err = json.Marshal(body)
	require.NoError(t, err)
	r.headers = map[string]string{"X-Aino-Model-ID": option, "X-Aino-Operation-ID": r.operation, "X-Aino-Session-Id": uuid.NewString(), "X-Aino-Turn-Id": uuid.NewString(), "X-Aino-Call-Id": r.call, "X-Aino-Purpose": purpose}
	return r
}

func operationFaultDispatch(origin, key string, request operationFaultRequest) operationFaultResponse {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", origin+request.path, bytes.NewReader(request.body))
	if err != nil {
		return operationFaultResponse{err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	for name, value := range request.headers {
		req.Header.Set(name, value)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return operationFaultResponse{err: err}
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(response.Body)
	return operationFaultResponse{status: response.StatusCode, body: raw, err: err}
}

func operationFaultRetry(request operationFaultRequest) operationFaultRequest {
	copy := request
	copy.headers = make(map[string]string, len(request.headers))
	for name, value := range request.headers {
		copy.headers[name] = value
	}
	copy.call = uuid.NewString()
	copy.headers["X-Aino-Call-Id"] = copy.call
	return copy
}

func operationFaultUnknown(t *testing.T, response operationFaultResponse) {
	t.Helper()
	require.NoError(t, response.err)
	require.Equal(t, http.StatusConflict, response.status, string(response.body))
	require.Contains(t, string(response.body), "DESKTOP_OPERATION_UNKNOWN")
}

func operationFaultSettle(t *testing.T, f *devicePreflight, request operationFaultRequest) {
	t.Helper()
	var rows int
	require.Eventually(t, func() bool {
		err := repository.GetIntegrationDB().QueryRowContext(f.r.ctx, "SELECT count(*) FROM usage_logs WHERE user_id=$1 AND desktop_call_id=$2 AND settlement_status='settled'", f.userID, request.call).Scan(&rows)
		return err == nil && rows == 1
	}, 10*time.Second, 20*time.Millisecond)
	group, subID, cost := f.textGroup, int64(0), "0.00010000"
	if strings.Contains(request.headers["X-Aino-Model-ID"], "image") {
		cost = "0.08000000"
	}
	switch request.headers["X-Aino-Model-ID"] {
	case "fixture-text-subscription":
		group, subID = f.subText.GroupID, f.subText.ID
	case nativeSubscriptionImageCatalogID:
		group, subID = f.subImage.GroupID, f.subImage.ID
	}
	var actualGroup, actualSub int64
	var actualCost string
	require.NoError(t, repository.GetIntegrationDB().QueryRowContext(f.r.ctx, "SELECT group_id,coalesce(subscription_id,0),actual_cost::numeric(20,8)::text FROM usage_logs WHERE user_id=$1 AND desktop_call_id=$2", f.userID, request.call).Scan(&actualGroup, &actualSub, &actualCost))
	require.Equal(t, group, actualGroup)
	require.Equal(t, subID, actualSub)
	require.Equal(t, cost, actualCost)
}

func operationFaultAssertCharge(t *testing.T, f *devicePreflight, before subscriptionLifecycleState, option string, applied bool) {
	t.Helper()
	want := before
	if applied {
		cost := 0.0001
		if strings.Contains(option, "image") {
			cost = 0.08
		}
		switch option {
		case "fixture-text-subscription":
			for i := range want.Text {
				want.Text[i] += cost
			}
		case nativeSubscriptionImageCatalogID:
			for i := range want.Image {
				want.Image[i] += cost
			}
		default:
			want.Balance -= cost
		}
		want.LedgerRows++
		want.UpstreamCalls++
	}
	got := subscriptionLifecycleSnapshot(t, f)
	require.Equal(t, fmt.Sprintf("%.8f", want.Balance), fmt.Sprintf("%.8f", got.Balance))
	for i := range 3 {
		require.Equal(t, fmt.Sprintf("%.8f", want.Text[i]), fmt.Sprintf("%.8f", got.Text[i]))
		require.Equal(t, fmt.Sprintf("%.8f", want.Image[i]), fmt.Sprintf("%.8f", got.Image[i]))
	}
	require.Equal(t, want.LedgerRows, got.LedgerRows)
	require.Equal(t, want.UpstreamCalls, got.UpstreamCalls)
}

// The API has already returned success; only its client-facing socket is lost.
func operationFaultDropResponse(t *testing.T, target string) (*httptest.Server, <-chan operationFaultResponse) {
	t.Helper()
	completed := make(chan operationFaultResponse, 1)
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		upstream, err := http.NewRequestWithContext(ctx, request.Method, target+request.URL.Path, request.Body)
		if err != nil {
			completed <- operationFaultResponse{err: err}
			return
		}
		upstream.Header = request.Header.Clone()
		response, err := http.DefaultClient.Do(upstream)
		if err != nil {
			completed <- operationFaultResponse{err: err}
			return
		}
		defer func() { _ = response.Body.Close() }()
		raw, err := io.ReadAll(response.Body)
		completed <- operationFaultResponse{status: response.StatusCode, body: raw, err: err}
		connection, _, err := http.NewResponseController(w).Hijack()
		if err == nil {
			_ = connection.Close()
		}
	}))
	t.Cleanup(front.Close)
	return front, completed
}

// The controlled provider executes before its real HTTP response is withheld or lost.
func operationFaultUpstream(t *testing.T, f *devicePreflight, hold bool) (<-chan struct{}, chan<- struct{}) {
	t.Helper()
	accepted, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	inner := &devicePreflightUpstream{protocol: f.protocol}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Aino-Operation-ID") != "" {
			t.Error("private operation identity reached the controlled provider")
		}
		recorder := httptest.NewRecorder()
		inner.ServeHTTP(recorder, request)
		if calls.Add(1) == 1 {
			close(accepted)
			if hold {
				select {
				case <-release:
				case <-request.Context().Done():
				case <-time.After(30 * time.Second):
				}
			} else {
				connection, _, err := http.NewResponseController(w).Hijack()
				if err == nil {
					_ = connection.Close()
				}
				return
			}
		}
		for name, values := range recorder.Header() {
			w.Header()[name] = values
		}
		w.WriteHeader(recorder.Code)
		_, _ = io.Copy(w, bytes.NewReader(recorder.Body.Bytes()))
	}))
	t.Cleanup(upstream.Close)
	var accountID int64
	require.NoError(t, repository.GetIntegrationDB().QueryRowContext(f.r.ctx, "SELECT id FROM accounts WHERE name=$1", "fixture-"+f.r.runID).Scan(&accountID))
	repo := repository.NewAccountRepository(f.r.client, repository.GetIntegrationDB(), nil)
	account, err := repo.GetByID(f.r.ctx, accountID)
	require.NoError(t, err)
	account.Credentials["base_url"] = upstream.URL
	mutable, ok := repo.(interface {
		UpdateCredentials(context.Context, int64, map[string]any) error
	})
	require.True(t, ok)
	require.NoError(t, mutable.UpdateCredentials(f.r.ctx, accountID, account.Credentials))
	standby := &service.Account{Name: "fixture-operation-failover-" + uuid.NewString(), Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 64, Credentials: account.Credentials, Extra: map[string]any{"openai_responses_mode": "force_chat_completions"}}
	require.NoError(t, repo.Create(f.r.ctx, standby))
	require.NoError(t, repo.BindGroups(f.r.ctx, standby.ID, []int64{f.textGroup, f.subText.GroupID, f.subImage.GroupID}))
	t.Cleanup(func() { _ = repo.Delete(f.r.ctx, standby.ID) })
	return accepted, release
}

func operationFaultRestart(t *testing.T, f *devicePreflight, instance int) {
	t.Helper()
	previous := f.commands[instance]
	require.NoError(t, previous.Process.Kill())
	_ = previous.Wait()
	logfile, err := os.OpenFile(filepath.Join(f.dir, fmt.Sprintf("instance-%d", instance+1), "restart.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	require.NoError(t, err)
	command := exec.Command(previous.Path, previous.Args[1:]...)
	command.Dir, command.Env = previous.Dir, append([]string(nil), previous.Env...)
	command.Stdout, command.Stderr = logfile, logfile
	require.NoError(t, command.Start())
	t.Cleanup(func() { _ = command.Process.Signal(os.Interrupt); _ = command.Wait(); _ = logfile.Close() })
	require.Eventually(t, func() bool {
		response, err := http.Get(f.origins[instance] + "/health")
		if err != nil {
			return false
		}
		defer func() { _ = response.Body.Close() }()
		return response.StatusCode == 200
	}, 45*time.Second, 100*time.Millisecond)
	t.Logf("OPERATION_RESTART instance=%d old_pid=%d new_pid=%d same_config_and_storage=true", instance+1, previous.Process.Pid, command.Process.Pid)
}
