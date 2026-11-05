package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// ImageTaskRoute is private creation-time routing metadata, never a current
// entitlement or a new debit authorization. Ownership is checked separately.
type ImageTaskRoute struct {
	Group        *Group            `json:"group"`
	Platform     string            `json:"platform"`
	Subscription *UserSubscription `json:"subscription,omitempty"`
}

func ImageTaskRouteFromRequest(key *APIKey, sub *UserSubscription, platforms ...string) *ImageTaskRoute {
	if key == nil || key.Group == nil {
		return nil
	}
	route := &ImageTaskRoute{Group: key.Group, Platform: key.Group.Platform}
	if len(platforms) > 0 {
		route.Platform = platforms[0]
	}
	if sub != nil {
		route.Subscription = &UserSubscription{ID: sub.ID, UserID: sub.UserID, GroupID: sub.GroupID}
	}
	// Group and subscription contain values only. A serialization copy detaches
	// their maps, slices and optional fields from the accepted request.
	data, err := json.Marshal(route)
	if err != nil {
		return nil
	}
	var result ImageTaskRoute
	if json.Unmarshal(data, &result) != nil {
		return nil
	}
	return &result
}

func (s *ImageTaskService) GetRoute(ctx context.Context, owner ImageTaskOwner, id string) (*ImageTaskRoute, error) {
	if s == nil || s.store == nil {
		return nil, ErrImageTaskUnavailable
	}
	task, err := s.store.Get(ctx, strings.TrimSpace(id))
	if errors.Is(err, ErrImageTaskNotFound) {
		return nil, ErrImageTaskNotFound
	}
	if err != nil {
		return nil, ErrImageTaskUnavailable.WithCause(err)
	}
	if task.UserID != owner.UserID || task.APIKeyID != owner.APIKeyID {
		return nil, ErrImageTaskNotFound
	}
	if task.Route == nil || task.Route.Group == nil {
		return nil, ErrImageTaskUnavailable
	}
	data, err := json.Marshal(task.Route)
	if err != nil {
		return nil, ErrImageTaskUnavailable.WithCause(err)
	}
	var route ImageTaskRoute
	if err := json.Unmarshal(data, &route); err != nil {
		return nil, ErrImageTaskUnavailable.WithCause(err)
	}
	return &route, nil
}
