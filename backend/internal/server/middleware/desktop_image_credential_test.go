package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type imageLeaseRepo struct {
	service.DesktopCredentialRepository
	lease *service.DesktopCredential
}

func (r imageLeaseRepo) GetByAPIKeyID(context.Context, int64) (*service.DesktopCredential, error) {
	return r.lease, nil
}

type imageKeyRepo struct {
	service.APIKeyRepository
	key *service.APIKey
}

func (r imageKeyRepo) GetByID(context.Context, int64) (*service.APIKey, error) {
	key := *r.key
	return &key, nil
}

type imageUserRepo struct {
	service.UserRepository
	user *service.User
}

func (r imageUserRepo) GetByID(context.Context, int64) (*service.User, error) { return r.user, nil }

type imageGroupRepo struct {
	service.GroupRepository
	group *service.Group
}

func (r imageGroupRepo) GetByID(context.Context, int64) (*service.Group, error) { return r.group, nil }

type imageSettingsRepo struct{ service.SettingRepository }

func (imageSettingsRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{"desktop.enabled": "true", "desktop.models": `[{"id":"image","group_id":11,"model":"fixture-image","display_name":"Image","provider_label":"Aino","platform":"openai","api_mode":"images_generations","capabilities":{"image_generation":true}},{"id":"chat","group_id":11,"model":"fixture-chat","display_name":"Chat","provider_label":"Aino","platform":"openai","api_mode":"chat_completions","agent_verified":true,"capabilities":{"tools":true}}]`}, nil
}

type imageParentCache struct{ service.RefreshTokenCache }

func (imageParentCache) ActiveFamilyDeadline(context.Context, int64, string) (time.Time, error) {
	return time.Now().Add(time.Hour), nil
}

func TestDesktopImageCredentialSeparatesInferenceRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	group := &service.Group{ID: 11, Platform: service.PlatformOpenAI, Status: service.StatusActive, AllowImageGeneration: true}
	user := &service.User{ID: 17, Status: service.StatusActive, TokenVersionResolved: true}
	expires := time.Now().Add(time.Hour)
	fresh := &service.APIKey{ID: 2, UserID: 17, DesktopManaged: true, GroupID: &group.ID, Status: service.StatusActive, ExpiresAt: &expires}
	for _, purpose := range []string{service.DesktopPurposeChat, service.DesktopPurposeImage} {
		for _, path := range []string{"/v1/images/generations", "/v1/images/edits", "/v1/images/generations/async", "/v1/chat/completions", "/v1/responses", "/v1/messages"} {
			t.Run(purpose+path, func(t *testing.T) {
				cfg := &config.Config{RunMode: config.RunModeSimple}
				keys := service.NewAPIKeyService(imageKeyRepo{key: fresh}, imageUserRepo{user: user}, imageGroupRepo{group: group}, nil, nil, nil, cfg)
				keys.SetDesktopCredentialDependencies(imageLeaseRepo{lease: &service.DesktopCredential{Purpose: purpose, UserID: 17, GroupID: 11, ExpiresAt: expires, SessionFamilyID: "family"}}, imageParentCache{}, service.NewSettingService(imageSettingsRepo{}, cfg))
				router := gin.New()
				router.POST(path, func(c *gin.Context) {
					key := *fresh
					if enforceDesktopCredential(c, keys, &key) {
						c.Status(http.StatusOK)
					}
				})
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
				want := http.StatusForbidden
				if purpose == service.DesktopPurposeImage && (path == "/v1/images/generations" || path == "/v1/images/edits") || purpose == service.DesktopPurposeChat && (path == "/v1/chat/completions" || path == "/v1/responses" || path == "/v1/messages") {
					want = http.StatusOK
				}
				require.Equal(t, want, rec.Code, rec.Body.String())
			})
		}
	}
}
