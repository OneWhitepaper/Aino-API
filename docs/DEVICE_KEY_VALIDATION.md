# Device-key subscription and reliability validation

Validated against API main `409b0e7dcc43472c826006f3e67af5b7cf2a7b16` on 2026-11-05.
This patch adds integration coverage only. It changes no runtime behavior.

**Rollout gate: logical HTTP retries can execute and charge twice.** Passing
the characterization tests below does not satisfy a guarantee that resubmitting
the same logical operation never creates a second charge.

## Reproduce

Run from `backend/` with Docker available. `CI=1` makes unavailable Docker fail
instead of allowing the repository's local integration-test skip.

```sh
CGO_ENABLED=0 go build -trimpath -o /tmp/aino-device-validation-server ./cmd/server
CI=1 go test -tags=integration,nativeconsumer ./internal/repository \
  -run '^Test(DeviceSubscriptionLifecycle|DeviceBillingHTTPToOriginalGroupLedger|DesktopCatalogRealPermissionsAndSubscription|DeviceReliabilityValidationAPI)' \
  -count=1 -timeout=5m -v -args \
  -aino-preflight-server=/tmp/aino-device-validation-server
```

The root verification completed in 103.318 seconds with seven parent tests passing
and no skips. The reliability and subscription-lifecycle fixtures use disposable
PostgreSQL and Redis containers, two real API processes, synthetic users/packages,
and loopback simulated providers. The additional billing/catalog tests use
in-process HTTP fixtures. No fixture reads production credentials or contacts
real model providers.

## Observed contracts

| Area | Result |
| --- | --- |
| Subscription ownership | One device key can select text/image wallet and subscription model options; ledger group and subscription IDs match the selected option. |
| Debit order | Subscription options debit their own package; wallet options debit balance. Zero balance does not block an eligible subscription. |
| Exhaustion | Exhausted text allowance does not consume the image package. Exhausted packages reject even with wallet funds and a configured fallback group; a wallet option must be selected explicitly. |
| Counters | Daily, weekly and monthly usage are attributed to the correct subscription. Daily-limit exhaustion is covered. |
| Weekly/monthly exhaustion | Independent text/image caps reject on both instances with funded wallet and configured fallback, retaining exact package counters and settled row ownership. |
| Window reset | Synthetic stale daily/weekly/monthly anchors reset only the elapsed counter; other counters and the other package are retained. A second instance adds rather than erases the first charge. Daily uses UTC midnight in this fixture; weekly/monthly use rolling 7-day/30-day anchors. |
| Expiry | Two warmed API instances reject packages after a synthetic 20-second expiry, while explicit wallet options remain usable with the same key. Startup precedes expiry assignment and warm-up margin is checked. |
| Concurrent HTTP | Existing 48-request batch has zero errors; added 16-request matrix covers two processes, four routes and two calls per route/process, with exact settled row counts and monetary ownership. |
| Revocation | Both warmed instances deny the revoked key. Explicit reauthorization changes the secret and restores access on both instances. |
| Missed invalidation | Suppressing outbox/broadcast still makes both instances deny after the existing approximately 55-second cache hard age. |
| Settlement replay | For wallet and subscription, 24 simultaneous `Apply` callers apply once; after archival, 24 further callers apply zero times. Fingerprint conflicts are rejected without changing money or counters. |
| Logical HTTP replay | **Fails the desired no-double-charge condition:** identical payload and logical/correlation headers sent to two instances execute upstream twice and settle twice, for all four wallet/subscription text/image routes. |

The additional window tests passed independently in 8.347 seconds, with five
scenarios, 18 accepted requests and 20 rejected requests. They seed timestamps
after both processes start and gate setup on the existing test deadline to avoid
an unintended UTC midnight crossing. These are synthetic timestamp checks, not
real elapsed-day/week/month observation.

In the last combined run, the 48-request batch had p95 102.679 ms and the added
16-request matrix had p95 55.326 ms. These small loopback batches are correctness
checks, not production capacity measurements.

## Retry boundary

The HTTP middleware generates a fresh internal client request ID for each inbound
dispatch. The billing resolver prefers that ID, and the ledger deduplicates a
server settlement request ID plus API key. Desktop session/turn/call headers
currently provide attribution rather than a durable operation identity.

The replay test verifies identical request bytes, identical desktop attribution
and inbound correlation headers, two upstream executions, two distinct server
request IDs, two settled usage rows and two billing claims. Each text pair costs
`0.00020000`; each image pair costs `0.16000000` in the synthetic fixture. A shared
provider response ID also does not collapse separate HTTP dispatches.

Primary Desktop SDK retries are disabled, but Hermes streaming/turn recovery can
still resubmit after ordinary network errors. The test resends a completed
request; actual response-loss timing and native Desktop retry execution have not
been measured here. Their risk follows from the current source path.

Keep rollout limited pending a defined retry policy. A follow-up must either
provide a durable operation/result boundary or stop automatic retries after an
ambiguous accepted managed request. Deduplicating only the debit while executing
the provider twice would leave upstream cost unresolved. The follow-up needs a
two-instance fault test where upstream acceptance precedes response loss.

## Limits and separate operational evidence

These tests do not cover live-cache real midnight/time-zone/DST rollover,
concurrent reset races, real 365-day retention, archive cleanup racing the first
settlement, database or Redis outages, crash recovery during settlement, sustained load, or a real
24-hour usage cycle. Synthetic expiry and archived timestamps are not elapsed-time
evidence. Production observation receipts belong in private deployment records;
this test report contains only synthetic-account results.
