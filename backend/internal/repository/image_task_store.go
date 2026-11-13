package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const imageTaskKeyPrefix = "image_task:"

type imageTaskStore struct {
	rdb *redis.Client
	db  *sql.DB
}

func NewImageTaskStore(rdb *redis.Client, databases ...*sql.DB) service.ImageTaskStore {
	store := &imageTaskStore{rdb: rdb}
	if len(databases) > 0 {
		store.db = databases[0]
	}
	return store
}

func (s *imageTaskStore) Save(ctx context.Context, task *service.ImageTaskRecord, ttl time.Duration) error {
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}
	if s.db != nil {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO image_task_records(task_id,payload,expires_at) VALUES($1,$2::jsonb,$3) ON CONFLICT(task_id) DO UPDATE SET payload=EXCLUDED.payload,expires_at=EXCLUDED.expires_at`, task.ID, string(data), time.Now().Add(ttl)); err != nil {
			return err
		}
	}
	if s.rdb == nil {
		return nil
	}
	err = s.rdb.Set(ctx, imageTaskKey(task.ID), data, ttl).Err()
	if s.db != nil {
		return nil
	}
	return err
}

func (s *imageTaskStore) Get(ctx context.Context, id string) (*service.ImageTaskRecord, error) {
	var data []byte
	var err error
	if s.db != nil {
		err = s.db.QueryRowContext(ctx, `SELECT payload FROM image_task_records WHERE task_id=$1 AND expires_at>NOW()`, strings.TrimSpace(id)).Scan(&data)
		if err == nil {
			var task service.ImageTaskRecord
			if err = json.Unmarshal(data, &task); err != nil {
				return nil, err
			}
			return &task, nil
		}
	}
	if s.rdb != nil {
		data, err = s.rdb.Get(ctx, imageTaskKey(id)).Bytes()
	} else if err == nil || err == sql.ErrNoRows {
		err = redis.Nil
	}

	if err != nil {
		if err == redis.Nil {
			return nil, service.ErrImageTaskNotFound
		}
		return nil, err
	}
	var task service.ImageTaskRecord
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func imageTaskKey(id string) string {
	return imageTaskKeyPrefix + strings.TrimSpace(id)
}

// ProvideImageTaskStore keeps Wire's database dependency non-variadic while
// existing tests may still construct a Redis-only store.
func ProvideImageTaskStore(rdb *redis.Client, db *sql.DB) service.ImageTaskStore {
	return NewImageTaskStore(rdb, db)
}
