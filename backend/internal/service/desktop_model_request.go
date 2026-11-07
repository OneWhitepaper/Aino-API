package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type DesktopModelRequest struct {
	Key   *APIKey
	Route *CompositeRouteDecision
}

const desktopProjectionTTL = 15 * time.Second

type desktopCatalogProjection struct {
	CheckedAt time.Time
	Data      []byte
}

// The existing auth L1 owns eviction and lifetime. Only authorization/routing
// projections enter it; balances and subscription usage remain billing inputs.
func (s *DesktopModelService) catalogProjection(ctx context.Context, userID int64) ([]ResolvedPlatformModel, error) {
	cacheKey := fmt.Sprintf("desktop:catalog:%d", userID)
	read := func() ([]byte, bool) {
		if s.keys == nil || s.keys.authCacheL1 == nil {
			return nil, false
		}
		raw, ok := s.keys.authCacheL1.Get(cacheKey)
		cached, valid := raw.(desktopCatalogProjection)
		return cached.Data, ok && valid && time.Since(cached.CheckedAt) < desktopProjectionTTL
	}
	data, hit := read()
	if !hit {
		value, err, _ := s.resolveFlight.Do(cacheKey, func() (any, error) {
			if cached, ok := read(); ok {
				return cached, nil
			}
			checkedAt := time.Now()
			entries, err := s.buildCatalogProjection(ctx, userID)
			if err != nil {
				return nil, err
			}
			encoded, err := json.Marshal(entries)
			if err != nil {
				return nil, err
			}
			remaining := time.Until(checkedAt.Add(desktopProjectionTTL))
			if remaining <= 0 {
				return nil, ErrServiceUnavailable
			}
			if s.keys != nil && s.keys.authCacheL1 != nil {
				s.keys.authCacheL1.SetWithTTL(cacheKey, desktopCatalogProjection{CheckedAt: checkedAt, Data: encoded}, 1+int64(len(encoded))/1024, remaining)
			}
			return encoded, nil
		})
		if err != nil {
			return nil, err
		}
		var valid bool
		data, valid = value.([]byte)
		if !valid {
			return nil, ErrServiceUnavailable
		}
	}
	// Decode per request so downstream group changes cannot mutate a projection
	// shared by concurrent requests or retain repository-owned maps and slices.
	var entries []ResolvedPlatformModel
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func (s *DesktopModelService) resolveCatalog(ctx context.Context, userID int64) ([]ResolvedPlatformModel, error) {
	entries, err := s.catalogProjection(ctx, userID)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		entry := &entries[i]
		if entry.Model.State == "available" {
			s.applyEligibility(ctx, &User{ID: userID}, entry.Group, entry.Platform, &entry.Model)
		}
		entry.Model.IsDefault = entry.Model.IsDefault && entry.Model.State == "available"
	}
	return entries, nil
}

// ResolveRequest binds the selected catalog option to this dispatch. The key
// record and the identity cache never acquire a billing group.
func (s *DesktopModelService) ResolveRequest(ctx context.Context, key *APIKey, modelID, endpoint string, models []string, purpose string, requireModel bool) (*DesktopModelRequest, error) {
	if s == nil || key == nil || key.User == nil {
		return nil, ErrServiceUnavailable
	}
	// Authorization and routing must reflect the account's current group access
	// for every inference request. The short-lived catalog projection is only a
	// display optimization and must not keep a revoked model callable.
	entries, err := s.buildRequestProjection(ctx, key.UserID, modelID)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.Model.ID != modelID {
			continue
		}
		model := &entry.Model
		if model.State != "available" {
			return nil, infraerrors.Forbidden(*model.ReasonCode, "selected platform model is unavailable")
		}
		if endpoint != "" && model.APIMode != endpoint {
			return nil, infraerrors.Forbidden("DESKTOP_MODEL_ENDPOINT_MISMATCH", "selected model does not support this endpoint")
		}
		if purpose != "" && (purpose == DesktopPurposeImage) != (model.Purpose == DesktopPurposeImage) {
			return nil, infraerrors.Forbidden("DESKTOP_MODEL_PURPOSE_MISMATCH", "selected model does not support this purpose")
		}
		if requireModel && len(models) == 0 {
			return nil, infraerrors.BadRequest("DESKTOP_MODEL_REQUIRED", "request model is required")
		}
		for _, candidate := range models {
			if candidate != model.Model {
				return nil, infraerrors.BadRequest("DESKTOP_MODEL_MISMATCH", "request model differs from selected catalog option")
			}
		}
		// Billing performs current balance, subscription windows and limits after
		// routing. No unavailable option silently falls back to another paid group.
		copy := *key
		group := *entry.Group
		group.ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{model.Model}}
		group.FallbackGroupID, group.FallbackGroupIDOnInvalidRequest = nil, nil
		copy.GroupID, copy.Group, copy.DesktopPurpose = &group.ID, &group, model.Purpose
		user := *key.User
		if requireModel && !group.IsSubscriptionType() && s.eligibility.cfg.RunMode != config.RunModeSimple {
			balance, balanceErr := s.eligibility.GetUserBalance(ctx, key.UserID)
			if balanceErr != nil {
				return nil, ErrBillingServiceUnavailable.WithCause(balanceErr)
			}
			user.Balance = balance
		}
		user.UserGroupRPMOverride = entry.UserGroupRPMOverride
		copy.User = &user
		return &DesktopModelRequest{Key: &copy, Route: entry.Route}, nil
	}
	return nil, infraerrors.Forbidden("DESKTOP_MODEL_NOT_ALLOWED", "selected platform model is not available to this account")
}

// User authorization invalidation also evicts this service's bounded projection.
func (s *APIKeyService) InvalidateDesktopCatalogProjection(userID int64) {
	if s.authCacheL1 != nil {
		s.authCacheL1.Del(fmt.Sprintf("desktop:catalog:%d", userID))
	}
}
