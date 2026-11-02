package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	VideoEndpointCreate                     GrokMediaEndpoint        = "video_create"
	VideoEndpointStatus                     GrokMediaEndpoint        = "video_lookup"
	VideoEndpointContent                    GrokMediaEndpoint        = "video_download"
	OpenAIEndpointCapabilityVideoGeneration OpenAIEndpointCapability = "video_generation"
)

type VideoCreateResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

// VideoBillingSnapshot freezes the accepted per-second price and customer rate.
// An asynchronous completion must not pick up a later group/channel price or a
// different platform's default rate card.
type VideoBillingSnapshot struct {
	PricePerSecond float64 `json:"price_per_second"`
	RateMultiplier float64 `json:"rate_multiplier"`
}

func (s *OpenAIGatewayService) SnapshotVideoBilling(ctx context.Context, key *APIKey, userID int64, model, resolution string) (*VideoBillingSnapshot, error) {
	if key == nil || key.Group == nil {
		return nil, fmt.Errorf("video group pricing is required")
	}
	price := key.Group.GetVideoPriceForModel(model, resolution)
	if price == nil {
		return nil, fmt.Errorf("video price is not configured")
	}
	rate := key.Group.RateMultiplier
	if key.GroupID != nil {
		rate = s.ResolveUserGroupRateMultiplier(ctx, userID, *key.GroupID, rate)
	}
	snapshot := &VideoBillingSnapshot{PricePerSecond: *price, RateMultiplier: resolveVideoRateMultiplier(key, rate)}
	if !snapshot.valid() {
		return nil, fmt.Errorf("invalid video pricing")
	}
	return snapshot, nil
}

func (p *VideoBillingSnapshot) valid() bool {
	return p != nil && p.PricePerSecond >= 0 && p.RateMultiplier >= 0 &&
		!math.IsNaN(p.PricePerSecond) && !math.IsInf(p.PricePerSecond, 0) &&
		!math.IsNaN(p.RateMultiplier) && !math.IsInf(p.RateMultiplier, 0)
}

func (p *VideoBillingSnapshot) cost(result *OpenAIForwardResult) (*CostBreakdown, error) {
	if !p.valid() || result == nil || result.VideoCount <= 0 || result.VideoDurationSeconds < 1 || result.VideoDurationSeconds > 15 {
		return nil, fmt.Errorf("invalid video completion billing snapshot")
	}
	total := p.PricePerSecond * float64(result.VideoCount) * float64(result.VideoDurationSeconds)
	return &CostBreakdown{TotalCost: total, ActualCost: total * p.RateMultiplier, BillingMode: string(BillingModeVideo)}, nil
}

func (s *OpenAIGatewayService) WriteVideoCreateResponse(c *gin.Context, result *OpenAIForwardResult) {
	if result == nil || result.VideoCreateResponse == nil {
		return
	}
	response := result.VideoCreateResponse
	writeGrokMediaResponse(c, &http.Response{StatusCode: response.StatusCode, Header: response.Headers}, response.Body, s.responseHeaderFilter)
	result.VideoCreateResponse = nil
}

func (e GrokMediaEndpoint) IsVideoPlatform() bool {
	return e == VideoEndpointCreate || e == VideoEndpointStatus || e == VideoEndpointContent
}

func DefaultVideoModelIDs() []string {
	return []string{"veo-3.1", "veo-3.1-fast", "veo-3.1-lite", "omni-flash"}
}

func validateVideoCredentials(accountType string, credentials map[string]any) error {
	if accountType != AccountTypeAPIKey {
		return fmt.Errorf("video accounts only support API key credentials")
	}
	for _, key := range []string{"base_url", "api_key"} {
		value, ok := credentials[key].(string)
		if !ok || strings.TrimSpace(value) == "" {
			return fmt.Errorf("video account requires %s", key)
		}
	}
	if protocol, ok := credentials["video_protocol"].(string); ok && protocol != "" && protocol != "videos" {
		return fmt.Errorf("unsupported video protocol")
	}
	return nil
}

