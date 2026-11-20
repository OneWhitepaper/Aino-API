-- Application provenance is display/filter metadata, separate from funding.
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS desktop_app VARCHAR(64) NOT NULL DEFAULT '';

-- All managed credentials predating application classification were issued by
-- the legacy Aino endpoint. Include archived keys so history stays attributable.
UPDATE api_keys SET desktop_app = 'aino' WHERE desktop_managed AND desktop_app = '';

ALTER TABLE api_keys ADD CONSTRAINT api_keys_desktop_app_shape CHECK (
    (NOT desktop_managed AND desktop_app = '') OR
    (desktop_managed AND desktop_app ~ '^[a-z][a-z0-9]*(_[a-z0-9]+)*$')
);

CREATE INDEX IF NOT EXISTS api_keys_user_desktop_app
    ON api_keys (user_id, desktop_app) WHERE desktop_managed;

CREATE FUNCTION protect_desktop_application() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    -- Old binaries omit this column; keep their Aino issuance compatible during
    -- a rolling upgrade, without letting later metadata updates relabel a key.
    IF TG_OP = 'INSERT' THEN
        IF NEW.desktop_managed AND NEW.desktop_app = '' THEN
            NEW.desktop_app := 'aino';
        END IF;
    ELSIF OLD.desktop_app IS DISTINCT FROM NEW.desktop_app THEN
        RAISE EXCEPTION 'desktop application identity is immutable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_protect_desktop_application BEFORE INSERT OR UPDATE ON api_keys
FOR EACH ROW EXECUTE FUNCTION protect_desktop_application();
