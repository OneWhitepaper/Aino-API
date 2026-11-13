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
	if isFundingTransportRequest(c) {
		// Stateful transports authorize each response turn (or a bounded voice
		// session) after reading its actual payload in the adapter.
		c.Request = c.Request.WithContext(service.WithFundingTransportAdmission(c.Request.Context()))
		return next, subscription, nil
	}
	limit := keys.FundingBodyReadLimit()
	var body []byte
	if c.Request.Body != nil {
		originalBody := c.Request.Body
		body, err = io.ReadAll(io.LimitReader(originalBody, limit+1))
		if err != nil {
			return nil, nil, service.ErrTeamFundingCostUnbounded.WithCause(err)
		}
		if int64(len(body)) > limit {
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
	next, subscription, prepared, err := keys.PrepareFundingRequestForTransport(c.Request.Context(), key, c.Request.URL.Path, c.GetHeader("Content-Type"), body, c.GetHeader("Idempotency-Key"))
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
	if next.Funding.Plan != nil && next.Funding.Plan.Kind == service.FundingRequestControl {
		c.Request = c.Request.WithContext(service.WithReadOnlyFunding(c.Request.Context()))
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
	if c.Request.Method == http.MethodPost && strings.HasSuffix(c.FullPath(), "/responses/*subpath") && c.Param("subpath") == "/input_tokens" {
		return true
	}

	if c.Request.Method == http.MethodPatch && strings.Contains(c.FullPath(), "/cachedContents/") {
		return true
	}
	switch c.Request.Method {
	case http.MethodGet:
		switch c.FullPath() {
		case "/backend-api/codex/:call_id", "/v1/live/:call_id", "/v1/images/batches", "/v1/images/batches/models",
			"/v1beta/cachedContents", "/v1beta/cachedContents/:cacheID",
			"/antigravity/v1beta/cachedContents", "/antigravity/v1beta/cachedContents/:cacheID",
			"/v1/models", "/v1/models/:model", "/models", "/models/:model",
			"/backend-api/codex/models", "/antigravity/v1/models",
			"/v1beta/models", "/v1beta/models/:model",
			"/antigravity/v1beta/models", "/antigravity/v1beta/models/:model":
			return true
		}
	case http.MethodPost:
		switch c.FullPath() {
		case "/backend-api/codex/realtime/calls", "/v1/live", "/v1/messages/count_tokens", "/messages/count_tokens", "/antigravity/v1/messages/count_tokens":
			return true
		}
	}
	return false
}

func releaseUndispatchedRequestFunding(c *gin.Context) {
	if release, ok := c.Get("funding_release_undispatched"); ok {
		if releaseFn, valid := release.(func()); valid {
			releaseFn()
		}
	}
}

// Restore only registered task-resource routes. The PostgreSQL registry checks
// actor and API-key ownership before exposing its private payer/group snapshot.
func restoreAsyncRequestFunding(c *gin.Context, keys *service.APIKeyService, key *service.APIKey) (*service.APIKey, *service.UserSubscription, bool, error) {
	path := strings.TrimPrefix(c.FullPath(), "/v1")
	if path == "" {
		return key, nil, false, nil
	}
	provider, id := "", ""
	if (c.Request.Method == http.MethodGet || c.Request.Method == http.MethodDelete || c.Request.Method == http.MethodPatch) && strings.Contains(path, "/cachedContents/:cacheID") {
		provider, id = "gemini_cache", "cachedContents/"+c.Param("cacheID")
	}
	if c.Request.Method == http.MethodGet && path == "/images/tasks/:task_id" {
		provider, id = service.AsyncFundingProviderImage, c.Param("task_id")
	}
	if c.Request.Method == http.MethodGet && strings.HasPrefix(path, "/videos/") && strings.Contains(path, ":request_id") {
		id = c.Param("request_id")
		provider = service.AsyncVideoFundingProvider(id)
	}
	if (c.Request.Method == http.MethodGet || c.Request.Method == http.MethodDelete) && strings.HasSuffix(path, "/contents/generations/tasks/:task_id") {
		provider, id = service.AsyncFundingProviderSeedance, service.SeedanceTaskKey(c.Param("task_id"))
	}
	if strings.HasPrefix(path, "/images/batches/:id") && (c.Request.Method == http.MethodGet || c.Request.Method == http.MethodDelete || c.Request.Method == http.MethodPost && strings.HasSuffix(path, "/cancel")) {
		provider, id = service.AsyncFundingProviderBatchImage, c.Param("id")
	}
	if provider == "" || id == "" {
		return key, nil, false, nil
	}
	next, sub, err := keys.RestoreAsyncFundingRequest(c.Request.Context(), key, provider, id)
	if errors.Is(err, service.ErrTeamFundingNotFound) || errors.Is(err, service.ErrFundingUnavailable) {
		// Older tasks predate the registry. Their owned-resource handler retains
		// legacy settlement; a new preference must not replace that payer.
		c.Request = c.Request.WithContext(service.WithFundingFollowup(c.Request.Context()))
		return key, nil, true, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	c.Request = c.Request.WithContext(service.WithFundingFollowup(c.Request.Context()))
	c.Set("funding_async_followup", true)
	return next, sub, true, nil
}

// Collection history has no single task snapshot. Its handlers apply ownership
// filters; listing past work must remain possible after shared access is removed.
func isFundingHistoryRequest(c *gin.Context) bool {
	if c.Request.Method != http.MethodGet {
		return false
	}
	switch c.FullPath() {
	case "/v1/images/batches", "/v1/images/batches/models", "/v1beta/cachedContents", "/antigravity/v1beta/cachedContents", "/v1/live/:call_id", "/backend-api/codex/:call_id":
		return true
	}
	return false
}

func isFundingTransportRequest(c *gin.Context) bool {
	if c.Request.Method != http.MethodGet || !strings.EqualFold(c.GetHeader("Upgrade"), "websocket") {
		return false
	}
	switch c.FullPath() {
	case "/v1/responses", "/responses", "/backend-api/codex/responses", "/v1/realtime", "/realtime":
		return true
	}
	return false
}
