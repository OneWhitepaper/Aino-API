-- Accepted asynchronous jobs retain the original actor, payer and pricing even
-- when preferences change or the presentation cache is lost. Queued escrow is
-- owned by this registry and is never released by a finished HTTP handler.
ALTER TABLE team_funding_reservations DROP CONSTRAINT IF EXISTS team_funding_reservations_status_check;
ALTER TABLE team_funding_reservations ADD CONSTRAINT team_funding_reservations_status_check CHECK (status IN ('reserved','queued','dispatched','settled','released'));
ALTER TABLE personal_funding_reservations DROP CONSTRAINT IF EXISTS personal_funding_reservations_status_check;
ALTER TABLE personal_funding_reservations ADD CONSTRAINT personal_funding_reservations_status_check CHECK (status IN ('reserved','queued','dispatched','settled','released'));
CREATE TABLE IF NOT EXISTS async_funding_tasks (
 id BIGSERIAL PRIMARY KEY,
 operation_id TEXT NOT NULL,
 actor_user_id BIGINT NOT NULL REFERENCES users(id),
 api_key_id BIGINT NOT NULL REFERENCES api_keys(id),
 provider TEXT NOT NULL,
 task_id TEXT NOT NULL,
 snapshot JSONB NOT NULL,
 state TEXT NOT NULL DEFAULT 'queued' CHECK (state IN ('queued','dispatching','accepted','usage_pending','settled','released')),
 terminal_usage JSONB,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(operation_id,api_key_id),
 UNIQUE(provider,task_id,actor_user_id,api_key_id)
);
CREATE INDEX IF NOT EXISTS async_funding_tasks_pending_idx ON async_funding_tasks(state,updated_at) WHERE state IN ('queued','usage_pending');
ALTER TABLE batch_image_jobs ADD COLUMN IF NOT EXISTS funding_snapshot JSONB;
CREATE TABLE IF NOT EXISTS image_task_records (
 task_id TEXT PRIMARY KEY,
 payload JSONB NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS image_task_records_expiry_idx ON image_task_records(expires_at);
