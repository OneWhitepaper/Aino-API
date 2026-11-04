# Aino private desktop diagnostics deployment

Status: support-log diagnostics were deployed through the existing Aino release flow. The
consented usage-statistics extension below is a deployment candidate; this document does not
claim that the new endpoint has been deployed.

The desktop now has an Aino-owned destination for explicit, per-upload diagnostic consent. It retains the Hermes redacted gzip format `hermes-debug-share/1`; model credentials and account tokens are not sent as log-upload credentials. The upload uses the existing Aino account JWT. Only authorized administrators can view the stored content. There is no public log URL or Nous fallback.

## HTTP and storage

| Method/path | Access |
| --- | --- |
| `POST /api/v1/desktop/diagnostics` | Signed-in user, JSON `bundle_base64`; owner derived from JWT |
| `GET /api/v1/desktop/diagnostics/:id` | Owner metadata only |
| `DELETE /api/v1/desktop/diagnostics/:id` | Owner payload erasure |
| `GET /api/v1/admin/diagnostics` | Existing admin authentication/compliance guard; pagination and optional exact `id` |
| `GET /api/v1/admin/diagnostics/:id` | Administrator content read, audited through existing middleware |
| `DELETE /api/v1/admin/diagnostics/:id` | Administrator payload erasure, existing mutation audit |

Responses are `Cache-Control: no-store`. The admin page at `/admin/diagnostics` displays content as plain text. Requests do not require any new administrator role or public storage permission.

Migration `247_private_desktop_diagnostics.sql` uses the existing PostgreSQL connection and migration runner. Bundles are private BYTEA records, limited to 8 MiB compressed and 16 MiB expanded JSON. The service rejects malformed/redaction-disabled envelopes, invalid filenames, oversized or concatenated gzip streams. A redaction marker is an assertion, not a guarantee that a log contains no sensitive content.

Five accepted uploads per account per rolling 24 hours are enforced in a transaction locking the user's existing row, including across service instances. Deleting a bundle sets its body to NULL immediately; a content-free receipt stays until expiry so delete/re-upload cannot evade quota. At 14 days, reads exclude the record. The existing timing wheel purges expired rows on startup and hourly, and cancels cleanup at shutdown. Backups, WAL and snapshots follow the deployment's existing retention policy; an online delete does not erase a prior backup.

## Deployment sequence

1. Build the frontend into `backend/internal/web/dist` and build the API using the repository's existing commands. Include the generated `backend/cmd/server/wire_gen.go`.
2. Deploy the API first. Let the existing migration runner apply migration 247; do not manually edit migration checksums or add a public/static mapping for this table.
3. Ensure the existing reverse proxy accepts the JSON/base64 representation (up to 11,185,836 bytes) for this upload route. Keep normal JWT/admin gates and HTTPS. No new secret, environment variable, cloud bucket or scheduler is needed.
4. Verify with a synthetic bundle: owner upload/receipt, other-owner rejection, non-admin rejection, admin detail audit without response contents, immediate erase, expired read rejection and startup/hourly cleanup logs. Confirm the operational backup policy matches the published privacy copy.
5. Distribute the matching Aino desktop and runtime after the API is available. Old API or runtime versions fail visibly; they never fall back to third-party upload.

If rolling back, preserve the diagnostics table and an active expiry purge for existing data. Do not roll back to a binary that silently leaves retained bundles indefinitely. Installed desktops and downloadable installers change only through their normal release process.

## Verified locally

- Entire backend unit suite passed using `go test -tags=unit ./...`.
- Real Testcontainers PostgreSQL/Redis integration passed: authentication/ownership/admin paths, payload erase, expiry/purge and eight concurrent uploads accepting exactly five. Deletion does not reset quota.
- Cleanup lifecycle and content-free audit tests passed with the race detector. The timer test uses the existing timing-wheel library's test ticker to exercise startup, recurring runs and cancellation during a purge.
- Frontend targeted/locale tests, typecheck, changed-file lint, production build and backend build passed. Built UI was checked with synthetic HTTP responses at 1280x900 and 390x844, including View/Close and plain text rendering.
- Compatible golangci-lint 2.15 reports zero new issues against `d24c2f5ac`. The pinned 2.13 tool cannot decode this host Go toolchain's export data; its pin was not changed.

No production logs, account, database or paid model was used. Independent reviewer dispatch returned no scope and therefore provides no independent approval. Native client uploads against the deployed production service remain unverified.

## Consented usage statistics

Migration `249_desktop_shared_metrics.sql` extends the same table with `kind` and a random,
profile-scoped `install_id`. Existing support logs keep their authenticated account ownership;
statistics carry no account token or user association. The existing admin page labels both kinds.

`POST /api/v1/desktop/shared-metrics` accepts the unchanged `hermes.shared_metrics.v4` schema
only: bounded counters and dimensions, never conversation text or log files. Both raw and
expanded bodies are limited to 1 MiB; gzip is supported. The existing Redis limiter allows
30 attempts per IP per minute and fails closed. The repository enforces 32 packages per
installation per rolling day, including erased receipts. Identical package retries are accepted;
conflicting identities or payloads are rejected, and erased payloads never reappear on retry.

The existing diagnostics authentication, audit, fourteen-day expiry and hourly purge apply.
Deploy the migration, API and embedded admin frontend before the matching desktop runtime.
Use synthetic counters for acceptance and retain the user's opt-in gate; old Nous consent does
not authorize Aino. No new collector, queue, scheduler or cloud storage is introduced.

The complete local Go unit suite passes after fixing the repository-free rate-resolver fallback's
unused cache janitors; its memory threshold was preserved. The complete integration suite and
all API frontend tests pass. Production acceptance must still be recorded separately.
