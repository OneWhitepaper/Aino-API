//go:build unit

package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Use the real request projection, rather than trusting user supplied headers.
func catalogImagesContext(t *testing.T, model, endpoint, contentType string, body []byte) *gin.Context {
	t.Helper()
	models, keys := desktopImageFixture(t)
	repo := models.settings.settingRepo.(*desktopSettingsRepo)
	repo.values["desktop.models"] = strings.ReplaceAll(repo.values["desktop.models"], "fixture-image", model)
	keys.groupRepo.(*desktopGroupRepo).groups[0].ModelPricing[0].Models = []string{model}
	identity := &APIKey{ID: 2, UserID: 17, DesktopManaged: true, DesktopCredentialType: "device", User: &User{ID: 17}}
	selected, err := models.ResolveRequest(context.Background(), identity, "image", "images_generations", []string{model}, DesktopPurposeImage, true)
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", contentType)
	c.Request = c.Request.WithContext(WithDesktopCredentialGroup(c.Request.Context(), *selected.Key.GroupID))
	c.Set("api_key", selected.Key)
	return c
}

func TestCatalogImagesAliasParsesAndForwards(t *testing.T) {
	for _, kind := range []string{"generation", "async_generation", "json_edit", "multipart_edit", "channel_mapping", "account_mapping", "native_mapped_alias", "composite_multipart", "composite_json"} {
		t.Run(kind, func(t *testing.T) {
			model, upstreamModel, channelModel := "Nano Banana 2", "Nano Banana 2", ""
			endpoint, contentType := openAIImagesGenerationsEndpoint, "application/json"
			credentials := map[string]any{"api_key": "fixture-key", "base_url": "https://compatible.example/v1"}
			switch kind {
			case "async_generation":
				endpoint += "/async"
			case "channel_mapping":
				channelModel, upstreamModel = "Configured Image Alias", "Configured Image Alias"
			case "account_mapping":
				upstreamModel = "gemini-3.1-flash-image"
				credentials["model_mapping"] = map[string]any{model: upstreamModel}
			case "native_mapped_alias":
				model = "gpt-image-2"
				credentials["model_mapping"] = map[string]any{model: upstreamModel}
			case "composite_multipart", "composite_json":
				upstreamModel = "Configured Image Alias"
			}
			body := []byte(fmt.Sprintf(`{"model":%q,"prompt":"draw","size":"1024x1024","quality":"auto","custom_field":"preserved"}`, model))
			if kind == "json_edit" {
				endpoint = openAIImagesEditsEndpoint
				body = []byte(fmt.Sprintf(`{"model":%q,"prompt":"edit","images":[{"image_url":"https://example.com/input.png"}],"custom_field":"preserved"}`, model))
			}
			if strings.Contains(kind, "multipart") {
				endpoint = openAIImagesEditsEndpoint
				var buf bytes.Buffer
				writer := multipart.NewWriter(&buf)
				require.NoError(t, writer.WriteField("model", model))
				require.NoError(t, writer.WriteField("prompt", "edit"))
				require.NoError(t, writer.WriteField("custom_field", "preserved"))
				part, err := writer.CreateFormFile("image", "input.png")
				require.NoError(t, err)
				_, err = part.Write([]byte("original-image-bytes"))
				require.NoError(t, err)
				require.NoError(t, writer.Close())
				body, contentType = buf.Bytes(), writer.FormDataContentType()
			}
			c := catalogImagesContext(t, model, endpoint, contentType, body)
			if strings.HasPrefix(kind, "composite_") {
				c.Request = c.Request.WithContext(WithCompositeRouteDecision(c.Request.Context(), CompositeRouteDecision{Matched: true, TargetPlatform: PlatformOpenAI, UpstreamModel: upstreamModel, PublicModel: model}))
				if kind == "composite_json" {
					// Composite middleware rewrites JSON before the images parser.
					body = bytes.ReplaceAll(body, []byte(model), []byte(upstreamModel))
				}
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"data":[{"b64_json":"aW1hZ2U="}],"usage":{"input_tokens":10,"output_tokens":5}}`))}}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			if kind != "native_mapped_alias" {
				require.Equal(t, OpenAIImagesCapabilityAPIKey, parsed.RequiredCapabilityForModel(channelModel))
				for _, typ := range []string{AccountTypeOAuth, AccountTypeSetupToken} {
					require.False(t, (&Account{Platform: PlatformOpenAI, Type: typ}).SupportsOpenAIImageCapability(parsed.RequiredCapability))
				}
			}
			result, err := svc.ForwardImages(c.Request.Context(), c, &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: credentials}, body, parsed, channelModel)
			require.NoError(t, err)
			require.Equal(t, upstreamModel, result.UpstreamModel)
			require.Equal(t, firstNonEmptyString(channelModel, parsed.Model), result.Model)
			require.Equal(t, 1, result.ImageCount)
			require.Equal(t, 10, result.Usage.InputTokens)
			require.Equal(t, 5, result.Usage.OutputTokens)
			require.True(t, result.ImageEndpoint)
			require.Equal(t, "https://compatible.example"+parsed.Endpoint, upstream.lastReq.URL.String())
			if parsed.Multipart {
				req := httptest.NewRequest(http.MethodPost, endpoint, bytes.NewReader(upstream.lastBody))
				req.Header.Set("Content-Type", upstream.lastReq.Header.Get("Content-Type"))
				require.NoError(t, req.ParseMultipartForm(1<<20))
				require.Equal(t, upstreamModel, req.FormValue("model"))
				require.Equal(t, "preserved", req.FormValue("custom_field"))
				file, _, err := req.FormFile("image")
				require.NoError(t, err)
				defer file.Close()
				data, err := io.ReadAll(file)
				require.NoError(t, err)
				require.Equal(t, "original-image-bytes", string(data))
			} else {
				require.Equal(t, upstreamModel, gjson.GetBytes(upstream.lastBody, "model").String())
				require.Equal(t, "preserved", gjson.GetBytes(upstream.lastBody, "custom_field").String())
			}
		})
	}
}

func TestCatalogImagesAliasRequiresVerifiedImageGrant(t *testing.T) {
	for _, kind := range []string{"ordinary_key", "headers_only", "text_purpose", "other_model", "no_allowlist", "wildcard_allowlist", "image_disabled", "no_fixed_group", "different_fixed_group"} {
		t.Run(kind, func(t *testing.T) {
			body := []byte(`{"model":"Nano Banana 2","prompt":"draw"}`)
			c := catalogImagesContext(t, "Nano Banana 2", openAIImagesGenerationsEndpoint, "application/json", body)
			key := getAPIKeyFromContext(c)
			switch kind {
			case "ordinary_key":
				key.DesktopManaged = false
			case "headers_only":
				c.Set("api_key", nil)
				c.Request.Header.Set("X-Aino-Model-ID", "image")
				c.Request.Header.Set("X-Aino-Purpose", "image")
			case "text_purpose":
				key.DesktopPurpose = DesktopPurposeChat
			case "other_model":
				key.Group.ModelAllowlist.Models = []string{"other"}
			case "no_allowlist":
				key.Group.ModelAllowlist.Enabled = false
			case "wildcard_allowlist":
				key.Group.ModelAllowlist.Models = []string{"*"}
			case "image_disabled":
				key.Group.AllowImageGeneration = false
			case "no_fixed_group":
				c.Request = c.Request.WithContext(context.Background())
			case "different_fixed_group":
				c.Request = c.Request.WithContext(WithDesktopCredentialGroup(c.Request.Context(), 999))
			}
			_, err := (&OpenAIGatewayService{}).ParseOpenAIImagesRequest(c, body)
			require.ErrorContains(t, err, "images endpoint requires an image model")
		})
	}
}

func TestCatalogImagesForwardRevalidatesGrant(t *testing.T) {
	for _, kind := range []string{"model_changed", "known_model_changed", "group_changed", "key_changed", "allowlist_disabled", "image_disabled", "oauth", "oauth_mapped_native"} {
		t.Run(kind, func(t *testing.T) {
			body := []byte(`{"model":"Nano Banana 2","prompt":"draw"}`)
			c := catalogImagesContext(t, "Nano Banana 2", openAIImagesGenerationsEndpoint, "application/json", body)
			upstream := &httpUpstreamRecorder{}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			key := getAPIKeyFromContext(c)
			account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
			channelModel := ""
			switch kind {
			case "model_changed":
				parsed.Model = "other"
			case "known_model_changed":
				parsed.Model = "gpt-image-2"
			case "group_changed":
				groupID := int64(999)
				key.GroupID, key.Group.ID = &groupID, groupID
			case "key_changed":
				key.ID++
			case "allowlist_disabled":
				key.Group.ModelAllowlist.Enabled = false
			case "image_disabled":
				key.Group.AllowImageGeneration = false
			case "oauth":
				account.Type = AccountTypeOAuth
			case "oauth_mapped_native":
				account.Type, channelModel = AccountTypeOAuth, "gpt-image-2"
			}
			_, err = svc.ForwardImages(c.Request.Context(), c, account, body, parsed, channelModel)
			require.Error(t, err)
			require.Empty(t, upstream.requests)
		})
	}
}

func TestImageCapabilityHonorsAccountModelMapping(t *testing.T) {
	for _, target := range []string{"Nano Banana 2", "gemini-3.1-flash-image", "gpt-image-2"} {
		t.Run(target, func(t *testing.T) {
			account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-image-2": target}}}
			allowed := accountSupportsOpenAICapabilities(context.Background(), account, "gpt-image-2", "", OpenAIImagesCapabilityNative)
			require.Equal(t, target == "gpt-image-2", allowed)
			if target != "gpt-image-2" {
				apiKeyAccount := *account
				apiKeyAccount.Type = AccountTypeAPIKey
				require.True(t, accountSupportsOpenAICapabilities(context.Background(), &apiKeyAccount, "gpt-image-2", "", OpenAIImagesCapabilityNative))
			}
		})
	}
}

func TestImageCapabilityUsesChannelThenAccountMapping(t *testing.T) {
	for _, tt := range []struct {
		name, originalTarget, channelTarget string
		allowed                             bool
	}{
		{"channel_requires_apikey", "gpt-image-2", "Nano Banana 2", false},
		{"channel_keeps_native", "Nano Banana 2", "gpt-image-1", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-image-2": tt.originalTarget, "gpt-image-1": tt.channelTarget}}}
			ctx := context.WithValue(context.Background(), openAIImagesChannelModelKey{}, "gpt-image-1")
			require.Equal(t, tt.allowed, accountSupportsOpenAICapabilities(ctx, account, "gpt-image-2", "", OpenAIImagesCapabilityNative))
			account.Type = AccountTypeAPIKey
			require.True(t, accountSupportsOpenAICapabilities(ctx, account, "gpt-image-2", "", OpenAIImagesCapabilityNative))
		})
	}
}

func TestCatalogImageAliasHonorsAccountModelAllowlist(t *testing.T) {
	for _, tt := range []struct {
		name    string
		mapping map[string]any
		allowed bool
	}{
		{"catalog_alias_present", map[string]any{"Nano Banana 2": "Nano Banana 2"}, true},
		{"other_images_only", map[string]any{"gpt-image-2": "gpt-image-2", "gemini-3.1-flash-image": "gemini-3.1-flash-image"}, false},
		{"explicit_wildcard", map[string]any{"*": "*"}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"model_mapping": tt.mapping}}
			require.Equal(t, tt.allowed, supportsOpenAIRequestModel(account, "Nano Banana 2", false))
		})
	}
}

func TestCatalogImagesAliasKeepsLegacyImageLease(t *testing.T) {
	models, keys := desktopImageFixture(t)
	groupID := int64(11)
	expires := time.Now().Add(time.Hour)
	key := &APIKey{ID: 2, UserID: 17, GroupID: &groupID, Status: StatusActive, DesktopManaged: true, ExpiresAt: &expires}
	keys.apiKeyRepo = desktopImageAPIKeys{key: key}
	repo := &desktopImageLeaseRepo{lease: &DesktopCredential{Purpose: DesktopPurposeImage, APIKeyID: 2, UserID: 17, GroupID: groupID, SessionFamilyID: "family", ExpiresAt: expires}}
	keys.SetDesktopCredentialDependencies(repo, desktopImageParentCache{}, models.settings)
	require.NoError(t, keys.ValidateDesktopCredential(context.Background(), key))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, openAIImagesGenerationsEndpoint, nil)
	c.Request = c.Request.WithContext(WithDesktopCredentialGroup(c.Request.Context(), groupID))
	c.Set("api_key", key)
	parsed, err := (&OpenAIGatewayService{}).ParseOpenAIImagesRequest(c, []byte(`{"model":"fixture-image","prompt":"draw"}`))
	require.NoError(t, err)
	require.Equal(t, OpenAIImagesCapabilityAPIKey, parsed.RequiredCapability)
}
