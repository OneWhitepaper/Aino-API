# Aino / Sub2API v0.2.20 compatibility

This records source integration and isolated validation on 2026-11-08. It is not a production deployment receipt.

## Source revisions

- Aino baseline: `da576a9ccce66aaec6a42be1c0424e2b0fe22f7c`.
- Upstream: `6b4b37db43443b5e88e424fe3c69602cc3810dfb`, including release tag `v0.2.20` (`dab829a4e9b7d1cc307189aea8b0319eb29e29d0`) and its version synchronization.
- All 312 existing SQL migration files remain byte-identical. This integration introduces no new SQL migration.

## Compatibility decisions

- Retain Aino account cloud sync, private attachment ownership, model-catalog aliases, device-key permission freshness, inference-operation deduplication, existing public pages, phone/TOTP, video routes, management MCP and manual update protection.
- Keep image-quality mappings after the final channel/account model mapping, for JSON generation/edit and multipart edits. Preserve the original client quality, actual output dimensions and existing image billing rules while adding upstream 1K/2K/4K account routing and GPT Image 2.6 support.
- Compose the async image task's independent request-body reservation with its existing persisted group/subscription route. Polling retains its original ownership and authorization checks.
- Compose bounded upstream drain with Aino's one-dispatch inference operation. Redirects/body replay remain disabled for paid device dispatch; artifact GET/HEAD reads keep their existing redirect policy.
- Bump API-key authentication snapshots to version 28: both source branches used version 27 for different fields. Reload old snapshots instead of defaulting absent privacy/RPM policy fields, while preserving device identity, freshness checks and image-count billing fields.
- Regenerate Wire from the combined providers. Both Aino IQ/diagnostic cleanup and upstream digest-cache/notification-worker shutdown remain registered.
- Keep public upstream-balance errors as service unavailable, with original provider diagnostics available internally. Local site-user balance failures retain their original semantics.

## Runtime defaults to review when deploying

The new keys belong to `gateway`, as implemented in `deploy/config.example.yaml`:

- `request_body_memory_budget_bytes`: 1 GiB shared ingress allocation budget by default; exhausted capacity returns 503 with `Retry-After`. This does not cap total process RSS. Size it to the actual app container with headroom for SSE frames, caches, connections and GC.
- `request_body_read_timeout_seconds` / `downstream_write_timeout_seconds`: 120 seconds per stalled socket operation; healthy stream duration is not limited to 120 seconds.
- Client-disconnect drain is finite, so usage arriving after its expiry cannot be included in that request's settlement. Image forwarding keeps its image-specific window.
- GPT-6.1 Terra adopts upstream context/effort descriptors and subscription long-context exemptions. API-key pricing follows the configured long-context rules; unsupported `minimal` effort returns 400.

Source integration does not rewrite deployed settings, account mappings, prices, credentials, allowlists or existing deployment configuration. New provider model support still requires an upstream account that actually serves that model.

## Validation performed

- Clean-baseline targeted Aino regressions before the merge.
- Full backend unit suite and full integration suite with `CI=1`, using disposable PostgreSQL and Redis; missing Docker cannot count as success.
- Device subscription lifecycle/admin changes, original-group billing, operation faults and inference idempotency HTTP contracts against a newly built API validation server.
- Targeted race checks across request-body memory, detached drain, async task ownership, device operations and combined auth snapshots.
- Frontend ESLint, TypeScript, all 3,313 Vitest tests, and production build.
- Backend build with embedded frontend and golangci-lint v2.14.0: zero issues.
- Regression demonstrating that a pre-upgrade version-27 snapshot is accepted before the version bump and rejected after it, plus combined device/image/privacy snapshot and single-dispatch/drain coverage.
- Independent read-only compatibility review; the detected scheduler-call build mismatch was corrected and the complete suites were run afterward.

Live model calls, payments, production changes and the separate desktop application's opt-in native cloud-sync consumer were not run for this source integration. API cloud-sync HTTP/storage behavior is covered by the integration suite.
