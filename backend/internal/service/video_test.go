//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func videoTestAccount() *Account {
	return &Account{ID: 91, Platform: "video", Type: AccountTypeAPIKey, Credentials: map[string]any{
		"api_key": "video-test-secret", "base_url": "https://video.example/relay/v1",
		"model_mapping": map[string]any{"public-video": "veo-3.1-fast"},
	}}
}

func TestVideoAccountsAdmitOnlyVideoRequests(t *testing.T) {
	a := videoTestAccount()
	require.True(t, a.SupportsOpenAIEndpointCapability("video_generation"))
	for _, capability := range []OpenAIEndpointCapability{OpenAIEndpointCapabilityChatCompletions, OpenAIEndpointCapabilityResponses, OpenAIEndpointCapabilityEmbeddings, OpenAIEndpointCapabilityGrokMediaGeneration, OpenAIEndpointCapabilitySeedance} {
		require.False(t, a.SupportsOpenAIEndpointCapability(capability), capability)
	}
	a.Type = AccountTypeOAuth
	require.False(t, a.SupportsOpenAIEndpointCapability("video_generation"))
	a.Type = AccountTypeAPIKey
	delete(a.Credentials, "base_url")
	require.False(t, a.SupportsOpenAIEndpointCapability("video_generation"))
}

func TestVideoModelSyncUsesConfiguredEndpointAndCredential(t *testing.T) {
	s := &AccountTestService{cfg: &config.Config{}}
	req, err := s.buildUpstreamModelsRequest(context.Background(), videoTestAccount())
	require.NoError(t, err)
	require.Equal(t, "https://video.example/relay/v1/models", req.URL.String())
	require.Equal(t, "Bearer video-test-secret", req.Header.Get("Authorization"))
}

func TestVideoRequestUsesDocumentedUnitsAndDefaults(t *testing.T) {
	for _, tc := range []struct {
		body       string
		seconds    int
		resolution string
	}{
		{`{"model":"veo-3.1","prompt":"waves"}`, 8, "720p"},
		{`{"model":"omni-flash","prompt":"waves"}`, 3, "720p"},
		{`{"model":"veo-3.1-fast","prompt":"waves","seconds":"6","duration":4,"resolution":"720p","size":"1080x1920"}`, 6, "1080p"},
	} {
		info, err := ParseVideoRequest("application/json", []byte(tc.body))
		require.NoError(t, err)
		require.Equal(t, tc.seconds, info.DurationSeconds)
		require.Equal(t, tc.resolution, info.Resolution)
	}
	for _, body := range []string{
		`[]`, `{"model":"veo-3.1"}`, `{"model":"veo-3.1","prompt":"waves","seconds":5}`,
		`{"model":"veo-3.1","prompt":"waves","seconds":4.5}`,
		`{"model":"omni-flash","prompt":"waves","resolution":"1080p"}`,
		`{"model":"omni-flash","prompt":"waves","seconds":11}`,
		`{"model":"veo-3.1-lite","prompt":"waves","image_urls":["a","b","c"]}`,
		`{"model":"veo-3.1","prompt":"waves","last_frame":"a"}`,
		`{"model":"veo-3.1","prompt":"waves","first_frame":"a","image_urls":["b"]}`,
	} {
		_, err := ParseVideoRequest("application/json", []byte(body))
		require.Error(t, err, body)
	}
}

func TestVideoForwardPreservesPayloadAndIsolatesTaskIDs(t *testing.T) {
	body := []byte(`{"model":"public-video","prompt":"waves","seconds":"6","resolution":"1080p","reference_images":[{"url":"https://cdn.example/ref.png"}],"audio":true,"future_field":{"keep":true}}`)
	upstream := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(`{"id":"task-1","object":"video","model":"veo-3.1-fast","status":"queued","progress":0}`)}
	s := &OpenAIGatewayService{httpUpstream: upstream}
	c, w := grokMediaContentTestContext(http.MethodPost, "/v1/videos", nil)
	result, err := s.ForwardVideo(context.Background(), c, videoTestAccount(), VideoEndpointCreate, "", body, "application/json")
	require.NoError(t, err)
	require.Empty(t, w.Body.String(), "create must be buffered until owner and pricing metadata are saved")
	s.WriteVideoCreateResponse(c, result)
	require.Equal(t, "https://video.example/relay/v1/videos", upstream.request.URL.String())
	require.Equal(t, "Bearer video-test-secret", upstream.request.Header.Get("Authorization"))
	forwarded, err := io.ReadAll(upstream.request.Body)
	require.NoError(t, err)
	require.Equal(t, "veo-3.1-fast", gjson.GetBytes(forwarded, "model").String())
	for _, field := range []string{"seconds", "reference_images", "audio", "future_field"} {
		require.Equal(t, gjson.GetBytes(body, field).Raw, gjson.GetBytes(forwarded, field).Raw)
	}
	require.Equal(t, "public-video", gjson.Get(w.Body.String(), "model").String())
	publicID := gjson.Get(w.Body.String(), "id").String()
	require.Equal(t, result.ResponseID, publicID)
	require.NotEqual(t, "task-1", publicID)
	require.Zero(t, result.VideoCount, "accepted jobs must not bill")
	require.Equal(t, 6, result.VideoDurationSeconds)
	require.Equal(t, "1080p", result.VideoResolution)

	upstream.response = grokMediaContentStatusResponse(`{"id":"task-1","object":"video","model":"veo-3.1-fast","status":"completed","seconds":"6","url":"https://video.example/relay/v1/videos/task-1/content"}`)
	c, w = grokMediaContentTestContext(http.MethodGet, "/v1/videos/"+publicID, nil)
	result, err = s.ForwardVideo(context.Background(), c, videoTestAccount(), VideoEndpointStatus, publicID, nil, "")
	require.NoError(t, err)
	require.Equal(t, "/relay/v1/videos/task-1", upstream.request.URL.Path)
	require.Equal(t, publicID, gjson.Get(w.Body.String(), "id").String())
	require.Contains(t, gjson.Get(w.Body.String(), "url").String(), "/videos/"+publicID+"/content")
	require.NotContains(t, w.Body.String(), "video.example")
	require.Equal(t, 1, result.VideoCount)
	require.Empty(t, result.BillingModel, "upstream model must not override public-model billing")
	other := videoTestAccount()
	other.ID = 92
	_, err = s.ForwardVideo(context.Background(), c, other, VideoEndpointStatus, publicID, nil, "")
	require.Error(t, err, "task cannot move to another account")
}

