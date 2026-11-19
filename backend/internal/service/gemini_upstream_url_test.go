package service

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildGeminiAIStudioModelActionURL(t *testing.T) {
	const base = "https://generativelanguage.googleapis.com"

	got, err := buildGeminiAIStudioModelActionURL(base, "gemini-2.5-pro", "generateContent", false)
	require.NoError(t, err)
	require.Equal(t, base+"/v1beta/models/gemini-2.5-pro:generateContent", got)

	got, err = buildGeminiAIStudioModelActionURL(base+"/", " gemini-2.5-flash ", "streamGenerateContent", true)
	require.NoError(t, err)
	require.Equal(t, base+"/v1beta/models/gemini-2.5-flash:streamGenerateContent?alt=sse", got)

	got, err = buildGeminiAIStudioModelActionURL(base, "gemini-2.5-pro", "countTokens", false)
	require.NoError(t, err)
	require.Equal(t, base+"/v1beta/models/gemini-2.5-pro:countTokens", got)
}

func TestBuildGeminiAIStudioModelActionURLResellerModels(t *testing.T) {
	for _, tc := range []struct {
		model   string
		escaped string
	}{
		{"gemini-4-argon[次]", "gemini-4-argon%5B%E6%AC%A1%5D"},
		{"gemini-4-carbon[次]", "gemini-4-carbon%5B%E6%AC%A1%5D"},
		{"gemini-3.8-flash-high[次]", "gemini-3.8-flash-high%5B%E6%AC%A1%5D"},
		{"[次]gemini-4-neon", "%5B%E6%AC%A1%5Dgemini-4-neon"},
	} {
		t.Run(tc.model, func(t *testing.T) {
			for _, action := range []string{"generateContent", "streamGenerateContent", "countTokens"} {
				stream := action == "streamGenerateContent"
				got, err := buildGeminiAIStudioModelActionURL("https://relay.example.com/gateway/", tc.model, action, stream)
				require.NoError(t, err)
				require.True(t, IsSafeGeminiModelPathSegment(tc.model))
				wantURI := "/gateway/v1beta/models/" + tc.escaped + ":" + action
				if stream {
					wantURI += "?alt=sse"
				}
				require.Equal(t, "https://relay.example.com"+wantURI, got)
				req, err := http.NewRequest(http.MethodPost, got, nil)
				require.NoError(t, err)
				require.Equal(t, "/gateway/v1beta/models/"+tc.model+":"+action, req.URL.Path)
				require.Equal(t, wantURI, req.URL.RequestURI())
				require.Empty(t, req.URL.Fragment)
			}
			// Responses subpaths retain their stricter ASCII-only guard.
			_, ok := sanitizedUpstreamPathSuffix("/" + tc.model)
			require.False(t, ok)
		})
	}
}

// TestBuildGeminiAIStudioModelActionURLRejectsNonConformingModel 锁定不变式：
// 模型名来自客户端（native 路由的 URL 片段 / compat 路由的请求体），
// 只有合规的路径片段才允许拼进上游 URL。
func TestBuildGeminiAIStudioModelActionURLRejectsNonConformingModel(t *testing.T) {
	const base = "https://generativelanguage.googleapis.com"

	for _, model := range []string{
		"../../x/y",
		"..",
		".",
		"gemini-2.5-pro/../../x",
		`..\..\x`,
		"gemini-2.5-pro?a=b",
		"gemini-2.5-pro#frag",
		"gemini-2.5-pro%2f..",
		"gemini 2.5 pro",
		"gemini\x00pro",
		"gemini-2.5-pro@001",
		"gemini~pro",
		"models/gemini-2.5-pro",
		"...",
		"",
		"   ",
		"gemini[次]/../../x",
		`gemini[次]\x`,
		"gemini[次]?key=secret",
		"gemini[次]#fragment",
		"gemini[次]:countTokens",
		"gemini%5B%E6%AC%A1%5D",
		"gemini[次]%252f..",
		"gemini[次]／x",
		"gemini[次]∕x",
		"gemini[次]\u200b",
		"gemini[次]\u202e",
		"gemini[次]\x00",
		"gemini[次]\xff",
		strings.Repeat("a", maxUpstreamPathSegmentLen+1),
	} {
		t.Run("model_"+model, func(t *testing.T) {
			_, err := buildGeminiAIStudioModelActionURL(base, model, "generateContent", false)
			require.Error(t, err, "model %q must be rejected", model)
			require.False(t, IsSafeGeminiModelPathSegment(model))
		})
	}

	// action 只允许已知取值，避免未来把可变字符串拼进 path。
	_, err := buildGeminiAIStudioModelActionURL(base, "gemini-2.5-pro", "deleteModel", false)
	require.Error(t, err)

	_, err = buildGeminiAIStudioModelActionURL("", "gemini-2.5-pro", "generateContent", false)
	require.Error(t, err)
}
