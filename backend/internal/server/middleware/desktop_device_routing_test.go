//go:build unit

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDeviceKeyReadUsageDoesNotRequireLeaseOrBalance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	key := &service.APIKey{ID: 2, UserID: 17, Key: "device-test", Status: service.StatusActive, DesktopManaged: true, DesktopCredentialType: "device", User: &service.User{ID: 17, Status: service.StatusActive}}
	repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { copy := *key; return &copy, nil }}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	keys := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
	router := gin.New()
	router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(keys, nil, cfg)))
	router.GET("/v1/usage", func(c *gin.Context) {
		got, _ := GetAPIKeyFromContext(c)
		require.Nil(t, got.GroupID)
		c.Status(http.StatusOK)
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/usage", nil)
	req.Header.Set("Authorization", "Bearer device-test")
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestDeviceScopeRejectsUnsupportedEndpointsWithoutChangingManualKeys(t *testing.T) {
	for _, path := range []string{"/v1/images/batches", "/v1/embeddings", "/v1/videos"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt"}`))
		require.True(t, enforceDesktopCredential(c, nil, &service.APIKey{}))
		require.False(t, enforceDesktopCredential(c, nil, &service.APIKey{DesktopManaged: true, DesktopCredentialType: "device"}))
		require.Equal(t, http.StatusForbidden, c.Writer.Status())
	}
}

type deviceRouteSettings struct {
	service.SettingRepository
	values map[string]string
}

func (r *deviceRouteSettings) GetMultiple(_ context.Context, names []string) (map[string]string, error) {
	return r.values, nil
}

type deviceRouteUsers struct {
	service.UserRepository
	user *service.User
}

func (r *deviceRouteUsers) GetByID(context.Context, int64) (*service.User, error) { return r.user, nil }

type deviceRouteGroups struct {
	service.GroupRepository
	groups []service.Group
}

func (r *deviceRouteGroups) ListActive(context.Context) ([]service.Group, error) {
	return r.groups, nil
}

type deviceRouteSubscriptions struct {
	service.UserSubscriptionRepository
	subs []service.UserSubscription
}

func (r *deviceRouteSubscriptions) ListActiveByUserID(context.Context, int64) ([]service.UserSubscription, error) {
	return r.subs, nil
}
func (r *deviceRouteSubscriptions) GetActiveByUserIDAndGroupID(_ context.Context, _ int64, group int64) (*service.UserSubscription, error) {
	for _, s := range r.subs {
		if s.GroupID == group {
			return &s, nil
		}
	}
	return nil, service.ErrSubscriptionNotFound
}

func deviceRouteFixture(t *testing.T, taskServices ...*service.ImageTaskService) (*gin.Engine, *service.APIKey, *deviceRouteSubscriptions) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeStandard}
	user := &service.User{ID: 17, Status: service.StatusActive, Concurrency: 4, Balance: 0}
	key := &service.APIKey{ID: 2, UserID: 17, Key: "device-test", Status: service.StatusActive, DesktopManaged: true, DesktopCredentialType: "device", User: user}
	keysRepo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { copy := *key; return &copy, nil }}
	input, output, limit := 1e-6, 2e-6, 10.0
	groups := &deviceRouteGroups{groups: []service.Group{
		{ID: 11, Platform: service.PlatformOpenAI, Hydrated: true, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeSubscription, DailyLimitUSD: &limit, AllowImageGeneration: true},
		{ID: 12, Platform: service.PlatformOpenAI, Hydrated: true, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeSubscription, DailyLimitUSD: &limit, WeeklyLimitUSD: &limit, ModelPricing: []service.ChannelModelPricing{{Models: []string{"shared"}, InputPrice: &input, OutputPrice: &output}}},
		{ID: 13, Platform: service.PlatformOpenAI, Hydrated: true, Status: service.StatusActive, ModelPricing: []service.ChannelModelPricing{{Models: []string{"shared"}, InputPrice: &input, OutputPrice: &output}}},
	}}
	now := time.Now()
	subs := &deviceRouteSubscriptions{subs: []service.UserSubscription{
		{ID: 21, UserID: 17, GroupID: 11, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Status: service.SubscriptionStatusActive, DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now},
		{ID: 22, UserID: 17, GroupID: 12, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Status: service.SubscriptionStatusActive, DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now},
	}}
	users := &deviceRouteUsers{user: user}
	keys := service.NewAPIKeyService(keysRepo, users, groups, subs, nil, nil, cfg)
	settings := service.NewSettingService(&deviceRouteSettings{values: map[string]string{
		"desktop.enabled": "true",
		"desktop.models":  `[{"id":"image-sub","group_id":11,"model":"shared","display_name":"Image","provider_label":"Aino","platform":"openai","api_mode":"images_generations","capabilities":{"image_generation":true}},{"id":"text-sub","group_id":12,"model":"shared","display_name":"Text Subscription","provider_label":"Aino","platform":"openai","api_mode":"chat_completions","agent_verified":true,"capabilities":{"tools":true}},{"id":"text-balance","group_id":13,"model":"shared","display_name":"Text Balance","provider_label":"Aino","platform":"openai","api_mode":"chat_completions","agent_verified":true,"capabilities":{"tools":true}}]`,
	}}, cfg)
	billing := service.NewBillingService(cfg, nil)
	eligibility := service.NewBillingCacheService(nil, users, subs, nil, nil, nil, cfg, nil)
	t.Cleanup(eligibility.Stop)
	models := service.NewDesktopModelService(settings, keys, billing, service.NewModelPricingResolver(nil, billing), eligibility, nil)
	subscriptions := service.NewSubscriptionService(nil, subs, nil, nil, cfg)
	t.Cleanup(subscriptions.Stop)
	router := gin.New()
	var tasks *service.ImageTaskService
	if len(taskServices) > 0 {
		tasks = taskServices[0]
	}
	router.Use(apiKeyAuthWithDeviceRouting(keys, subscriptions, cfg, models, tasks))
	check := func(c *gin.Context) {
		selected, _ := GetAPIKeyFromContext(c)
		subscription, _ := GetSubscriptionFromContext(c)
		if c.Request.Header.Get("X-Aino-Model-ID") != "" {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.JSON(http.StatusOK, gin.H{"key_id": selected.ID, "group_id": selected.GroupID, "subscription_id": subscription.ID})
	}
	router.POST("/v1/chat/completions", check)
	router.POST("/v1/images/generations", check)
	router.GET("/v1/images/tasks/:task_id", check)
	router.GET("/v1/models", func(c *gin.Context) { c.Status(http.StatusInternalServerError) })
	router.GET("/v1/sub2api/billing", func(c *gin.Context) {
		selected, _ := GetAPIKeyFromContext(c)
		c.JSON(http.StatusOK, gin.H{"group_id": selected.GroupID})
	})
	return router, key, subs
}

