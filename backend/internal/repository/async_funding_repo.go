package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

func (r *teamFundingRepository) QueueAsyncFunding(ctx context.Context, task *service.AsyncFundingTask) error {
	if task == nil || task.Snapshot == nil || task.Snapshot.Funding == nil {
		return service.ErrTeamFundingInvalid
	}
	snap := task.Snapshot
	funding := snap.Funding
	payload, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	return teamFundingTx(ctx, r.db, func(tx *sql.Tx) error {
		table := "personal_funding_reservations"
		if funding.IsShared() {
			table = "team_funding_reservations"
		}
		if funding.RequestID != task.OperationID {
			return service.ErrTeamFundingConflict
		}
		if funding.IsShared() {
			original, err := scanTeamReservation(tx.QueryRowContext(ctx, teamReservationSQL+" WHERE id=$1", funding.ReservationID))
			if err != nil {
				return err
			}
			if original.PayerUserID != funding.PayerUserID || original.TeamID != funding.TeamID || original.Source != funding.Source || !asyncFundingSameSubscription(original.SubscriptionID, funding.SubscriptionID) {
				return service.ErrTeamFundingConflict
			}
		} else {
			original, err := scanPersonalReservation(tx.QueryRowContext(ctx, personalReservationSQL+" WHERE id=$1", funding.ReservationID))
			if err != nil {
				return err
			}
			if funding.PayerUserID != snap.ActorUserID || original.Source != funding.Source || !asyncFundingSameSubscription(original.SubscriptionID, funding.SubscriptionID) {
				return service.ErrTeamFundingConflict
			}
		}
		var status string
		if err := tx.QueryRowContext(ctx, "SELECT status FROM "+table+" WHERE id=$1 AND request_id=$2 AND api_key_id=$3 AND member_user_id=$4 FOR UPDATE", funding.ReservationID, task.OperationID, snap.APIKeyID, snap.ActorUserID).Scan(&status); err != nil {
			return err
		}
		if status != "reserved" {
			return service.ErrTeamFundingConflict
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO async_funding_tasks(operation_id,actor_user_id,api_key_id,provider,task_id,snapshot) VALUES($1,$2,$3,$4,$5,$6::jsonb)`, task.OperationID, snap.ActorUserID, snap.APIKeyID, task.Provider, task.TaskID, string(payload)); err != nil {
			return mapAsyncFundingError(err)
		}
		_, err := tx.ExecContext(ctx, "UPDATE "+table+" SET status='queued',updated_at=NOW() WHERE id=$1", funding.ReservationID)
		return err
	})
}
func (r *teamFundingRepository) BindAsyncFunding(ctx context.Context, task *service.AsyncFundingTask) error {
	if task == nil || task.Snapshot == nil {
		return service.ErrTeamFundingInvalid
	}
	snapshot := task.Snapshot
	// Only routing/pricing enrichments may change after the escrow is queued.
	accountJSON, _ := json.Marshal(snapshot.AccountID)
	pricing := snapshot.PricingSnapshot
	if len(pricing) == 0 {
		pricing = []byte("null")
	}
	result, err := r.db.ExecContext(ctx, `UPDATE async_funding_tasks SET task_id=$4, snapshot=jsonb_set(jsonb_set(snapshot,'{account_id}',$5::jsonb),'{pricing_snapshot}',$6::jsonb),state=CASE WHEN state='dispatching' THEN 'accepted' ELSE state END,updated_at=NOW() WHERE operation_id=$1 AND api_key_id=$2 AND actor_user_id=$3 AND provider=$7 AND state IN ('queued','dispatching','accepted')`, task.OperationID, snapshot.APIKeyID, snapshot.ActorUserID, task.TaskID, string(accountJSON), string(pricing), task.Provider)
	if err != nil {
		return mapAsyncFundingError(err)
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return service.ErrTeamFundingConflict
	}
	return err
}
func mapAsyncFundingError(err error) error {
	if isUniqueViolation(err) {
		return service.ErrTeamFundingConflict
	}
	return err
}
func (r *teamFundingRepository) GetAsyncFunding(ctx context.Context, provider, taskID string, user, key int64) (*service.AsyncFundingTask, error) {
	task := &service.AsyncFundingTask{}
	var snapshot, usage []byte
	err := r.db.QueryRowContext(ctx, `SELECT operation_id,provider,task_id,state,snapshot,terminal_usage FROM async_funding_tasks WHERE provider=$1 AND task_id=$2 AND actor_user_id=$3 AND api_key_id=$4`, provider, taskID, user, key).Scan(&task.OperationID, &task.Provider, &task.TaskID, &task.State, &snapshot, &usage)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrTeamFundingNotFound
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(snapshot, &task.Snapshot); err != nil {
		return nil, err
	}
	task.TerminalUsage = usage
	return task, nil
}
func (r *teamFundingRepository) BeginAsyncFundingDispatch(ctx context.Context, operation string, key, user, id int64, shared bool) error {
	return teamFundingTx(ctx, r.db, func(tx *sql.Tx) error {
		var registryID int64
		if err := tx.QueryRowContext(ctx, `UPDATE async_funding_tasks SET state='dispatching',updated_at=NOW() WHERE operation_id=$1 AND api_key_id=$2 AND actor_user_id=$3 AND state='queued' RETURNING id`, operation, key, user).Scan(&registryID); errors.Is(err, sql.ErrNoRows) {
			return service.ErrTeamFundingConflict
		} else if err != nil {
			return err
		}
		var claim int64
		if err := tx.QueryRowContext(ctx, `INSERT INTO funding_dispatch_operations(request_id,api_key_id,user_id) VALUES($1,$2,$3) ON CONFLICT(request_id,api_key_id) DO NOTHING RETURNING id`, operation, key, user).Scan(&claim); errors.Is(err, sql.ErrNoRows) {
			return service.ErrUsageBillingRequestConflict
		} else if err != nil {
			return err
		}
		table := "personal_funding_reservations"
		if shared {
			table = "team_funding_reservations"
		}
		res, err := tx.ExecContext(ctx, "UPDATE "+table+" SET status='dispatched',updated_at=NOW() WHERE id=$1 AND member_user_id=$2 AND api_key_id=$3 AND request_id=$4 AND status='queued'", id, user, key, operation)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err == nil && n != 1 {
			return service.ErrTeamFundingConflict
		}
		return err
	})
}
func (r *teamFundingRepository) CancelQueuedAsyncFunding(ctx context.Context, operation string, key, user int64) error {
	return teamFundingTx(ctx, r.db, func(tx *sql.Tx) error {
		var payload []byte
		var state string
		err := tx.QueryRowContext(ctx, `SELECT snapshot,state FROM async_funding_tasks WHERE operation_id=$1 AND api_key_id=$2 AND actor_user_id=$3 FOR UPDATE`, operation, key, user).Scan(&payload, &state)
		if err != nil {
			return err
		}
		if state == "released" {
			return nil
		}
		if state != "queued" {
			return service.ErrTeamFundingConflict
		}
		var snap service.AsyncFundingSnapshot
		if err = json.Unmarshal(payload, &snap); err != nil {
			return err
		}
		d := snap.Funding
		if d == nil {
			return service.ErrTeamFundingInvalid
		}
		if d.IsShared() {
			v, err := scanTeamReservation(tx.QueryRowContext(ctx, teamReservationSQL+" WHERE id=$1 FOR UPDATE", d.ReservationID))
			if err != nil {
				return err
			}
			if v.Status != "queued" {
				return service.ErrTeamFundingConflict
			}
			if _, err = refundTeamEscrow(ctx, tx, v, v.Amount); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE team_members SET reserved=reserved-$3::numeric,updated_at=NOW() WHERE team_id=$1 AND user_id=$2`, v.TeamID, v.MemberUserID, v.Amount); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE team_funding_reservations SET status='released',updated_at=NOW() WHERE id=$1`, v.ID); err != nil {
				return err
			}
		} else {
			v, err := scanPersonalReservation(tx.QueryRowContext(ctx, personalReservationSQL+" WHERE id=$1 FOR UPDATE", d.ReservationID))
			if err != nil {
				return err
			}
			if v.Status != "queued" {
				return service.ErrTeamFundingConflict
			}
			if _, err = refundPersonalEscrow(ctx, tx, v, decimal.RequireFromString(v.Amount)); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE personal_funding_reservations SET status='released',updated_at=NOW() WHERE id=$1`, v.ID); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE async_funding_tasks SET state='released',updated_at=NOW() WHERE operation_id=$1 AND api_key_id=$2 AND actor_user_id=$3`, operation, key, user)
		return err
	})
}
func (r *teamFundingRepository) StoreAsyncFundingUsage(ctx context.Context, operation string, key, user int64, usage json.RawMessage) (json.RawMessage, error) {
	var payload []byte
	err := r.db.QueryRowContext(ctx, `UPDATE async_funding_tasks SET terminal_usage=COALESCE(terminal_usage,$4::jsonb),state=CASE WHEN state='settled' THEN state ELSE 'usage_pending' END,updated_at=NOW() WHERE operation_id=$1 AND api_key_id=$2 AND actor_user_id=$3 AND state IN ('dispatching','accepted','usage_pending','settled') RETURNING terminal_usage`, operation, key, user, string(usage)).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrTeamFundingNotFound
	}
	return payload, err
}
func (r *teamFundingRepository) MarkAsyncFundingSettled(ctx context.Context, operation string, key, user int64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE async_funding_tasks SET state='settled',updated_at=NOW() WHERE operation_id=$1 AND api_key_id=$2 AND actor_user_id=$3 AND terminal_usage IS NOT NULL`, operation, key, user)
	return err
}

