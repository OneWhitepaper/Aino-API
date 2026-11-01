//go:build integration

package repository_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestDesktopImageLeasePersistsSeparatePurposeAndRevokesWithParent(t *testing.T) {
	r := newDesktopCredentialRig(t)
	chat := r.issue(t, "fixture-a")
	key, err := r.keys.GetByKey(r.ctx, chat.APIKey)
	require.NoError(t, err)
	group, err := r.groups.GetByID(r.ctx, *key.GroupID)
	require.NoError(t, err)
	group.AllowImageGeneration = true
	require.NoError(t, r.groups.Update(r.ctx, group))
	image := desktopEntry("fixture-image", group.ID)
	image.APIMode = "images_generations"
	image.AgentVerified = false
	image.Capabilities = service.PlatformModelCapabilities{ImageGeneration: true}
	r.catalog(t, []service.DesktopModelEntry{desktopEntry("fixture-a", group.ID), image}, "fixture-a")
	issue := func(purpose string) *service.DesktopCredentialResponse {
		body, err := json.Marshal(map[string]string{"device_id": r.device, "connection_grant_id": r.grant, "model_id": image.ID, "purpose": purpose})
		require.NoError(t, err)
		response := r.request(http.MethodPost, "/api/v1/desktop/credentials", string(body), r.token)
		if purpose == "chat" {
			require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
			return nil
		}
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var envelope struct {
			Data service.DesktopCredentialResponse `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
		return &envelope.Data
	}
	require.Nil(t, issue("chat"))
	lease := issue("image")
	require.Equal(t, "image", lease.Purpose)
	require.NotEqual(t, chat.APIKey, lease.APIKey)
	reused := issue("image")
	require.Equal(t, lease.APIKey, reused.APIKey)
	stored, err := repository.NewDesktopCredentialRepository(r.client).GetByAPIKeyID(r.ctx, func() int64 {
		imageKey, e := r.keys.GetByKey(r.ctx, lease.APIKey)
		require.NoError(t, e)
		return imageKey.ID
	}())
	require.NoError(t, err)
	require.Equal(t, "image", stored.Purpose)
	require.Equal(t, http.StatusOK, r.request(http.MethodPost, "/v1/images/generations", `{"model":"fixture-image"}`, lease.APIKey).Code)
	require.Equal(t, http.StatusNotFound, r.request(http.MethodPost, "/v1/images/generations", `{"model":"fixture-a"}`, lease.APIKey).Code, "image credential must not inherit chat model names")
	require.Equal(t, http.StatusForbidden, r.request(http.MethodPost, "/v1/chat/completions", `{"model":"fixture-a"}`, lease.APIKey).Code)
	require.Equal(t, http.StatusForbidden, r.request(http.MethodPost, "/v1/images/generations", `{"model":"fixture-image"}`, chat.APIKey).Code)
	require.NoError(t, r.auth.RevokeSessionFamily(r.ctx, r.family))
	require.Equal(t, http.StatusUnauthorized, r.request(http.MethodPost, "/v1/images/generations", `{"model":"fixture-image"}`, lease.APIKey).Code)
	require.Equal(t, http.StatusUnauthorized, r.request(http.MethodPost, "/v1/chat/completions", `{"model":"fixture-a"}`, chat.APIKey).Code)
}