// The public namespace prevents identical upstream IDs from colliding between
// accounts/providers. It is not authorization: the owner binding is mandatory.
func videoPublicTaskID(accountID int64, upstreamID string) string {
	return "video_" + base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(accountID, 10)+":"+upstreamID))
}

func IsVideoPlatformTaskID(id string) bool { return strings.HasPrefix(id, "video_") }

func videoUpstreamTaskID(publicID string, accountID int64) (string, error) {
	if !IsVideoPlatformTaskID(publicID) || len(publicID) > 2048 {
		return "", fmt.Errorf("invalid video task ID")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(publicID, "video_"))
	if err != nil {
		return "", fmt.Errorf("invalid video task ID")
	}
	owner, id, ok := strings.Cut(string(raw), ":")
	if !ok || owner != strconv.FormatInt(accountID, 10) || id == "" {
		return "", fmt.Errorf("video task does not belong to the selected account")
	}
	if err := validateUpstreamPathSegment("video task ID", id); err != nil {
		return "", err
	}
	return id, nil
}

var videoReferenceFields = []string{"reference", "image_urls", "images", "image", "reference_images", "input_reference"}

func extractVideoUpstreamModelIDs(body []byte) ([]string, error) {
	ids, err := extractUpstreamModelIDs(body)
	if err != nil {
		return nil, err
	}
	videoIDs := map[string]bool{}
	for _, id := range DefaultVideoModelIDs() {
		videoIDs[id] = true
	}
	entries := gjson.GetBytes(body, "data")
	if !entries.IsArray() {
		entries = gjson.ParseBytes(body)
	}
	for _, entry := range entries.Array() {
		for _, endpoint := range entry.Get("supported_endpoints").Array() {
			if endpoint.String() == "videos" || strings.HasPrefix(endpoint.String(), "/v1/videos") {
				videoIDs[entry.Get("id").String()] = true
			}
		}
		for _, modality := range entry.Get("output_modalities").Array() {
			if modality.String() == "video" {
				videoIDs[entry.Get("id").String()] = true
			}
		}
	}
	filtered := make([]string, 0, len(ids))
	for _, id := range ids {
		if videoIDs[id] {
			filtered = append(filtered, id)
		}
	}
	return filtered, nil
}

