package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	hcplugin "github.com/hashicorp/go-plugin"
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

type operationRecordingHTTPUpstream struct {
	HTTPUpstream
	request     *http.Request
	proxyURL    string
	accountID   int64
	concurrency int
}

func (u *operationRecordingHTTPUpstream) Do(request *http.Request, proxyURL string, accountID int64, concurrency int) (*http.Response, error) {
	u.request, u.proxyURL, u.accountID, u.concurrency = request, proxyURL, accountID, concurrency
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("HTTP response"))}, nil
}

func operationPluginGateway(upstream HTTPUpstream, api pluginv1.TransportPluginClient) *OpenAIGatewayService {
	manager := &PluginManager{}
	manager.route.Store(&pluginRoute{
		pluginID:       1,
		rolloutPercent: 100,
		runtime:        &pluginRuntime{client: &hcplugin.Client{}, api: api},
	})
	return &OpenAIGatewayService{pluginManager: manager, httpUpstream: upstream}
}

func TestOpenAIPluginTransportOperationUsesHTTP(t *testing.T) {
	plugin, upstream := &operationPluginClient{}, &operationRecordingHTTPUpstream{}
	gateway := operationPluginGateway(upstream, plugin)
	account := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 3}
	ctx := WithDesktopInferenceOperation(t.Context(), "operation")
	request, err := http.NewRequestWithContext(context.WithoutCancel(ctx), http.MethodPost, "https://example.com/v1/responses", strings.NewReader("oauth payload"))
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer fixture-oauth-token")
	request.Header.Set("ChatGPT-Account-ID", "fixture-chatgpt-account")
	response, err := gateway.doOpenAIUpstream(request, "http://fixture-proxy:8080", account)
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, "HTTP response", string(body))
	require.Same(t, request, upstream.request)
	require.Equal(t, "operation", DesktopInferenceOperationID(upstream.request.Context()))
	require.Equal(t, "Bearer fixture-oauth-token", upstream.request.Header.Get("Authorization"))
	require.Equal(t, "fixture-chatgpt-account", upstream.request.Header.Get("ChatGPT-Account-ID"))
	body, err = io.ReadAll(upstream.request.Body)
	require.NoError(t, err)
	require.NoError(t, upstream.request.Body.Close())
	require.Equal(t, "oauth payload", string(body))
	require.Equal(t, "http://fixture-proxy:8080", upstream.proxyURL)
	require.Equal(t, account.ID, upstream.accountID)
	require.Equal(t, account.Concurrency, upstream.concurrency)
	require.Zero(t, plugin.calls.Load(), "plugin protocol cannot enforce operation replay policy")
}

func TestOpenAIPluginTransportOrdinaryRequestStillUsesPlugin(t *testing.T) {
	plugin, upstream := &operationPluginClient{}, &operationRecordingHTTPUpstream{}
	gateway := operationPluginGateway(upstream, plugin)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://example.com/v1/responses", strings.NewReader("payload"))
	require.NoError(t, err)
	response, err := gateway.doOpenAIUpstream(request, "", &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1})
	require.NoError(t, err)
	_, err = io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.EqualValues(t, 1, plugin.calls.Load())
	require.Nil(t, upstream.request)
}

func TestOpenAIPluginTransportFundingOperationUsesHTTP(t *testing.T) {
	plugin, upstream := &operationPluginClient{}, &operationRecordingHTTPUpstream{}
	gateway := operationPluginGateway(upstream, plugin)
	ctx := WithFundingDispatchGuard(t.Context(), "funding-operation", func(context.Context) error { return nil }, nil)
	request, err := http.NewRequestWithContext(context.WithoutCancel(ctx), http.MethodPost, "https://example.com/v1/responses", strings.NewReader("payload"))
	require.NoError(t, err)
	response, err := gateway.doOpenAIUpstream(request, "", &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth})
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Same(t, request, upstream.request)
	require.Zero(t, plugin.calls.Load(), "funding dispatch policy must also apply to non-desktop calls")
}
