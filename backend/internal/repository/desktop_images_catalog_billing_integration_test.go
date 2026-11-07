//go:build integration

package repository_test

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Actual auth projection, Images passthrough and the transactional ledger must
// agree on the catalog model and billing group, with no group baked into a key.
func TestDesktopCatalogImageAliasUsesSameKeyAndSettles(t *testing.T) {
	r := newDesktopModelRig(t)
	user := r.user(t, service.StatusActive, false)
	balanceGroup, subGroup := r.group(t, false), r.group(t, true)
	const model = "Nano Banana 2"
	price := 0.04
	for _, group := range []*service.Group{balanceGroup, subGroup} {
		group.AllowImageGeneration = true
		group.ModelPricing = []service.ChannelModelPricing{{Models: []string{model}, BillingMode: service.BillingModeImage, PerRequestPrice: &price}}
		require.NoError(t, r.groups.Update(r.ctx, group))
	}
	require.NoError(t, r.userRepo.AddGroupToAllowedGroups(r.ctx, user.ID, balanceGroup.ID))
	now := time.Now()
	sub := &service.UserSubscription{UserID: user.ID, GroupID: subGroup.ID, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Status: service.SubscriptionStatusActive, DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now}
	require.NoError(t, r.subs.Create(r.ctx, sub))
	balanceEntry, subEntry := desktopEntry("image-wallet", balanceGroup.ID), desktopEntry("image-subscription", subGroup.ID)
	for _, entry := range []*service.DesktopModelEntry{&balanceEntry, &subEntry} {
		entry.Model, entry.APIMode, entry.Capabilities = model, "images_generations", service.PlatformModelCapabilities{ImageGeneration: true}
	}
	r.catalog(t, []service.DesktopModelEntry{balanceEntry, subEntry}, "")
	device := service.NewDesktopDeviceKeyService(repository.NewDesktopDeviceKeyRepository(r.client), r.keys)
	identity, err := device.Ensure(r.ctx, user.ID, service.DesktopDeviceKeyRequest{DeviceID: uuid.NewString()})
	require.NoError(t, err)
	db := repository.GetIntegrationDB()
	logs := repository.NewUsageLogRepository(r.client, db)
	accountRow, err := r.client.Account.Create().SetName("catalog-image-fixture").SetPlatform(service.PlatformOpenAI).SetType(service.AccountTypeAPIKey).SetCredentials(map[string]any{}).Save(r.ctx)
	require.NoError(t, err)
	upstreamRequests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		upstreamRequests++
		if strings.HasPrefix(req.Header.Get("Content-Type"), "multipart/") {
			require.NoError(t, req.ParseMultipartForm(1<<20))
			require.Equal(t, "gemini-3.1-flash-image", req.FormValue("model"))
			file, _, err := req.FormFile("image")
			require.NoError(t, err)
			defer file.Close()
			data, err := io.ReadAll(file)
			require.NoError(t, err)
			require.Equal(t, "original-image-bytes", string(data))
		} else {
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.Equal(t, model, gjson.GetBytes(body, "model").String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"b64_json":"aW1hZ2U="}],"usage":{"input_tokens":10,"output_tokens":5}}`)
	}))
	defer upstream.Close()
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.AllowPrivateHosts = true
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	billingCache := service.NewBillingCacheService(nil, r.userRepo, r.subs, nil, nil, r.rates, cfg, nil)
	t.Cleanup(billingCache.Stop)
	gateway := service.NewOpenAIGatewayService(nil, logs, repository.NewUsageBillingRepository(r.client, db), r.userRepo, r.subs, r.rates, nil, cfg, nil, nil, r.billing, nil, billingCache, repository.NewHTTPUpstream(cfg), &service.DeferredService{}, nil, nil, r.pricing, nil, nil, nil, nil)
	subscriptions := service.NewSubscriptionService(r.groups, r.subs, billingCache, r.client, cfg)
	t.Cleanup(subscriptions.Stop)
	router := gin.New()
	forward := func(c *gin.Context) {
		key, _ := middleware.GetAPIKeyFromContext(c)
		subscription, _ := middleware.GetSubscriptionFromContext(c)
		body, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		parsed, err := gateway.ParseOpenAIImagesRequest(c, body)
		require.NoError(t, err)
		require.Equal(t, service.OpenAIImagesCapabilityAPIKey, parsed.RequiredCapability)
		account := &service.Account{ID: accountRow.ID, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"base_url": upstream.URL, "api_key": "fixture-only"}}
		if parsed.IsEdits() {
			account.Credentials["model_mapping"] = map[string]any{model: "gemini-3.1-flash-image"}
		}
		result, err := gateway.ForwardImages(c.Request.Context(), c, account, body, parsed, "")
		require.NoError(t, err)
		require.Equal(t, model, result.Model)
		result.RequestID = c.GetHeader("X-Test-Request")
		require.NoError(t, gateway.RecordUsage(context.Background(), &service.OpenAIRecordUsageInput{Result: result, APIKey: key, User: key.User, Account: account, Subscription: subscription}))
	}
	for _, endpoint := range []string{"/v1/images/generations", "/v1/images/edits"} {
		router.POST(endpoint, gin.HandlerFunc(middleware.ProvideAPIKeyAuthMiddleware(r.keys, subscriptions, cfg, r.models, nil)), forward)
	}
	dispatch := func(option, endpoint, contentType, requestID string, body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("Authorization", "Bearer "+identity.APIKey)
		req.Header.Set("X-Aino-Model-ID", option)
		req.Header.Set("X-Aino-Purpose", "image")
		req.Header.Set("X-Aino-Session-Id", uuid.NewString())
		req.Header.Set("X-Aino-Turn-Id", uuid.NewString())
		req.Header.Set("X-Aino-Call-Id", requestID)
		req.Header.Set("X-Test-Request", requestID)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	var before, after float64
	require.NoError(t, db.QueryRowContext(r.ctx, "SELECT balance FROM users WHERE id=$1", user.ID).Scan(&before))
	balanceRequest, subRequest := uuid.NewString(), uuid.NewString()
	generation := []byte(`{"model":"Nano Banana 2","prompt":"draw","quality":"auto","size":"1024x1024"}`)
	response := dispatch(balanceEntry.ID, "/v1/images/generations", "application/json", balanceRequest, generation)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var edit bytes.Buffer
	writer := multipart.NewWriter(&edit)
	require.NoError(t, writer.WriteField("model", model))
	require.NoError(t, writer.WriteField("prompt", "edit"))
	part, err := writer.CreateFormFile("image", "input.png")
	require.NoError(t, err)
	_, err = part.Write([]byte("original-image-bytes"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	response = dispatch(subEntry.ID, "/v1/images/edits", writer.FormDataContentType(), subRequest, edit.Bytes())
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NoError(t, db.QueryRowContext(r.ctx, "SELECT balance FROM users WHERE id=$1", user.ID).Scan(&after))
	require.InDelta(t, 0.08, before-after, 1e-8)
	var subUsed float64
	require.NoError(t, db.QueryRowContext(r.ctx, "SELECT daily_usage_usd FROM user_subscriptions WHERE id=$1", sub.ID).Scan(&subUsed))
	require.InDelta(t, 0.08, subUsed, 1e-8)
	for _, expected := range []struct {
		request      string
		group        int64
		subscription *int64
	}{{balanceRequest, balanceGroup.ID, nil}, {subRequest, subGroup.ID, &sub.ID}} {
		var rowID int64
		require.NoError(t, db.QueryRowContext(r.ctx, "SELECT id FROM usage_logs WHERE request_id=$1 ORDER BY id LIMIT 1", expected.request).Scan(&rowID))
		row, err := logs.GetByID(r.ctx, rowID)
		require.NoError(t, err)
		require.Equal(t, expected.group, *row.GroupID)
		require.Equal(t, expected.subscription, row.SubscriptionID)
		require.Equal(t, model, row.Model)
		require.Equal(t, 1, row.ImageCount)
		require.InDelta(t, 0.08, row.ActualCost, 1e-8)
		require.Equal(t, "settled", row.SettlementStatus)
	}
	// Removing image permission takes effect on the next request of this key.
	subGroup.AllowImageGeneration = false
	require.NoError(t, r.groups.Update(r.ctx, subGroup))
	response = dispatch(subEntry.ID, "/v1/images/generations", "application/json", uuid.NewString(), generation)
	require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
	require.Equal(t, 2, upstreamRequests)
	stored, err := repository.NewDesktopDeviceKeyRepository(r.client).GetDeviceKey(r.ctx, user.ID, identity.DeviceID)
	require.NoError(t, err)
	require.Nil(t, stored.GroupID)
	require.Nil(t, stored.ExpiresAt)
}