func serveDeviceRequest(router *gin.Engine, method, path, id, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer device-test")
	req.Header.Set("Content-Type", "application/json")
	if id != "" {
		req.Header.Set("X-Aino-Model-ID", id)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestDeviceRoutesSameKeyConcurrentSubscriptionTextAndImageAtZeroBalance(t *testing.T) {
	router, key, _ := deviceRouteFixture(t)
	var wg sync.WaitGroup
	failures := make(chan string, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path, id, expected := "/v1/chat/completions", "text-sub", `"group_id":12,"key_id":2,"subscription_id":22`
			if i%2 == 0 {
				path, id, expected = "/v1/images/generations", "image-sub", `"group_id":11,"key_id":2,"subscription_id":21`
			}
			w := serveDeviceRequest(router, http.MethodPost, path, id, `{"model":"shared"}`)
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), expected) {
				failures <- w.Body.String()
			}
		}(i)
	}
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
	require.Nil(t, key.GroupID)
	require.Nil(t, key.Group)
}

func TestDeviceRouteRequiresCatalogAndNeverFallsBackToBalance(t *testing.T) {
	router, key, subs := deviceRouteFixture(t)
	for _, test := range []struct {
		id, path, body string
		status         int
	}{
		{"", "/v1/chat/completions", `{"model":"shared"}`, 400},
		{"text-sub", "/v1/chat/completions", `{"model":"other"}`, 400},
		{"text-sub", "/v1/chat/completions", `{"model":"shared","MODEL":"other"}`, 400},
		{"text-sub", "/v1/images/generations", `{"model":"shared"}`, 403},
		{"text-balance", "/v1/chat/completions", `{"model":"shared"}`, 403},
	} {
		w := serveDeviceRequest(router, http.MethodPost, test.path, test.id, test.body)
		require.Equal(t, test.status, w.Code, w.Body.String())
	}
	key.User.Balance = 100
	subs.subs[1].WeeklyUsageUSD = 11
	w := serveDeviceRequest(router, http.MethodPost, "/v1/chat/completions", "text-sub", `{"model":"shared"}`)
	require.Equal(t, http.StatusTooManyRequests, w.Code, w.Body.String())
	subs.subs[1].WeeklyUsageUSD = 0
	subs.subs[1].ExpiresAt = time.Now().Add(-time.Second)
	w = serveDeviceRequest(router, http.MethodPost, "/v1/chat/completions", "text-sub", `{"model":"shared"}`)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
}

