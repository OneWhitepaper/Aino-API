//go:build integration && nativeconsumer

package repository_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const nativeImageCatalogID = "fixture-image-model"
const nativeImageUpstreamModel = "gpt-image-fixture"

type ainoNativeImageRequest struct {
	Path          string `json:"path"`
	Model         string `json:"model"`
	Quality       string `json:"quality"`
	Purpose       string `json:"purpose"`
	Size          string `json:"size"`
	InputImages   int    `json:"input_images"`
	SourceMatches bool   `json:"source_matches"`
	OutputSHA256  string `json:"output_sha256"`
	OutputWidth   int    `json:"output_width"`
	OutputHeight  int    `json:"output_height"`
}

type ainoNativeImageState struct {
	mu                                            sync.Mutex
	requests                                      []ainoNativeImageRequest
	purposeByBody                                 map[[32]byte]string
	generatedPath                                 string
	protocol                                      []ainoNativeImageProtocolEvidence
	generationRequests, editRequests, toolResults atomic.Int64
}

func (s *ainoNativeImageState) snapshot() []ainoNativeImageRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ainoNativeImageRequest{}, s.requests...)
}

func (p *ainoNativeProtocol) configureNativeImageCatalog(t *testing.T, r *ainoPlatformFixture, group *service.Group) service.DesktopModelEntry {
	t.Helper()
	price := 0.04
	group.AllowImageGeneration = true
	group.ModelAllowlist.Models = append(group.ModelAllowlist.Models, nativeImageUpstreamModel)
	group.ModelPricing = append(group.ModelPricing, service.ChannelModelPricing{Models: []string{nativeImageUpstreamModel}, BillingMode: service.BillingModeImage, PerRequestPrice: &price})
	before, beforeErr := r.settingRepo.GetValue(r.ctx, "desktop.default_image_model_id")
	t.Cleanup(func() {
		if beforeErr != nil {
			_ = r.settingRepo.Delete(r.ctx, "desktop.default_image_model_id")
		} else {
			_ = r.settingRepo.Set(r.ctx, "desktop.default_image_model_id", before)
		}
	})
	require.NoError(t, r.settingRepo.Set(r.ctx, "desktop.default_image_model_id", nativeImageCatalogID))
	r.nativeImageObserver = p.observeNativeImageRequest
	return service.DesktopModelEntry{ID: nativeImageCatalogID, GroupID: group.ID, Model: nativeImageUpstreamModel, DisplayName: "Fixture Image", ProviderLabel: "Fixture", Platform: service.PlatformOpenAI, APIMode: "images_generations", Capabilities: service.PlatformModelCapabilities{ImageGeneration: true, ImageQualityOptions: []string{"auto", "high"}}}
}

// Record the purpose from the real authenticated managed lease, correlated by
// canonical body digest because private attribution must never reach the provider.
func (p *ainoNativeProtocol) observeNativeImageRequest(c *gin.Context) {
	key, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || !key.DesktopManaged || key.DesktopPurpose != service.DesktopPurposeImage {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	digest, err := nativeImageBodyDigest(body, c.GetHeader("Content-Type"))
	if err != nil {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	p.images.mu.Lock()
	if p.images.purposeByBody == nil {
		p.images.purposeByBody = make(map[[32]byte]string)
	}
	p.images.purposeByBody[digest] = key.DesktopPurpose
	p.images.mu.Unlock()
	c.Next()
}

// Production rewrites the model and multipart boundary before proxying. Hash
// canonical JSON or ordered part content so that transport formatting is ignored.
func nativeImageBodyDigest(raw []byte, contentType string) ([32]byte, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return [32]byte{}, err
	}
	if mediaType != "multipart/form-data" {
		var body any
		if err := json.Unmarshal(raw, &body); err != nil {
			return [32]byte{}, err
		}
		canonical, err := json.Marshal(body)
		return sha256.Sum256(canonical), err
	}
	var canonical bytes.Buffer
	reader := multipart.NewReader(bytes.NewReader(raw), params["boundary"])
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return sha256.Sum256(canonical.Bytes()), nil
		}
		if err != nil {
			return [32]byte{}, err
		}
		_, _ = fmt.Fprintf(&canonical, "%s\x00%s\x00", part.FormName(), part.FileName())
		_, err = io.Copy(&canonical, part)
		_ = part.Close()
		if err != nil {
			return [32]byte{}, err
		}
		canonical.WriteByte(0)
	}
}

func nativeFixtureImage(edited bool) *image.NRGBA {
	canvas := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			pixel := color.NRGBA{R: 0x22, G: 0x99, B: 0xdd, A: 0xff}
			if x >= 16 && y >= 16 {
				pixel = color.NRGBA{R: 0xee, G: 0xbb, B: 0x44, A: 0xff}
			}
			if edited {
				pixel.R, pixel.B = pixel.B, pixel.R
			}
			canvas.SetNRGBA(x, y, pixel)
		}
	}
	return canvas
}