func TestVideoMultipartPreservesFilesAndMapsModel(t *testing.T) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range map[string]string{"model": "public-video", "prompt": "waves", "seconds": "4", "resolution": "720p"} {
		require.NoError(t, w.WriteField(k, v))
	}
	file, err := w.CreateFormFile("reference[]", "ref.png")
	require.NoError(t, err)
	_, err = file.Write([]byte("image-bytes"))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	upstream := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(`{"id":"task-2","status":"queued"}`)}
	s := &OpenAIGatewayService{httpUpstream: upstream}
	c, _ := grokMediaContentTestContext(http.MethodPost, "/v1/videos", nil)
	_, err = s.ForwardVideo(context.Background(), c, videoTestAccount(), VideoEndpointCreate, "", buf.Bytes(), w.FormDataContentType())
	require.NoError(t, err)
	require.NoError(t, upstream.request.ParseMultipartForm(1<<20))
	defer upstream.request.MultipartForm.RemoveAll()
	require.Equal(t, "veo-3.1-fast", upstream.request.FormValue("model"))
	image, _, err := upstream.request.FormFile("reference[]")
	require.NoError(t, err)
	defer image.Close()
	data, err := io.ReadAll(image)
	require.NoError(t, err)
	require.Equal(t, "image-bytes", string(data))
}

func TestVideoCompositeMultipartHonorsRouteAndAccountMapping(t *testing.T) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for key, value := range map[string]string{"model": "public-video", "prompt": "waves", "seconds": "4"} {
		require.NoError(t, form.WriteField(key, value))
	}
	require.NoError(t, form.Close())
	account := videoTestAccount()
	account.Credentials["model_mapping"] = map[string]any{"routed-video": "veo-3.1-fast"}
	upstream := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(`{"id":"task-1","status":"queued"}`)}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	c, _ := grokMediaContentTestContext(http.MethodPost, "/v1/videos", nil)
	ctx := WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{Matched: true, TargetPlatform: PlatformVideo, PublicModel: "public-video", UpstreamModel: "routed-video"})
	result, err := svc.ForwardVideo(ctx, c, account, VideoEndpointCreate, "", body.Bytes(), form.FormDataContentType())
	require.NoError(t, err)
	require.NoError(t, upstream.request.ParseMultipartForm(1<<20))
	defer upstream.request.MultipartForm.RemoveAll()
	require.Equal(t, "veo-3.1-fast", upstream.request.FormValue("model"))
	require.Equal(t, "public-video", result.Model)
	require.Equal(t, "public-video", result.BillingModel)
}

func TestVideoFailedOrPendingTasksDoNotBillAndCreateDoesNotRetry(t *testing.T) {
	a := videoTestAccount()
	id := videoPublicTaskID(a.ID, "task-1")
	for _, status := range []string{"queued", "in_progress", "failed", "completed"} {
		upstream := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(`{"id":"task-1","status":"` + status + `"}`)}
		s := &OpenAIGatewayService{httpUpstream: upstream}
		c, _ := grokMediaContentTestContext(http.MethodGet, "/v1/videos/"+id, nil)
		result, err := s.ForwardVideo(context.Background(), c, a, VideoEndpointStatus, id, nil, "")
		require.NoError(t, err)
		require.Zero(t, result.VideoCount, "completed without output must not bill")
	}
	upstream := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(`{"error":{"message":"busy"}}`)}
	upstream.response.StatusCode = 503
	s := &OpenAIGatewayService{httpUpstream: upstream}
	c, w := grokMediaContentTestContext(http.MethodPost, "/v1/videos", nil)
	_, err := s.ForwardVideo(context.Background(), c, a, VideoEndpointCreate, "", []byte(`{"model":"veo-3.1","prompt":"waves"}`), "application/json")
	require.Error(t, err)
	require.Equal(t, 503, w.Code)
	require.Len(t, upstream.requests, 1)
	var retry *UpstreamFailoverError
	require.NotErrorAs(t, err, &retry, "ambiguous video creates must not retry")
}

