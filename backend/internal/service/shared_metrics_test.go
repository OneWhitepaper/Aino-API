//go:build unit

package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type metricStoreProbe struct {
	DiagnosticsRepository
	records []*DiagnosticRecord
}

func (p *metricStoreProbe) CreateSharedMetrics(_ context.Context, record *DiagnosticRecord) error {
	p.records = append(p.records, record)
	return nil
}

func metricFixture(t *testing.T) map[string]any {
	t.Helper()
	end := time.Now().UTC().Truncate(24 * time.Hour)
	return map[string]any{
		"schema_version": "hermes.shared_metrics.v4", "package_id": uuid.NewString(), "install_id": uuid.NewString(),
		"period_start": end.Add(-24 * time.Hour).Format(time.RFC3339), "period_end": end.Format(time.RFC3339), "generated_at": end.Format(time.RFC3339),
		"resource": map[string]any{"architecture": "arm64", "hermes_version": "0.22.5", "install_method": "git", "os_family": "macos"},
		"metrics":  []any{map[string]any{"name": "hermes.client.active", "type": "counter", "dimensions": map[string]any{}, "value": 1}},
	}
}

func TestSharedMetricsValidatesBeforeStorageAndDoesNotCaptureAccounts(t *testing.T) {
	probe := &metricStoreProbe{}
	svc := NewDiagnosticsService(probe)
	for _, name := range []string{"prompt", "user_id", "invalid period", "unknown metric"} {
		t.Run(name, func(t *testing.T) {
			pkg := metricFixture(t)
			switch name {
			case "prompt":
				pkg["prompt"] = "private conversation"
			case "user_id":
				pkg["user_id"] = 77
			case "invalid period":
				pkg["period_end"] = pkg["period_start"]
			case "unknown metric":
				pkg["metrics"].([]any)[0].(map[string]any)["name"] = "private_metric"
			}
			raw, err := json.Marshal(pkg)
			require.NoError(t, err)
			require.ErrorIs(t, svc.UploadSharedMetrics(t.Context(), raw), ErrInvalidSharedMetrics)
		})
	}
	require.Empty(t, probe.records)
	pkg := metricFixture(t)
	raw, err := json.Marshal(pkg)
	require.NoError(t, err)
	require.NoError(t, svc.UploadSharedMetrics(t.Context(), raw))
	require.Len(t, probe.records, 1)
	record := probe.records[0]
	require.Zero(t, record.UserID)
	require.Equal(t, pkg["install_id"], record.InstallID)
	envelope, err := DecodeDiagnosticBundle(record.Bundle)
	require.NoError(t, err)
	var stored map[string]any
	require.NoError(t, json.Unmarshal([]byte(envelope.Files["usage-statistics.json"]), &stored))
	require.Equal(t, pkg["package_id"], stored["package_id"])
}

func TestSharedMetricsBoundsExpandedGzipAndPlaintext(t *testing.T) {
	for _, encoding := range []string{"", "gzip"} {
		var buf bytes.Buffer
		large := strings.Repeat("x", MaxSharedMetricsBytes+1)
		if encoding == "gzip" {
			z := gzip.NewWriter(&buf)
			_, err := z.Write([]byte(large))
			require.NoError(t, err)
			require.NoError(t, z.Close())
		} else {
			buf.WriteString(large)
		}
		_, err := ReadSharedMetricsBody(&buf, encoding)
		require.ErrorIs(t, err, ErrInvalidSharedMetrics)
	}
	_, err := ReadSharedMetricsBody(strings.NewReader("{}"), "br")
	require.ErrorIs(t, err, ErrInvalidSharedMetrics)
}
