CREATE TABLE IF NOT EXISTS user_iq_tests (
    id UUID PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    request_id UUID NOT NULL,
    fingerprint VARCHAR(64) NOT NULL,
    config JSONB NOT NULL,
    status VARCHAR(20) NOT NULL CHECK (status IN ('running','completed','failed','cancelled','interrupted')),
    verdict VARCHAR(20) NOT NULL DEFAULT '',
    basis VARCHAR(64) NOT NULL DEFAULT '',
    error_code VARCHAR(64) NOT NULL DEFAULT '',
    result JSONB NOT NULL DEFAULT '{}',
    reference_cost_usd DOUBLE PRECISION,
    created_at TIMESTAMPTZ NOT NULL,
    deadline_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    UNIQUE (user_id, request_id)
);
CREATE INDEX IF NOT EXISTS idx_user_iq_tests_owner_created ON user_iq_tests(user_id, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_iq_tests_one_active ON user_iq_tests(user_id) WHERE status = 'running';
