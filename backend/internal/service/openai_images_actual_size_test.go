package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestDetectOpenAIImageResultSize(t *testing.T) {
	pngEncoded := encodeOpenAIImageTestPNG(t, 1672, 941)
	jpegEncoded := encodeOpenAIImageTestJPEG(t, 640, 360)
	webpVP8XEncoded := encodeOpenAIImageTestWebPVP8X(1920, 1080)
	webpVP8Encoded := encodeOpenAIImageTestWebPVP8(1280, 720)
	webpVP8LEncoded := encodeOpenAIImageTestWebPVP8L(640, 480)

	require.Equal(t, "1672x941", detectOpenAIImageResultSize(pngEncoded))
	require.Equal(t, "1672x941", detectOpenAIImageResultSize(strings.TrimRight(pngEncoded, "=")))
	require.Equal(t, "1672x941", detectOpenAIImageResultSize("data:image/png;base64,"+pngEncoded))
	require.Equal(t, "640x360", detectOpenAIImageResultSize(jpegEncoded))
	require.Equal(t, "1920x1080", detectOpenAIImageResultSize(webpVP8XEncoded))
	require.Equal(t, "1280x720", detectOpenAIImageResultSize(webpVP8Encoded))
	require.Equal(t, "640x480", detectOpenAIImageResultSize(webpVP8LEncoded))
	require.Empty(t, detectOpenAIImageResultSize("data:image/png;base64"))
	require.Empty(t, detectOpenAIImageResultSize("not-image-data"))
}

func TestOpenAIGatewayServiceForwardImages_OAuthUsesDecodedOutputDimensions(t *testing.T) {
	run := runOpenAIOAuthImageActualSizeTest(t, false)

	require.Equal(t, "3840x2160", gjson.GetBytes(run.upstream.lastBody, "tools.0.size").String())
	require.Equal(t, "low", gjson.GetBytes(run.upstream.lastBody, "tools.0.quality").String())
	require.Equal(t, "1672x941", gjson.Get(run.recorder.Body.String(), "size").String())
	require.Equal(t, "auto", gjson.Get(run.recorder.Body.String(), "quality").String())
	require.Equal(t, []string{"1672x941"}, run.result.ImageOutputSizes)

	ApplyOpenAIImageBillingResolution(run.result)
	require.Equal(t, ImageBillingSize2K, run.result.ImageSize)
	require.Equal(t, "1672x941", run.result.ImageOutputSize)
	require.Equal(t, ImageSizeSourceOutput, run.result.ImageSizeSource)
}

func TestOpenAIGatewayServiceForwardImages_OAuthStreamingUsesDecodedOutputDimensions(t *testing.T) {
	run := runOpenAIOAuthImageActualSizeTest(t, true)

	events := parseOpenAIImageTestSSEEvents(run.recorder.Body.String())
	completed, ok := findOpenAIImageTestSSEEvent(events, "image_generation.completed")
	require.True(t, ok)
	require.Equal(t, "1672x941", gjson.Get(completed.Data, "size").String())
	require.Equal(t, "auto", gjson.Get(completed.Data, "quality").String())
	require.Equal(t, []string{"1672x941"}, run.result.ImageOutputSizes)
}

