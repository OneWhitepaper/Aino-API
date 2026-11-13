package repository

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestFundingDispatchOnlyOnceAndDefinitiveRejectionRefund(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var network, marked, refunded atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				network.Add(1)
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","message":"rejected"}}`)
			}))
			defer server.Close()
			ctx := service.WithFundingDispatchGuard(context.Background(), "funding-test", func(context.Context) error { marked.Add(1); return nil }, func(context.Context) error { refunded.Add(1); return nil })
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, strings.NewReader(`{"model":"m"}`))
			require.NoError(t, err)
			resp, err := doUpstreamRequest(server.Client(), req)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			require.EqualValues(t, 1, network.Load())
			require.EqualValues(t, 1, marked.Load())
			if status == 400 {
				require.EqualValues(t, 1, refunded.Load())
			} else {
				require.Zero(t, refunded.Load(), "unknown provider failures must retain escrow")
			}
			_, err = doUpstreamRequest(server.Client(), req)
			require.ErrorIs(t, err, service.ErrTeamFundingConflict)
			require.EqualValues(t, 1, network.Load())
		})
	}
}

func TestFundingDispatchRejectsRemovedOutputCapBeforeNetwork(t *testing.T) {
	var network, marked atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { network.Add(1) }))
	defer server.Close()
	ctx := service.WithFundingDispatchGuard(context.Background(), "oauth-reject", func(context.Context) error { marked.Add(1); return nil }, nil)
	service.WithFundingDispatchLimits(ctx, 100000, 4096, 1)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, strings.NewReader(`{"model":"gpt-5","input":"hello"}`))
	require.NoError(t, err)
	_, err = doUpstreamRequest(server.Client(), req)
	require.ErrorIs(t, err, service.ErrTeamFundingCostUnbounded)
	require.Zero(t, network.Load())
	require.Zero(t, marked.Load(), "the hold remains undispatched and may be refunded")
}

func TestFundingRejectionWithUsageIsNotRefundable(t *testing.T) {
	resp := &http.Response{StatusCode: 400, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error"},"usage":{"input_tokens":10}}`))}
	require.False(t, fundingResponseProvesNoCharge(resp))
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(data), "input_tokens")
}