func TestDeviceCatalogAndBillingReadsDoNotCheckSpendableFunds(t *testing.T) {
	router, _, subs := deviceRouteFixture(t)
	subs.subs[1].WeeklyUsageUSD = 11
	w := serveDeviceRequest(router, http.MethodGet, "/v1/models", "", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"id":"text-sub"`)
	require.Contains(t, w.Body.String(), `"id":"text-balance"`)
	require.Contains(t, w.Body.String(), `"state":"quota_exhausted"`)
	w = serveDeviceRequest(router, http.MethodGet, "/v1/sub2api/billing", "", "")
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	w = serveDeviceRequest(router, http.MethodGet, "/v1/sub2api/billing", "text-sub", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

type deviceRouteTaskStore struct{ task *service.ImageTaskRecord }

func (s *deviceRouteTaskStore) Save(_ context.Context, task *service.ImageTaskRecord, _ time.Duration) error {
	s.task = task
	return nil
}
func (s *deviceRouteTaskStore) Get(_ context.Context, id string) (*service.ImageTaskRecord, error) {
	if s.task == nil || s.task.ID != id {
		return nil, service.ErrImageTaskNotFound
	}
	return s.task, nil
}

func TestDeviceTaskReadRestoresOriginalRouteWithoutCurrentEntitlement(t *testing.T) {
	store := &deviceRouteTaskStore{}
	tasks := service.NewImageTaskService(store)
	groupID := int64(89)
	accepted := &service.APIKey{ID: 2, UserID: 17, GroupID: &groupID, Group: &service.Group{ID: 89, Platform: service.PlatformGrok, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeSubscription, IsExclusive: true}}
	sub := &service.UserSubscription{ID: 30, UserID: 17, GroupID: 89}
	task, err := tasks.Create(context.Background(), service.ImageTaskOwner{UserID: 17, APIKeyID: 2}, service.ImageTaskRouteFromRequest(accepted, sub))
	require.NoError(t, err)
	store.task.Route.Group.Status = service.StatusDisabled
	router, key, subs := deviceRouteFixture(t, tasks)
	// Current account has no group 89 and no subscription; paid results still
	// belong to the unchanged key after secret rotation, expiry, or quota use.
	subs.subs = nil
	key.Status = service.StatusAPIKeyQuotaExhausted
	w := serveDeviceRequest(router, http.MethodGet, "/v1/images/tasks/"+task.ID, "", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"group_id":89`)
	require.Contains(t, w.Body.String(), `"subscription_id":30`)
	store.task.APIKeyID = 3
	w = serveDeviceRequest(router, http.MethodGet, "/v1/images/tasks/"+task.ID, "", "")
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	store.task.APIKeyID = 2
	store.task.Route = nil
	w = serveDeviceRequest(router, http.MethodGet, "/v1/images/tasks/"+task.ID, "", "")
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String(), "old records cannot be assigned a guessed billing group")
}
