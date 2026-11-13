package service

import (
	"context"
	"encoding/json"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Session-installed tools must participate in the next response's quote even
// when the client omits them from response.create. Materializing only standard
// response fields also lets the request's tool and output limits override the
// inherited session configuration on the wire.
type openAIWSFundingSessionState map[string]json.RawMessage

func (s openAIWSFundingSessionState) observe(payload []byte) {
	if gjson.GetBytes(payload, "type").String() != "session.update" {
		return
	}
	var frame struct {
		Session map[string]json.RawMessage `json:"session"`
	}
	if json.Unmarshal(payload, &frame) != nil {
		return
	}
	for _, name := range []string{"tools", "tool_choice", "instructions", "max_tool_calls", "max_output_tokens", "max_completion_tokens", "max_tokens", "modalities", "audio"} {
		if raw, ok := frame.Session[name]; ok {
			s[name] = append(json.RawMessage(nil), raw...)
		}
	}
}

func (s openAIWSFundingSessionState) apply(payload []byte) ([]byte, error) {
	if len(s) == 0 {
		return payload, nil
	}
	var frame map[string]json.RawMessage
	if err := json.Unmarshal(payload, &frame); err != nil {
		return nil, err
	}
	for name, value := range s {
		if _, explicit := frame[name]; !explicit {
			frame[name] = value
		}
	}
	return json.Marshal(frame)
}

func prepareOpenAIWSFundingRequest(account *Account, hooks *OpenAIWSIngressHooks, turn int, payload []byte, model string) ([]byte, error) {
	if hooks == nil || hooks.PrepareRequest == nil {
		return payload, nil
	}
	prepared, err := hooks.PrepareRequest(turn, payload, model)
	if err != nil {
		return nil, err
	}
	// OAuth authorizes the provider's declared output maximum; sending a
	// client limit to the Codex protocol would make the provider reject it.
	if account != nil && account.UsesOpenAICodexProtocol() {
		for _, field := range []string{"max_output_tokens", "max_completion_tokens", "max_tokens"} {
			prepared, err = sjson.DeleteBytes(prepared, field)
			if err != nil {
				return nil, err
			}
		}
	}
	return prepared, nil
}

func openAIWSFundingDispatchContext(ctx context.Context, hooks *OpenAIWSIngressHooks, turn int, payload []byte, begin bool) (context.Context, error) {
	if hooks != nil && hooks.RequestContext != nil {
		next, err := hooks.RequestContext(turn, payload)
		if err != nil {
			return nil, err
		}
		if next != nil {
			// Keep the transport's write deadline and cancellation. Only the
			// request-local dispatch snapshots belong to the funding context.
			if guard, ok := next.Value(fundingDispatchKey{}).(*fundingDispatchGuard); ok {
				ctx = context.WithValue(ctx, fundingDispatchKey{}, guard)
			}
			if operation, ok := next.Value(desktopInferenceOperationKey{}).(*desktopInferenceOperation); ok {
				ctx = context.WithValue(ctx, desktopInferenceOperationKey{}, operation)
			}
		}
	}
	if begin {
		if err := BeginFundingDispatch(ctx); err != nil {
			return nil, err
		}
		if err := BeginDesktopInferenceDispatch(ctx); err != nil {
			return nil, err
		}
	}
	return ctx, nil
}
