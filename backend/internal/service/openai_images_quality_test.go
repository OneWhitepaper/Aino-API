//go:build unit

package service

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestImagesQualityMappingScopesToSelectedAccountAndUpstreamModel(t *testing.T) {
	for _, kind := range []string{"generation", "async", "json_edit", "multipart_edit", "omitted_json_quality", "omitted_multipart_quality", "channel_and_account_model", "unconfigured_account", "unconfigured_model", "explicit_quality"} {
		t.Run(kind, func(t *testing.T) {
			model, upstreamModel := "Nano Banana 2", "nano-upstream"
			endpoint, contentType := openAIImagesGenerationsEndpoint, "application/json"
			quality, expected, channelModel := "auto", "1k", ""
			mapping := map[string]any{upstreamModel: map[string]any{"auto": "1k"}}
			if kind == "unconfigured_account" {
				mapping = nil
				expected = "auto"
			}
			if kind == "unconfigured_model" {
				mapping = map[string]any{"other-model": map[string]any{"auto": "1k"}}
				expected = "auto"
			}
			if kind == "explicit_quality" {
				quality, expected = "high", "high"
			}
			if kind == "async" {
				endpoint += "/async"
			}
			body := []byte(`{"model":"Nano Banana 2","prompt":"synthetic image","quality":"` + quality + `","size":"auto","custom_field":"keep"}`)
			if kind == "omitted_json_quality" {
				body = bytes.ReplaceAll(body, []byte(`,"quality":"auto"`), nil)
				quality = ""
			}
			if kind == "json_edit" {
				endpoint = openAIImagesEditsEndpoint
				body = bytes.ReplaceAll(body, []byte(`"custom_field"`), []byte(`"images":[{"image_url":"https://example.com/image.png"}],"custom_field"`))
			}
			multipartBody := kind == "multipart_edit" || kind == "omitted_multipart_quality"
			if multipartBody {
				endpoint = openAIImagesEditsEndpoint
				var buffer bytes.Buffer
				writer := multipart.NewWriter(&buffer)
				for key, value := range map[string]string{"model": model, "prompt": "synthetic edit", "quality": quality, "custom_field": "keep"} {
					if kind == "omitted_multipart_quality" && key == "quality" {
						continue
					}
					require.NoError(t, writer.WriteField(key, value))
				}
				file, err := writer.CreateFormFile("image", "fixture.png")
				require.NoError(t, err)
				_, err = file.Write([]byte("unchanged-image"))
				require.NoError(t, err)
				require.NoError(t, writer.Close())
				body, contentType = buffer.Bytes(), writer.FormDataContentType()
				if kind == "omitted_multipart_quality" {
					quality = ""
				}
			}
			c := catalogImagesContext(t, model, endpoint, contentType, body)
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"data":[{"b64_json":"aW1hZ2U="}],"usage":{"input_tokens":3,"output_tokens":4}}`))}}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			account := &Account{ID: 17, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key", "base_url": "https://example.com/v1", "model_mapping": map[string]any{model: upstreamModel}, "image_quality_mapping": mapping}}
			if kind == "channel_and_account_model" {
				channelModel = "channel-alias"
				account.Credentials["model_mapping"] = map[string]any{channelModel: upstreamModel}
			}
			result, err := svc.ForwardImages(c.Request.Context(), c, account, body, parsed, channelModel)
			require.NoError(t, err)
			require.Equal(t, firstNonEmptyString(channelModel, model), result.Model)
			require.Equal(t, upstreamModel, result.UpstreamModel)
			require.Equal(t, 1, result.ImageCount)
			require.Equal(t, quality, parsed.Quality, "original request remains the authorization and accounting identity")
			if multipartBody {
				req := httptest.NewRequest(http.MethodPost, endpoint, bytes.NewReader(upstream.lastBody))
				req.Header.Set("Content-Type", upstream.lastReq.Header.Get("Content-Type"))
				require.NoError(t, req.ParseMultipartForm(1<<20))
				require.Equal(t, expected, req.FormValue("quality"))
				require.Equal(t, "keep", req.FormValue("custom_field"))
				file, _, err := req.FormFile("image")
				require.NoError(t, err)
				defer file.Close()
				data, err := io.ReadAll(file)
				require.NoError(t, err)
				require.Equal(t, "unchanged-image", string(data))
			} else {
				require.Equal(t, expected, gjson.GetBytes(upstream.lastBody, "quality").String())
				require.Equal(t, "keep", gjson.GetBytes(upstream.lastBody, "custom_field").String())
			}
		})
	}
}

func TestImagesQualityMappingIgnoresOtherCredentialTypes(t *testing.T) {
	for _, account := range []*Account{
		{Platform: PlatformOpenAI, Type: AccountTypeOAuth},
		{Platform: PlatformAnthropic, Type: AccountTypeAPIKey},
	} {
		account.Credentials = map[string]any{"image_quality_mapping": "invalid configuration ignored outside OpenAI API keys"}
		quality, mapped, err := imageQualityForUpstream(account, "Nano Banana 2", "auto")
		require.NoError(t, err)
		require.False(t, mapped)
		require.Equal(t, "auto", quality)
	}
}

func TestImagesQualityMappingValidatedBeforeAccountWrite(t *testing.T) {
	for _, invalid := range []any{"not-an-object", map[string]any{"*": map[string]any{"auto": "1k"}}, map[string]any{"Nano Banana 2": map[string]any{"auto": "arbitrary-value"}}} {
		repo := &updateAccountCredsRepoStub{account: &Account{ID: 17, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Credentials: map[string]any{"api_key": "retained"}}}
		_, err := (&adminServiceImpl{accountRepo: repo}).UpdateAccount(t.Context(), 17, &UpdateAccountInput{Credentials: map[string]any{"image_quality_mapping": invalid}})
		require.Error(t, err)
		require.Zero(t, repo.updateCalls)
	}
}
