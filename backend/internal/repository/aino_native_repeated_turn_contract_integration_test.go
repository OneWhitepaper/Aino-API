//go:build integration && nativeconsumer

package repository_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAinoNativeReadProtocolPairsUniqueCallsAcrossTurns(t *testing.T) {
	protocol := &ainoNativeProtocol{rig: &ainoPlatformFixture{runID: "repeated-native-read"}, path: "/tmp/fixture-read.txt", content: "fixture-file-content"}
	messages := []any{}
	request := func() map[string]any {
		t.Helper()
		raw, err := json.Marshal(map[string]any{"model": "fixture-tool-model", "messages": messages, "tools": []any{map[string]any{"function": map[string]any{"name": "read_file", "parameters": map[string]any{"properties": map[string]any{"path": map[string]any{}}}}}}})
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer fixture-upstream-key")
		response := httptest.NewRecorder()
		protocol.ServeHTTP(response, req)
		require.Equal(t, 200, response.Code, response.Body.String())
		var reply struct {
			Choices []struct{ Message map[string]any }
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &reply))
		require.Len(t, reply.Choices, 1)
		return reply.Choices[0].Message
	}
	previous := ""
	for range 2 {
		messages = append(messages, map[string]any{"role": "user", "content": "Read the fixture again"})
		assistant := request()
		calls, ok := assistant["tool_calls"].([]any)
		require.True(t, ok)
		require.Len(t, calls, 1)
		call, ok := calls[0].(map[string]any)
		require.True(t, ok)
		id, ok := call["id"].(string)
		require.True(t, ok)
		require.NotEqual(t, previous, id, "each native turn needs a fresh provider tool ID")
		messages = append(messages, assistant, map[string]any{"role": "tool", "tool_call_id": id, "content": protocol.content})
		final := request()
		require.Equal(t, "Verified "+protocol.content, final["content"])
		messages = append(messages, final)
		previous = id
	}
	require.Equal(t, int64(2), protocol.toolResults.Load())
}
