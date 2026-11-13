package repository

// Pending escrow remains real payer money after the last team is deleted.
// Retain protected owner billing until accepted shared or personal work finishes,
// without creating preferences or granting any new team access.
const fundingOwnerRequiresEscrowProtectionSQL = `SELECT
 EXISTS(SELECT 1 FROM teams WHERE owner_user_id=$1 AND status='active')
 OR EXISTS(SELECT 1 FROM team_funding_reservations WHERE payer_user_id=$1 AND status IN ('reserved','queued','dispatched'))
 OR EXISTS(SELECT 1 FROM personal_funding_reservations WHERE member_user_id=$1 AND status IN ('reserved','queued','dispatched'))`
