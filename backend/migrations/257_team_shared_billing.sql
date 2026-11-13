-- Team sharing is opt-in. Legacy users have no funding_preferences row.
CREATE TABLE teams (
 id BIGSERIAL PRIMARY KEY,
 owner_user_id BIGINT NOT NULL REFERENCES users(id),
 name VARCHAR(100) NOT NULL CHECK (btrim(name) <> ''),
 status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active','paused')),
 shared_mode VARCHAR(30) NOT NULL DEFAULT 'balance_first' CHECK (shared_mode IN ('balance_first','subscription_first','balance_only','subscription_only')),
 suggested_mode VARCHAR(30) NOT NULL DEFAULT '' CHECK (suggested_mode IN ('','personal_first','personal_only','team_first','team_only','subscription_first','subscription_only')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX teams_owner_idx ON teams(owner_user_id);
CREATE TABLE team_members (
 team_id BIGINT NOT NULL REFERENCES teams(id), user_id BIGINT NOT NULL REFERENCES users(id),
 status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active','paused','removed')),
 spend_limit NUMERIC(20,8) CHECK (spend_limit >= 0), spent NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (spent >= 0),
 reserved NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (reserved >= 0), unauthorized_amount NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (unauthorized_amount >= 0),
 shared_mode VARCHAR(30) CHECK (shared_mode IN ('balance_first','subscription_first','balance_only','subscription_only')),
 suggested_mode VARCHAR(30) CHECK (suggested_mode IN ('personal_first','personal_only','team_first','team_only','subscription_first','subscription_only')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), PRIMARY KEY(team_id,user_id)
);
CREATE INDEX team_members_user_idx ON team_members(user_id);
CREATE TABLE team_shared_subscriptions (
 team_id BIGINT NOT NULL REFERENCES teams(id), subscription_id BIGINT NOT NULL REFERENCES user_subscriptions(id), PRIMARY KEY(team_id,subscription_id)
);
CREATE TABLE team_invitations (
 id BIGSERIAL PRIMARY KEY, team_id BIGINT NOT NULL REFERENCES teams(id), user_id BIGINT NOT NULL REFERENCES users(id),
 status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','accepted','declined','revoked')),
 expires_at TIMESTAMPTZ NOT NULL DEFAULT (NOW() + INTERVAL '7 days'), created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX team_invitations_pending_idx ON team_invitations(team_id,user_id) WHERE status='pending';
CREATE INDEX team_invitations_target_idx ON team_invitations(user_id);
CREATE TABLE funding_preferences (
 user_id BIGINT PRIMARY KEY REFERENCES users(id),
 mode VARCHAR(30) NOT NULL CHECK (mode IN ('personal_first','personal_only','team_first','team_only','subscription_first','subscription_only')),
 team_id BIGINT REFERENCES teams(id), allow_personal_balance BOOLEAN NOT NULL, allow_personal_subscription BOOLEAN NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- Escrow is durable and occupies the real payer balance/subscription windows.
-- A dispatched reservation is never automatically expired or refunded.
CREATE TABLE team_funding_reservations (
 id BIGSERIAL PRIMARY KEY, request_id TEXT NOT NULL, api_key_id BIGINT NOT NULL REFERENCES api_keys(id),
 team_id BIGINT NOT NULL REFERENCES teams(id), member_user_id BIGINT NOT NULL REFERENCES users(id), payer_user_id BIGINT NOT NULL REFERENCES users(id),
 source VARCHAR(30) NOT NULL CHECK (source IN ('team_balance','team_subscription')), subscription_id BIGINT REFERENCES user_subscriptions(id),
 amount NUMERIC(20,8) NOT NULL CHECK (amount >= 0), settled_amount NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (settled_amount >= 0),
 unauthorized_amount NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (unauthorized_amount >= 0),
 status VARCHAR(20) NOT NULL DEFAULT 'reserved' CHECK (status IN ('reserved','dispatched','settled','released')),
 daily_window_start TIMESTAMPTZ, weekly_window_start TIMESTAMPTZ, monthly_window_start TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(request_id,api_key_id), CHECK ((source='team_balance' AND subscription_id IS NULL) OR (source='team_subscription' AND subscription_id IS NOT NULL))
);
CREATE INDEX team_funding_reservations_member_idx ON team_funding_reservations(team_id,member_user_id);
CREATE TABLE team_funding_ledger (
 id BIGSERIAL PRIMARY KEY, reservation_id BIGINT NOT NULL UNIQUE REFERENCES team_funding_reservations(id), request_id TEXT NOT NULL,
 team_id BIGINT NOT NULL REFERENCES teams(id), user_id BIGINT NOT NULL REFERENCES users(id), payer_user_id BIGINT NOT NULL REFERENCES users(id),
 source VARCHAR(30) NOT NULL CHECK (source IN ('team_balance','team_subscription')), subscription_id BIGINT REFERENCES user_subscriptions(id),
 amount NUMERIC(20,8) NOT NULL CHECK (amount >= 0), unauthorized_amount NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (unauthorized_amount >= 0), created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX team_funding_ledger_team_idx ON team_funding_ledger(team_id,id DESC);
CREATE INDEX team_funding_ledger_user_idx ON team_funding_ledger(team_id,user_id,id DESC);
