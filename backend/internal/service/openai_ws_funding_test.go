package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func TestWebSocketFundingSessionIncludesInheritedTools(t *testing.T) {
	state := make(openAIWSFundingSessionState)
	state.observe([]byte(`{"type":"session.update","session":{"tools":[{"type":"web_search"}],"instructions":"inherited","max_tool_calls":2}}`))
	payload, err := state.apply([]byte(`{"type":"response.create","model":"m","max_tool_calls":1}`))
	require.NoError(t, err)
	require.Equal(t, "web_search", gjson.GetBytes(payload, "tools.0.type").String())
	require.Equal(t, "inherited", gjson.GetBytes(payload, "instructions").String())
	require.Equal(t, int64(1), gjson.GetBytes(payload, "max_tool_calls").Int())
}

func TestWebSocketFundingSnapshotPreservesTransportDeadline(t *testing.T) {
	snapshot := WithFundingDispatchGuard(context.Background(), "snapshot", func(context.Context) error { return nil }, nil)
	transport, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ctx, err := openAIWSFundingDispatchContext(transport, &OpenAIWSIngressHooks{RequestContext: func(int, []byte) (context.Context, error) { return snapshot, nil }}, 1, nil, false)
	require.NoError(t, err)
	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	expected, _ := transport.Deadline()
	require.Equal(t, expected, deadline)
	require.Equal(t, "snapshot", FundingDispatchRequestID(ctx))
	cancel()
	require.ErrorIs(t, ctx.Err(), context.Canceled)
}

func TestPassthroughFundingPreparesAndDispatchesEveryTurnOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controlCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	upstream := newStagedPassthroughConn()
	upstream.Send(`{"type":"response.completed","response":{"id":"funding-1","usage":{"input_tokens":1,"output_tokens":1}}}`)
	var mu sync.Mutex
	contexts := map[int]context.Context{}
	prepared := []int{}
	dispatched := []int{}
	hooks := &OpenAIWSIngressHooks{
		PrepareRequest: func(turn int, payload []byte, _ string) ([]byte, error) {
			mu.Lock()
			defer mu.Unlock()
			prepared = append(prepared, turn)
			contexts[turn] = WithFundingDispatchGuard(controlCtx, "turn", func(context.Context) error {
				mu.Lock()
				defer mu.Unlock()
				dispatched = append(dispatched, turn)
				return nil
			}, nil)
			return sjson.SetBytes(payload, "max_output_tokens", turn*10)
		},
		RequestContext: func(turn int, _ []byte) (context.Context, error) {
			mu.Lock()
			defer mu.Unlock()
			return contexts[turn], nil
		},
	}
	server, _ := startPassthroughHookRecordingServer(t, controlCtx, newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream), passthroughLifecycleAccount(), hooks)
	defer server.Close()
	client := dialPassthroughLifecycleClient(t, server)
	defer client.CloseNow()
	require.Equal(t, int64(10), gjson.GetBytes(requirePassthroughUpstreamWrite(t, upstream, time.Second), "max_output_tokens").Int())
	_, err := readPassthroughLifecycleFrame(t, client, 3*time.Second)
	require.NoError(t, err)
	require.NoError(t, client.Write(controlCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1"}`)))
	require.Equal(t, int64(20), gjson.GetBytes(requirePassthroughUpstreamWrite(t, upstream, time.Second), "max_output_tokens").Int())
	upstream.Send(`{"type":"response.completed","response":{"id":"funding-2","usage":{"input_tokens":1,"output_tokens":1}}}`)
	_, err = readPassthroughLifecycleFrame(t, client, 3*time.Second)
	require.NoError(t, err)
	mu.Lock()
	require.Equal(t, []int{1, 2}, prepared)
	require.Equal(t, []int{1, 2}, dispatched)
	first, second := contexts[1], contexts[2]
	mu.Unlock()
	require.ErrorIs(t, BeginFundingDispatch(first), ErrTeamFundingConflict)
	require.ErrorIs(t, BeginFundingDispatch(second), ErrTeamFundingConflict)
}

func TestGrokRealtimeAbnormalCloseRetainsObservedUsage(t *testing.T) {
	upstream := newStagedPassthroughConn()
	upstream.Send(`{"type":"response.output_audio.delta","audio":"YWJj"}`)
	upstream.Fail(io.ErrUnexpectedEOF)
	type outcome struct {
		audio bool
		err   error
	}
	finished := make(chan outcome, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client, err := coderws.Accept(w, r, nil)
		if err != nil {
			finished <- outcome{err: err}
			return
		}
		defer client.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		audio, err := (&OpenAIGatewayService{}).ProxyGrokRealtimeConn(ctx, nil, client, &GrokRealtimeUpstream{conn: upstream})
		finished <- outcome{audio: audio, err: err}
	}))
	defer server.Close()
	client, _, err := coderws.Dial(t.Context(), "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer client.CloseNow()
	select {
	case result := <-finished:
		require.True(t, result.audio, "abnormal termination must not erase already-observed billable audio")
		require.ErrorIs(t, result.err, io.ErrUnexpectedEOF)
	case <-time.After(2 * time.Second):
		t.Fatal("realtime relay did not stop")
	}
}

func TestGrokRealtimeRelayStopsAtReservedDeadline(t *testing.T) {
	upstream := newStagedPassthroughConn()
	finished := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client, err := coderws.Accept(w, r, nil)
		if err != nil {
			finished <- err
			return
		}
		defer client.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Millisecond)
		defer cancel()
		_, err = (&OpenAIGatewayService{}).ProxyGrokRealtimeConn(ctx, nil, client, &GrokRealtimeUpstream{conn: upstream})
		finished <- err
	}))
	defer server.Close()
	started := time.Now()
	client, _, err := coderws.Dial(t.Context(), "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer client.CloseNow()
	select {
	case err := <-finished:
		require.True(t, errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled), "deadline must terminate both relay directions: %v", err)
		require.Less(t, time.Since(started), time.Second)
	case <-time.After(2 * time.Second):
		t.Fatal("realtime relay outlived its reserved duration")
	}
}
