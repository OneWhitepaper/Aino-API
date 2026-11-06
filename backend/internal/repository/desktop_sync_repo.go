package repository

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type desktopSyncRepository struct{ db *sql.DB }

func NewDesktopSyncRepository(db *sql.DB) service.DesktopSyncRepository {
	return &desktopSyncRepository{db: db}
}

type syncAccount struct{ revision, used, quota int64 }

func lockSyncAccount(ctx context.Context, tx *sql.Tx, userID, quota int64) (syncAccount, error) {
	var a syncAccount
	_, err := tx.ExecContext(ctx, `INSERT INTO desktop_sync_accounts (user_id,quota_bytes) VALUES ($1,$2) ON CONFLICT DO NOTHING`, userID, quota)
	if err != nil {
		return a, err
	}
	err = tx.QueryRowContext(ctx, `SELECT revision,used_bytes,quota_bytes FROM desktop_sync_accounts WHERE user_id=$1 FOR UPDATE`, userID).Scan(&a.revision, &a.used, &a.quota)
	return a, err
}
func chargeSync(a *syncAccount, delta int64) error {
	if delta > 0 && delta > a.quota-a.used {
		return service.ErrDesktopSyncQuota
	}
	a.used += delta
	return nil
}

func (r *desktopSyncRepository) Exchange(ctx context.Context, userID int64, in service.DesktopSyncRequest, quota int64) (*service.DesktopSyncResponse, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	a, err := lockSyncAccount(ctx, tx, userID, quota)
	if err != nil {
		return nil, err
	}
	after, err := service.ParseDesktopSyncRevision(in.After)
	if err != nil || after > a.revision {
		return nil, service.ErrDesktopSyncInvalid
	}
	out := &service.DesktopSyncResponse{Cursor: in.After, Results: make([]service.DesktopSyncResult, 0, len(in.Operations)), Changes: []service.DesktopSyncRecord{}}
	// Reserve the envelope and cursor before admitting result and change payloads.
	encoded, err := service.EncodeDesktopSyncJSON(out)
	if err != nil {
		return nil, err
	}
	used := len(encoded) + 256
	for _, op := range in.Operations {
		result, err := applySyncOperation(ctx, tx, userID, in.DeviceID, op, &a)
		if err != nil {
			var pgError *pq.Error
			if errors.As(err, &pgError) && pgError.Code.Class() == "22" {
				return nil, service.ErrDesktopSyncInvalid
			}
			return nil, err
		}
		encoded, err = service.EncodeDesktopSyncJSON(result)
		if err != nil {
			return nil, err
		}
		used += len(encoded) + 1
		if used > service.DesktopSyncResponseBytes {
			return nil, service.ErrDesktopSyncRebatch
		}
		out.Results = append(out.Results, *result)
	}
	rows, err := tx.QueryContext(ctx, `SELECT key,revision,deleted,payload,device_id FROM desktop_sync_documents WHERE user_id=$1 AND revision>$2 ORDER BY revision LIMIT $3`, userID, after, in.Limit+1)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var record service.DesktopSyncRecord
		var revision int64
		var payload []byte
		if err = rows.Scan(&record.Key, &revision, &record.Deleted, &payload, &record.DeviceID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		record.Payload = compactSyncPayload(payload)
		record.Revision = strconv.FormatInt(revision, 10)
		// JSONB text has insignificant whitespace; normalize it for bounded wire accounting.
		encoded, err = service.EncodeDesktopSyncJSON(record)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		if len(out.Changes) >= in.Limit || used+len(encoded)+1 > service.DesktopSyncResponseBytes {
			out.HasMore = true
			break
		}
		out.Changes = append(out.Changes, record)
		out.Cursor = record.Revision
		used += len(encoded) + 1
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	if !out.HasMore {
		out.Cursor = strconv.FormatInt(a.revision, 10)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE desktop_sync_accounts SET revision=$2,used_bytes=$3 WHERE user_id=$1`, userID, a.revision, a.used); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

func applySyncOperation(ctx context.Context, tx *sql.Tx, userID int64, device string, op service.DesktopSyncOperation, a *syncAccount) (*service.DesktopSyncResult, error) {
	var fingerprint string
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT fingerprint,result FROM desktop_sync_operations WHERE user_id=$1 AND operation_id=$2`, userID, op.OperationID).Scan(&fingerprint, &raw)
	if err == nil {
		if fingerprint != op.Fingerprint {
			return nil, service.ErrDesktopSyncOperationReuse
		}
		var result service.DesktopSyncResult
		if err = json.Unmarshal(raw, &result); err != nil {
			return nil, err
		}
		result.Record.Payload = compactSyncPayload(result.Record.Payload)
		if result.Status == "applied" && !result.Record.Deleted {
			result.Record.Payload = op.Payload
		}
		return &result, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	current := service.DesktopSyncRecord{Key: op.Key, Revision: "0"}
	var revision, oldSize int64
	var payload []byte
	err = tx.QueryRowContext(ctx, `SELECT revision,deleted,payload,device_id,size_bytes FROM desktop_sync_documents WHERE user_id=$1 AND key=$2`, userID, op.Key).Scan(&revision, &current.Deleted, &payload, &current.DeviceID, &oldSize)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	current.Payload = compactSyncPayload(payload)
	current.Revision = strconv.FormatInt(revision, 10)
	result := &service.DesktopSyncResult{OperationID: op.OperationID, Status: "conflict", Record: current}
	if !current.Deleted && current.Revision == op.BaseRevision {
		if a.revision == math.MaxInt64 {
			return nil, service.ErrDesktopSyncQuota
		}
		a.revision++
		result.Status = "applied"
		result.Record = service.DesktopSyncRecord{Key: op.Key, Revision: strconv.FormatInt(a.revision, 10), Deleted: op.Deleted, DeviceID: device}
		if !op.Deleted {
			result.Record.Payload = op.Payload
		}
		storedRecord := result.Record
		if !op.Deleted {
			var normalized []byte
			if err := tx.QueryRowContext(ctx, `SELECT $1::jsonb::text`, string(op.Payload)).Scan(&normalized); err != nil {
				return nil, err
			}
			storedRecord.Payload = compactSyncPayload(normalized)
			if len(storedRecord.Payload) > service.DesktopSyncDocumentBytes {
				return nil, service.ErrDesktopSyncInvalid
			}
		}
		encoded, err := service.EncodeDesktopSyncJSON(storedRecord)
		if err != nil {
			return nil, err
		}
		size := int64(len(encoded) + 128)
		if err = chargeSync(a, size-oldSize); err != nil {
			return nil, err
		}
		var payload any
		if !op.Deleted {
			payload = string(storedRecord.Payload)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO desktop_sync_documents (user_id,key,revision,deleted,payload,device_id,size_bytes) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (user_id,key) DO UPDATE SET revision=EXCLUDED.revision,deleted=EXCLUDED.deleted,payload=EXCLUDED.payload,device_id=EXCLUDED.device_id,size_bytes=EXCLUDED.size_bytes`, userID, op.Key, a.revision, op.Deleted, payload, device, size)
		if err != nil {
			return nil, err
		}
	}
	receipt := *result
	// The immutable hash binds all input, so applied payloads can be recovered from a retry.
	// Conflict payloads must be retained because the current document may change later.
	if receipt.Status == "applied" {
		receipt.Record.Payload = nil
	}
	encoded, err := service.EncodeDesktopSyncJSON(receipt)
	if err != nil {
		return nil, err
	}
	size := int64(len(encoded) + 128)
	if err = chargeSync(a, size); err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO desktop_sync_operations (user_id,operation_id,fingerprint,result,size_bytes) VALUES ($1,$2,$3,$4,$5)`, userID, op.OperationID, op.Fingerprint, string(encoded), size)
	return result, err
}

func (r *desktopSyncRepository) PutBlob(ctx context.Context, userID int64, hash string, data []byte, quota int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	a, err := lockSyncAccount(ctx, tx, userID, quota)
	if err != nil {
		return err
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM desktop_sync_blobs WHERE user_id=$1 AND sha256=$2)`, userID, hash).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		if err = chargeSync(&a, int64(len(data)+128)); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO desktop_sync_blobs (user_id,sha256,content) VALUES ($1,$2,$3)`, userID, hash, data); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE desktop_sync_accounts SET used_bytes=$2 WHERE user_id=$1`, userID, a.used); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (r *desktopSyncRepository) GetBlob(ctx context.Context, userID int64, hash string) ([]byte, error) {
	var data []byte
	err := r.db.QueryRowContext(ctx, `SELECT content FROM desktop_sync_blobs WHERE user_id=$1 AND sha256=$2`, userID, hash).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrDesktopSyncNotFound
	}
	return data, err
}

// PostgreSQL JSONB can be SQL NULL for tombstones. Scan through []byte because
// json.RawMessage is a jsontext.Value alias in Go 1.27 and rejects SQL NULL.
func compactSyncPayload(raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var out bytes.Buffer
	if err := json.Compact(&out, raw); err != nil {
		return json.RawMessage(raw)
	}
	return json.RawMessage(out.Bytes())
}
