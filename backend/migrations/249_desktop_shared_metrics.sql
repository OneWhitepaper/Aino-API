-- Reuse private diagnostic retention and admin access for consented aggregate counts.
-- Aggregate counts are pseudonymous and never assigned to a logged-in account.
ALTER TABLE desktop_diagnostics ALTER COLUMN user_id DROP NOT NULL;
ALTER TABLE desktop_diagnostics ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'diagnostic_log'
    CHECK (kind IN ('diagnostic_log', 'usage_statistics'));
ALTER TABLE desktop_diagnostics ADD COLUMN IF NOT EXISTS install_id UUID;
ALTER TABLE desktop_diagnostics ADD CONSTRAINT desktop_diagnostics_identity
    CHECK ((kind='diagnostic_log' AND user_id IS NOT NULL AND install_id IS NULL)
        OR (kind='usage_statistics' AND user_id IS NULL AND install_id IS NOT NULL));
CREATE INDEX IF NOT EXISTS idx_desktop_diagnostics_install_created
    ON desktop_diagnostics(install_id, created_at DESC) WHERE kind='usage_statistics';
