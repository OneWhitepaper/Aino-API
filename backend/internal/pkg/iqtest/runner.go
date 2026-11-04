package iqtest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"
)

type Runner struct{ external, local *http.Client }

func NewRunner(external *http.Client, gateway *GatewayTransport) *Runner {
	if external == nil {
		external = newPublicClient()
	}
	var local *http.Client
	if gateway != nil {
		local = &http.Client{Transport: gateway}
	}
	return &Runner{external: external, local: local}
}

func NewDefaultRunner(gateway *GatewayTransport) *Runner { return NewRunner(nil, gateway) }

func (r *Runner) request(ctx context.Context, connection Connection, method, suffix string, body []byte) (*http.Response, error) {
	client, base := r.external, connection.BaseURL
	if connection.Source == "site" {
		client, base = r.local, "http://aino.internal/v1"
	} else {
		var err error
		base, err = NormalizeBaseURL(base)
		if err != nil {
			return nil, err
		}
	}
	if client == nil || connection.APIKey == "" || len(connection.APIKey) > 8192 || strings.IndexFunc(connection.APIKey, unicode.IsControl) >= 0 {
		return nil, errors.New("invalid_credentials")
	}
	req, err := http.NewRequestWithContext(ctx, method, base+suffix, bytes.NewReader(body))
	if err != nil {
		return nil, errEndpoint
	}
	req.Header.Set("Authorization", "Bearer "+connection.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if connection.Source == "site" {
		req = WithClientIP(req, connection.ClientIP)
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("connection_failed")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		code := "upstream_error"
		switch resp.StatusCode {
		case 401, 403:
			code = "upstream_auth"
		case 402:
			code = "upstream_balance"
		case 429:
			code = "upstream_rate_limit"
		case 400, 404, 422:
			code = "upstream_unsupported"
		}
		return nil, errors.New(code)
	}
	return resp, nil
}

func (r *Runner) Run(ctx context.Context, input RunInput, progress func(string)) (result Result, err error) {
	started := time.Now()
	defer func() { result.DurationMS = time.Since(started).Milliseconds() }()
	prompt := CandyPrompt
	if input.Kind == "pelican" {
		prompt = PelicanPrompt
	}
	payload := map[string]any{"model": input.Model, "input": prompt, "stream": input.Stream, "store": false, "max_output_tokens": 8192}
	if input.Effort != "" {
		payload["reasoning"] = map[string]string{"effort": input.Effort}
	}
	if input.Tier == "priority" {
		payload["service_tier"] = "priority"
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return result, errors.New("invalid_request")
	}
	resp, err := r.request(ctx, input.Connection, http.MethodPost, "/responses", body)
	if err != nil {
		return result, err
	}
	defer func() { _ = resp.Body.Close() }()
	limited := &io.LimitedReader{R: resp.Body, N: MaxResponseBytes + 1}
	if strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		result, err = parseStream(limited, progress)
	} else {
		var data []byte
		data, err = io.ReadAll(limited)
		if err == nil {
			result, err = parseResponse(data, "")
		}
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if limited.N == 0 {
		return result, errors.New("response_too_large")
	}
	if err != nil {
		return result, safeParseError(err)
	}
	return result, nil
}

type responseDocument struct {
	Status      string          `json:"status"`
	OutputText  string          `json:"output_text"`
	ServiceTier string          `json:"service_tier"`
	Error       json.RawMessage `json:"error"`
	Output      []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Usage *struct {
		Input        *int64 `json:"input_tokens"`
		Output       *int64 `json:"output_tokens"`
		InputDetails *struct {
			Cached *int64 `json:"cached_tokens"`
		} `json:"input_tokens_details"`
		OutputDetails *struct {
			Reasoning *int64 `json:"reasoning_tokens"`
		} `json:"output_tokens_details"`
	} `json:"usage"`
}

func parseResponse(data []byte, partial string) (Result, error) {
	var doc responseDocument
	if json.Unmarshal(data, &doc) != nil {
		return Result{Output: partial}, errors.New("invalid_response")
	}
	result := Result{Output: doc.OutputText, ServiceTier: doc.ServiceTier}
	if result.Output == "" {
		var texts []string
		for _, item := range doc.Output {
			if item.Type == "message" {
				for _, content := range item.Content {
					if content.Type == "output_text" {
						texts = append(texts, content.Text)
					}
				}
			}
		}
		result.Output = strings.Join(texts, "\n")
	}
	if result.Output == "" {
		result.Output = partial
	}
	if len(result.Output) > MaxOutputBytes {
		result.Output = ""
		return result, errors.New("response_too_large")
	}
	if doc.Usage != nil {
		u := doc.Usage
		if u.Input == nil || u.Output == nil || *u.Input < 0 || *u.Output < 0 || *u.Input > 1_000_000_000 || *u.Output > 1_000_000_000 {
			return result, errors.New("invalid_response")
		}
		result.Usage = &Usage{InputTokens: *u.Input, OutputTokens: *u.Output}
		if u.InputDetails != nil {
			result.Usage.CachedTokens = u.InputDetails.Cached
		}
		if u.OutputDetails != nil {
			result.Usage.ReasoningTokens = u.OutputDetails.Reasoning
		}
		if c := result.Usage.CachedTokens; c != nil && (*c < 0 || *c > *u.Input) {
			return result, errors.New("invalid_response")
		}
		if n := result.Usage.ReasoningTokens; n != nil && (*n < 0 || *n > *u.Output) {
			return result, errors.New("invalid_response")
		}
	}
	if doc.Status != "completed" || (len(doc.Error) > 0 && string(doc.Error) != "null") {
		return result, errors.New("incomplete_response")
	}
	if strings.TrimSpace(result.Output) == "" {
		return result, errors.New("empty_response")
	}
	return result, nil
}

func parseStream(reader io.Reader, progress func(string)) (Result, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), MaxResponseBytes)
	var output strings.Builder
	var completed *Result
	var eventData []string
	consume := func() error {
		data := strings.Join(eventData, "\n")
		eventData = nil
		if data == "" || data == "[DONE]" {
			return nil
		}
		var event struct {
			Type     string          `json:"type"`
			Delta    string          `json:"delta"`
			Response json.RawMessage `json:"response"`
		}
		if json.Unmarshal([]byte(data), &event) != nil {
			return errors.New("invalid_response")
		}
		switch event.Type {
		case "response.output_text.delta":
			if output.Len()+len(event.Delta) > MaxOutputBytes {
				return errors.New("response_too_large")
			}
			_, _ = output.WriteString(event.Delta)
			if progress != nil {
				progress(output.String())
			}
		case "response.completed":
			result, err := parseResponse(event.Response, output.String())
			if err != nil {
				return err
			}
			completed = &result
		case "error", "response.failed", "response.incomplete":
			return errors.New("incomplete_response")
		}
		return nil
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if err := consume(); err != nil {
				return Result{Output: output.String()}, err
			}
		} else if strings.HasPrefix(line, "data:") {
			eventData = append(eventData, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return Result{Output: output.String()}, errors.New("incomplete_response")
	}
	if err := consume(); err != nil {
		return Result{Output: output.String()}, err
	}
	if completed == nil {
		return Result{Output: output.String()}, errors.New("incomplete_response")
	}
	return *completed, nil
}

