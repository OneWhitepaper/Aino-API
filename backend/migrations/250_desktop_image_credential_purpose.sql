-- Existing desktop credentials retain chat-only authorization. Image leases
-- use the same identity boundaries but cannot be reused as chat credentials.
ALTER TABLE desktop_model_credentials
    ADD COLUMN IF NOT EXISTS purpose VARCHAR(16) NOT NULL DEFAULT 'chat'
    CHECK (purpose IN ('chat', 'image'));

DROP INDEX IF EXISTS idx_desktop_credentials_active_lookup;
CREATE UNIQUE INDEX idx_desktop_credentials_active_lookup
    ON desktop_model_credentials
    (user_id, device_id, connection_grant_id, session_family_id, group_id, purpose)
    WHERE revoked_at IS NULL;
