package service

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// The diagnostic stores a bounded schema fingerprint, never response fragments,
// arbitrary JSON keys/values, prompt content, or request/response headers.
type openAIUsageDiagnostic struct {
	Payloads         int                                   `json:"payloads"`
	Bytes            int                                   `json:"bytes"`
	InvalidJSON      int                                   `json:"invalid_json"`
	UsageMarkers     int                                   `json:"usage_marker_payloads"`
	UsagePaths       map[string]*openAIUsagePathDiagnostic `json:"paths,omitempty"`
	FinishReasons    map[string]bool                       `json:"finish_reasons,omitempty"`
	Status           string                                `json:"status,omitempty"`
	SawNonzeroUsage  bool                                  `json:"saw_nonzero_usage"`
	ZeroAfterNonzero bool                                  `json:"zero_after_nonzero"`
}

type openAIUsagePathDiagnostic struct {
	Frames        int              `json:"frames"`
	Kind          string           `json:"last_kind"`
	NumericFields map[string]int64 `json:"numeric_fields,omitempty"`
	InvalidFields map[string]bool  `json:"invalid_numeric_fields,omitempty"`
}

var openAIUsageDiagnosticPaths = [...]string{"usage", "response.usage", "data.usage", "data.response.usage"}
var openAIUsageDiagnosticCounters = [...]string{
	"input_tokens", "output_tokens", "prompt_tokens", "completion_tokens", "total_tokens",
	"cache_read_input_tokens", "cache_creation_input_tokens", "cache_write_input_tokens",
	"cache_read_tokens", "cache_write_tokens", "cache_creation_tokens", "cached_tokens",
	"prompt_cache_hit_tokens", "prompt_cache_miss_tokens",
	"input_tokens_details.cached_tokens", "prompt_tokens_details.cached_tokens",
	"input_tokens_details.cache_write_tokens", "prompt_tokens_details.cache_write_tokens",
	"output_tokens_details.reasoning_tokens", "completion_tokens_details.reasoning_tokens",
	"input_tokens_details.image_tokens", "output_tokens_details.image_tokens",
}

func (d *openAIUsageDiagnostic) observe(payload []byte, eventType string) {
	if d == nil || len(payload) == 0 || bytes.Equal(bytes.TrimSpace(payload), []byte("[DONE]")) {
		return
	}
	d.Payloads++
	d.Bytes += len(payload)
	if bytes.Contains(payload, []byte(`"usage"`)) {
		d.UsageMarkers++
		if !gjson.ValidBytes(payload) {
			d.InvalidJSON++
			return
		}
		for _, path := range openAIUsageDiagnosticPaths {
			value := gjson.GetBytes(payload, path)
			if !value.Exists() {
				continue
			}
			if d.UsagePaths == nil {
				d.UsagePaths = make(map[string]*openAIUsagePathDiagnostic, len(openAIUsageDiagnosticPaths))
			}
			entry := d.UsagePaths[path]
			if entry == nil {
				entry = &openAIUsagePathDiagnostic{}
				d.UsagePaths[path] = entry
			}
			entry.observe(value)
			if parsed, ok := openAIUsageFromGJSON(value); ok {
				if openAIUsageHasTokens(&parsed) {
					d.SawNonzeroUsage = true
				} else if d.SawNonzeroUsage {
					d.ZeroAfterNonzero = true
				}
			}
		}
	}
	if bytes.Contains(payload, []byte(`"finish_reason"`)) {
		for _, choice := range gjson.GetBytes(payload, "choices").Array() {
			if reason := choice.Get("finish_reason"); reason.Type == gjson.String && reason.Str != "" {
				if d.FinishReasons == nil {
					d.FinishReasons = make(map[string]bool)
				}
				d.FinishReasons[openAIUsageDiagnosticState(reason.Str)] = true
			}
		}
	}
	if (eventType == "" || isUpstreamResponseModelTerminalEvent(eventType)) && bytes.Contains(payload, []byte(`"status"`)) {
		if status := firstValidTrimmedGJSONString(payload, "response.status", "status"); status != "" {
			d.Status = openAIUsageDiagnosticState(status)
		}
	}
}

func (d *openAIUsagePathDiagnostic) observe(value gjson.Result) {
	d.Frames++
	d.Kind = "non_object"
	d.NumericFields = nil
	d.InvalidFields = nil
	if value.Type == gjson.Null {
		d.Kind = "null"
	}
	if !value.IsObject() {
		return
	}
	d.Kind = "object"
	// Keep only the latest usage object's fixed numeric schema. Earlier values
	// cannot be mistaken for the final provider counters when an empty tail wins.
	for _, field := range openAIUsageDiagnosticCounters {
		number := value.Get(field)
		if !number.Exists() {
			continue
		}
		count, err := strconv.ParseInt(number.Raw, 10, 64)
		if number.Type != gjson.Number || err != nil || count < 0 {
			if d.InvalidFields == nil {
				d.InvalidFields = make(map[string]bool)
			}
			d.InvalidFields[field] = true
			continue
		}
		if d.NumericFields == nil {
			d.NumericFields = make(map[string]int64)
		}
		d.NumericFields[field] = count
	}
}

func openAIUsageDiagnosticState(value string) string {
	switch value {
	case "stop", "length", "tool_calls", "function_call", "content_filter", "completed", "incomplete", "failed", "cancelled", "in_progress", "queued":
		return value
	default:
		return "other"
	}
}

func openAIUsageDiagnosticSlug(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 200 {
		return ""
	}
	for _, c := range value {
		allowed := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._/+", c)
		if !allowed {
			return ""
		}
	}
	return value
}

func openAIUsageDiagnosticTerminal(event string) bool {
	switch event {
	case "response.completed", "response.done", "[DONE]", "json", "finish_reason", "usage_chunk", "client_aborted":
		return true
	default:
		return false
	}
}

func openAIUsageDiagnosticEndpoint(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return ""
	}
	path := c.Request.URL.Path
	switch path {
	case "/v1/chat/completions", "/chat/completions", "/v1/responses", "/responses", "/v1/messages", "/messages", "/v1/responses/compact", "/responses/compact":
		return path
	default:
		return "other"
	}
}

func openAIMissingUsageDiagnosticFields(c *gin.Context, usage *OpenAIUsage, terminalEvent string) []zap.Field {
	fields := []zap.Field{zap.Bool("stream", terminalEvent != "json")}
	if usage != nil {
		fields = append(fields, zap.Int("input_tokens", usage.InputTokens), zap.Int("output_tokens", usage.OutputTokens),
			zap.Int("cache_read_input_tokens", usage.CacheReadInputTokens), zap.Int("cache_creation_input_tokens", usage.CacheCreationInputTokens))
	}
	if c == nil {
		return fields
	}
	fields = append(fields, zap.String("upstream_model", openAIUsageDiagnosticSlug(c.GetString(OpsUpstreamModelKey))))
	if key := getAPIKeyFromContext(c); key != nil {
		fields = append(fields, zap.Int64("api_key_id", key.ID), zap.Int64("user_id", key.UserID))
		if key.GroupID != nil {
			fields = append(fields, zap.Int64("group_id", *key.GroupID))
		}
		if key.DesktopUsage != nil && ValidDesktopUsageID(key.DesktopUsage.CallID) {
			fields = append(fields, zap.String("desktop_call_id", key.DesktopUsage.CallID))
		}
	}
	if observer := upstreamResponseModelObserverFromContext(c); observer != nil {
		fields = append(fields, zap.String("upstream_response_model", openAIUsageDiagnosticSlug(observer.Model())), zap.Any("usage_shape", observer.usageDiagnostic))
	}
	return fields
}
