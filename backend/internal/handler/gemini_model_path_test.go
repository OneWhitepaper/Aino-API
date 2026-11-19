package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/gemini"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newGeminiModelPathTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	f := newGeminiDeclaredModelsFixture(t, []service.Account{{
		ID: 1, Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "test-key",
			"model_mapping": map[string]any{
				"gemini-4-argon[次]":        "gemini-4-argon",
				"gemini-4-carbon[次]":       "gemini-4-carbon",
				"gemini-3.8-flash-high[次]": "gemini-3.8-flash-high",
				"[次]gemini-4-neon":         "gemini-4-neon",
			},
		},
	}})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{
			ID: 1, GroupID: &f.groupID,
			Group: &service.Group{ID: f.groupID, Platform: service.PlatformGemini},
		})
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1, Concurrency: 1})
		c.Next()
	})
	router.GET("/v1beta/models/:model", f.handler.GeminiV1BetaGetModel)
	router.POST("/v1beta/models/*modelAction", f.handler.GeminiV1BetaModels)
	return router
}

func TestGeminiModelPathSupportsDeclaredChineseSuffixAliases(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := newGeminiModelPathTestRouter(t)
	for _, tt := range []struct {
		model string
		path  string
	}{
		{"gemini-4-argon[次]", "/v1beta/models/gemini-4-argon%5B%E6%AC%A1%5D"},
		{"gemini-4-carbon[次]", "/v1beta/models/gemini-4-carbon%5B%E6%AC%A1%5D"},
		{"gemini-3.8-flash-high[次]", "/v1beta/models/gemini-3.8-flash-high%5B%E6%AC%A1%5D"},
		{"[次]gemini-4-neon", "/v1beta/models/%5B%E6%AC%A1%5Dgemini-4-neon"},
	} {
		t.Run(tt.model, func(t *testing.T) {
			t.Run("declared_model", func(t *testing.T) {
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				var got gemini.Model
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
				require.Equal(t, "models/"+tt.model, got.Name)
				require.Contains(t, got.SupportedGenerationMethods, "generateContent")
			})

			// An empty body stops immediately after the real native model-path
			// validation, without needing billing or account scheduling fixtures.
			t.Run("native_entry", func(t *testing.T) {
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, tt.path+":generateContent", nil))
				require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
				require.Contains(t, rec.Body.String(), `"message":"Request body is empty"`)
			})
		})
	}
}

func TestGeminiModelPathRejectsEncodedStructureInjection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := newGeminiModelPathTestRouter(t)
	for _, model := range []string{
		"%2e%2e",
		"gemini-4-argon%2F..%2Fsecret",
		"gemini-4-argon%252Fsecret",
		"gemini-4-argon%3Fkey%3Dinjected",
		"gemini-4-argon%23fragment",
		"gemini-4-argon%5Csecret",
		"gemini-4-argon%00suffix",
	} {
		t.Run(model, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1beta/models/"+model+":generateContent", nil))
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), `"message":"Invalid model in URL"`)
		})
	}
}