func TestVideoDownloadUsesBoundAccountAndRange(t *testing.T) {
	id := videoPublicTaskID(91, "task-1")
	upstream := &grokMediaContentUpstreamStub{responses: []*http.Response{
		grokMediaContentStatusResponse(`{"id":"task-1","status":"completed","seconds":"8","url":"https://video.example/relay/v1/videos/task-1/content"}`),
		{StatusCode: 206, Header: http.Header{"Content-Type": []string{"video/mp4"}, "Content-Range": []string{"bytes 0-4/100"}}, Body: io.NopCloser(bytes.NewBufferString("video"))},
	}}
	s := &OpenAIGatewayService{httpUpstream: upstream}
	c, w := grokMediaContentTestContext(http.MethodGet, "/v1/videos/"+id+"/content", map[string]string{"Range": "bytes=0-4"})
	result, err := s.ForwardVideo(context.Background(), c, videoTestAccount(), VideoEndpointContent, id, nil, "")
	require.NoError(t, err)
	require.Equal(t, 206, w.Code)
	require.Equal(t, "video", w.Body.String())
	require.Equal(t, "bytes 0-4/100", w.Header().Get("Content-Range"))
	require.Len(t, upstream.requests, 2)
	require.Equal(t, "/relay/v1/videos/task-1/content", upstream.requests[1].URL.Path)
	require.Equal(t, "Bearer video-test-secret", upstream.requests[1].Header.Get("Authorization"))
	require.Equal(t, "bytes=0-4", upstream.requests[1].Header.Get("Range"))
	require.Equal(t, 1, result.VideoCount)
	require.Equal(t, 8, result.VideoDurationSeconds)
}

func TestVideoAccountConnectionCheckDoesNotSubmitGeneration(t *testing.T) {
	upstream := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(`{"object":"list","data":[{"id":"veo-3.1-fast","object":"model"}]}`)}
	s := &AccountTestService{httpUpstream: upstream, cfg: &config.Config{}}
	c, w := grokMediaContentTestContext(http.MethodPost, "/api/v1/admin/accounts/91/test", nil)
	require.NoError(t, s.testVideoAccountConnection(c, videoTestAccount(), "public-video", "", "default", AccountTestOptions{}))
	require.Len(t, upstream.requests, 1)
	require.Equal(t, http.MethodGet, upstream.request.Method)
	require.Equal(t, "/relay/v1/models", upstream.request.URL.Path)
	require.Contains(t, w.Body.String(), `"success":true`)
}

func TestVideoAccountGenerationTestWaitsForCompletionAndDownloads(t *testing.T) {
	upstream := &grokMediaContentUpstreamStub{responses: []*http.Response{
		grokMediaContentStatusResponse(`{"id":"job-1","status":"queued"}`),
		grokMediaContentStatusResponse(`{"id":"job-1","status":"completed","seconds":"4","url":"https://video.example/relay/v1/videos/job-1/content"}`),
		{StatusCode: 200, Header: http.Header{"Content-Type": []string{"video/mp4"}}, Body: io.NopCloser(strings.NewReader("video-test-content"))},
	}}
	s := &AccountTestService{httpUpstream: upstream, cfg: &config.Config{}}
	c, w := grokMediaContentTestContext(http.MethodPost, "/api/v1/admin/accounts/91/test", nil)
	require.NoError(t, s.testVideoAccountConnection(c, videoTestAccount(), "public-video", "waves", "video", AccountTestOptions{}))
	require.Len(t, upstream.requests, 3)
	require.Equal(t, http.MethodPost, upstream.requests[0].Method)
	require.Equal(t, "/relay/v1/videos/job-1/content", upstream.requests[2].URL.Path)
	require.Contains(t, w.Body.String(), `"type":"video"`)
	require.Contains(t, w.Body.String(), `"success":true`)
}

func TestVideoModelSyncDoesNotAdvertiseTextModels(t *testing.T) {
	upstream := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(`{"data":[{"id":"veo-3.1"},{"id":"gpt-6"},{"id":"custom-movie","supported_endpoints":["/v1/videos"]},{"id":"omni-flash"}]}`)}
	s := &AccountTestService{httpUpstream: upstream, cfg: &config.Config{}}
	models, _, err := s.fetchUpstreamModelList(context.Background(), videoTestAccount())
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"veo-3.1", "omni-flash", "custom-movie"}, models)
}
