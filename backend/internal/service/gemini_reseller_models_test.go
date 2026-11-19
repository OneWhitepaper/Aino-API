//go:build unit

package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const geminiResellerTestKey = "reseller-secret-key"
const geminiResellerTestResponse = `{"candidates":[{"content":{"role":"model","parts":[{"text":"relay hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":11,"candidatesTokenCount":7,"totalTokenCount":18}}`

func geminiResellerTestAccount(baseURL string) *Account {
	return &Account{
		ID: 989, Platform: PlatformGemini, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{"api_key": geminiResellerTestKey, "base_url": baseURL},
	}
}

func requireGeminiResellerRequest(t *testing.T, req *http.Request, wantURI string) {
	t.Helper()
	require.Equal(t, wantURI, req.URL.RequestURI())
	require.Equal(t, geminiResellerTestKey, req.Header.Get("x-goog-api-key"))
	require.Empty(t, req.Header.Get("Authorization"))
	require.NotContains(t, req.URL.String(), geminiResellerTestKey)
	require.Empty(t, req.URL.Query().Get("key"))
}

func TestGeminiResellerAccountTestRequestPreservesModelAndEncodesURI(t *testing.T) {
	svc := &AccountTestService{cfg: &config.Config{}}
	payload := []byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)
	for _, tt := range []struct {
		model   string
		wantURI string
	}{
		{"gemini-4-argon[次]", "/relay/v1beta/models/gemini-4-argon%5B%E6%AC%A1%5D:streamGenerateContent?alt=sse"},
		{"gemini-4-carbon[次]", "/relay/v1beta/models/gemini-4-carbon%5B%E6%AC%A1%5D:streamGenerateContent?alt=sse"},
		{"gemini-3.8-flash-high[次]", "/relay/v1beta/models/gemini-3.8-flash-high%5B%E6%AC%A1%5D:streamGenerateContent?alt=sse"},
		{"[次]gemini-4-neon", "/relay/v1beta/models/%5B%E6%AC%A1%5Dgemini-4-neon:streamGenerateContent?alt=sse"},
	} {
		t.Run(tt.model, func(t *testing.T) {
			req, err := svc.buildGeminiAPIKeyRequest(context.Background(), geminiResellerTestAccount("https://relay.example.com/relay"), tt.model, payload)
			require.NoError(t, err)
			require.Equal(t, http.MethodPost, req.Method)
			require.Equal(t, "/relay/v1beta/models/"+tt.model+":streamGenerateContent", req.URL.Path)
			requireGeminiResellerRequest(t, req, tt.wantURI)
			require.Equal(t, "alt=sse", req.URL.RawQuery)
			gotPayload, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.Equal(t, payload, gotPayload)
			require.NoError(t, req.Body.Close())
		})
	}
}

// Only the external transport is replaced: requests still cross net/http into a
// local server, so RequestURI verifies the actual escaped bytes on the wire.
type geminiResellerLocalUpstream struct{ client *http.Client }

func (u *geminiResellerLocalUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.client.Do(req)
}

func (u *geminiResellerLocalUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}

type geminiResellerReceivedRequest struct {
	request *http.Request
	body    []byte
}

func newGeminiResellerLocalService(t *testing.T, response string) (*GeminiMessagesCompatService, *Account, <-chan geminiResellerReceivedRequest) {
	t.Helper()
	received := make(chan geminiResellerReceivedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		received <- geminiResellerReceivedRequest{request: r.Clone(context.Background()), body: body}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "reseller-request-1")
		_, _ = io.WriteString(w, response)
	}))
	t.Cleanup(server.Close)
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	return &GeminiMessagesCompatService{cfg: cfg, httpUpstream: &geminiResellerLocalUpstream{client: server.Client()}},
		geminiResellerTestAccount(server.URL + "/relay"), received
}

