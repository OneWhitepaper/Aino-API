//go:build unit

package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type cachedFundingCurrentKeyRepo struct {
	service.APIKeyRepository
	key   *service.APIKey
	reads int
}

func (r *cachedFundingCurrentKeyRepo) GetByID(context.Context, int64) (*service.APIKey, error) {
	r.reads++
	return r.key, nil
}

func TestGeminiCachedContentsPatch_LegacyFollowupRechecksCurrentKeyBeforePaidExtension(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*service.APIKey)
	}{
		{name: "expired status", edit: func(key *service.APIKey) { key.Status = service.StatusAPIKeyExpired }},
		{name: "expired timestamp", edit: func(key *service.APIKey) { expired := time.Now().Add(-time.Minute); key.ExpiresAt = &expired }},
		{name: "exhausted quota", edit: func(key *service.APIKey) { key.Quota = 1; key.QuotaUsed = 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newCachedBillingEnv(t, 1000, 900)
			env.seed(time.Now().Add(time.Hour))
			require.Nil(t, env.repo.records["abc"].FundingSnapshot)
			current := *env.apiKey
			tc.edit(&current)
			repo := &cachedFundingCurrentKeyRepo{key: &current}
			env.h.apiKeyService = service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, &config.Config{})

			rec := env.serve(http.MethodPatch, "/v1beta/cachedContents/abc?updateMask=ttl", `{"ttl":"86400s"}`,
				gin.Params{{Key: "cacheID", Value: "abc"}}, func(c *gin.Context) {
					c.Request = c.Request.WithContext(service.WithFundingFollowup(c.Request.Context()))
					env.h.GeminiCachedContentsPatch(c)
					require.False(t, service.IsFundingFollowup(c.Request.Context()), "a paid extension must leave the historical followup exemption")
				})
			require.NotEqual(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Equal(t, 1, repo.reads, "the active authentication snapshot cannot override current key controls")
			require.Zero(t, env.upstream.methodCalls(http.MethodPatch))
			require.Empty(t, env.usage.totals())
			require.False(t, env.repo.pendingMutation(100))
		})
	}
}
