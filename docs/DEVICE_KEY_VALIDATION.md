# Device-key billing and durable inference operation validation

Validated on 2026-11-05 against API main
`409b0e7dcc43472c826006f3e67af5b7cf2a7b16` plus this PR's validation and
runtime fix. This report describes isolated test evidence and the proposed
release sequence; it is not a production deployment receipt.

**Release gate: complete the fix, independent review, required tests, exact-SHA
CI and local actual validation before deployment.** The user authorized deploying
after those checks and continuing observation on the same explicitly authorized
account/device; completing 24 hours first is no longer a release prerequisite.
This PR does not distribute Desktop installers or expand the rollout.

## Runtime contract

A managed inference POST carrying a valid `X-Aino-Operation-ID` claims a record
in the existing PostgreSQL idempotency store **before provider dispatch**. Its
scope is the authenticated user and API key. The fingerprint includes the method,
route, exact request-body digest, content type, query, selected catalog option,
resolved group and subscription. Reusing the ID with different content or a
different model/billing selection returns `409 IDEMPOTENCY_KEY_CONFLICT`.

The claim starts and stays `inference_unknown`. It is an immutable reservation,
not an indication that inference is still running. A matching duplicate returns
`409 DESKTOP_OPERATION_UNKNOWN`, including after success, response loss, process
restart, expiration timestamps or normal cleanup. It never replays a stored
response, reclaims the reservation, or authorizes another provider execution.
Store failure denies the request before provider dispatch. Ordinary idempotency
reclaim/completion/failure updates and expiry cleanup preserve these records;
ordinary expiring records still clean up.

The request context permits one inference network dispatch across forwarding
retries and account failover. Operation-aware OAuth forwarding uses the existing
guarded HTTP transport rather than the plugin bridge. Inference POST redirects
are not followed; artifact GET/HEAD requests and their redirects remain usable. Private operation headers are stripped before
forwarding. Read-only routes and token counting do not claim operations. Clients
without the new header, and ordinary non-managed API keys, retain their existing
behavior. A fresh operation ID means new billable work; clients must retain the
same ID when the original outcome is ambiguous and must not silently replace it.

This is a conservative at-most-one-dispatch boundary. It does not guarantee a
result or exactly-once settlement: if a process dies after provider acceptance
but before usage returns, the operation remains unknown and may have provider
cost without a local debit. Even a failure before provider acceptance can leave
an unknown reservation once claimed. There is no result-recovery/replay endpoint
or automatic reconciliation in this patch. Tombstones are retained indefinitely;
their storage growth needs operational tracking and a separately reviewed
retention policy that cannot reopen old operations.

## Reproduce

Run from `backend/` with the version in `go.mod` and Docker available. `CI=1`
makes unavailable Docker fail instead of allowing a local integration-test skip.
The explicit CI selector includes both fault and idempotency contracts.

```sh
CGO_ENABLED=0 go build -trimpath -o /tmp/aino-device-validation-server ./cmd/server
CI=1 go test -tags=integration,nativeconsumer ./internal/repository \
  -run '^Test(DeviceSubscriptionLifecycle|DeviceBillingHTTPToOriginalGroupLedger|DesktopCatalogRealPermissionsAndSubscription|DeviceReliabilityValidationAPI|DeviceOperation|Idempotency)' \
  -count=1 -timeout=5m -v -args \
  -aino-preflight-server=/tmp/aino-device-validation-server
```

The final combined run passed all **16 parent tests without skips in 121.857
seconds**, using the rebuilt API with artifact-read/redirect handling and guarded
OAuth transport. A preceding
focused run passed the three fault parent tests and all 12 route/fault cases in
15.145 seconds. Targeted middleware, service and HTTP transport tests also passed
with the race detector, including a separate OAuth plugin-bypass contract. The
repository fault contracts were demonstrated red
against the unpatched main API: matching retries returned 200 instead of 409 and
the text provider-loss path could fail over and return another result. Repository
tombstone tests also failed before the guards, exposing reclaim, mutation and
cleanup of unknown reservations.

Fixtures use disposable PostgreSQL/Redis containers, two real API processes,
synthetic users/packages and loopback providers. Billing/catalog contracts also
use in-process HTTP fixtures. No fixture reads production credentials or contacts
real model providers. Production observation receipts remain in private
deployment records.

## Observed contracts

