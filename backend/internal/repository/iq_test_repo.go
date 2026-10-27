package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type iqTestRepository struct{ db *sql.DB }

func NewIQTestRepository(db *sql.DB) service.IQTestRepository { return &iqTestRepository{db: db} }

const iqTestColumns = `id, user_id, request_id, fingerprint, config, status, verdict, basis, error_code, result, reference_cost_usd, created_at, deadline_at, finished_at`

type iqTestScanner interface{ Scan(...any) error }

func scanIQTest(row iqTestScanner) (*service.IQTest, error) {
	task := &service.IQTest{}
	var config, result []byte
	err := row.Scan(&task.ID, &task.UserID, &task.RequestID, &task.Fingerprint, &config, &task.Status, &task.Verdict, &task.Basis, &task.ErrorCode, &result, &task.ReferenceCostUSD, &task.CreatedAt, &task.DeadlineAt, &task.FinishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrIQTestNotFound
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(config, &task.Config); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(result, &task.Result); err != nil {
		return nil, err
	}
	return task, nil
}

func (r *iqTestRepository) Create(ctx context.Context, task *service.IQTest) (*service.IQTest, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()
	// This short transaction serializes admission across application instances.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(739406024810001)`); err != nil {
		return nil, false, err
	}
	existing, err := scanIQTest(tx.QueryRowContext(ctx, `SELECT `+iqTestColumns+` FROM user_iq_tests WHERE user_id = $1 AND request_id = $2`, task.UserID, task.RequestID))
	if err == nil {
		return existing, false, tx.Commit()
	}
	if !errors.Is(err, service.ErrIQTestNotFound) {
		return nil, false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE user_iq_tests SET status = 'interrupted', error_code = 'interrupted', finished_at = NOW() WHERE status = 'running' AND deadline_at < NOW()`); err != nil {
		return nil, false, err
	}
	var own, total int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FILTER (WHERE user_id = $1), COUNT(*) FROM user_iq_tests WHERE status = 'running'`, task.UserID).Scan(&own, &total); err != nil {
		return nil, false, err
	}
	if own > 0 {
		return nil, false, service.ErrIQTestBusy
	}
	if total >= 8 {
		return nil, false, infraerrors.TooManyRequests("IQ_TEST_CAPACITY", "Test runner is busy; try again later")
	}
	config, err := json.Marshal(task.Config)
	if err != nil {
		return nil, false, err
	}
	result, err := json.Marshal(task.Result)
	if err != nil {
		return nil, false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO user_iq_tests (id,user_id,request_id,fingerprint,config,status,result,created_at,deadline_at) VALUES ($1,$2,$3,$4,$5,'running',$6,$7,$8)`, task.ID, task.UserID, task.RequestID, task.Fingerprint, config, result, task.CreatedAt, task.DeadlineAt)
	if err != nil {
		return nil, false, err
	}
	if err = tx.Commit(); err != nil {
		return nil, false, err
	}
	return task, true, nil
}

func (r *iqTestRepository) Get(ctx context.Context, userID int64, id string) (*service.IQTest, error) {
	return scanIQTest(r.db.QueryRowContext(ctx, `SELECT `+iqTestColumns+` FROM user_iq_tests WHERE user_id = $1 AND id = $2`, userID, id))
}
func (r *iqTestRepository) GetByRequest(ctx context.Context, userID int64, id string) (*service.IQTest, error) {
	return scanIQTest(r.db.QueryRowContext(ctx, `SELECT `+iqTestColumns+` FROM user_iq_tests WHERE user_id = $1 AND request_id = $2`, userID, id))
}
func (r *iqTestRepository) List(ctx context.Context, userID int64, offset, limit int) ([]*service.IQTest, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, user_id, request_id, fingerprint, config, status, verdict, basis, error_code, result - 'output', reference_cost_usd, created_at, deadline_at, finished_at FROM user_iq_tests WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := []*service.IQTest{}
	for rows.Next() {
		task, scanErr := scanIQTest(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, task)
	}
	return items, rows.Err()
}
func (r *iqTestRepository) Finish(ctx context.Context, task *service.IQTest) (bool, error) {
	result, err := json.Marshal(task.Result)
	if err != nil {
		return false, err
	}
	update, err := r.db.ExecContext(ctx, `UPDATE user_iq_tests SET status = $1, verdict = $2, basis = $3, error_code = $4, result = $5, reference_cost_usd = $6, finished_at = $7 WHERE id = $8 AND user_id = $9 AND status = 'running'`, task.Status, task.Verdict, task.Basis, task.ErrorCode, result, task.ReferenceCostUSD, task.FinishedAt, task.ID, task.UserID)
	if err != nil {
		return false, err
	}
	changed, err := update.RowsAffected()
	return changed > 0, err
}