var _ service.AsyncFundingRepository = (*teamFundingRepository)(nil)

func (r *usageBillingRepository) ReleaseQueuedBatchImageFunding(ctx context.Context, snapshot *service.AsyncFundingSnapshot) error {
	if snapshot == nil || snapshot.Funding == nil {
		return service.ErrTeamFundingInvalid
	}
	repo := &teamFundingRepository{db: r.db}
	return repo.CancelQueuedAsyncFunding(ctx, snapshot.Funding.RequestID, snapshot.APIKeyID, snapshot.ActorUserID)
}

// Persist the exact completed batch command before applying the reservation.
func (r *usageBillingRepository) ApplyBatchImageFunding(ctx context.Context, cmd *service.UsageBillingCommand) error {
	cmd.Normalize()
	payload, err := json.Marshal(map[string]any{"batch_command": cmd})
	if err != nil {
		return err
	}
	registry := &teamFundingRepository{db: r.db}
	payload, err = registry.StoreAsyncFundingUsage(ctx, cmd.RequestID, cmd.APIKeyID, cmd.UserID, payload)
	if err != nil {
		return err
	}
	var durable struct {
		Command *service.UsageBillingCommand `json:"batch_command"`
	}
	if err = json.Unmarshal(payload, &durable); err != nil {
		return err
	}
	if durable.Command == nil {
		return service.ErrTeamFundingConflict
	}
	if _, err = r.Apply(ctx, durable.Command); err != nil {
		return err
	}
	return nil
}