// ParseVideoRequest extracts billing/moderation metadata without changing the
// provider payload. seconds and size have the documented precedence over aliases.
func ParseVideoRequest(contentType string, body []byte) (GrokMediaRequestInfo, error) {
	info := GrokMediaRequestInfo{N: 1}
	metadata := body
	if mediaType, params, err := mime.ParseMediaType(contentType); err == nil && mediaType == "multipart/form-data" {
		if params["boundary"] == "" {
			return info, fmt.Errorf("multipart boundary is required")
		}
		values := map[string]string{}
		reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return info, fmt.Errorf("invalid video multipart body: %w", err)
			}
			data, err := io.ReadAll(part)
			_ = part.Close()
			if err != nil {
				return info, err
			}
			name := strings.TrimSuffix(part.FormName(), "[]")
			if part.FileName() == "" {
				if _, duplicate := values[name]; duplicate {
					return info, fmt.Errorf("duplicate video field: %s", name)
				}
				values[name] = string(data)
			} else {
				info.Uploads = append(info.Uploads, OpenAIImagesUpload{FieldName: name, FileName: part.FileName(), ContentType: part.Header.Get("Content-Type"), Data: data})
			}
		}
		metadata, _ = json.Marshal(values)
	} else if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return info, fmt.Errorf("video request must be a JSON object or multipart form")
	}
	root := gjson.ParseBytes(metadata)
	if root.Get("model").Type != gjson.String || strings.TrimSpace(root.Get("model").String()) == "" {
		return info, fmt.Errorf("model is required")
	}
	if root.Get("prompt").Type != gjson.String || strings.TrimSpace(root.Get("prompt").String()) == "" {
		return info, fmt.Errorf("prompt is required")
	}
	info.Model, info.Prompt = strings.TrimSpace(root.Get("model").String()), root.Get("prompt").String()
	info.AspectRatio = firstNonEmpty(root.Get("aspect").String(), root.Get("ratio").String(), root.Get("aspect_ratio").String())
	seconds := root.Get("seconds")
	if !seconds.Exists() {
		seconds = root.Get("duration")
	}
	if seconds.Exists() {
		n, err := strconv.Atoi(seconds.String())
		if err != nil || n < 1 || n > 15 {
			return info, fmt.Errorf("seconds must be an integer between 1 and 15")
		}
		info.DurationSeconds = n
	} else if strings.HasPrefix(info.Model, "veo-3.1") {
		info.DurationSeconds = 8
	} else if info.Model == "omni-flash" {
		info.DurationSeconds = 3
	}
	info.Resolution = strings.ToLower(strings.TrimSpace(root.Get("resolution").String()))
	info.Size = strings.ToLower(strings.TrimSpace(root.Get("size").String()))
	if info.Size != "" {
		if info.Size == "720p" || info.Size == "1080p" {
			info.Resolution = info.Size
		} else {
			var width, height int
			if _, err := fmt.Sscanf(info.Size, "%dx%d", &width, &height); err != nil || width <= 0 || height <= 0 {
				return info, fmt.Errorf("invalid video size")
			}
			switch short := min(width, height); {
			case short >= 1080:
				info.Resolution = "1080p"
			case short >= 720:
				info.Resolution = "720p"
			default:
				return info, fmt.Errorf("video size must be at least 720p")
			}
		}
	}
	if info.Resolution == "" {
		info.Resolution = "720p"
	}
	if info.Resolution != "720p" && info.Resolution != "1080p" {
		return info, fmt.Errorf("video resolution must be 720p or 1080p")
	}
	references, first, last := 0, 0, 0
	for _, key := range videoReferenceFields {
		value := root.Get(key)
		if !value.Exists() {
			continue
		}
		items := []gjson.Result{value}
		if value.IsArray() {
			items = value.Array()
		}
		for _, item := range items {
			if item.IsObject() {
				item = item.Get("url")
			}
			if item.Type != gjson.String || strings.TrimSpace(item.String()) == "" {
				return info, fmt.Errorf("invalid video image reference")
			}
			references++
			info.InputImageURLs = append(info.InputImageURLs, item.String())
		}
	}
	for _, key := range []string{"first_frame", "last_frame"} {
		value := root.Get(key)
		if value.Exists() && value.String() != "" {
			if value.Type != gjson.String {
				return info, fmt.Errorf("invalid %s", key)
			}
			info.InputImageURLs = append(info.InputImageURLs, value.String())
			if key == "first_frame" {
				first++
			} else {
				last++
			}
		}
	}
	for _, upload := range info.Uploads {
		switch upload.FieldName {
		case "first_frame":
			first++
		case "last_frame":
			last++
		default:
			for _, key := range videoReferenceFields {
				if upload.FieldName == key {
					references++
					break
				}
			}
		}
	}
	if first > 1 || last > 1 || (last > 0 && first == 0) {
		return info, fmt.Errorf("last_frame requires a single first_frame")
	}
	if references > 0 && (first > 0 || last > 0) {
		return info, fmt.Errorf("reference images and first/last frames cannot be combined")
	}
	limit := 3
	if info.Model == "veo-3.1-lite" {
		limit = 2
	}
	if references > limit {
		return info, fmt.Errorf("too many video reference images (maximum %d)", limit)
	}
	if strings.HasPrefix(info.Model, "veo-3.1") && info.DurationSeconds != 4 && info.DurationSeconds != 6 && info.DurationSeconds != 8 {
		return info, fmt.Errorf("Veo seconds must be 4, 6 or 8")
	}
	if info.Model == "omni-flash" {
		if info.DurationSeconds < 3 || info.DurationSeconds > 10 || info.Resolution != "720p" {
			return info, fmt.Errorf("Omni Flash requires 3–10 seconds and 720p")
		}
		if first > 0 || last > 0 || root.Get("audio").Bool() || root.Get("generate_audio").Bool() {
			return info, fmt.Errorf("Omni Flash supports reference images only, without generated audio")
		}
	}
	return info, nil
}

