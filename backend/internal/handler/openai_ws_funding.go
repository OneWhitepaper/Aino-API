package handler

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type openAIWSTurnFundingSnapshot struct {
	key          *service.APIKey
	subscription *service.UserSubscription
	ctx          context.Context
	body         []byte
}

type openAIWSFundingPreparer interface {
	PrepareFundingRequestForTransport(context.Context, *service.APIKey, string, string, []byte, string) (*service.APIKey, *service.UserSubscription, []byte, error)
	ReleaseUndispatchedFunding(context.Context, *service.APIKey)
}

// Each turn owns its payer and reservation until its usage task settles. A
// following turn cannot replace those inputs while billing runs asynchronously.
type openAIWSTurnFunding struct {
	mu        sync.Mutex
	keys      openAIWSFundingPreparer
	operation string
	turns     map[int]*openAIWSTurnFundingSnapshot
}

func (f *openAIWSTurnFunding) prepare(ctx context.Context, turn int, key *service.APIKey, body []byte, model string) ([]byte, error) {
	if f == nil || f.keys == nil || key == nil {
		return body, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if snapshot := f.turns[turn]; snapshot != nil {
		return snapshot.body, nil
	}
	if strings.TrimSpace(gjson.GetBytes(body, "model").String()) == "" && model != "" {
		var err error
		body, err = sjson.SetBytes(body, "model", model)
		if err != nil {
			return nil, err
		}
	}
	operation := fmt.Sprintf("ws:%s:turn:%d", f.operation, turn)
	if event := strings.TrimSpace(gjson.GetBytes(body, "event_id").String()); event != "" {
		operation = "ws:" + f.operation + ":event:" + event
	}
	// Membership and source preferences are DB authoritative on every turn.
	requestKey := *key
	requestKey.Funding = nil
	// Desktop operation IDs otherwise take precedence over the turn's HTTP
	// idempotency argument and would collapse every frame onto the first hold.
	if key.Funding != nil || service.DesktopInferenceOperationID(ctx) != "" || key.DesktopUsage != nil {
		ctx = service.WithDesktopInferenceOperation(ctx, operation)
	}
	preparedKey, subscription, prepared, err := f.keys.PrepareFundingRequestForTransport(ctx, &requestKey, "/v1/responses", "application/json", body, operation)
	if err != nil {
		return nil, err
	}
	if f.turns == nil {
		f.turns = make(map[int]*openAIWSTurnFundingSnapshot)
	}
	for old, snapshot := range f.turns {
		if old < turn-1 {
			f.keys.ReleaseUndispatchedFunding(snapshot.ctx, snapshot.key)
			delete(f.turns, old)
		}
	}
	f.turns[turn] = &openAIWSTurnFundingSnapshot{key: preparedKey, subscription: subscription, ctx: service.FundingDispatchContext(ctx, preparedKey), body: prepared}
	return prepared, nil
}

func (f *openAIWSTurnFunding) snapshot(turn int) *openAIWSTurnFundingSnapshot {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.turns[turn]
}

func (f *openAIWSTurnFunding) close() {
	if f == nil || f.keys == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, snapshot := range f.turns {
		f.keys.ReleaseUndispatchedFunding(snapshot.ctx, snapshot.key)
	}
}
