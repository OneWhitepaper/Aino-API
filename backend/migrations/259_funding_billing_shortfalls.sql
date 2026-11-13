-- Retain costs from legacy requests that were already running when a wallet
-- became a shared pool. Such requests cannot consume another request's escrow.
CREATE TABLE IF NOT EXISTS funding_billing_shortfalls (
 id BIGSERIAL PRIMARY KEY,
 request_id TEXT NOT NULL,
 api_key_id BIGINT NOT NULL REFERENCES api_keys(id),
 payer_user_id BIGINT NOT NULL REFERENCES users(id),
 subscription_id BIGINT REFERENCES user_subscriptions(id),
 source VARCHAR(30) NOT NULL DEFAULT 'personal_balance' CHECK (source IN ('personal_balance','personal_subscription')),
 requested_amount NUMERIC(20,8) NOT NULL CHECK (requested_amount >= 0),
 charged_amount NUMERIC(20,8) NOT NULL CHECK (charged_amount >= 0),
 unpaid_amount NUMERIC(20,8) NOT NULL CHECK (unpaid_amount > 0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(request_id,api_key_id)
);
CREATE INDEX IF NOT EXISTS funding_billing_shortfalls_payer_idx ON funding_billing_shortfalls(payer_user_id,id DESC);