func TestGeminiResellerForwardingPreservesUsageAndMappedModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		name        string
		model       string
		clientModel string
		wantURI     string
		textPath    string
	}{
		{"native", "gemini-4-argon[次]", "gemini-4-argon[次]", "/relay/v1beta/models/gemini-4-argon%5B%E6%AC%A1%5D:generateContent", "candidates.0.content.parts.0.text"},
		{"native_prefix", "[次]gemini-4-neon", "[次]gemini-4-neon", "/relay/v1beta/models/%5B%E6%AC%A1%5Dgemini-4-neon:generateContent", "candidates.0.content.parts.0.text"},
		{"chat_completions_alias", "gemini-4-carbon[次]", "gemini-relay-chat", "/relay/v1beta/models/gemini-4-carbon%5B%E6%AC%A1%5D:generateContent", "choices.0.message.content"},
		{"anthropic_alias", "gemini-3.8-flash-high[次]", "claude-relay-sonnet", "/relay/v1beta/models/gemini-3.8-flash-high%5B%E6%AC%A1%5D:generateContent", "content.0.text"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, account, received := newGeminiResellerLocalService(t, geminiResellerTestResponse)
			if tt.clientModel != tt.model {
				account.Credentials["model_mapping"] = map[string]any{tt.clientModel: tt.model}
			}
			body := []byte(fmt.Sprintf(`{"model":%q,"max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`, tt.clientModel))
			path := "/v1/messages"
			if tt.name == "native" || tt.name == "native_prefix" {
				body = []byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)
				path = "/v1beta/models/" + tt.clientModel + ":generateContent"
			} else if tt.name == "chat_completions_alias" {
				path = "/v1/chat/completions"
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
			var result *ForwardResult
			var err error
			switch tt.name {
			case "native", "native_prefix":
				result, err = svc.ForwardNative(context.Background(), c, account, tt.clientModel, "generateContent", false, body)
			case "chat_completions_alias":
				result, err = svc.ForwardAsChatCompletions(context.Background(), c, account, body)
			case "anthropic_alias":
				result, err = svc.Forward(context.Background(), c, account, body)
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Equal(t, "relay hello", gjson.GetBytes(recorder.Body.Bytes(), tt.textPath).String())
			require.Equal(t, tt.clientModel, result.Model)
			require.Equal(t, tt.model, result.UpstreamModel)
			require.Equal(t, 11, result.Usage.InputTokens)
			require.Equal(t, 7, result.Usage.OutputTokens)
			require.Len(t, received, 1, "the reseller must receive the upstream request")
			req := <-received
			require.Equal(t, http.MethodPost, req.request.Method)
			require.Equal(t, "/relay/v1beta/models/"+tt.model+":generateContent", req.request.URL.Path)
			requireGeminiResellerRequest(t, req.request, tt.wantURI)
			require.Equal(t, tt.wantURI, req.request.RequestURI)
			require.Empty(t, req.request.URL.RawQuery)
			require.Equal(t, "hello", gjson.GetBytes(req.body, "contents.0.parts.0.text").String())
		})
	}
}

func TestGeminiResellerForwardGetModelEncodesURI(t *testing.T) {
	const model = "gemini-4-argon[次]"
	const response = `{"name":"models/gemini-4-argon[次]","supportedGenerationMethods":["generateContent"]}`
	svc, account, received := newGeminiResellerLocalService(t, response)

	result, err := svc.ForwardGetModel(context.Background(), account, model)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.StatusCode)
	require.JSONEq(t, response, string(result.Body))
	require.Len(t, received, 1, "the reseller must receive the model lookup")
	req := <-received
	require.Equal(t, http.MethodGet, req.request.Method)
	require.Equal(t, "/relay/v1beta/models/"+model, req.request.URL.Path)
	requireGeminiResellerRequest(t, req.request, "/relay/v1beta/models/gemini-4-argon%5B%E6%AC%A1%5D")
	require.Equal(t, "/relay/v1beta/models/gemini-4-argon%5B%E6%AC%A1%5D", req.request.RequestURI)
	require.Empty(t, req.request.URL.RawQuery)
}

func TestGeminiResellerForwardGetModelRejectsURLInjection(t *testing.T) {
	for _, accountType := range []string{"api_key", "vertex_api_key", "vertex_service_account"} {
		t.Run(accountType, func(t *testing.T) {
			svc, account, received := newGeminiResellerLocalService(t, `{}`)
			if accountType != "api_key" {
				account.Type = AccountTypeServiceAccount
				account.Credentials["project_id"] = "test-project"
				account.Credentials["auth_mode"] = "service_account"
				if accountType == "vertex_api_key" {
					account.Credentials["auth_mode"] = "apikey"
				}
			}
			for _, model := range []string{
				"gemini-4-argon[次]/../../cachedContents",
				"gemini-4-argon%2Fescape",
				"gemini-4-argon[次]:countTokens",
				"gemini-4-argon[次]?key=leak",
				"gemini-4-argon[次]#fragment",
				`gemini-4-argon\escape`,
			} {
				t.Run(model, func(t *testing.T) {
					result, err := svc.ForwardGetModel(context.Background(), account, model)
					require.ErrorContains(t, err, "invalid gemini model")
					require.Nil(t, result)
					require.Empty(t, received, "invalid model must be rejected before contacting the upstream")
				})
			}
		})
	}
}

func TestGeminiResellerModelsGETRequiresFixedCollection(t *testing.T) {
	const response = `{"models":[]}`
	svc, account, received := newGeminiResellerLocalService(t, response)

	result, err := svc.forwardGeminiModelsGET(context.Background(), account, "/v1beta/models")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.StatusCode)
	require.JSONEq(t, response, string(result.Body))
	require.Len(t, received, 1)
	req := <-received
	require.Equal(t, http.MethodGet, req.request.Method)
	requireGeminiResellerRequest(t, req.request, "/relay/v1beta/models")
	require.Equal(t, "/relay/v1beta/models", req.request.RequestURI)

	for _, path := range []string{
		"",
		"/v1beta/cachedContents/cache-1",
		"/v1beta/models-extra",
		"/v1beta/models/",
		"/v1beta/models/gemini-4-argon[次]/extra",
		"/v1beta/models/gemini-4-argon[次]?key=leak",
		"/v1beta/models?alt=sse",
		"//relay.example.com/v1beta/models",
	} {
		t.Run(path, func(t *testing.T) {
			result, err := svc.forwardGeminiModelsGET(context.Background(), account, path)
			require.Error(t, err)
			require.Nil(t, result)
			require.Empty(t, received, "only the models collection and a single model detail path are allowed")
		})
	}
}
