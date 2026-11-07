//go:build integration

package repository_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type desktopFreshnessGroupRepository struct {
	service.GroupRepository
	reads atomic.Int64
}

func (r *desktopFreshnessGroupRepository) ListActive(ctx context.Context) ([]service.Group, error) {
	r.reads.Add(1)
	return r.GroupRepository.ListActive(ctx)
}

func TestDesktopDeviceRequestsUseCurrentGroupAndPermissionWithWarmCatalog(t *testing.T) {
	r := newDesktopModelRig(t)
	user := r.user(t, service.StatusActive, false)
	firstGroup, nextGroup := r.group(t, false), r.group(t, false)
	const modelName = "fixture-request-current"
	input, output := 2e-6, 6e-6
	for _, group := range []*service.Group{firstGroup, nextGroup} {
		group.ModelPricing = []service.ChannelModelPricing{{Models: []string{modelName}, InputPrice: &input, OutputPrice: &output}}
		require.NoError(t, r.groups.Update(r.ctx, group))
		require.NoError(t, r.userRepo.AddGroupToAllowedGroups(r.ctx, user.ID, group.ID))
	}
	entry := desktopEntry("fixture-option", firstGroup.ID)
	entry.Model = modelName
	entry.DisplayName = "Original option"
	r.catalog(t, []service.DesktopModelEntry{entry}, entry.ID)

	cfg := &config.Config{RunMode: config.RunModeStandard, APIKeyAuth: config.APIKeyAuthCacheConfig{L1Size: 1000, L1TTLSeconds: 60, L2TTLSeconds: 60}}
	cfg.Default.RateMultiplier = 1
	groups := &desktopFreshnessGroupRepository{GroupRepository: r.groups}
	keys := service.NewAPIKeyService(repository.NewAPIKeyRepository(r.client, repository.GetIntegrationDB()), r.userRepo, groups, r.subs, r.rates, repository.NewAPIKeyCache(r.redis), cfg)
	eligibility := service.NewBillingCacheService(nil, r.userRepo, r.subs, nil, nil, r.rates, cfg, nil)
	t.Cleanup(eligibility.Stop)
	models := service.NewDesktopModelService(r.settings, keys, r.billing, r.pricing, eligibility, nil)
	listed, err := models.ListForUser(r.ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, "available", listed[0].State)
	// Ristretto writes asynchronously. Observe an actual cache hit before
	// changing server state so this regression cannot pass with a cold cache.
	require.Eventually(t, func() bool {
		before := groups.reads.Load()
		_, listErr := models.ListForUser(r.ctx, user.ID)
		return listErr == nil && groups.reads.Load() == before
	}, time.Second, time.Millisecond)

	deviceRepo := repository.NewDesktopDeviceKeyRepository(r.client)
	device := service.NewDesktopDeviceKeyService(deviceRepo, keys)
	identity, err := device.Ensure(r.ctx, user.ID, service.DesktopDeviceKeyRequest{DeviceID: uuid.NewString()})
	require.NoError(t, err)
	require.NotEmpty(t, identity.APIKey)
	assertIdentity := func() {
		t.Helper()
		stored, getErr := deviceRepo.GetDeviceKey(r.ctx, user.ID, identity.DeviceID)
		require.NoError(t, getErr)
		require.Equal(t, identity.KeyID, strconv.FormatInt(stored.ID, 10))
		require.Nil(t, stored.GroupID)
		require.Nil(t, stored.ExpiresAt)
	}
	assertIdentity()

	var dispatchedGroups []int64
	router := gin.New()
	router.POST("/v1/chat/completions", gin.HandlerFunc(middleware.ProvideAPIKeyAuthMiddleware(keys, nil, cfg, models, nil)), func(c *gin.Context) {
		key, ok := middleware.GetAPIKeyFromContext(c)
		require.True(t, ok)
		require.Equal(t, user.ID, key.UserID)
		require.Equal(t, identity.KeyID, strconv.FormatInt(key.ID, 10))
		require.NotNil(t, key.GroupID)
		dispatchedGroups = append(dispatchedGroups, *key.GroupID)
		c.JSON(http.StatusOK, gin.H{"group_id": key.GroupID})
	})
	dispatch := func() *httptest.ResponseRecorder {
		t.Helper()
		body, marshalErr := json.Marshal(map[string]any{"model": modelName, "messages": []map[string]string{{"role": "user", "content": "fixture"}}})
		require.NoError(t, marshalErr)
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(body)))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+identity.APIKey)
		request.Header.Set("X-Aino-Model-ID", entry.ID)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		assertIdentity()
		return response
	}

	response := dispatch()
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, []int64{firstGroup.ID}, dispatchedGroups)

	entry.GroupID, entry.DisplayName = nextGroup.ID, "Updated option"
	r.catalog(t, []service.DesktopModelEntry{entry}, entry.ID)
	listed, err = models.ListForUser(r.ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, "Original option", listed[0].DisplayName, "the UI catalog remains warm while inference must use the new route")
	response = dispatch()
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, []int64{firstGroup.ID, nextGroup.ID}, dispatchedGroups)

	require.NoError(t, r.userRepo.RemoveGroupFromUserAllowedGroups(r.ctx, user.ID, nextGroup.ID))
	response = dispatch()
	require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "DESKTOP_MODEL_NOT_ALLOWED")
	require.Equal(t, []int64{firstGroup.ID, nextGroup.ID}, dispatchedGroups, "revoked access must not reach inference")

	require.NoError(t, r.userRepo.AddGroupToAllowedGroups(r.ctx, user.ID, nextGroup.ID))
	response = dispatch()
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, []int64{firstGroup.ID, nextGroup.ID, nextGroup.ID}, dispatchedGroups, "new permission must take effect with the same device key")
}