| Area | Result |
| --- | --- |
| Client loses completed API response | Across wallet/subscription text/image, the provider executes once and one debit settles. Four concurrent matching retries across two instances all return unknown; changed body or catalog selection conflicts. |
| First-claim race | Two real API processes receive the same operation concurrently; one executes and settles, the other returns unknown. Fresh IDs and independent API-key scopes can execute separately. |
| Provider accepts, API crashes | The provider accepts once; the owning process is killed and restarted before usage returns. Both processes reject matching retries before/after restart and after synthetic claim expiry plus cleanup. There is no local settled debit for the interrupted original. |
| Provider accepts, response is lost | The controlled provider closes the connection after acceptance. There is one provider execution, no internal failover reexecution and no settled debit; a retry to the other API returns unknown. |
| PostgreSQL claim and permanence | Of 24 concurrent claims, one wins. Unknown reservations resist ordinary reclaim/success/retryable mutations, survive expiry cleanup without occupying its batch, and cannot reopen through ordinary execution. Backdating by 400 days is synthetic evidence. |
| No-header compatibility | Identical requests with the older logical/correlation headers still execute and settle twice across two instances. Those attribution headers alone do not opt into the operation boundary. |
| Subscription ownership | One device key can select text/image wallet and subscription model options; ledger group and subscription IDs match the selected option. |
| Debit order and exhaustion | Subscription options debit their own package and wallet options debit balance. Zero wallet balance does not block an eligible subscription. Exhausted packages reject despite funded wallet and configured fallback; choosing wallet is explicit. |
| Counters and window caps | Daily/weekly/monthly usage belongs to the correct subscription. Independent text/image caps reject on both instances with exact package counters and settled row ownership. |
| Window reset | Synthetic stale anchors reset only the elapsed counter and retain other counters/packages. The second instance adds rather than erases the first charge. Daily uses UTC midnight in the fixture; weekly/monthly use rolling 7-day/30-day anchors. |
| Expiry | Two warmed instances reject packages after a synthetic 20-second expiry while explicit wallet options remain usable with the same key. |
| Concurrent HTTP | The existing 48-request batch and added 16-request four-route/two-process matrix complete without errors and retain exact settled row counts and monetary ownership. These are correctness checks, not capacity measurements. |
| Revocation and missed invalidation | Both warmed instances deny revoked keys; explicit reauthorization changes the secret and restores access. Suppressing outbox/broadcast still denies after the existing approximately 55-second cache hard age. |
| Settlement replay | Wallet/subscription settlement applies once across 24 simultaneous callers, and zero further times after archival. Conflicting fingerprints do not change money or counters. This ledger boundary is separate from the provider-dispatch boundary. |

## Release order and rollback

1. Complete the release gate above and review the existing private observation
   evidence and anomalies. Preserve its receipt as an interrupted pre-deployment
   observation when the serving container changes. Record deployment time and
   start a separately labeled observation on the candidate; a finite checkpoint
   cycle is not a completed 24-hour observation.
2. Finish review/CI on the exact API revision, record its SHA and artifact hash,
   and retain the currently deployed artifact and tested backup/restore receipts.
   Use the existing deployment procedure; this fix adds no migration and does not
   change applied SQL checksums.
3. Deploy the compatible API before using the operation-aware client. Preserve
   the exact existing `desktop_device_keys.rollout_allowlist` account/device pair
   and existing model/package permissions. Preserve configuration bytes, the
   active device identity, PostgreSQL/Redis and all operation/ledger records.
   Verify API health plus the authorized
   canary's authentication, normal text/image billing, attribution and operation
   rejection behavior. Keep this bounded to the existing canary; do not publish
   Desktop installers or enable additional accounts/devices.
4. Reconcile any unknown operation using private provider/API/ledger evidence.
   Never clear its claim, regenerate its ID, or repeat the inference to infer its
   outcome. Any explicitly requested new work uses a fresh ID and may cost money.
5. On a regression, stop the canary's affected managed requests first. Set
   `desktop_device_keys.enabled: false` to pause new issuance/rotation; this gate
   does **not** stop already issued keys. If needed, revoke the canary device via
   the existing revocation path and verify denial on both instances. Keep the
   allowlist intact; an empty allowlist with the gate enabled opens issuance to
   everyone.
6. Retain all operation records and original usage/billing data during rollback.
   Restore the prior recorded artifact only after the managed canary is stopped
   and no `inference_unknown` claims exist, or after its cleanup code has been
   updated to preserve them. The unpatched older API can delete these claims
   during startup cleanup and lacks this duplicate-dispatch guarantee. Do not resume an
   operation-aware canary against it. Use the established backup/restore procedure
   for any database recovery; binary rollback does not reverse migrations or
   justify deleting immutable claims. Resume only after the reviewed fix and
   canary checks pass again.

## Limits

Fault providers are controlled loopback HTTP fixtures, not real provider or
native Desktop executions. The four-route fault matrix covers non-streaming
wallet/subscription chat completions and image generation; it does not establish
all streaming, Responses, Anthropic, image-edit or artifact-provider behavior.
Authentication/API-key rotation creates a new key scope and is not cross-key
deduplication. Existing no-header clients retain their prior retry risk.

The tests do not cover live-cache real midnight/time-zone/DST rollover,
concurrent reset races, actual 365-day retention, database/Redis outages,
settlement crash recovery, sustained load or a real 24-hour usage cycle.
Synthetic timestamp expiry, process restart and archived replay are not
elapsed-time production evidence. A successful finite checkpoint cycle does not
prove 24 hours of observation or authorize a wider rollout.
