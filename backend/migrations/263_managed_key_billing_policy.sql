ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS billing_policy VARCHAR(32) NOT NULL DEFAULT '';

ALTER TABLE api_keys ADD CONSTRAINT api_keys_billing_policy_check
    CHECK (billing_policy IN ('', 'balance_only') AND (billing_policy = '' OR desktop_managed));
