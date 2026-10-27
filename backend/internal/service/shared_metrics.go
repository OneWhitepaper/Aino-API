package service

import (
	"bytes"
	"compress/gzip"
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"sync"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const MaxSharedMetricsBytes = 1024 * 1024

var (
	ErrInvalidSharedMetrics = infraerrors.BadRequest("INVALID_SHARED_METRICS", "Invalid aggregate usage statistics")
	ErrSharedMetricsQuota   = infraerrors.New(429, "SHARED_METRICS_UPLOAD_LIMIT", "At most 32 usage-statistics packages per installation per day")
)

// This is the existing Hermes v4 contract, copied without changing its field allowlists.
//
//go:embed schemas/hermes.shared_metrics.v4.schema.json
var sharedMetricsSchemaJSON []byte

var compiledSharedMetricsSchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(sharedMetricsSchemaJSON))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	const name = "https://aino.invalid/schemas/shared-metrics-v4"
	if err := c.AddResource(name, doc); err != nil {
		return nil, err
	}
	return c.Compile(name)
})

// UploadSharedMetrics reuses the private diagnostic envelope, store, and expiry worker.
// No token, account, prompt, log text, or extra dimensions are accepted by this endpoint.
func (s *DiagnosticsService) UploadSharedMetrics(ctx context.Context, raw []byte) error {
	if len(raw) == 0 || len(raw) > MaxSharedMetricsBytes || !utf8.Valid(raw) {
		return ErrInvalidSharedMetrics
	}
	schema, err := compiledSharedMetricsSchema()
	if err != nil {
		return err
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil || schema.Validate(doc) != nil {
		return ErrInvalidSharedMetrics
	}
	var pkg struct {
		ID        string    `json:"package_id"`
		InstallID string    `json:"install_id"`
		Start     time.Time `json:"period_start"`
		End       time.Time `json:"period_end"`
		Generated time.Time `json:"generated_at"`
	}
	if json.Unmarshal(raw, &pkg) != nil {
		return ErrInvalidSharedMetrics
	}
	now := time.Now().UTC()
	if !pkg.Start.Before(pkg.End) || pkg.End.Sub(pkg.Start) > 24*time.Hour ||
		pkg.End.After(now.Add(5*time.Minute)) || pkg.Start.Before(now.Add(-DiagnosticRetention)) ||
		pkg.Generated.Before(pkg.End) || pkg.Generated.After(now.Add(5*time.Minute)) {
		return ErrInvalidSharedMetrics
	}
	canonical, err := json.Marshal(doc)
	if err != nil {
		return ErrInvalidSharedMetrics
	}
	var compressed bytes.Buffer
	z := gzip.NewWriter(&compressed)
	err = json.NewEncoder(z).Encode(DiagnosticEnvelope{
		Format: "hermes-debug-share/1", Redacted: true, Created: pkg.Generated.UTC().Format(time.RFC3339Nano),
		Files: map[string]string{"report": "Aino consented aggregate usage statistics", "usage-statistics.json": string(canonical)},
	})
	if err != nil {
		_ = z.Close()
		return err
	}
	if err := z.Close(); err != nil {
		return err
	}
	return s.repo.CreateSharedMetrics(ctx, &DiagnosticRecord{DiagnosticMetadata: DiagnosticMetadata{
		ID: pkg.ID, Kind: "usage_statistics", SizeBytes: compressed.Len(), CreatedAt: now, ExpiresAt: now.Add(DiagnosticRetention),
	}, Bundle: compressed.Bytes(), InstallID: pkg.InstallID})
}

// ReadSharedMetricsBody bounds decompression as well as incoming bytes.
func ReadSharedMetricsBody(body io.Reader, encoding string) ([]byte, error) {
	if encoding != "" && encoding != "gzip" {
		return nil, ErrInvalidSharedMetrics
	}
	input := io.LimitReader(body, MaxSharedMetricsBytes+1)
	if encoding == "gzip" {
		z, err := gzip.NewReader(input)
		if err != nil {
			return nil, ErrInvalidSharedMetrics
		}
		defer func() { _ = z.Close() }()
		input = io.LimitReader(z, MaxSharedMetricsBytes+1)
	}
	raw, err := io.ReadAll(input)
	if err != nil || len(raw) > MaxSharedMetricsBytes {
		return nil, ErrInvalidSharedMetrics
	}
	return raw, nil
}
