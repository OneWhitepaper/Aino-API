package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
)

const (
	MaxDiagnosticCompressedBytes = 8 * 1024 * 1024
	MaxDiagnosticJSONBytes       = 16 * 1024 * 1024
	DiagnosticRetention          = 14 * 24 * time.Hour
	DiagnosticDailyLimit         = 5
)

var (
	ErrInvalidDiagnosticBundle = infraerrors.BadRequest("INVALID_DIAGNOSTIC_BUNDLE", "Invalid or oversized redacted diagnostic bundle")
	ErrDiagnosticNotFound      = infraerrors.NotFound("DIAGNOSTIC_NOT_FOUND", "Diagnostic bundle not found or expired")
	ErrDiagnosticQuota         = infraerrors.New(429, "DIAGNOSTIC_UPLOAD_LIMIT", "At most five diagnostic uploads per account per day")
)

// DiagnosticEnvelope is the existing Hermes support format, not a new log collector.
type DiagnosticEnvelope struct {
	Format   string            `json:"format"`
	Redacted bool              `json:"redacted"`
	Created  string            `json:"created"`
	Files    map[string]string `json:"files"`
}

type DiagnosticMetadata struct {
	ID        string    `json:"id"`
	UserID    int64     `json:"user_id"`
	SizeBytes int       `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Kind      string    `json:"kind"`
}

type DiagnosticRecord struct {
	DiagnosticMetadata
	Bundle    []byte
	InstallID string
}

type DiagnosticsRepository interface {
	Create(context.Context, *DiagnosticRecord) error
	CreateSharedMetrics(context.Context, *DiagnosticRecord) error
	List(ctx context.Context, page, size int, id string) ([]DiagnosticMetadata, int64, error)
	Get(ctx context.Context, id string, ownerID int64) (*DiagnosticRecord, error)
	Delete(ctx context.Context, id string, ownerID int64) error
	DeleteExpired(context.Context, time.Time) (int64, error)
}

type DiagnosticsService struct {
	repo DiagnosticsRepository
}

func NewDiagnosticsService(repo DiagnosticsRepository) *DiagnosticsService {
	return &DiagnosticsService{repo: repo}
}

func validDiagnosticFiles(files map[string]string) bool {
	if len(files) < 1 || len(files) > 16 {
		return false
	}
	if _, ok := files["report"]; !ok {
		return false
	}
	for name, content := range files {
		label := strings.TrimPrefix(name, "client/")
		if len(label) == 0 || len(label) > 128 || strings.ContainsAny(label, "/\\\x00\r\n") || strings.Contains(label, "..") || !utf8.ValidString(content) {
			return false
		}
	}
	return true
}

// DecodeDiagnosticBundle bounds both compressed and expanded bytes before parsing.
// The redacted marker is a required client assertion, not a guarantee that logs are non-sensitive.
func DecodeDiagnosticBundle(blob []byte) (*DiagnosticEnvelope, error) {
	if len(blob) == 0 || len(blob) > MaxDiagnosticCompressedBytes {
		return nil, ErrInvalidDiagnosticBundle
	}
	input := bytes.NewReader(blob)
	z, err := gzip.NewReader(input)
	if err != nil {
		return nil, ErrInvalidDiagnosticBundle
	}
	z.Multistream(false)
	raw, err := io.ReadAll(io.LimitReader(z, MaxDiagnosticJSONBytes+1))
	closeErr := z.Close()
	if err != nil || closeErr != nil || len(raw) > MaxDiagnosticJSONBytes || input.Len() != 0 || !utf8.Valid(raw) {
		return nil, ErrInvalidDiagnosticBundle
	}
	var envelope DiagnosticEnvelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return nil, ErrInvalidDiagnosticBundle
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, ErrInvalidDiagnosticBundle
	}
	if envelope.Format != "hermes-debug-share/1" || !envelope.Redacted || !validDiagnosticFiles(envelope.Files) {
		return nil, ErrInvalidDiagnosticBundle
	}
	if _, err := time.Parse(time.RFC3339Nano, envelope.Created); err != nil {
		return nil, ErrInvalidDiagnosticBundle
	}
	return &envelope, nil
}

func (s *DiagnosticsService) Upload(ctx context.Context, userID int64, encoded string) (*DiagnosticMetadata, error) {
	if userID <= 0 || len(encoded) > base64.StdEncoding.EncodedLen(MaxDiagnosticCompressedBytes) {
		return nil, ErrInvalidDiagnosticBundle
	}
	blob, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return nil, ErrInvalidDiagnosticBundle
	}
	if _, err := DecodeDiagnosticBundle(blob); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	record := &DiagnosticRecord{DiagnosticMetadata: DiagnosticMetadata{
		ID: uuid.NewString(), UserID: userID, SizeBytes: len(blob), CreatedAt: now, ExpiresAt: now.Add(DiagnosticRetention), Kind: "diagnostic_log",
	}, Bundle: blob}
	if err := s.repo.Create(ctx, record); err != nil {
		return nil, err
	}
	return &record.DiagnosticMetadata, nil
}

func (s *DiagnosticsService) List(ctx context.Context, page, size int, id string) ([]DiagnosticMetadata, int64, error) {
	if id != "" && !validDiagnosticID(id) {
		return nil, 0, ErrDiagnosticNotFound
	}
	return s.repo.List(ctx, page, size, id)
}

func validDiagnosticID(id string) bool {
	parsed, err := uuid.Parse(id)
	return err == nil && parsed.String() == id
}

func (s *DiagnosticsService) Get(ctx context.Context, id string, ownerID int64) (*DiagnosticRecord, error) {
	if !validDiagnosticID(id) {
		return nil, ErrDiagnosticNotFound
	}
	return s.repo.Get(ctx, id, ownerID)
}

func (s *DiagnosticsService) Delete(ctx context.Context, id string, ownerID int64) error {
	if !validDiagnosticID(id) {
		return ErrDiagnosticNotFound
	}
	return s.repo.Delete(ctx, id, ownerID)
}

func (s *DiagnosticsService) PurgeExpired(ctx context.Context, now time.Time) (int64, error) {
	return s.repo.DeleteExpired(ctx, now)
}
