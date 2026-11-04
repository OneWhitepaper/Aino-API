//go:build integration

package repository_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSharedMetricsUsesPrivateDiagnosticsStorageWithoutAcceptingLogText(t *testing.T) {
	r := newDesktopModelRig(t)
	r.desktopHandler.SetDiagnosticsService(service.NewDiagnosticsService(repository.NewDiagnosticsRepository(repository.GetIntegrationDB())))
	id := uuid.NewString()
	end := time.Now().UTC().Truncate(24 * time.Hour)
	body := fmt.Sprintf(`{"schema_version":"hermes.shared_metrics.v4","package_id":%q,"install_id":%q,"period_start":%q,"period_end":%q,"generated_at":%q,"resource":{"architecture":"arm64","hermes_version":"0.22.5","install_method":"git","os_family":"macos"},"metrics":[{"name":"hermes.client.active","type":"counter","dimensions":{},"value":1}]}`, id, uuid.NewString(), end.Add(-24*time.Hour).Format(time.RFC3339), end.Format(time.RFC3339), end.Format(time.RFC3339))
	w := r.request(http.MethodPost, "/api/v1/desktop/shared-metrics", body, "")
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Equal(t, http.StatusAccepted, r.request(http.MethodPost, "/api/v1/desktop/shared-metrics", body, "").Code)
	var count int
	require.NoError(t, repository.GetIntegrationDB().QueryRowContext(r.ctx, `SELECT count(*) FROM desktop_diagnostics WHERE id=$1`, id).Scan(&count))
	require.Equal(t, 1, count, "a retried package must not be stored twice")
	record, err := repository.NewDiagnosticsRepository(repository.GetIntegrationDB()).Get(r.ctx, id, 0)
	require.NoError(t, err)
	require.Zero(t, record.UserID, "daily counts must not attach an account")
	require.Equal(t, "usage_statistics", record.Kind)
	envelope, err := service.DecodeDiagnosticBundle(record.Bundle)
	require.NoError(t, err)
	var stored map[string]any
	require.NoError(t, json.Unmarshal([]byte(envelope.Files["usage-statistics.json"]), &stored))
	require.Equal(t, id, stored["package_id"])
	bad := strings.Replace(body, `"dimensions":{}`, `"dimensions":{"prompt":"private conversation"}`, 1)
	require.Equal(t, http.StatusBadRequest, r.request(http.MethodPost, "/api/v1/desktop/shared-metrics", bad, "").Code)
	require.Equal(t, http.StatusUnauthorized, r.request(http.MethodGet, "/api/v1/desktop/diagnostics/"+id, "", "").Code)
	conflict := strings.Replace(body, `"value":1`, `"value":2`, 1)
	require.Equal(t, http.StatusBadRequest, r.request(http.MethodPost, "/api/v1/desktop/shared-metrics", conflict, "").Code)
	adminRepo := repository.NewDiagnosticsRepository(repository.GetIntegrationDB())
	require.NoError(t, adminRepo.Delete(r.ctx, id, 0))
	require.Equal(t, http.StatusAccepted, r.request(http.MethodPost, "/api/v1/desktop/shared-metrics", body, "").Code)
	_, err = adminRepo.Get(r.ctx, id, 0)
	require.ErrorIs(t, err, service.ErrDiagnosticNotFound, "retry must not restore erased content")
}

func TestSharedMetricsQuotaAndIdentityCannotBeResetByDeletingPayload(t *testing.T) {
	r := newDesktopModelRig(t)
	repo := repository.NewDiagnosticsRepository(repository.GetIntegrationDB())
	installID, now := uuid.NewString(), time.Now().UTC()
	for range 32 {
		record := &service.DiagnosticRecord{DiagnosticMetadata: service.DiagnosticMetadata{
			ID: uuid.NewString(), Kind: "usage_statistics", CreatedAt: now, ExpiresAt: now.Add(service.DiagnosticRetention),
		}, InstallID: installID, Bundle: []byte("synthetic quota fixture")}
		require.NoError(t, repo.CreateSharedMetrics(r.ctx, record))
		require.NoError(t, repo.Delete(r.ctx, record.ID, 0))
		// An ID belonging to one profile cannot acknowledge another profile's package.
		record.InstallID = uuid.NewString()
		require.ErrorIs(t, repo.CreateSharedMetrics(r.ctx, record), service.ErrInvalidSharedMetrics)
	}
	next := &service.DiagnosticRecord{DiagnosticMetadata: service.DiagnosticMetadata{
		ID: uuid.NewString(), Kind: "usage_statistics", CreatedAt: now, ExpiresAt: now.Add(service.DiagnosticRetention),
	}, InstallID: installID, Bundle: []byte("synthetic quota fixture")}
	require.ErrorIs(t, repo.CreateSharedMetrics(r.ctx, next), service.ErrSharedMetricsQuota)
}
