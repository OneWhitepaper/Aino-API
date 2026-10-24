package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type diagnosticsRepository struct{ db *sql.DB }

func NewDiagnosticsRepository(db *sql.DB) service.DiagnosticsRepository {
	return &diagnosticsRepository{db: db}
}

func (r *diagnosticsRepository) Create(ctx context.Context, record *service.DiagnosticRecord) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Serialize quota checks across instances, including the first upload for an account.
	var userID int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 AND status='active' AND deleted_at IS NULL FOR UPDATE`, record.UserID).Scan(&userID); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM desktop_diagnostics WHERE user_id=$1 AND created_at > $2`, userID, record.CreatedAt.Add(-24*time.Hour)).Scan(&count); err != nil {
		return err
	}
	if count >= service.DiagnosticDailyLimit {
		return service.ErrDiagnosticQuota
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO desktop_diagnostics(id,user_id,bundle,created_at,expires_at) VALUES($1,$2,$3,$4,$5)`, record.ID, userID, record.Bundle, record.CreatedAt, record.ExpiresAt)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *diagnosticsRepository) List(ctx context.Context, page, size int, id string) ([]service.DiagnosticMetadata, int64, error) {
	var total int64
	where := ` WHERE bundle IS NOT NULL AND expires_at > NOW() AND ($1='' OR id::text=$1)`
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM desktop_diagnostics`+where, id).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id::text,user_id,octet_length(bundle),created_at,expires_at FROM desktop_diagnostics`+where+` ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, id, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.DiagnosticMetadata, 0)
	for rows.Next() {
		var row service.DiagnosticMetadata
		if err := rows.Scan(&row.ID, &row.UserID, &row.SizeBytes, &row.CreatedAt, &row.ExpiresAt); err != nil {
			return nil, 0, err
		}
		items = append(items, row)
	}
	return items, total, rows.Err()
}

func (r *diagnosticsRepository) Get(ctx context.Context, id string, ownerID int64) (*service.DiagnosticRecord, error) {
	var row service.DiagnosticRecord
	err := r.db.QueryRowContext(ctx, `SELECT id::text,user_id,octet_length(bundle),created_at,expires_at,bundle FROM desktop_diagnostics
        WHERE id=$1 AND bundle IS NOT NULL AND expires_at>NOW() AND ($2::bigint=0 OR user_id=$2)`, id, ownerID).
		Scan(&row.ID, &row.UserID, &row.SizeBytes, &row.CreatedAt, &row.ExpiresAt, &row.Bundle)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrDiagnosticNotFound
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *diagnosticsRepository) Delete(ctx context.Context, id string, ownerID int64) error {
	// Erase payload immediately. Keep only the receipt until expiry so delete/re-upload
	// cannot evade the daily quota; the expiry purge removes the receipt as well.
	result, err := r.db.ExecContext(ctx, `UPDATE desktop_diagnostics SET bundle=NULL WHERE id=$1 AND bundle IS NOT NULL AND ($2::bigint=0 OR user_id=$2)`, id, ownerID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return service.ErrDiagnosticNotFound
	}
	return nil
}

func (r *diagnosticsRepository) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	result, err := r.db.ExecContext(ctx, `DELETE FROM desktop_diagnostics WHERE expires_at <= $1`, now)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
