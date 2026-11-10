# Desktop cloud sync — permanently removed

Desktop account cloud synchronization has been removed. It is no longer an
available or recoverable product feature. Local conversations, profiles and
attachments continue to be stored on the device.

## Retired HTTP endpoints

The following routes are no longer registered:

- `POST /api/v1/desktop/sync/exchange`
- `PUT /api/v1/desktop/sync/blobs/:sha256`
- `GET /api/v1/desktop/sync/blobs/:sha256`

Requests receive HTTP 404 without entering desktop JWT authentication or any
synchronization handler, service or repository. Older compatible desktop clients
recognize this response as `sync_unavailable` and stop automatic sync polling.
The dedicated sync implementation and its dependency-injection providers have
also been removed; there is no server-side sync worker or scheduled job to keep
running.

## Cloud-data cleanup

Forward migration `256_remove_desktop_cloud_sync.sql` permanently drops only:

- `desktop_sync_blobs`
- `desktop_sync_operations`
- `desktop_sync_documents`
- `desktop_sync_accounts`

These tables contain the former private cloud attachment bytes, operation
receipts, synchronized documents and synchronization revision/quota accounting.
Attachment bytes were stored entirely in the `desktop_sync_blobs.content` BYTEA
column, so there is no external object-store or filesystem cleanup.

The migration uses `DROP TABLE IF EXISTS` in child-to-parent order without
`CASCADE`. It is safe to rerun and does not remove unrelated dependencies. Keep
historical migration `254_desktop_cloud_sync.sql` unchanged to preserve deployed
migration checksums. Migration 255 and the separate device-information feature
remain intact. Running migration 256 deletes existing synchronized cloud data;
there is no retained recovery or re-enable path in this feature.

## Unaffected services and deployment state

Account login, users, permanent device Keys, device metadata, model catalogs,
model requests, subscriptions, account balances and billing retain their existing
behavior. The cleanup does not access or delete desktop-local conversation
history, profiles or attachment files. Sync quota accounting is separate from
account balances and model charges.

These source changes do not constitute a production deployment. No production
database has been contacted or migrated as part of this change. Cloud tables and
their existing data are deleted only when the new migration is executed during
an actual API upgrade; old deployed API instances keep their previous behavior
until replaced.
