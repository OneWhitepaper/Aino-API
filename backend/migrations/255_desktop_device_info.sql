-- Display-only installation metadata. No credential, entitlement or billing data.
CREATE TABLE IF NOT EXISTS desktop_device_info (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id UUID NOT NULL,
    installation_name VARCHAR(80) NOT NULL,
    os TEXT NOT NULL CHECK (os IN ('macos', 'windows', 'linux')),
    client_type TEXT NOT NULL CHECK (client_type IN ('desktop', 'desktop_dev', 'browser')),
    client_version VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, device_id),
    CHECK (btrim(installation_name) <> '' AND btrim(client_version) <> ''),
    CHECK (installation_name !~ '[[:cntrl:]]' AND client_version !~ '[[:cntrl:]]')
);
