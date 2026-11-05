-- Device identities reuse stable API Key IDs and retain revoked associations.
ALTER TABLE api_keys ADD COLUMN desktop_credential_type TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN desktop_device_id TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN desktop_key_version INTEGER NOT NULL DEFAULT 0;
ALTER TABLE api_keys ADD COLUMN key_hash_type TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX api_keys_device_identity_unique
    ON api_keys (user_id, desktop_device_id) WHERE desktop_credential_type = 'device';
ALTER TABLE api_keys ADD CONSTRAINT api_keys_device_identity_shape CHECK (
    desktop_credential_type <> 'device' OR
    (desktop_managed AND desktop_device_id <> '' AND desktop_key_version > 0 AND
     key_hash_type = 'sha256' AND group_id IS NULL AND expires_at IS NULL AND
     (key ~ '^sha256:[0-9a-f]{64}$' OR deleted_at IS NOT NULL)));

CREATE OR REPLACE FUNCTION api_key_auth_cache_index(stored_key TEXT, hash_type TEXT)
RETURNS TEXT LANGUAGE SQL IMMUTABLE AS $$
 SELECT CASE WHEN hash_type = 'sha256' AND stored_key ~ '^sha256:[0-9a-f]{64}$'
 THEN substring(stored_key FROM 8)
 ELSE encode(sha256(convert_to(stored_key, 'UTF8')), 'hex') END
$$;



CREATE OR REPLACE FUNCTION enqueue_user_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_user_id BIGINT;
BEGIN
    target_user_id := OLD.id;
    IF TG_OP = 'UPDATE'
       AND OLD.status IS NOT DISTINCT FROM NEW.status
       AND OLD.role IS NOT DISTINCT FROM NEW.role
       AND OLD.deleted_at IS NOT DISTINCT FROM NEW.deleted_at THEN
        RETURN NEW;
    END IF;

    INSERT INTO auth_cache_invalidation_outbox (cache_key)
    SELECT api_key_auth_cache_index(k.key, k.key_hash_type)
    FROM api_keys AS k
    WHERE k.user_id = target_user_id
      AND k.deleted_at IS NULL
      AND k.key <> '';
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION enqueue_allowed_group_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_user_id BIGINT;
    target_group_id BIGINT;
BEGIN
    IF TG_OP = 'UPDATE'
       AND (OLD.user_id IS DISTINCT FROM NEW.user_id
            OR OLD.group_id IS DISTINCT FROM NEW.group_id) THEN
        IF EXISTS (
            SELECT 1 FROM groups g
            WHERE g.id = OLD.group_id AND g.is_exclusive = TRUE
        ) THEN
            INSERT INTO auth_cache_invalidation_outbox (cache_key)
            SELECT api_key_auth_cache_index(k.key, k.key_hash_type)
            FROM api_keys AS k
            WHERE k.user_id = OLD.user_id
              AND k.group_id = OLD.group_id
              AND k.deleted_at IS NULL
              AND k.key <> '';
        END IF;
        target_user_id := NEW.user_id;
        target_group_id := NEW.group_id;
    ELSIF TG_OP = 'UPDATE' THEN
        RETURN NEW;
    ELSIF TG_OP = 'INSERT' THEN
        target_user_id := NEW.user_id;
        target_group_id := NEW.group_id;
    ELSE
        target_user_id := OLD.user_id;
        target_group_id := OLD.group_id;
    END IF;

    IF EXISTS (
        SELECT 1 FROM groups g
        WHERE g.id = target_group_id AND g.is_exclusive = TRUE
    ) THEN
        INSERT INTO auth_cache_invalidation_outbox (cache_key)
        SELECT api_key_auth_cache_index(k.key, k.key_hash_type)
        FROM api_keys AS k
        WHERE k.user_id = target_user_id
          AND k.group_id = target_group_id
          AND k.deleted_at IS NULL
          AND k.key <> '';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION enqueue_group_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_group_id BIGINT;
