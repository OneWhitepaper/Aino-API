package service_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type operationPluginClient struct {
	pluginv1.TransportPluginClient
	calls atomic.Int32
}

func (p *operationPluginClient) Forward(ctx context.Context, _ ...grpc.CallOption) (grpc.BidiStreamingClient[pluginv1.ForwardRequest, pluginv1.ForwardResponse], error) {
	p.calls.Add(1)
	return &operationPluginStream{ctx: ctx}, nil
}

type operationPluginStream struct {
	grpc.ClientStream
	ctx  context.Context
	read bool
}

func (s *operationPluginStream) Send(*pluginv1.ForwardRequest) error { return nil }
func (s *operationPluginStream) CloseSend() error                    { return nil }
func (s *operationPluginStream) Context() context.Context            { return s.ctx }
func (s *operationPluginStream) Recv() (*pluginv1.ForwardResponse, error) {
	if s.read {
		return nil, io.EOF
	}
	s.read = true
	return &pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_Start{Start: &pluginv1.ForwardResponseStart{StatusCode: http.StatusOK}}}, nil
}

func TestOpenAIPluginTransportOperationUsesHTTPWithoutReplay(t *testing.T) {
	var providerCalls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls.Add(1)
		require.Equal(t, "Bearer fixture-oauth-token", r.Header.Get("Authorization"))
		require.Equal(t, "fixture-chatgpt-account", r.Header.Get("ChatGPT-Account-ID"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, "oauth payload", string(body))
		_, _ = io.WriteString(w, "oauth HTTP response")
	}))
	t.Cleanup(provider.Close)
	plugin := &operationPluginClient{}
	dispatch := service.OpenAIPluginTransportForTest(repository.NewHTTPUpstream(nil), plugin)
	account := &service.Account{ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Concurrency: 1}
	ctx := service.WithDesktopInferenceOperation(t.Context(), "operation")
	request := func() *http.Request {
		req, err := http.NewRequestWithContext(context.WithoutCancel(ctx), http.MethodPost, provider.URL, strings.NewReader("oauth payload"))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer fixture-oauth-token")
		req.Header.Set("ChatGPT-Account-ID", "fixture-chatgpt-account")
		return req
	}
	response, err := dispatch(request(), "", account)
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, "oauth HTTP response", string(body))
	response, err = dispatch(request(), "", account)
	require.ErrorIs(t, err, service.ErrDesktopOperationUnknown)
	require.Nil(t, response)
	require.EqualValues(t, 1, providerCalls.Load())
	require.Zero(t, plugin.calls.Load(), "plugin protocol cannot enforce operation replay policy")
}

func TestOpenAIPluginTransportOrdinaryRequestStillUsesPlugin(t *testing.T) {
	var providerCalls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { providerCalls.Add(1) }))
	t.Cleanup(provider.Close)
	plugin := &operationPluginClient{}
	dispatch := service.OpenAIPluginTransportForTest(repository.NewHTTPUpstream(nil), plugin)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, provider.URL, strings.NewReader("payload"))
	require.NoError(t, err)
	response, err := dispatch(request, "", &service.Account{ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Concurrency: 1})
	require.NoError(t, err)
	_, err = io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.EqualValues(t, 1, plugin.calls.Load())
	require.Zero(t, providerCalls.Load())
}
