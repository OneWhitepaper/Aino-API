//go:build integration && nativeconsumer

package repository_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// This catches a native fixture that bypasses managed image-purpose auth or
// fails to wire the production image handler and its settlement worker.
func TestAinoNativeImageGatewayUsesRealLeaseAndSettles(t *testing.T) {
	r := newAinoPlatformFixture(t)
	u := r.user(t, service.StatusActive, false)
	_, err := r.userRepo.SetBalance(r.ctx, u.ID, 10)
	require.NoError(t, err)
	protocol := &ainoNativeProtocol{rig: r}
	r.modelProvider = protocol
	r.wireModel(t, u.ID)
	r.start(t)
	pair, err := r.auth.GenerateTokenPair(r.ctx, u, "")
	require.NoError(t, err)
	device, grant := uuid.NewString(), uuid.NewString()
	issue := func(id, purpose string) service.DesktopCredentialResponse {
		body, marshalErr := json.Marshal(map[string]string{"device_id": device, "connection_grant_id": grant, "model_id": id, "purpose": purpose})
		require.NoError(t, marshalErr)
		status, data := r.http(t, http.MethodPost, "/api/v1/desktop/credentials", string(body), pair.AccessToken, nil)
		require.Equal(t, http.StatusOK, status, "real fixture lease issuance failed")
		return fixtureDecode[service.DesktopCredentialResponse](t, data)
	}
	chat, image := issue("fixture-tool-model", "chat"), issue("fixture-image-model", "image")
	require.Equal(t, "image", image.Purpose)
	session, turn := uuid.NewString(), uuid.NewString()
	headers := map[string]string{"X-Aino-Session-Id": session, "X-Aino-Turn-Id": turn, "X-Aino-Call-Id": uuid.NewString(), "X-Aino-Purpose": "image"}
	generate := `{"model":"gpt-image-fixture","prompt":"fixture-image-generate","quality":"auto","size":"1024x1024","n":1}`
	status, _ := r.http(t, http.MethodPost, "/v1/images/generations", generate, chat.APIKey, headers)
	require.Equal(t, http.StatusForbidden, status)
	status, _ = r.http(t, http.MethodPost, "/v1/images/generations", `{"model":"gpt-image-forbidden","prompt":"fixture-image-generate"}`, image.APIKey, headers)
	require.Equal(t, http.StatusNotFound, status)
	status, data := r.http(t, http.MethodPost, "/v1/images/generations", generate, image.APIKey, headers)
	require.Equal(t, http.StatusOK, status, string(data))
	var generated struct {
		Data []struct {
			Base64 string `json:"b64_json"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(data, &generated))
	require.Len(t, generated.Data, 1)
	imageBytes, err := base64.StdEncoding.DecodeString(generated.Data[0].Base64)
	require.NoError(t, err)
	decoded, err := png.Decode(bytes.NewReader(imageBytes))
	require.NoError(t, err)
	require.Equal(t, 32, decoded.Bounds().Dx())
	red, green, blue, alpha := decoded.At(0, 0).RGBA()
	require.Equal(t, []uint32{0x2222, 0x9999, 0xdddd, 0xffff}, []uint32{red, green, blue, alpha})
	var edit bytes.Buffer
	writer := multipart.NewWriter(&edit)
	for key, value := range map[string]string{"model": "gpt-image-fixture", "prompt": "fixture-image-edit", "quality": "auto", "size": "1024x1024", "n": "1"} {
		require.NoError(t, writer.WriteField(key, value))
	}
	part, err := writer.CreateFormFile("image", "generated.png")
	require.NoError(t, err)
	_, err = part.Write(imageBytes)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	request, err := http.NewRequest(http.MethodPost, r.server.URL+"/v1/images/edits", &edit)
	require.NoError(t, err)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", "Bearer "+image.APIKey)
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	request.Header.Set("X-Aino-Call-Id", uuid.NewString())
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	data, err = io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode, string(data))
	var count int
	var cost string
	require.Eventually(t, func() bool {
		err := repository.GetIntegrationDB().QueryRowContext(r.ctx, "SELECT count(*),coalesce(sum(actual_cost),0)::text FROM usage_logs WHERE user_id=$1 AND desktop_turn_id=$2 AND desktop_purpose='image' AND settlement_status='settled'", u.ID, turn).Scan(&count, &cost)
		return err == nil && count == 2
	}, 10*time.Second, 20*time.Millisecond)
	charge, err := strconv.ParseFloat(cost, 64)
	require.NoError(t, err)
	require.InDelta(t, 0.16, charge, 1e-8, "2 images × per-request price0.04 × group rate2")
}

// The fixture must follow the advertised bridge rather than require a deferred
// tool to appear directly in the model's frozen schema list.
func TestAinoNativeImageProtocolUsesAdvertisedDiscoveryBridge(t *testing.T) {
	protocol := &ainoNativeProtocol{rig: &ainoPlatformFixture{runID: "native-image-discovery-contract"}}
	messages := []any{map[string]any{"role": "user", "content": "fixture-image-generate"}}
	tools := []any{}
	for name, property := range map[string]string{"tool_search": "queries", "tool_describe": "names", "tool_call": "calls"} {
		tools = append(tools, map[string]any{"function": map[string]any{"name": name, "parameters": map[string]any{"properties": map[string]any{property: map[string]any{}}}}})
	}
	request := func(wantName string) map[string]any {
		encoded, err := json.Marshal(map[string]any{"model": "fixture-tool-model", "messages": messages, "tools": tools})
		require.NoError(t, err)
		var body ainoNativeChatRequest
		require.NoError(t, json.Unmarshal(encoded, &body))
		recorder := httptest.NewRecorder()
		require.True(t, protocol.serveNativeImageTool(recorder, &body))
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		var result struct {
			Choices []struct {
				Message map[string]any `json:"message"`
			} `json:"choices"`
		}
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
		require.Len(t, result.Choices, 1)
		message := result.Choices[0].Message
		call := message["tool_calls"].([]any)[0].(map[string]any)
		require.Equal(t, wantName, call["function"].(map[string]any)["name"])
		messages = append(messages, message)
		return call
	}
	search := request("tool_search")
	messages = append(messages, map[string]any{"role": "tool", "tool_call_id": search["id"], "content": `{"results":[{"query":"image_generate","matches":["image_generate"]}],"tools":{"image_generate":{"description":"Generate images"}}}`})
	describe := request("tool_describe")
	messages = append(messages, map[string]any{"role": "tool", "tool_call_id": describe["id"], "content": `{"tools":{"image_generate":{"description":"Generate images","parameters":{"type":"object","properties":{"prompt":{"type":"string"},"image_url":{"type":"string"}}}}}}`})
	call := request("tool_call")
	var invocation struct {
		Calls []struct {
			Name      string            `json:"name"`
			Arguments map[string]string `json:"arguments"`
		} `json:"calls"`
	}
	require.NoError(t, json.Unmarshal([]byte(call["function"].(map[string]any)["arguments"].(string)), &invocation))
	require.Len(t, invocation.Calls, 1)
	require.Equal(t, "image_generate", invocation.Calls[0].Name)
	require.Equal(t, "fixture-image-generate", invocation.Calls[0].Arguments["prompt"])
}

func TestAinoNativeImageProtocolFinishesOnlyKnownCatalogs(t *testing.T) {
	var image bytes.Buffer
	require.NoError(t, png.Encode(&image, nativeFixtureImage(false)))
	path := filepath.Join(t.TempDir(), "generated.png")
	require.NoError(t, os.WriteFile(path, image.Bytes(), 0600))
	for _, test := range []struct {
		id     string
		status int
	}{
		{nativeImageCatalogID, 200}, {"fixture-image-subscription", 200}, {"untrusted-image-catalog", 400},
	} {
		t.Run(test.id, func(t *testing.T) {
			result, err := json.Marshal(map[string]any{"success": true, "provider": "aino", "model": test.id, "quality": "auto", "image": path})
			require.NoError(t, err)
			wire, err := json.Marshal(map[string]any{"model": "fixture-tool-model", "messages": []any{
				map[string]any{"role": "user", "content": "fixture-image-generate"},
				nativeImageFunctionCall("fixture-native-image-generate", "image_generate", `{"prompt":"fixture-image-generate"}`),
				map[string]any{"role": "tool", "tool_call_id": "fixture-native-image-generate", "content": string(result)},
			}, "tools": []any{map[string]any{"function": map[string]any{"name": "image_generate", "parameters": map[string]any{"properties": map[string]any{"prompt": map[string]any{}}}}}}})
			require.NoError(t, err)
			var request ainoNativeChatRequest
			require.NoError(t, json.Unmarshal(wire, &request))
			protocol := &ainoNativeProtocol{rig: &ainoPlatformFixture{runID: "known-image-catalog-contract"}}
			response := httptest.NewRecorder()
			require.True(t, protocol.serveNativeImageTool(response, &request))
			require.Equal(t, test.status, response.Code, response.Body.String())
			if test.status == 200 {
				require.Contains(t, response.Body.String(), "Fixture image generated")
				require.Contains(t, response.Body.String(), path)
			}
		})
	}
}
