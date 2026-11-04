//go:build integration

package repository_test

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func encodedDiagnostic(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	z := gzip.NewWriter(&buf)
	require.NoError(t, json.NewEncoder(z).Encode(map[string]any{
		"format": "hermes-debug-share/1", "redacted": true, "created": time.Now().UTC().Format(time.RFC3339),
		"files": map[string]string{"report": "synthetic private support report", "agent.log": "synthetic log <script>alert(1)</script>"},
	}))
	require.NoError(t, z.Close())
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestDiagnosticHTTPAccountAdminExpiryAndDeletion(t *testing.T) {
	r := newDesktopModelRig(t)
	svc := service.NewDiagnosticsService(repository.NewDiagnosticsRepository(repository.GetIntegrationDB()))
	r.desktopHandler.SetDiagnosticsService(svc)
	adminRoutes := r.router.Group("/api/v1/admin")
	adminRoutes.Use(gin.HandlerFunc(middleware.NewAdminAuthMiddleware(r.auth, r.users, r.settings, nil)))
	adminRoutes.GET("/diagnostics", r.desktopHandler.ListDiagnostics)
	adminRoutes.GET("/diagnostics/:id", r.desktopHandler.ReadDiagnostic)
	adminRoutes.DELETE("/diagnostics/:id", r.desktopHandler.DeleteDiagnostic)
	owner, other, admin := r.user(t, service.StatusActive, false), r.user(t, service.StatusActive, false), r.user(t, service.StatusActive, false)
	_, err := r.client.User.UpdateOneID(admin.ID).SetRole(service.RoleAdmin).Save(r.ctx)
	require.NoError(t, err)
	admin.Role = service.RoleAdmin
	token, err := r.auth.GenerateToken(r.ctx, owner)
	require.NoError(t, err)
	otherToken, err := r.auth.GenerateToken(r.ctx, other)
	require.NoError(t, err)
	adminToken, err := r.auth.GenerateToken(r.ctx, admin)
	require.NoError(t, err)
	body := fmt.Sprintf(`{"bundle_base64":%q,"user_id":%d}`, encodedDiagnostic(t), other.ID)
	require.Equal(t, 401, r.request(http.MethodPost, "/api/v1/desktop/diagnostics", body, "").Code)
	w := r.request(http.MethodPost, "/api/v1/desktop/diagnostics", body, token)
	require.Equal(t, 201, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var payload struct {
		Data service.DiagnosticMetadata `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	m := payload.Data
	require.Equal(t, owner.ID, m.UserID, "body cannot select an owner")
	require.WithinDuration(t, m.CreatedAt.Add(14*24*time.Hour), m.ExpiresAt, time.Second)
	path := "/api/v1/desktop/diagnostics/" + m.ID
	w = r.request(http.MethodGet, path, "", token)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "synthetic private support report", "owner receipt does not expose log content")
	require.Equal(t, 404, r.request(http.MethodGet, path, "", otherToken).Code)
	require.Equal(t, 404, r.request(http.MethodDelete, path, "", otherToken).Code)
	require.Equal(t, 401, r.request(http.MethodGet, "/api/v1/admin/diagnostics/"+m.ID, "", "").Code)
	require.Equal(t, 403, r.request(http.MethodGet, "/api/v1/admin/diagnostics/"+m.ID, "", token).Code)
	w = r.request(http.MethodGet, "/api/v1/admin/diagnostics/"+m.ID, "", adminToken)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "synthetic private support report")
	w = r.request(http.MethodGet, "/api/v1/admin/diagnostics?id="+m.ID, "", adminToken)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), m.ID)

	require.Equal(t, 200, r.request(http.MethodDelete, path, "", token).Code)
	require.Equal(t, 404, r.request(http.MethodGet, "/api/v1/admin/diagnostics/"+m.ID, "", adminToken).Code)
	var erased bool
	require.NoError(t, repository.GetIntegrationDB().QueryRowContext(r.ctx, `SELECT bundle IS NULL FROM desktop_diagnostics WHERE id=$1`, m.ID).Scan(&erased))
	require.True(t, erased, "deletion must erase content, not just hide the receipt")

	w = r.request(http.MethodPost, "/api/v1/desktop/diagnostics", body, token)
	require.Equal(t, 201, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	expiredID := payload.Data.ID
	_, err = repository.GetIntegrationDB().ExecContext(r.ctx, `UPDATE desktop_diagnostics SET created_at=NOW()-INTERVAL '15 days', expires_at=NOW()-INTERVAL '1 day' WHERE id=$1`, expiredID)
	require.NoError(t, err)
	require.Equal(t, 404, r.request(http.MethodGet, "/api/v1/admin/diagnostics/"+expiredID, "", adminToken).Code)
	count, err := svc.PurgeExpired(r.ctx, time.Now().UTC())
	require.NoError(t, err)
	require.GreaterOrEqual(t, count, int64(1))
	var remaining int
	require.NoError(t, repository.GetIntegrationDB().QueryRowContext(r.ctx, `SELECT count(*) FROM desktop_diagnostics WHERE id=$1`, expiredID).Scan(&remaining))
	require.Zero(t, remaining)
}

func TestDiagnosticDailyQuotaSerializesAndCannotBeResetByDeleting(t *testing.T) {
	r := newDesktopModelRig(t)
	owner := r.user(t, service.StatusActive, false)
	svc := service.NewDiagnosticsService(repository.NewDiagnosticsRepository(repository.GetIntegrationDB()))
	encoded := encodedDiagnostic(t)
	type result struct {
		meta *service.DiagnosticMetadata
		err  error
	}
	results := make(chan result, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { meta, err := svc.Upload(r.ctx, owner.ID, encoded); results <- result{meta, err} })
	}
	wg.Wait()
	close(results)
	success := 0
	var id string
	for res := range results {
		if res.err == nil {
			success++
			id = res.meta.ID
		} else {
			require.ErrorIs(t, res.err, service.ErrDiagnosticQuota)
		}
	}
	require.Equal(t, 5, success)
	require.NoError(t, svc.Delete(r.ctx, id, owner.ID))
	_, err := svc.Upload(r.ctx, owner.ID, encoded)
	require.ErrorIs(t, err, service.ErrDiagnosticQuota)
}
