//go:build integration && nativeconsumer

package repository_test

import (
	"encoding/json"
	"net/http"
	"sort"
)

type ainoNativeImageProtocolEvidence struct {
	Tools      []string `json:"tools"`
	LastToolID string   `json:"last_tool_id,omitempty"`
	Action     string   `json:"action"`
}

func (p *ainoNativeProtocol) recordNativeImageProtocol(body *ainoNativeChatRequest, action string) {
	evidence := ainoNativeImageProtocolEvidence{Action: action, LastToolID: body.Messages[len(body.Messages)-1].ToolCallID}
	for _, tool := range body.Tools {
		evidence.Tools = append(evidence.Tools, tool.Function.Name)
	}
	sort.Strings(evidence.Tools)
	p.images.mu.Lock()
	p.images.protocol = append(p.images.protocol, evidence)
	p.images.mu.Unlock()
}

func (s *ainoNativeImageState) protocolSnapshot() []ainoNativeImageProtocolEvidence {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ainoNativeImageProtocolEvidence{}, s.protocol...)
}

func nativeImageFunctionCall(id, name, arguments string) map[string]any {
	return map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": id, "type": "function", "function": map[string]any{"name": name, "arguments": arguments}}}}
}

// Discovery changes neither the session's frozen tool surface nor its registry.
// The fixture responds through only bridges advertised by the actual Agent.
func (p *ainoNativeProtocol) discoverNativeImageTool(w http.ResponseWriter, body *ainoNativeChatRequest, edited bool) (map[string]any, bool) {
	toolID := "fixture-native-image-generate"
	if edited {
		toolID = "fixture-native-image-edit"
	}
	toolID = nativeFixtureToolCallID(toolID, body)
	last := body.Messages[len(body.Messages)-1]
	bridges := map[string]string{"tool_search": "queries", "tool_describe": "names", "tool_call": "calls"}
	for _, tool := range body.Tools {
		if property, ok := bridges[tool.Function.Name]; ok {
			props, _ := tool.Function.Parameters["properties"].(map[string]any)
			if props[property] != nil {
				delete(bridges, tool.Function.Name)
			}
		}
	}
	fail := func(reason string) (map[string]any, bool) {
		p.recordNativeImageProtocol(body, reason)
		http.Error(w, reason, http.StatusBadRequest)
		return nil, true
	}
	if len(bridges) != 0 {
		return fail("image_discovery_bridge_missing")
	}
	if last.Role != "tool" {
		p.recordNativeImageProtocol(body, "discover_image_search")
		p.writeNativeImageChat(w, body, nativeImageFunctionCall(toolID+"-search", "tool_search", `{"queries":["image_generate"],"limit":1}`), "tool_calls")
		return nil, true
	}
	if last.ToolCallID == toolID+"-search" {
		payload, args, ok := nativeImageDiscoveryResult(body.Messages, toolID+"-search", "tool_search")
		var query []string
		var tools map[string]json.RawMessage
		if !ok || json.Unmarshal(args["queries"], &query) != nil || len(query) != 1 || query[0] != "image_generate" || json.Unmarshal(payload["tools"], &tools) != nil || len(tools["image_generate"]) == 0 {
			return fail("image_search_did_not_find_actual_tool")
		}
		p.recordNativeImageProtocol(body, "discover_image_describe")
		p.writeNativeImageChat(w, body, nativeImageFunctionCall(toolID+"-describe", "tool_describe", `{"names":["image_generate"]}`), "tool_calls")
		return nil, true
	}
	if last.ToolCallID != toolID+"-describe" && last.ToolCallID != toolID {
		return fail("image_discovery_unexpected_tool_result")
	}
	payload, args, ok := nativeImageDiscoveryResult(body.Messages, toolID+"-describe", "tool_describe")
	var names []string
	var described struct {
		Image struct {
			Parameters struct {
				Properties map[string]any `json:"properties"`
			} `json:"parameters"`
		} `json:"image_generate"`
	}
	if !ok || json.Unmarshal(args["names"], &names) != nil || len(names) != 1 || names[0] != "image_generate" || json.Unmarshal(payload["tools"], &described) != nil || described.Image.Parameters.Properties["prompt"] == nil {
		return fail("image_describe_missing_actual_schema")
	}
	return described.Image.Parameters.Properties, false
}

// Pair each catalog result with the Agent's actual matching assistant call.
func nativeImageDiscoveryResult(messages []ainoNativeMessage, id, name string) (map[string]json.RawMessage, map[string]json.RawMessage, bool) {
	for i := len(messages) - 1; i >= 0; i-- {
		result := messages[i]
		if result.Role != "tool" || result.ToolCallID != id {
			continue
		}
		var content string
		var payload map[string]json.RawMessage
		if json.Unmarshal(result.Content, &content) != nil || json.Unmarshal([]byte(content), &payload) != nil {
			return nil, nil, false
		}
		for previous := i - 1; previous >= 0; previous-- {
			if messages[previous].Role == "tool" {
				continue
			}
			for _, call := range messages[previous].ToolCalls {
				var args map[string]json.RawMessage
				if messages[previous].Role == "assistant" && call.ID == id && call.Function.Name == name && json.Unmarshal([]byte(call.Function.Arguments), &args) == nil {
					return payload, args, true
				}
			}
			break
		}
		return nil, nil, false
	}
	return nil, nil, false
}
