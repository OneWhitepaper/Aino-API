package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// The default account test is deliberately read-only. An explicit video mode
// creates a paid upstream job and succeeds only after completion and download.
func (s *AccountTestService) testVideoAccountConnection(c *gin.Context, account *Account, model, prompt, mode string, opts AccountTestOptions) error {
	if err := validateVideoCredentials(account.Type, account.Credentials); err != nil {
		return s.sendErrorAndEnd(c, err.Error())
	}
	if mode != AccountTestModeGrokVideo {
		s.sendEvent(c, TestEvent{Type: "test_start", Model: model})
		models, _, err := s.fetchUpstreamModelList(c.Request.Context(), account)
		if err != nil {
			return s.sendErrorAndEnd(c, fmt.Sprintf("Video connection check failed: %s", err))
		}
		if model != "" {
			mapped := account.GetMappedModel(model)
			found := false
			for _, id := range models {
				if id == mapped {
					found = true
					break
				}
			}
			if !found {
				return s.sendErrorAndEnd(c, "Selected video model was not found in the upstream model list")
			}
		}
		s.sendEvent(c, TestEvent{Type: "content", Text: fmt.Sprintf("Video API key and model list verified (%d models). No video was generated; generation availability has not been tested.", len(models))})
		s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
		return nil
	}
	if strings.TrimSpace(model) == "" {
		return s.sendErrorAndEnd(c, "Select a video model for a real generation test")
	}
	upstreamModel := account.GetMappedModel(model)
	seconds := 4
	if upstreamModel == "omni-flash" {
		seconds = 3
	}
	payload := map[string]any{"model": upstreamModel, "prompt": firstNonEmpty(prompt, defaultGrokVideoTestPrompt), "seconds": seconds, "resolution": "720p"}
	if opts.ImageDataURL != "" {
		data := opts.ImageDataURL
		if strings.HasPrefix(data, "data:") {
			_, data, _ = strings.Cut(data, ",")
		}
		payload["reference_images"] = []string{data}
	}
	body, _ := json.Marshal(payload)
	if _, err := ParseVideoRequest("application/json", body); err != nil {
		return s.sendErrorAndEnd(c, err.Error())
	}
	base, err := s.validateUpstreamBaseURL(account.GetCredential("base_url"))
	if err != nil {
		return s.sendErrorAndEnd(c, err.Error())
	}
	target := buildOpenAIEndpointURL(base, "/v1/videos")
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
	defer cancel()
	request := func(method, target string, body []byte) (*http.Response, error) {
		req, err := http.NewRequestWithContext(WithHTTPUpstreamRedirectsDisabled(ctx), method, target, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(account.GetCredential("api_key")))
		req.Header.Set("Accept", "application/json")
		if method == http.MethodPost {
			req.Header.Set("Content-Type", "application/json")
		}
		if strings.HasSuffix(target, "/content") {
			req.Header.Set("Accept", "video/mp4")
		}
		account.ApplyHeaderOverrides(req.Header)
		return s.doUpstreamModelsRequest(req, upstreamModelsProxyURL(account), account)
	}
	readJSON := func(resp *http.Response) ([]byte, error) {
		defer resp.Body.Close()
		data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		if err != nil {
			return nil, err
		}
		if resp.StatusCode >= 300 {
			return nil, fmt.Errorf("video API returned HTTP %d%s", resp.StatusCode, videoAccountTestErrorDetail(account, data, resp.Header))
		}
		if !gjson.ValidBytes(data) {
			return nil, fmt.Errorf("invalid video API response")
		}
		return data, nil
	}
	s.sendEvent(c, TestEvent{Type: "test_start", Model: model})
	s.sendEvent(c, TestEvent{Type: "status", Text: "Submitting one real video generation (uses upstream credits)..."})
	resp, err := request(http.MethodPost, target, body)
	if err != nil {
		return s.sendErrorAndEnd(c, "Video submission failed; it will not be retried automatically")
	}
	data, err := readJSON(resp)
	if err != nil {
		return s.sendErrorAndEnd(c, err.Error())
	}
	id := gjson.GetBytes(data, "id").String()
	if id == "" {
		return s.sendErrorAndEnd(c, "Video submission did not return a task ID")
	}
	if err := validateUpstreamPathSegment("video task ID", id); err != nil {
		return s.sendErrorAndEnd(c, err.Error())
	}
	s.sendEvent(c, TestEvent{Type: "content", Text: "Video accepted. Task ID: " + id + "\n"})
	for {
		resp, err := request(http.MethodGet, target+"/"+id, nil)
		if err != nil {
			return s.sendErrorAndEnd(c, "Video status request failed (task ID: "+id+")")
		}
		data, err = readJSON(resp)
		if err != nil {
			return s.sendErrorAndEnd(c, err.Error()+" (task ID: "+id+")")
		}
		status := gjson.GetBytes(data, "status").String()
		s.sendEvent(c, TestEvent{Type: "status", Text: fmt.Sprintf("Video %s — %d%%", status, gjson.GetBytes(data, "progress").Int())})
		if status == "failed" {
			return s.sendErrorAndEnd(c, "Upstream video generation failed"+videoAccountTestErrorDetail(account, data, nil)+" (task ID: "+id+")")
		}
		if status == "completed" {
			resp, err := request(http.MethodGet, target+"/"+id+"/content", nil)
			if err != nil {
				return s.sendErrorAndEnd(c, "Video completed, but downloading failed (task ID: "+id+")")
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
				return s.sendErrorAndEnd(c, fmt.Sprintf("Video download returned HTTP %d%s", resp.StatusCode, videoAccountTestErrorDetail(account, body, resp.Header)))
			}
			content, err := io.ReadAll(io.LimitReader(resp.Body, (32<<20)+1))
			if err != nil || len(content) == 0 {
				return s.sendErrorAndEnd(c, "Video completed, but its content could not be read")
			}
			if !strings.HasPrefix(resp.Header.Get("Content-Type"), "video/") && resp.Header.Get("Content-Type") != "application/octet-stream" {
				return s.sendErrorAndEnd(c, "Video download did not return video content")
			}
			if len(content) <= 32<<20 {
				s.sendEvent(c, TestEvent{Type: "video", VideoURL: "data:video/mp4;base64," + base64.StdEncoding.EncodeToString(content), MimeType: "video/mp4"})
			}
			s.sendEvent(c, TestEvent{Type: "content", Text: "Real video creation, status lookup and content download verified."})
			s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
			return nil
		}
		select {
		case <-ctx.Done():
			return s.sendErrorAndEnd(c, "Video test stopped before completion. Task ID: "+id+". Do not resubmit this task.")
		case <-time.After(5 * time.Second):
		}
	}
}

// Keep actionable provider diagnostics in the admin test without returning an
// arbitrary response body or an API key echoed by the provider.
func videoAccountTestErrorDetail(account *Account, body []byte, headers http.Header) string {
	parts := make([]string, 0, 3)
	if code := strings.TrimSpace(extractUpstreamErrorCode(body)); code != "" {
		parts = append(parts, code)
	}
	if message := strings.TrimSpace(extractUpstreamErrorMessage(body)); message != "" {
		parts = append(parts, message)
	}
	if id := firstNonEmpty(headers.Get("x-request-id"), headers.Get("x-oneapi-request-id")); id != "" {
		parts = append(parts, "request ID: "+id)
	}
	if len(parts) == 0 {
		return ""
	}
	detail := strings.Join(parts, "; ")
	if account != nil {
		if key := strings.TrimSpace(account.GetCredential("api_key")); key != "" {
			detail = strings.ReplaceAll(detail, key, "[redacted]")
		}
	}
	return ": " + truncateString(sanitizeUpstreamErrorMessage(detail), 1024)
}
