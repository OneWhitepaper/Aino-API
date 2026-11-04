package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type desktopCatalogUserRepo struct{ service.UserRepository }

func (desktopCatalogUserRepo) GetByID(context.Context, int64) (*service.User, error) {
	return &service.User{ID: 17, Status: service.StatusActive, AllowedGroups: []int64{11}, RestrictPublicGroups: true}, nil
}

type desktopCatalogGroupRepo struct {
	service.GroupRepository
	groups []service.Group
}

func (r desktopCatalogGroupRepo) ListActive(context.Context) ([]service.Group, error) {
	return r.groups, nil
}

type desktopCatalogSubscriptionRepo struct {
	service.UserSubscriptionRepository
}

func (desktopCatalogSubscriptionRepo) ListActiveByUserID(context.Context, int64) ([]service.UserSubscription, error) {
	return nil, nil
}

func desktopImageCatalogRouter(t *testing.T, allowImages bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeSimple}
	users := desktopCatalogUserRepo{}
	subscriptions := desktopCatalogSubscriptionRepo{}
	input, output := 0.000002, 0.000006
	groups := desktopCatalogGroupRepo{groups: []service.Group{
		{ID: 11, Status: service.StatusActive, Platform: service.PlatformOpenAI, RateMultiplier: 1, AllowImageGeneration: allowImages, ModelAllowlist: service.GroupModelAllowlist{Enabled: true, Models: []string{"fixture-chat", "fixture-image"}}, ModelPricing: []service.ChannelModelPricing{{Models: []string{"fixture-chat"}, InputPrice: &input, OutputPrice: &output}}},
		{ID: 22, Status: service.StatusActive, Platform: service.PlatformOpenAI, RateMultiplier: 1, AllowImageGeneration: true},
	}}
	entries := []service.DesktopModelEntry{}
	for _, entry := range []struct {
		id, model string
		groupID   int64
		image     bool
	}{
		{"chat", "fixture-chat", 11, false},
		{"image", "fixture-image", 11, true},
		{"denied-chat", "denied-chat", 11, false},
		{"denied-image", "denied-image", 11, true},
		{"foreign-chat", "foreign-chat", 22, false},
		{"foreign-image", "foreign-image", 22, true},
	} {
		mode := "chat_completions"
		if entry.image {
			mode = "images_generations"
		}
		entries = append(entries, service.DesktopModelEntry{ID: entry.id, Model: entry.model, GroupID: entry.groupID, DisplayName: entry.id, ProviderLabel: "Fixture", Platform: service.PlatformOpenAI, APIMode: mode, AgentVerified: !entry.image, Capabilities: service.PlatformModelCapabilities{Tools: !entry.image, ImageGeneration: entry.image}})
	}
	blob, err := json.Marshal(entries)
	require.NoError(t, err)
	settings := service.NewSettingService(desktopHandlerSettingRepo{values: map[string]string{"desktop.enabled": "true", "desktop.models": string(blob), "desktop.default_model_id": "chat", "desktop.default_image_model_id": "image"}}, cfg)
	keys := service.NewAPIKeyService(nil, users, groups, subscriptions, nil, nil, cfg)
	billing := service.NewBillingService(cfg, nil)
	eligibility := service.NewBillingCacheService(nil, users, subscriptions, nil, nil, nil, cfg, nil)
	t.Cleanup(eligibility.Stop)
	handler := NewDesktopHandler(service.NewDesktopModelService(settings, keys, billing, service.NewModelPricingResolver(nil, billing), eligibility, nil))
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 17})
		c.Next()
	})
	router.GET("/api/v1/desktop/models", handler.ListModels)
	router.GET("/api/v1/desktop/bootstrap", handler.GetBootstrap)
	return router
}

func TestDesktopImageCatalogRequiresExplicitOptInForLegacyClients(t *testing.T) {
	router := desktopImageCatalogRouter(t, true)
	for _, test := range []struct {
		name, query string
		wantIDs     []string
	}{
		{"legacy default", "", []string{"chat"}},
		{"explicit false", "?include_images=false", []string{"chat"}},
		{"empty default", "?include_images=", []string{"chat"}},
		{"image opt in", "?include_images=true", []string{"chat", "image"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/desktop/models"+test.query, nil))
			require.Equal(t, http.StatusOK, recorder.Code)
			var envelope struct {
				Data []service.PlatformModel `json:"data"`
			}
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
			ids := []string{}
			for _, model := range envelope.Data {
				ids = append(ids, model.ID)
				require.Equal(t, "available", model.State)
				if model.Purpose == service.DesktopPurposeChat {
					require.Contains(t, []string{"chat_completions", "responses", "anthropic_messages"}, model.APIMode)
				}
			}
			require.Equal(t, test.wantIDs, ids, "query opt-in must preserve group and model allowlists")
			require.NotContains(t, recorder.Body.String(), "group_id")
		})
	}
}

func TestDesktopImageCatalogOptInPreservesImagePermission(t *testing.T) {
	router := desktopImageCatalogRouter(t, false)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/desktop/models?include_images=true", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	var envelope struct {
		Data []service.PlatformModel `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Len(t, envelope.Data, 2)
	require.Equal(t, "available", envelope.Data[0].State)
	require.Equal(t, "unavailable", envelope.Data[1].State)
	require.Equal(t, "IMAGE_GENERATION_NOT_ALLOWED", *envelope.Data[1].ReasonCode)
	require.False(t, envelope.Data[1].IsDefault)
}

func TestDesktopImageCatalogRejectsInvalidOptIn(t *testing.T) {
	router := desktopImageCatalogRouter(t, true)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/desktop/models?include_images=maybe", nil))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "Invalid include_images value")
}

func TestDesktopImageCatalogBootstrapKeepsSeparateDefaults(t *testing.T) {
	router := desktopImageCatalogRouter(t, true)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/desktop/bootstrap", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	var envelope struct {
		Data service.DesktopBootstrap `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Equal(t, "chat", *envelope.Data.DefaultModelID)
	require.Equal(t, "image", *envelope.Data.DefaultImageModelID)
}
