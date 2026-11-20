-- Give the model gateway its own brand, separate from the Aino desktop app.
-- Only installations still using a legacy gateway name adopt this branding.
-- Run once at upgrade; an administrator's later branding changes survive replay.
DO $$
BEGIN
    UPDATE settings
    SET value = 'OmniFlow API', updated_at = NOW()
    WHERE key = 'site_name'
      AND LOWER(TRIM(value)) IN ('sub2api', 'aino', 'aino api', 'aino-api');

    IF FOUND THEN
        INSERT INTO settings (key, value, updated_at)
        VALUES ('site_logo', '/omniflow-logo.png', NOW())
        ON CONFLICT (key) DO UPDATE
        SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at;

        INSERT INTO settings (key, value, updated_at)
        VALUES ('site_subtitle', 'AI Model Gateway', NOW())
        ON CONFLICT (key) DO UPDATE
        SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at
        WHERE LOWER(TRIM(settings.value)) IN (
            '',
            'subscription to api conversion platform',
            'aino subscription to api conversion platform'
        );
    END IF;
END;
$$;