func rewriteVideoRequestModel(body []byte, contentType, model string) ([]byte, string, error) {
	mediaType, params, _ := mime.ParseMediaType(contentType)
	if mediaType != "multipart/form-data" {
		out, err := sjson.SetBytes(body, "model", model)
		return out, "application/json", err
	}
	var out bytes.Buffer
	writer := multipart.NewWriter(&out)
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", err
		}
		dst, err := writer.CreatePart(part.Header)
		if err != nil {
			return nil, "", err
		}
		if part.FormName() == "model" {
			_, err = io.WriteString(dst, model)
		} else {
			_, err = io.Copy(dst, part)
		}
		_ = part.Close()
		if err != nil {
			return nil, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return out.Bytes(), writer.FormDataContentType(), nil
}

// ForwardVideo uses the provider's Videos protocol, not the xAI body conversion.
// Creation is never automatically retried after a possibly accepted request.
func (s *OpenAIGatewayService) ForwardVideo(ctx context.Context, c *gin.Context, account *Account, endpoint GrokMediaEndpoint, taskID string, body []byte, contentType string) (*OpenAIForwardResult, error) {
	if account == nil || account.Platform != PlatformVideo || !account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityVideoGeneration) || !endpoint.IsVideoPlatform() {
		return nil, fmt.Errorf("video requires an API key account with a custom base URL")
	}
	base, err := s.validateUpstreamBaseURL(account.GetCredential("base_url"))
	if err != nil {
		return nil, err
	}
	started := time.Now()
	result := &OpenAIForwardResult{}
	target := buildOpenAIEndpointURL(base, "/v1/videos")
	method := http.MethodGet
	if endpoint == VideoEndpointCreate {
		info, err := ParseVideoRequest(contentType, body)
		if err != nil {
			return nil, err
		}
		result.Model, result.BillingModel = info.Model, info.Model
		if publicModel, ok := RequestedPublicModelFromContext(ctx); ok {
			result.Model, result.BillingModel = publicModel, publicModel
		}
		routingModel := info.Model
		if resolved, ok := ResolvedUpstreamModelFromContext(ctx); ok {
			routingModel = resolved
		}
		result.UpstreamModel = account.GetMappedModel(routingModel)
		body, contentType, err = rewriteVideoRequestModel(body, contentType, result.UpstreamModel)
		if err != nil {
			return nil, err
		}
		// Validate/default against the mapped upstream model, including aliases.
		info, err = ParseVideoRequest(contentType, body)
		if err != nil {
			return nil, err
		}
		if info.DurationSeconds <= 0 {
			return nil, fmt.Errorf("seconds is required for custom video models")
		}
		result.VideoDurationSeconds, result.VideoResolution = info.DurationSeconds, info.Resolution
		method = http.MethodPost
	} else {
		upstreamID, err := videoUpstreamTaskID(taskID, account.ID)
		if err != nil {
			return nil, err
		}
		target += "/" + upstreamID
		result.ResponseID = taskID
	}
	proxy := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxy = account.Proxy.URL()
	}
	request := func(target, method string, data []byte, ct string, download bool) (*http.Response, error) {
		req, err := http.NewRequestWithContext(WithHTTPUpstreamRedirectsDisabled(ctx), method, target, bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(account.GetCredential("api_key")))
		req.Header.Set("Accept", "application/json")
		if method == http.MethodPost {
			req.Header.Set("Content-Type", ct)
		}
		if download {
			req.Header.Set("Accept", "video/mp4")
			for _, key := range []string{"Range", "If-Range"} {
				if v := c.GetHeader(key); v != "" {
					req.Header.Set(key, v)
				}
			}
		}
		account.ApplyHeaderOverrides(req.Header)
		return s.httpUpstream.Do(req, proxy, account.ID, account.Concurrency)
	}
	resp, err := request(target, method, body, contentType, false)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(started).Milliseconds())
	if err != nil {
		return nil, fmt.Errorf("video upstream request failed: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		if resp.StatusCode >= 400 {
			writeGrokMediaResponse(c, resp, responseBody, s.responseHeaderFilter)
		}
		return nil, fmt.Errorf("video upstream status %d", resp.StatusCode)
	}
	if !gjson.ValidBytes(responseBody) || !gjson.ParseBytes(responseBody).IsObject() {
		return nil, fmt.Errorf("invalid video upstream response")
	}
	result.RequestID = resp.Header.Get("x-request-id")
	result.ResponseHeaders = resp.Header.Clone()
	if endpoint == VideoEndpointCreate {
		id := gjson.GetBytes(responseBody, "id").String()
		if id == "" {
			return nil, fmt.Errorf("video create response missing task ID")
		}
		if err := validateUpstreamPathSegment("video task ID", id); err != nil {
			return nil, err
		}
		result.ResponseID = videoPublicTaskID(account.ID, id)
	} else {
		result.UpstreamModel = gjson.GetBytes(responseBody, "model").String()
		if publicModel, ok := RequestedPublicModelFromContext(ctx); ok {
			responseBody, _ = sjson.SetBytes(responseBody, "model", publicModel)
		}
		if gjson.GetBytes(responseBody, "status").String() == "completed" && strings.TrimSpace(gjson.GetBytes(responseBody, "url").String()) != "" {
			result.VideoCount = 1
			if seconds := gjson.GetBytes(responseBody, "seconds"); seconds.Exists() {
				result.VideoDurationSeconds, _ = strconv.Atoi(seconds.String())
			}
			if seconds := gjson.GetBytes(responseBody, "duration"); result.VideoDurationSeconds <= 0 && seconds.Exists() {
				result.VideoDurationSeconds, _ = strconv.Atoi(seconds.String())
			}
			result.VideoResolution = gjson.GetBytes(responseBody, "resolution").String()
		}
	}
	if endpoint == VideoEndpointContent {
		if result.VideoCount <= 0 {
			c.JSON(http.StatusConflict, gin.H{"error": gin.H{"type": "video_not_ready", "message": "Video is not completed"}})
			MarkResponseCommitted(c)
			return nil, fmt.Errorf("video is not completed")
		}
		contentResp, err := request(target+"/content", http.MethodGet, nil, "", true)
		if err != nil {
			return nil, err
		}
		defer contentResp.Body.Close()
		if contentResp.StatusCode != 200 && contentResp.StatusCode != 206 && contentResp.StatusCode != 416 {
			return nil, fmt.Errorf("video download upstream status %d", contentResp.StatusCode)
		}
		if err := writeGrokMediaContentResponse(c, contentResp); err != nil {
			return nil, err
		}
	} else {
		responseBody, _ = sjson.SetBytes(responseBody, "id", result.ResponseID)
		if result.Model != "" {
			responseBody, _ = sjson.SetBytes(responseBody, "model", result.Model)
		}
		if gjson.GetBytes(responseBody, "url").Exists() {
			responseBody, _ = sjson.SetBytes(responseBody, "url", grokMediaContentProxyURL(c, result.ResponseID))
		}
		if endpoint == VideoEndpointCreate {
			result.VideoCreateResponse = &VideoCreateResponse{StatusCode: resp.StatusCode, Headers: resp.Header.Clone(), Body: responseBody}
		} else {
			writeGrokMediaResponse(c, resp, responseBody, s.responseHeaderFilter)
		}
	}
	result.Duration = time.Since(started)
	return result, nil
}
