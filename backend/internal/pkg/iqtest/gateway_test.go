package iqtest

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGatewayTransportPreservesOriginalClientAndStripsForwardedHeaders(t *testing.T) {
	bridge := NewGatewayTransport()
	bridge.SetHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "203.0.113.8:0", r.RemoteAddr)
		require.Empty(t, r.Header.Get("X-Forwarded-For"))
		require.Equal(t, "Bearer own-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, "data: first\n\n")
		flusher, ok := w.(http.Flusher)
		require.True(t, ok)
		flusher.Flush()
		<-r.Context().Done()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://aino.internal/v1/responses", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer own-key")
	req.Header.Set("X-Forwarded-For", "1.1.1.1")
	req = WithClientIP(req, "203.0.113.8")
	resp, err := (&http.Client{Transport: bridge}).Do(req)
	require.NoError(t, err)
	buf := make([]byte, len("data: first\n\n"))
	_, err = io.ReadFull(resp.Body, buf)
	require.NoError(t, err)
	require.Equal(t, "data: first\n\n", string(buf))
	cancel()
	_ = resp.Body.Close()
}

func TestGatewayTransportRejectsArbitraryPathsAndUnsetHandler(t *testing.T) {
	bridge := NewGatewayTransport()
	for _, path := range []string{"http://aino.internal/api/v1/admin/users", "https://evil.example/v1/models", "http://aino.internal/v1/models"} {
		req, _ := http.NewRequest(http.MethodGet, path, nil)
		_, err := bridge.RoundTrip(req)
		require.Error(t, err)
	}
}
