-- Configured bounded text requests reserve personal funds atomically too.
CREATE TABLE IF NOT EXISTS personal_funding_reservations (
 id BIGSERIAL PRIMARY KEY, request_id TEXT NOT NULL,
 api_key_id BIGINT NOT NULL REFERENCES api_keys(id), member_user_id BIGINT NOT NULL REFERENCES users(id),
 source VARCHAR(30) NOT NULL CHECK (source IN ('personal_balance','personal_subscription')), subscription_id BIGINT REFERENCES user_subscriptions(id),
 amount NUMERIC(20,8) NOT NULL CHECK (amount >= 0), settled_amount NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (settled_amount >= 0), unauthorized_amount NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK(unauthorized_amount>=0),
 status VARCHAR(20) NOT NULL DEFAULT 'reserved' CHECK (status IN ('reserved','dispatched','settled','released')),
 daily_window_start TIMESTAMPTZ, weekly_window_start TIMESTAMPTZ, monthly_window_start TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(request_id,api_key_id),
 CHECK ((source='personal_balance' AND subscription_id IS NULL) OR (source='personal_subscription' AND subscription_id IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS personal_funding_reservations_member_idx ON personal_funding_reservations(member_user_id,id DESC);

-- An operation may choose a new source only while it has not dispatched.
-- This permanent claim also prevents retries across a changed payer/source.
CREATE TABLE IF NOT EXISTS funding_dispatch_operations (
 id BIGSERIAL PRIMARY KEY,
 request_id TEXT NOT NULL,
 api_key_id BIGINT NOT NULL REFERENCES api_keys(id),
 user_id BIGINT NOT NULL REFERENCES users(id),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(request_id,api_key_id)
);

-- Per-handler ownership prevents a duplicate caller from releasing another
-- caller's still-undispatched hold while storage reserve retries stay idempotent.
CREATE TABLE IF NOT EXISTS funding_request_admissions (
 id BIGSERIAL PRIMARY KEY,
 request_id TEXT NOT NULL,
 api_key_id BIGINT NOT NULL REFERENCES api_keys(id),
 user_id BIGINT NOT NULL REFERENCES users(id),
 owner_token UUID NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(request_id,api_key_id)
);
