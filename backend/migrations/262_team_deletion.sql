-- Retain team identities, membership history and accepted funding snapshots after
-- the owner deletes a team. New sharing is allowed only for active teams.
ALTER TABLE teams DROP CONSTRAINT IF EXISTS teams_status_check;
ALTER TABLE teams ADD CONSTRAINT teams_status_check
 CHECK (status IN ('active', 'paused', 'deleted'));

-- Pending work must continue protecting the real payer's funds after the last
-- active team is deleted, without scanning completed funding history per request.
CREATE INDEX IF NOT EXISTS team_funding_reservations_pending_payer_idx
 ON team_funding_reservations(payer_user_id, subscription_id)
 WHERE status IN ('reserved', 'queued', 'dispatched');
CREATE INDEX IF NOT EXISTS personal_funding_reservations_pending_payer_idx
 ON personal_funding_reservations(member_user_id, subscription_id)
 WHERE status IN ('reserved', 'queued', 'dispatched');