BEGIN
    target_group_id := OLD.id;
    IF TG_OP = 'UPDATE'
       AND OLD.status IS NOT DISTINCT FROM NEW.status
       AND OLD.is_exclusive IS NOT DISTINCT FROM NEW.is_exclusive
       AND OLD.allow_image_generation IS NOT DISTINCT FROM NEW.allow_image_generation
       AND OLD.image_count_billing IS NOT DISTINCT FROM NEW.image_count_billing
       AND OLD.platform IS NOT DISTINCT FROM NEW.platform
       AND OLD.subscription_type IS NOT DISTINCT FROM NEW.subscription_type
       AND OLD.rate_multiplier IS NOT DISTINCT FROM NEW.rate_multiplier
       AND OLD.peak_rate_enabled IS NOT DISTINCT FROM NEW.peak_rate_enabled
       AND OLD.peak_start IS NOT DISTINCT FROM NEW.peak_start
       AND OLD.peak_end IS NOT DISTINCT FROM NEW.peak_end
       AND OLD.peak_rate_multiplier IS NOT DISTINCT FROM NEW.peak_rate_multiplier
       AND OLD.profit_control_enabled IS NOT DISTINCT FROM NEW.profit_control_enabled
       AND OLD.profit_min_margin IS NOT DISTINCT FROM NEW.profit_min_margin
       AND OLD.profit_safety_buffer IS NOT DISTINCT FROM NEW.profit_safety_buffer
       AND OLD.deleted_at IS NOT DISTINCT FROM NEW.deleted_at THEN
        RETURN NEW;
    END IF;

    INSERT INTO auth_cache_invalidation_outbox (cache_key)
    SELECT api_key_auth_cache_index(k.key, k.key_hash_type)
    FROM api_keys AS k
    WHERE k.group_id = target_group_id
      AND k.deleted_at IS NULL
      AND k.key <> '';
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

-- Device coordinates and storage type cannot be reassigned through generic Key updates.
CREATE FUNCTION protect_desktop_device_identity() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.desktop_credential_type = 'device' AND
    (OLD.desktop_credential_type IS DISTINCT FROM NEW.desktop_credential_type OR
     OLD.desktop_device_id IS DISTINCT FROM NEW.desktop_device_id OR
     OLD.user_id IS DISTINCT FROM NEW.user_id OR OLD.key_hash_type IS DISTINCT FROM NEW.key_hash_type OR
     NOT NEW.desktop_managed) THEN
   RAISE EXCEPTION 'device identity is immutable';
 END IF;
 IF OLD.desktop_credential_type = 'device' AND
    (OLD.status IS DISTINCT FROM NEW.status OR OLD.deleted_at IS DISTINCT FROM NEW.deleted_at) AND
    NEW.desktop_key_version = OLD.desktop_key_version THEN
   NEW.desktop_key_version := OLD.desktop_key_version + 1;
 END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER trg_protect_desktop_device_identity BEFORE UPDATE ON api_keys
FOR EACH ROW EXECUTE FUNCTION protect_desktop_device_identity();

-- Preserve legacy identity-change revocation. Password/email policy for the new
-- permanent identity is separate; account disable/deletion still revokes both.
CREATE OR REPLACE FUNCTION revoke_desktop_credentials_on_identity_change()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE account_revoked BOOLEAN;
BEGIN
 account_revoked := (NEW.status <> 'active' AND OLD.status IS DISTINCT FROM NEW.status)
                    OR OLD.deleted_at IS DISTINCT FROM NEW.deleted_at;
 IF OLD.password_hash IS DISTINCT FROM NEW.password_hash
    OR OLD.email IS DISTINCT FROM NEW.email OR account_revoked THEN
   UPDATE desktop_model_credentials
   SET revoked_at=NOW(), revoke_reason='identity_changed', updated_at=NOW()
   WHERE user_id=NEW.id AND revoked_at IS NULL;
   UPDATE api_keys SET status='disabled', updated_at=NOW()
   WHERE user_id=NEW.id AND desktop_managed AND status <> 'disabled'
     AND (desktop_credential_type <> 'device' OR account_revoked);
 END IF;
 RETURN NEW;
END; $$;

CREATE OR REPLACE FUNCTION enqueue_api_key_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        INSERT INTO auth_cache_invalidation_outbox(cache_key) VALUES(api_key_auth_cache_index(OLD.key, OLD.key_hash_type));
        RETURN OLD;
    END IF;

    IF OLD.key IS DISTINCT FROM NEW.key
       OR OLD.status IS DISTINCT FROM NEW.status
       OR OLD.deleted_at IS DISTINCT FROM NEW.deleted_at
       OR OLD.user_id IS DISTINCT FROM NEW.user_id
       OR OLD.group_id IS DISTINCT FROM NEW.group_id
       OR OLD.ip_whitelist IS DISTINCT FROM NEW.ip_whitelist
       OR OLD.ip_blacklist IS DISTINCT FROM NEW.ip_blacklist
       OR OLD.expires_at IS DISTINCT FROM NEW.expires_at THEN
        INSERT INTO auth_cache_invalidation_outbox(cache_key) VALUES(api_key_auth_cache_index(OLD.key, OLD.key_hash_type));
        IF NEW.deleted_at IS NULL AND NEW.key IS DISTINCT FROM OLD.key THEN
            INSERT INTO auth_cache_invalidation_outbox(cache_key) VALUES(api_key_auth_cache_index(NEW.key, NEW.key_hash_type));
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
