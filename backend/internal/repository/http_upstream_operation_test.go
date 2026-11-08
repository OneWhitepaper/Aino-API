package repository

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestDesktopOperationRetainsSingleDispatchDuringBoundedDrain(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_ = http.NewResponseController(w).Flush()
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-req.Context().Done():
				return
			case <-ticker.C:
				_, _ = io.WriteString(w, ": ping\n\n")
				_ = http.NewResponseController(w).Flush()
			}
		}
	}))
	t.Cleanup(server.Close)
	clientCtx, disconnect := context.WithCancel(t.Context())
	t.Cleanup(disconnect)
	ctx := service.WithDesktopInferenceOperation(pkghttputil.DetachUpstreamContext(clientCtx), "operation")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, strings.NewReader("request"))
	require.NoError(t, err)
	resp, err := doUpstreamRequest(server.Client(), req, 40*time.Millisecond)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	disconnect()
	started := time.Now()
	_, err = io.ReadAll(resp.Body)
	require.Error(t, err)
	require.Less(t, time.Since(started), time.Second, "pings must not prolong the drain")
	_, err = doUpstreamRequest(server.Client(), req, 40*time.Millisecond)
	require.ErrorIs(t, err, service.ErrDesktopOperationUnknown)
	require.EqualValues(t, 1, calls.Load(), "client disconnect must not permit another paid dispatch")
}

func TestDesktopOperationTransportAllowsArtifactReadsWithoutInferenceReplay(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, "artifact")
	}))
	t.Cleanup(server.Close)
	ctx := service.WithDesktopInferenceOperation(t.Context(), "operation")
	client := NewHTTPUpstream(nil)
	for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodHead, http.MethodPost} {
		req, err := http.NewRequestWithContext(ctx, method, server.URL, strings.NewReader("request"))
		require.NoError(t, err)
		resp, err := client.Do(req, "", 1, 1)
		if method == http.MethodPost && calls.Load() > 1 {
			require.ErrorIs(t, err, service.ErrDesktopOperationUnknown)
			continue
		}
		require.NoError(t, err)
		_, err = io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}
	require.EqualValues(t, 3, calls.Load())
}

func TestDesktopOperationTransportNeverFollowsRedirect(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalls.Add(1) }))
	t.Cleanup(target.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, target.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(origin.Close)
	req, err := http.NewRequestWithContext(service.WithDesktopInferenceOperation(t.Context(), "operation"), http.MethodPost, origin.URL, strings.NewReader("request"))
	require.NoError(t, err)
	resp, err := NewHTTPUpstream(nil).Do(req, "", 1, 1)
	require.NoError(t, err)
	require.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)
	require.NoError(t, resp.Body.Close())
	require.Zero(t, targetCalls.Load())
}

func TestDesktopOperationArtifactRedirectRetainsExistingPolicy(t *testing.T) {
	var redirects atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/redirect" {
			http.Redirect(w, req, "/artifact", http.StatusTemporaryRedirect)
			return
		}
		_, _ = io.WriteString(w, "artifact")
	}))
	t.Cleanup(server.Close)
	ctx := service.WithDesktopInferenceOperation(t.Context(), "operation")
	require.NoError(t, service.BeginDesktopInferenceDispatch(ctx))
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		redirects.Add(1)
		return nil
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		req, err := http.NewRequestWithContext(ctx, method, server.URL+"/redirect", nil)
		require.NoError(t, err)
		resp, err := doUpstreamRequest(client, req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.NoError(t, resp.Body.Close())
	}
	require.EqualValues(t, 2, redirects.Load())
	require.ErrorIs(t, service.BeginDesktopInferenceDispatch(ctx), service.ErrDesktopOperationUnknown)
}
