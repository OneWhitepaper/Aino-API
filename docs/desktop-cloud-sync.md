# Desktop cloud sync v1

The JWT-authenticated desktop route group exposes `POST /api/v1/desktop/sync/exchange`
and `PUT`/`GET /api/v1/desktop/sync/blobs/:sha256`. Exchange uses the ordinary
`data` response envelope; blob GET returns private raw bytes. These endpoints do
not invoke inference or provision credentials. The authenticated JWT subject is
the only source of account ownership. Responses are marked `Cache-Control: no-store`.

Exchange accepts version 1, a nonempty device ID (at most 256 UTF-8 bytes), a
canonical nonnegative decimal `after`, a `limit` from 1 to 50, and up to 16
operations. An operation supplies a UUID `operation_id`, a 1–256-character key
from `[A-Za-z0-9._:/-]`, a decimal `base_revision`, a boolean `deleted`, and a
JSON object payload (or null/omitted for a tombstone). Request bodies and exchange
responses are limited to 16 MiB; individual documents and raw blobs to 8 MiB.
Invalid, unknown, trailing, oversized, or invalid UTF-8 request input is rejected.

Apply compares the current revision (`0` for absent keys) with `base_revision`.
A tombstone cannot be resurrected. Each result is `applied` or `conflict`, with
its record; the absent conflicting record has revision `0` and null payload.
The client must create a new key for recovered or divergent work. Identical
operation retries return their original result, even after subsequent writes;
changed content for a used operation ID returns HTTP 409. Input object key order
and whitespace do not affect the operation fingerprint. Integer precision is
preserved. Exponent-form numeric expansion is bounded before database conversion;
inside the transaction, document validation and quota charges use PostgreSQL's
actual normalized representation. JSONB-incompatible values return HTTP 400 and roll back the exchange.

Migration `254_desktop_cloud_sync.sql` creates the four private tables. Each
exchange and blob write locks its account row for the entire transaction. Revision
advancement, current documents, byte accounting, and receipts commit together.
The feed returns current records ordered by revision. `cursor` advances only
through emitted records when `has_more` is true, and otherwise to the committed
account revision. `after` ahead of the committed revision is rejected. Results
consume response space first; a page can contain zero changes with `has_more`
when retries/results fill the byte budget. Continue with the returned cursor and
without the already-acknowledged operations.

The initial quota is 256 MiB per account, persisted as `quota_bytes`. `used_bytes`
counts current document JSON/metadata, durable operation receipts/metadata, and
private blob content/metadata. Replacements subtract the previous document charge;
identical blob retries add nothing. Applied receipts keep the result metadata and
input fingerprint, reconstructing their payload from the validated identical retry
input. Conflict receipts preserve their original record. Tombstones, receipts,
and blobs are retained; they are removed only with account deletion. Thus long-lived
receipt/blob retention can eventually exhaust quota. HTTP 413
`DESKTOP_SYNC_QUOTA` rolls back the entire exchange or blob write. This is logical
content/metadata accounting, not a measurement of PostgreSQL physical disk usage.

Blob names are lowercase SHA-256 hex. PUT verifies the digest before storage;
GET verifies it again before responding. The primary key includes account ID, so
a hash stored by one account grants another account no read access.

The migration is forward-only and follows the normal application migration
runner. No deployment, production migration, data import, or storage cleanup is
performed by adding these source files. Rollout requires separate review.
