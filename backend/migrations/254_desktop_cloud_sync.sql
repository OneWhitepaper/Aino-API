-- Account locks serialize revisions, byte accounting, and durable receipts across API instances.
CREATE TABLE IF NOT EXISTS desktop_sync_accounts (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    revision BIGINT NOT NULL DEFAULT 0 CHECK (revision >= 0),
    used_bytes BIGINT NOT NULL DEFAULT 0 CHECK (used_bytes >= 0),
    quota_bytes BIGINT NOT NULL DEFAULT 268435456 CHECK (quota_bytes > 0)
);
CREATE TABLE IF NOT EXISTS desktop_sync_documents (
    user_id BIGINT NOT NULL REFERENCES desktop_sync_accounts(user_id) ON DELETE CASCADE,
    key VARCHAR(256) NOT NULL CHECK (key ~ '^[A-Za-z0-9._:/-]+$'),
    revision BIGINT NOT NULL CHECK (revision > 0),
    deleted BOOLEAN NOT NULL,
    payload JSONB,
    device_id VARCHAR(256) NOT NULL,
    size_bytes BIGINT NOT NULL CHECK (size_bytes >= 0),
    PRIMARY KEY (user_id, key),
    UNIQUE (user_id, revision),
    CHECK ((deleted AND payload IS NULL) OR (NOT deleted AND jsonb_typeof(payload) = 'object'))
);
CREATE TABLE IF NOT EXISTS desktop_sync_operations (
    user_id BIGINT NOT NULL REFERENCES desktop_sync_accounts(user_id) ON DELETE CASCADE,
    operation_id UUID NOT NULL,
    fingerprint CHAR(64) NOT NULL,
    result JSONB NOT NULL,
    size_bytes BIGINT NOT NULL CHECK (size_bytes >= 0),
    PRIMARY KEY (user_id, operation_id)
);
-- Applied receipts omit the payload: the fingerprint validates the retry's original payload.
-- Conflicts retain their original record, so a later write cannot alter the replayed result.
CREATE TABLE IF NOT EXISTS desktop_sync_blobs (
    user_id BIGINT NOT NULL REFERENCES desktop_sync_accounts(user_id) ON DELETE CASCADE,
    sha256 CHAR(64) NOT NULL CHECK (sha256 ~ '^[a-f0-9]{64}$'),
    content BYTEA NOT NULL CHECK (octet_length(content) <= 8388608),
    PRIMARY KEY (user_id, sha256)
);
