-- Private support bundles are never mapped to static/public object storage.
CREATE TABLE IF NOT EXISTS desktop_diagnostics (
    id UUID PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    bundle BYTEA CHECK (octet_length(bundle) BETWEEN 1 AND 8388608),
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL CHECK (expires_at > created_at)
);
CREATE INDEX IF NOT EXISTS idx_desktop_diagnostics_owner_created ON desktop_diagnostics(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_desktop_diagnostics_expiry ON desktop_diagnostics(expires_at);
