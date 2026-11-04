package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type siteCacheUsageRepo struct {
	service.UsageLogRepository
	filters usagestats.UsageLogFilters
	calls   int
	stats   *usagestats.UsageStats
}

func (r *siteCacheUsageRepo) GetStatsWithFilters(_ context.Context, filters usagestats.UsageLogFilters) (*usagestats.UsageStats, error) {
	r.calls++
	r.filters = filters
	return r.stats, nil
}

type siteCacheSettingRepo struct {
	service.SettingRepository
	value string
	err   error
}

func (r *siteCacheSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	return r.value, r.err
}

func TestSiteCacheRateVisibilityAndScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, role, setting string
		settingErr          error
		status              int
		queries             int
	}{
		{name: "hidden from users by default", role: "user", status: 200},
		{name: "hidden if setting missing", role: "user", settingErr: service.ErrSettingNotFound, status: 200},
		{name: "hidden if explicitly disabled", role: "user", setting: "false", status: 200},
		{name: "enabled for users", role: "user", setting: "true", status: 200, queries: 1},
		{name: "always available to admin", role: "admin", status: 200, queries: 1},
		{name: "settings failure returns no aggregate", role: "user", settingErr: errors.New("database unavailable"), status: 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &siteCacheUsageRepo{stats: &usagestats.UsageStats{TotalInputTokens: 200, TotalCacheReadTokens: 500, TotalCacheCreationTokens: 300, TotalOutputTokens: 100000, TotalRequests: 999, TotalCost: 42}}
			settings := service.NewSettingService(&siteCacheSettingRepo{value: tc.setting, err: tc.settingErr}, nil)
			h := NewUsageHandler(service.NewUsageService(repo, nil, nil, nil), nil, nil, settings)
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
				c.Set(string(middleware.ContextKeyUserRole), tc.role)
			})
			router.GET("/usage/site-cache-rate", h.SiteCacheRate)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/usage/site-cache-rate?start_date=2026-10-27&end_date=2026-10-28&timezone=Asia%2FShanghai&user_id=42&model=private&api_key_id=123&role=admin&visible_to_users=true", nil))
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.queries, repo.calls)
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			if tc.status != http.StatusOK {
				return
			}
			var resp struct{ Data map[string]any }
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			require.Equal(t, tc.setting == "true", resp.Data["visible_to_users"])
			if tc.queries > 0 {
				require.Equal(t, float64(50), resp.Data["cache_hit_rate"])
				require.Equal(t, int64(0), repo.filters.UserID)
				require.Equal(t, int64(0), repo.filters.APIKeyID)
				require.Empty(t, repo.filters.Model)
				require.Equal(t, "2026-10-26T16:00:00Z", repo.filters.StartTime.UTC().Format(time.RFC3339))
				require.Equal(t, "2026-10-28T16:00:00Z", repo.filters.EndTime.UTC().Format(time.RFC3339))
			} else {
				require.Nil(t, resp.Data["cache_hit_rate"])
			}
			// The endpoint never exposes site request counts, volumes, or costs.
			require.LessOrEqual(t, len(resp.Data), 2)
		})
	}
}

func TestSiteCacheRateRejectsInvalidRangesAndUnauthenticatedRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewUsageHandler(nil, nil, nil, nil)
	for _, query := range []string{"", "?start_date=bad&end_date=2026-10-28", "?start_date=2026-10-29&end_date=2026-10-28"} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/usage/site-cache-rate"+query, nil)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
		c.Set(string(middleware.ContextKeyUserRole), "admin")
		h.SiteCacheRate(c)
		require.Equal(t, http.StatusBadRequest, w.Code)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/usage/site-cache-rate", nil)
	h.SiteCacheRate(c)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}