func TestOpenAIGatewayServiceForwardImages_APIKeyRecordsOutputDimensions(t *testing.T) {
	encoded := encodeOpenAIImageTestPNG(t, 1672, 941)
	for _, tt := range []struct {
		name       string
		response   string
		stream     bool
		wantSize   string
		wantTier   string
		wantSource string
	}{
		{
			name:     "base64 without size metadata",
			response: fmt.Sprintf(`{"data":[{"b64_json":%q}]}`, encoded),
			wantSize: "1672x941", wantTier: "2K", wantSource: "output",
		},
		{
			name:     "image bytes override inaccurate size metadata",
			response: fmt.Sprintf(`{"size":"3840x2160","data":[{"b64_json":%q,"size":"3840x2160"}]}`, encoded),
			wantSize: "1672x941", wantTier: "2K", wantSource: "output",
		},
		{
			name:     "inline data URL without size metadata",
			response: fmt.Sprintf(`{"data":[{"url":%q}]}`, "data:image/png;base64,"+encoded),
			wantSize: "1672x941", wantTier: "2K", wantSource: "output",
		},
		{
			name:     "remote URL with response level size metadata",
			response: `{"size":"1024x1024","data":[{"url":"https://image-upstream.example/image.png"}]}`,
			wantSize: "1024x1024", wantTier: "1K", wantSource: "output",
		},
		{
			name:     "remote URL without metadata keeps requested size separate",
			response: `{"data":[{"url":"https://image-upstream.example/image.png"}]}`,
			wantTier: "4K", wantSource: "input",
		},
		{
			name:     "unreadable image retains explicit output metadata",
			response: `{"data":[{"b64_json":"not-image-data","size":"1024x1024"}]}`,
			wantSize: "1024x1024", wantTier: "1K", wantSource: "output",
		},
		{
			name:     "stream completion without size metadata",
			response: fmt.Sprintf("data: {\"type\":\"image_generation.completed\",\"b64_json\":%q}\n\ndata: [DONE]\n\n", encoded),
			stream:   true, wantSize: "1672x941", wantTier: "2K", wantSource: "output",
		},
		{
			name:     "stream data array without size metadata",
			response: fmt.Sprintf("data: {\"data\":[{\"b64_json\":%q}]}\n\ndata: [DONE]\n\n", encoded),
			stream:   true, wantSize: "1672x941", wantTier: "2K", wantSource: "output",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"model":"gpt-image-2.5-sunburst","prompt":"draw a test chart","size":"3840x2160","stream":%t}`, tt.stream))
			c, rec := newOpenAIImagesTestContext(t, body)
			contentType := "application/json"
			if tt.stream {
				contentType = "text/event-stream"
			}
			svc := newOpenAIImagesTestService(&httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{contentType}},
				Body:       io.NopCloser(strings.NewReader(tt.response)),
			}})
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			result, err := svc.ForwardImages(context.Background(), c, newOpenAIImagesAPIKeyAccount(), body, parsed, "")
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, 1, result.ImageCount)
			if tt.wantSize == "" {
				require.Empty(t, result.ImageOutputSizes)
			} else {
				require.Equal(t, []string{tt.wantSize}, result.ImageOutputSizes)
			}
			ApplyOpenAIImageBillingResolution(result)
			require.Equal(t, "3840x2160", result.ImageInputSize)
			require.Equal(t, tt.wantSize, result.ImageOutputSize)
			require.Equal(t, tt.wantTier, result.ImageSize)
			require.Equal(t, tt.wantSource, result.ImageSizeSource)
			if !tt.stream {
				require.JSONEq(t, tt.response, rec.Body.String(), "dimension recording must preserve the upstream response")
			}
		})
	}
}

type openAIOAuthImageActualSizeTestRun struct {
	result   *OpenAIForwardResult
	recorder *httptest.ResponseRecorder
	upstream *httpUpstreamRecorder
}

func runOpenAIOAuthImageActualSizeTest(t *testing.T, stream bool) openAIOAuthImageActualSizeTestRun {
	t.Helper()
	gin.SetMode(gin.TestMode)
	body := []byte(fmt.Sprintf(`{"model":"gpt-image-1","prompt":"draw a test chart","size":"3840x2160","quality":"low","output_format":"png","stream":%t}`, stream))
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set("api_key", &APIKey{ID: 42})

	encoded := encodeOpenAIImageTestPNG(t, 1672, 941)
	upstreamBody := fmt.Sprintf(
		"data: {\"type\":\"response.created\",\"response\":{\"created_at\":1710000000,\"tools\":[{\"type\":\"image_generation\",\"model\":\"gpt-image-1\",\"size\":\"auto\",\"quality\":\"auto\",\"output_format\":\"png\"}]}}\n\n"+
			"data: {\"type\":\"response.completed\",\"response\":{\"created_at\":1710000000,\"tools\":[{\"type\":\"image_generation\",\"model\":\"gpt-image-1\",\"size\":\"auto\",\"quality\":\"auto\",\"output_format\":\"png\"}],\"output\":[{\"id\":\"ig_actual_size\",\"type\":\"image_generation_call\",\"result\":%q}]}}\n\n"+
			"data: [DONE]\n\n",
		encoded,
	)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"X-Request-Id": []string{"req_img_actual_size"},
		},
		Body: io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:       1,
		Name:     "openai-oauth",
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":       "token-123",
			"chatgpt_account_id": "acct-123",
		},
	}
	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	return openAIOAuthImageActualSizeTestRun{result: result, recorder: rec, upstream: upstream}
}

func encodeOpenAIImageTestPNG(t *testing.T, width, height int) string {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	img.SetNRGBA(0, 0, color.NRGBA{R: 0xff, A: 0xff})
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func encodeOpenAIImageTestJPEG(t *testing.T, width, height int) string {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	img.SetNRGBA(0, 0, color.NRGBA{G: 0xff, A: 0xff})
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, nil))
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func encodeOpenAIImageTestWebPVP8X(width, height int) string {
	header := make([]byte, 30)
	copy(header[0:4], "RIFF")
	copy(header[8:12], "WEBP")
	copy(header[12:16], "VP8X")
	width--
	height--
	header[24], header[25], header[26] = byte(width), byte(width>>8), byte(width>>16)
	header[27], header[28], header[29] = byte(height), byte(height>>8), byte(height>>16)
	return base64.StdEncoding.EncodeToString(header)
}

func encodeOpenAIImageTestWebPVP8(width, height int) string {
	header := make([]byte, 30)
	copy(header[0:4], "RIFF")
	copy(header[8:12], "WEBP")
	copy(header[12:16], "VP8 ")
	copy(header[23:26], "\x9d\x01\x2a")
	binary.LittleEndian.PutUint16(header[26:28], uint16(width))
	binary.LittleEndian.PutUint16(header[28:30], uint16(height))
	return base64.StdEncoding.EncodeToString(header)
}

func encodeOpenAIImageTestWebPVP8L(width, height int) string {
	header := make([]byte, 25)
	copy(header[0:4], "RIFF")
	copy(header[8:12], "WEBP")
	copy(header[12:16], "VP8L")
	header[20] = 0x2f
	width--
	height--
	header[21] = byte(width)
	header[22] = byte(width>>8)&0x3f | byte(height&0x03)<<6
	header[23] = byte(height >> 2)
	header[24] = byte(height>>10) & 0x0f
	return base64.StdEncoding.EncodeToString(header)
}