func safeParseError(err error) error {
	switch err.Error() {
	case "invalid_response", "incomplete_response", "empty_response", "response_too_large":
		return err
	default:
		return errors.New("incomplete_response")
	}
}

func (r *Runner) Models(ctx context.Context, connection Connection) ([]Model, error) {
	resp, err := r.request(ctx, connection, http.MethodGet, "/models", nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil || len(body) > MaxResponseBytes {
		return nil, errors.New("invalid_response")
	}
	var doc struct {
		Data []struct {
			ID      string            `json:"id"`
			Efforts []json.RawMessage `json:"supported_reasoning_efforts"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &doc) != nil || doc.Data == nil || len(doc.Data) > 2000 {
		return nil, errors.New("invalid_response")
	}
	models := make([]Model, 0, len(doc.Data))
	seen := map[string]bool{}
	for _, item := range doc.Data {
		if item.ID == "" || len(item.ID) > 200 || strings.IndexFunc(item.ID, unicode.IsControl) >= 0 || seen[item.ID] {
			continue
		}
		seen[item.ID] = true
		efforts := KnownEfforts(item.ID)
		if len(item.Efforts) > 0 {
			efforts = []string{}
			for _, raw := range item.Efforts {
				var value string
				if json.Unmarshal(raw, &value) != nil {
					var entry struct {
						Effort string `json:"effort"`
					}
					_ = json.Unmarshal(raw, &entry)
					value = entry.Effort
				}
				if ValidEffort(value) && value != "" {
					efforts = append(efforts, value)
				}
			}
		}
		models = append(models, Model{ID: item.ID, Efforts: efforts})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models, nil
}

func ValidEffort(value string) bool {
	switch value {
	case "", "low", "medium", "high", "xhigh", "max", "ultra":
		return true
	default:
		return false
	}
}
