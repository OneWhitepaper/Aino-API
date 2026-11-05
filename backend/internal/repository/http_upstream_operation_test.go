package repository

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

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
