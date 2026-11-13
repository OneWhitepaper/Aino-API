package middleware

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// resolveRequestFunding runs before legacy wallet gates. Absent preferences keep
// the old path (including its request body) untouched.
func resolveRequestFunding(c *gin.Context, keys *service.APIKeyService, key *service.APIKey) (*service.APIKey, *service.UserSubscription, error) {
	next, subscription, err := keys.ResolveRequestFunding(c.Request.Context(), key)
	if err != nil {
		if errors.Is(err, service.ErrUsageBillingRequestConflict) {
			return nil, nil, service.ErrTeamFundingConflict
		}
		return nil, nil, err
	}
	if next.Funding == nil {
		return next, subscription, nil
	}
	key = next
	if isReadOnlyFundingRequest(c) {
		c.Request = c.Request.WithContext(service.WithReadOnlyFunding(c.Request.Context()))
		return next, subscription, nil
	}
	if strings.EqualFold(c.GetHeader("Upgrade"), "websocket") {
		if next.Funding.IsShared() {
			return nil, nil, service.ErrTeamFundingCostUnbounded
		}
		return next, subscription, nil
	}
	if !service.FundingTextEndpoint(c.Request.URL.Path) && !next.Funding.IsShared() {
		return next, subscription, nil
	}
	var body []byte
	if c.Request.Body != nil {
		originalBody := c.Request.Body
		body, err = io.ReadAll(io.LimitReader(originalBody, service.ConfiguredFundingBodyReadLimit+1))
		if err != nil {
			return nil, nil, service.ErrTeamFundingCostUnbounded.WithCause(err)
		}
		if len(body) > service.ConfiguredFundingBodyReadLimit {
			if next.Funding.IsShared() {
				return nil, nil, service.ErrTeamFundingCostUnbounded
			}
			// Personal adapters keep their established body limit and billing
			// path. Reattach the consumed prefix without poisoning later reads.
			c.Request.Body = struct {
				io.Reader
				io.Closer
			}{Reader: io.MultiReader(bytes.NewReader(body), originalBody), Closer: originalBody}
			return next, subscription, nil
		}
	}
	next, subscription, prepared, err := keys.PrepareConfiguredFundingRequest(c.Request.Context(), key, c.Request.URL.Path, body, c.GetHeader("Idempotency-Key"))
	if err != nil {
		if errors.Is(err, service.ErrUsageBillingRequestConflict) {
			return nil, nil, service.ErrTeamFundingConflict
		}
		return nil, nil, err
	}
	if next == nil {
		return nil, nil, service.ErrFundingUnavailable
	}
	if next.Funding == nil {
		return next, subscription, nil
	}
	if next.Funding.HasReservation() {
		guarded := service.FundingDispatchContext(c.Request.Context(), next)
		c.Request = c.Request.WithContext(guarded)
		deferContext := guarded
		deferKey := next
		c.Set("funding_release_undispatched", func() { keys.ReleaseUndispatchedFunding(deferContext, deferKey) })
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(prepared))
	c.Request.ContentLength = int64(len(prepared))
	return next, subscription, nil
}

// These registered adapters only discover models or count input tokens; none
// executes generation or records paid usage. Matching full route patterns keeps
// unrelated media polling and other GET requests on normal funding admission.
func isReadOnlyFundingRequest(c *gin.Context) bool {
	switch c.Request.Method {
	case http.MethodGet:
		switch c.FullPath() {
		case "/v1/models", "/v1/models/:model", "/models", "/models/:model",
			"/backend-api/codex/models", "/antigravity/v1/models",
			"/v1beta/models", "/v1beta/models/:model",
			"/antigravity/v1beta/models", "/antigravity/v1beta/models/:model":
			return true
		}
	case http.MethodPost:
		switch c.FullPath() {
		case "/v1/messages/count_tokens", "/messages/count_tokens", "/antigravity/v1/messages/count_tokens":
			return true
		}
	}
	return false
}

func releaseUndispatchedRequestFunding(c *gin.Context) {
	if release, ok := c.Get("funding_release_undispatched"); ok {
		release.(func())()
	}
}
