package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
)

const (
	DesktopSyncRequestBytes        = 16 * 1024 * 1024
	DesktopSyncResponseBytes       = 16 * 1024 * 1024
	DesktopSyncDocumentBytes       = 8 * 1024 * 1024
	DesktopSyncDefaultQuota  int64 = 256 * 1024 * 1024
)

var (
	ErrDesktopSyncInvalid        = infraerrors.BadRequest("DESKTOP_SYNC_INVALID", "Invalid or oversized synchronization request")
	ErrDesktopSyncOperationReuse = infraerrors.Conflict("DESKTOP_SYNC_OPERATION_REUSED", "Operation ID was already used with different content")
	ErrDesktopSyncQuota          = infraerrors.New(413, "DESKTOP_SYNC_QUOTA", "Account synchronization storage quota exceeded")
	ErrDesktopSyncNotFound       = infraerrors.NotFound("DESKTOP_SYNC_NOT_FOUND", "Private synchronization blob not found")
	ErrDesktopSyncDigest         = infraerrors.BadRequest("DESKTOP_SYNC_DIGEST", "Blob SHA-256 does not match its content")
	syncKeyPattern               = regexp.MustCompile(`^[A-Za-z0-9._:/-]{1,256}$`)
	syncHashPattern              = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

type DesktopSyncOperation struct {
	OperationID  string          `json:"operation_id"`
	Key          string          `json:"key"`
	BaseRevision string          `json:"base_revision"`
	Deleted      bool            `json:"deleted"`
	Payload      json.RawMessage `json:"payload"`
	Fingerprint  string          `json:"-"`
}
type DesktopSyncRequest struct {
	Version    int                    `json:"version"`
	DeviceID   string                 `json:"device_id"`
	After      string                 `json:"after"`
	Limit      int                    `json:"limit"`
	Operations []DesktopSyncOperation `json:"operations"`
}
type DesktopSyncRecord struct {
	Key      string          `json:"key"`
	Revision string          `json:"revision"`
	Deleted  bool            `json:"deleted"`
	Payload  json.RawMessage `json:"payload"`
	DeviceID string          `json:"device_id"`
}
type DesktopSyncResult struct {
	OperationID string            `json:"operation_id"`
	Status      string            `json:"status"`
	Record      DesktopSyncRecord `json:"record"`
}
type DesktopSyncResponse struct {
	Cursor  string              `json:"cursor"`
	HasMore bool                `json:"has_more"`
	Results []DesktopSyncResult `json:"results"`
	Changes []DesktopSyncRecord `json:"changes"`
}

type DesktopSyncRepository interface {
	Exchange(context.Context, int64, DesktopSyncRequest, int64) (*DesktopSyncResponse, error)
	PutBlob(context.Context, int64, string, []byte, int64) error
	GetBlob(context.Context, int64, string) ([]byte, error)
}
type DesktopSyncService struct{ repo DesktopSyncRepository }

func NewDesktopSyncService(repo DesktopSyncRepository) *DesktopSyncService {
	return &DesktopSyncService{repo: repo}
}

// EncodeDesktopSyncJSON keeps transport sizing consistent with PureJSON, including non-ASCII text.
func EncodeDesktopSyncJSON(v any) ([]byte, error) {
	var b bytes.Buffer
	encoder := json.NewEncoder(&b)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}

func ParseDesktopSyncRevision(raw string) (int64, error) {
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 || strconv.FormatInt(n, 10) != raw {
		return 0, ErrDesktopSyncInvalid
	}
	return n, nil
}

func (s *DesktopSyncService) Exchange(ctx context.Context, userID int64, in DesktopSyncRequest) (*DesktopSyncResponse, error) {
	if userID <= 0 || in.Version != 1 || len(in.DeviceID) == 0 || len(in.DeviceID) > 256 || !utf8.ValidString(in.DeviceID) || strings.ContainsAny(in.DeviceID, "\x00\r\n") || in.Limit < 1 || in.Limit > 50 || len(in.Operations) > 16 {
		return nil, ErrDesktopSyncInvalid
	}
	if _, err := ParseDesktopSyncRevision(in.After); err != nil {
		return nil, err
	}
	// Copy before normalization so the caller's durable outbox snapshot remains unchanged.
	in.Operations = append([]DesktopSyncOperation(nil), in.Operations...)
	for i := range in.Operations {
		op := &in.Operations[i]
		if _, err := uuid.Parse(op.OperationID); err != nil || len(op.OperationID) != 36 || !syncKeyPattern.MatchString(op.Key) {
			return nil, ErrDesktopSyncInvalid
		}
		if _, err := ParseDesktopSyncRevision(op.BaseRevision); err != nil {
			return nil, err
		}
		if len(op.Payload) > DesktopSyncDocumentBytes {
			return nil, ErrDesktopSyncInvalid
		}
		if len(op.Payload) == 0 {
			op.Payload = json.RawMessage("null")
		}
		var value any
		dec := json.NewDecoder(bytes.NewReader(op.Payload))
		dec.UseNumber()
		if !utf8.Valid(op.Payload) || !json.Valid(op.Payload) || dec.Decode(&value) != nil {
			return nil, ErrDesktopSyncInvalid
		}
		if _, ok := value.(map[string]any); !ok && (!op.Deleted || value != nil) {
			return nil, ErrDesktopSyncInvalid
		}
		canonical, err := EncodeDesktopSyncJSON(value)
		if err != nil || len(canonical) > DesktopSyncDocumentBytes {
			return nil, ErrDesktopSyncInvalid
		}
		op.Payload = canonical
		fingerprint, err := EncodeDesktopSyncJSON(struct {
			DeviceID  string
			Operation DesktopSyncOperation
		}{in.DeviceID, *op})
		if err != nil {
			return nil, err
		}
		op.Fingerprint = fmt.Sprintf("%x", sha256.Sum256(fingerprint))
	}
	encoded, err := EncodeDesktopSyncJSON(in)
	if err != nil || len(encoded) > DesktopSyncRequestBytes {
		return nil, ErrDesktopSyncInvalid
	}
	return s.repo.Exchange(ctx, userID, in, DesktopSyncDefaultQuota)
}

func (s *DesktopSyncService) PutBlob(ctx context.Context, userID int64, hash string, data []byte) error {
	if userID <= 0 || !syncHashPattern.MatchString(hash) || len(data) > DesktopSyncDocumentBytes {
		return ErrDesktopSyncInvalid
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != hash {
		return ErrDesktopSyncDigest
	}
	return s.repo.PutBlob(ctx, userID, hash, data, DesktopSyncDefaultQuota)
}
func (s *DesktopSyncService) GetBlob(ctx context.Context, userID int64, hash string) ([]byte, error) {
	if userID <= 0 || !syncHashPattern.MatchString(hash) {
		return nil, ErrDesktopSyncInvalid
	}
	data, err := s.repo.GetBlob(ctx, userID, hash)
	if err != nil {
		return nil, err
	}
	if len(data) > DesktopSyncDocumentBytes || fmt.Sprintf("%x", sha256.Sum256(data)) != hash {
		return nil, ErrDesktopSyncDigest
	}
	return data, nil
}