func nativeImagePixelsMatch(raw []byte, edited bool) bool {
	actual, err := png.Decode(bytes.NewReader(raw))
	if err != nil || actual.Bounds().Dx() != 32 || actual.Bounds().Dy() != 32 {
		return false
	}
	expected := nativeFixtureImage(edited)
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			if color.NRGBAModel.Convert(actual.At(x+actual.Bounds().Min.X, y+actual.Bounds().Min.Y)) != expected.At(x, y) {
				return false
			}
		}
	}
	return true
}

func (p *ainoNativeProtocol) serveNativeImage(w http.ResponseWriter, req *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(req.Body, 8<<20))
	if err != nil || req.Method != http.MethodPost {
		http.Error(w, "invalid native image request", http.StatusBadRequest)
		return
	}
	digest, err := nativeImageBodyDigest(raw, req.Header.Get("Content-Type"))
	if err != nil {
		http.Error(w, "invalid native image body", http.StatusBadRequest)
		return
	}
	p.images.mu.Lock()
	purpose := p.images.purposeByBody[digest]
	p.images.mu.Unlock()
	entry := ainoNativeImageRequest{Path: req.URL.Path, Purpose: purpose, SourceMatches: true, OutputWidth: 32, OutputHeight: 32}
	edited := strings.HasSuffix(req.URL.Path, "/edits")
	var prompt string
	if edited {
		req.Body = io.NopCloser(bytes.NewReader(raw))
		if err := req.ParseMultipartForm(8 << 20); err != nil {
			http.Error(w, "native edit requires multipart PNG", http.StatusBadRequest)
			return
		}
		defer req.MultipartForm.RemoveAll()
		entry.Model, entry.Quality, entry.Size = req.FormValue("model"), req.FormValue("quality"), req.FormValue("size")
		prompt = req.FormValue("prompt")
		for _, parts := range req.MultipartForm.File {
			for _, part := range parts {
				file, openErr := part.Open()
				if openErr != nil {
					entry.SourceMatches = false
					continue
				}
				source, readErr := io.ReadAll(io.LimitReader(file, 1<<20))
				_ = file.Close()
				entry.InputImages++
				entry.SourceMatches = entry.SourceMatches && readErr == nil && nativeImagePixelsMatch(source, false)
			}
		}
		entry.SourceMatches = entry.SourceMatches && entry.InputImages == 1
	} else {
		var body struct {
			Model, Prompt, Quality, Size string
			N                            int
		}
		if json.Unmarshal(raw, &body) != nil || body.N != 1 {
			http.Error(w, "native generation requires one image", http.StatusBadRequest)
			return
		}
		entry.Model, entry.Quality, entry.Size, prompt = body.Model, body.Quality, body.Size, body.Prompt
	}
	marker := "fixture-image-generate"
	if edited {
		marker = "fixture-image-edit"
	}
	if entry.Purpose != service.DesktopPurposeImage || entry.Model != nativeImageUpstreamModel || entry.Quality != "auto" || !strings.Contains(prompt, marker) || !entry.SourceMatches {
		http.Error(w, "native image protocol mismatch", http.StatusBadRequest)
		return
	}
	var encoded bytes.Buffer
	if png.Encode(&encoded, nativeFixtureImage(edited)) != nil {
		http.Error(w, "synthetic PNG encoding failed", http.StatusInternalServerError)
		return
	}
	digest = sha256.Sum256(encoded.Bytes())
	entry.OutputSHA256 = hex.EncodeToString(digest[:])
	p.images.mu.Lock()
	p.images.requests = append(p.images.requests, entry)
	p.images.mu.Unlock()
	if edited {
		p.images.editRequests.Add(1)
	} else {
		p.images.generationRequests.Add(1)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("x-request-id", fmt.Sprintf("fixture-image-%s-%d", p.rig.runID, len(p.images.snapshot())))
	_ = json.NewEncoder(w).Encode(map[string]any{"created": time.Now().Unix(), "data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(encoded.Bytes())}}})
}

func (p *ainoNativeProtocol) serveNativeImageTool(w http.ResponseWriter, body *ainoNativeChatRequest) bool {
	var lastUser string
	for _, message := range body.Messages {
		if message.Role == "user" {
			lastUser = string(message.Content)
		}
	}
	edited := strings.Contains(lastUser, "fixture-image-edit")
	if !edited && !strings.Contains(lastUser, "fixture-image-generate") {
		return false
	}
	props := map[string]any{}
	for _, tool := range body.Tools {
		if tool.Function.Name == "image_generate" {
			props, _ = tool.Function.Parameters["properties"].(map[string]any)
		}
	}
	deferred := props["prompt"] == nil
	if deferred {
		var handled bool
		props, handled = p.discoverNativeImageTool(w, body, edited)
		if handled {
			return true
		}
	}
	if props["prompt"] == nil || edited && props["image_url"] == nil {
		p.recordNativeImageProtocol(body, "image_schema_missing")
		http.Error(w, "actual image_generate definition missing", http.StatusBadRequest)
		return true
	}
	marker, toolID := "fixture-image-generate", "fixture-native-image-generate"
	arguments := map[string]string{"prompt": marker, "aspect_ratio": "square"}
	if edited {
		marker, toolID = "fixture-image-edit", "fixture-native-image-edit"
		p.images.mu.Lock()
		arguments["image_url"] = p.images.generatedPath
		p.images.mu.Unlock()
		arguments["prompt"] = marker
		if arguments["image_url"] == "" {
			http.Error(w, "native image edit missing actual generated source", http.StatusBadRequest)
			return true
		}
	}
	last := body.Messages[len(body.Messages)-1]
	encodedArguments, _ := json.Marshal(arguments)
	name := "image_generate"
	if deferred {
		name = "tool_call"
		encodedArguments, _ = json.Marshal(map[string]any{"calls": []any{map[string]any{"name": "image_generate", "arguments": arguments}}})
	}
	message := nativeImageFunctionCall(toolID, name, string(encodedArguments))
	finish := "tool_calls"
	if last.Role == "tool" && last.ToolCallID == toolID {
		path, ok := nativeImageToolResultPath(body.Messages, toolID, marker, edited)
		if !ok {
			p.recordNativeImageProtocol(body, "image_result_mismatch")
			http.Error(w, "native image tool roundtrip mismatch", http.StatusBadRequest)
			return true
		}
		p.images.mu.Lock()
		if !edited {
			p.images.generatedPath = path
		}
		p.images.mu.Unlock()
		p.images.toolResults.Add(1)
		label := "Fixture image generated"
		if edited {
			label = "Fixture image edited"
		}
		message, finish = map[string]any{"role": "assistant", "content": label + "\nMEDIA:" + path}, "stop"
	}
	p.recordNativeImageProtocol(body, name+"_"+finish)
	p.writeNativeImageChat(w, body, message, finish)
	return true
}

func nativeImageToolResultPath(messages []ainoNativeMessage, toolID, marker string, edited bool) (string, bool) {
	last := messages[len(messages)-1]
	matched := false
	for i := len(messages) - 2; i >= 0; i-- {
		previous := messages[i]
		if previous.Role == "tool" {
			continue
		}
		if previous.Role == "assistant" {
			for _, call := range previous.ToolCalls {
				var args map[string]string
				if call.ID != toolID {
					continue
				}
				if call.Function.Name == "tool_call" {
					var bridge struct {
						Calls []struct {
							Name      string            `json:"name"`
							Arguments map[string]string `json:"arguments"`
						} `json:"calls"`
					}
					if json.Unmarshal([]byte(call.Function.Arguments), &bridge) == nil && len(bridge.Calls) == 1 && bridge.Calls[0].Name == "image_generate" {
						args = bridge.Calls[0].Arguments
					}
				} else if call.Function.Name == "image_generate" {
					_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
				}
				if args["prompt"] == marker {
					matched = !edited || args["image_url"] != ""
				}
			}
		}
		break
	}
	var content string
	var result struct {
		Success                         bool
		Image, Model, Provider, Quality string
	}
	if !matched || last.ToolCallID != toolID || json.Unmarshal(last.Content, &content) != nil || json.Unmarshal([]byte(content), &result) != nil || !result.Success || result.Provider != "aino" || result.Model != nativeImageCatalogID || result.Quality != "auto" {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(result.Image)
	if err != nil || !filepath.IsAbs(resolved) {
		return "", false
	}
	temp, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil || !strings.HasPrefix(resolved, temp+string(os.PathSeparator)) {
		return "", false
	}
	raw, err := os.ReadFile(resolved)
	return result.Image, err == nil && nativeImagePixelsMatch(raw, edited)
}

func (p *ainoNativeProtocol) writeNativeImageChat(w http.ResponseWriter, body *ainoNativeChatRequest, message map[string]any, finish string) {
	call := p.rig.modelCalls.Add(1)
	w.Header().Set("x-request-id", fmt.Sprintf("fixture-%s-%d", p.rig.runID, call))
	usage := map[string]int{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
	if !body.Stream {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": fmt.Sprintf("fixture-%d", call), "object": "chat.completion", "created": time.Now().Unix(), "model": body.Model, "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}}, "usage": usage})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "fixture streaming unsupported", http.StatusInternalServerError)
		return
	}
	if calls, ok := message["tool_calls"].([]any); ok {
		calls[0].(map[string]any)["index"] = 0
	}
	for _, chunk := range []struct {
		delta  map[string]any
		reason any
		usage  any
	}{{message, nil, nil}, {map[string]any{}, finish, usage}} {
		blob, _ := json.Marshal(map[string]any{"id": fmt.Sprintf("fixture-%d", call), "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": body.Model, "choices": []any{map[string]any{"index": 0, "delta": chunk.delta, "finish_reason": chunk.reason}}, "usage": chunk.usage})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", blob)
		flusher.Flush()
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}