func (r *teamFundingRepository) GetFundingCachedContent(ctx context.Context, user, key int64, name string) (*service.GeminiCachedContent, error) {
	id, ok := service.ParseGeminiCachedContentPublicID(name)
	if !ok {
		return nil, service.ErrGeminiCachedContentNotFound
	}
	record, err := (&geminiCachedContentRepository{sql: r.db}).GetForOwner(ctx, key, id)
	if err != nil {
		return nil, err
	}
	if record.UserID != user {
		return nil, service.ErrGeminiCachedContentNotFound
	}
	return record, nil
}

func (r *teamFundingRepository) ListAsyncFundingForResource(ctx context.Context, provider, prefix string, user, key int64) ([]*service.AsyncFundingTask, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT operation_id,provider,task_id,state,snapshot,terminal_usage FROM async_funding_tasks WHERE provider=$1 AND left(task_id,length($2))=$2 AND actor_user_id=$3 AND api_key_id=$4 AND state='usage_pending' ORDER BY id`, provider, prefix, user, key)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var tasks []*service.AsyncFundingTask
	for rows.Next() {
		task := &service.AsyncFundingTask{}
		var snapshot, usage []byte
		if err = rows.Scan(&task.OperationID, &task.Provider, &task.TaskID, &task.State, &snapshot, &usage); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(snapshot, &task.Snapshot); err != nil {
			return nil, err
		}
		task.TerminalUsage = usage
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (r *teamFundingRepository) ListPendingAsyncFundingUsage(ctx context.Context, limit int) ([]*service.AsyncFundingTask, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT operation_id,provider,task_id,state,snapshot,terminal_usage FROM async_funding_tasks WHERE state='usage_pending' AND terminal_usage IS NOT NULL ORDER BY updated_at,id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var tasks []*service.AsyncFundingTask
	for rows.Next() {
		task := &service.AsyncFundingTask{}
		var snapshot, usage []byte
		if err = rows.Scan(&task.OperationID, &task.Provider, &task.TaskID, &task.State, &snapshot, &usage); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(snapshot, &task.Snapshot); err != nil {
			return nil, err
		}
		task.TerminalUsage = usage
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (r *usageBillingRepository) MarkBatchImageFundingSettled(ctx context.Context, snapshot *service.AsyncFundingSnapshot) error {
	if snapshot == nil || snapshot.Funding == nil {
		return service.ErrTeamFundingInvalid
	}
	return (&teamFundingRepository{db: r.db}).MarkAsyncFundingSettled(ctx, snapshot.Funding.RequestID, snapshot.APIKeyID, snapshot.ActorUserID)
}

func asyncFundingSameSubscription(left, right *int64) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

// Failed envelopes move behind older pending costs so one damaged resource
// cannot starve every subsequent recovery batch.
func (r *teamFundingRepository) TouchPendingAsyncFundingUsage(ctx context.Context, operation string, key, user int64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE async_funding_tasks SET updated_at=NOW() WHERE operation_id=$1 AND api_key_id=$2 AND actor_user_id=$3 AND state='usage_pending'`, operation, key, user)
	return err
}
