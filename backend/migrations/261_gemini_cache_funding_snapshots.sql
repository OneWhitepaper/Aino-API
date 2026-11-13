-- Cache resource ownership and the original payer survive catalog and funding
-- preference changes. Paid extensions reserve a fresh operation from this
-- immutable source; they never reuse the already-settled creation escrow.
ALTER TABLE gemini_cached_contents ADD COLUMN IF NOT EXISTS funding_snapshot JSONB;
