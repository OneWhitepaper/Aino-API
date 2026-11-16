# API worktree reconciliation — 2026-11-17

This review compares the remaining local worktrees with `origin/main` at
`00c27d833851`. Historical branches and their original commits are retained.

| Worktree branch | Result |
| --- | --- |
| `codex/device-name-web` | The computer-name display from `e0aac2db3` was missing from `main`. It is reapplied on the current API key page, including device metadata lookup, compatibility fallback and cancellation of stale lookups. The separate Go 1.27.4 change in `8843df750` is already present in the current build, Docker and workflow configuration. |
| `codex/desktop-credential-hygiene` | The temporary-credential wording in `0ba66be74` predates the permanent device-key design. Current `main` distinguishes permanent device authorization from older renewable leases, so the obsolete wording is not restored. |
| `codex/desktop-distribution-fixture` | The repeated-tool-call problem addressed by `02a9adadb` is already handled by `55233ff57` on `main`. Its `nativeFixtureToolCallID` derives the matching request/result ID from the conversation turn. The newer implementation is retained. |
| `codex/image-quality-usage-acceptance`, `codex/aino-private-diagnostics`, `codex/aino-v028-phone-binding` | Their heads are ancestors of current `main`; no remaining working-tree changes were found. |
| `codex/team-shared-billing` | Its head matches current `main`, with a clean working tree. |
| `api-device-key-general-release` (detached) | Its head is already an ancestor of `main`, with a clean working tree. |

The old `/private/tmp/aino-cloud-sync-api-staging` and
`/private/tmp/aino-models-release-81119efd5` worktree registrations do not point to
usable Git checkouts. Their recorded commits remain in the repository; this
reconciliation does not recreate or delete those temporary directories.

The restored device labels are display metadata only. Device IDs still identify
authorizations, manual key names remain unchanged, and the current authorization,
revocation and per-request billing behavior is preserved. Missing metadata or an
older server keeps the existing key name and abbreviated device ID.

Validation: the computer-name, optional-metadata fallback and late-result tests
were reproduced failing on the current baseline before the implementation was
reapplied. The API key page's tests include these contracts alongside permanent
device authorization and revocation behavior. Final local validation passed:
423 frontend test files / 3,357 tests, ESLint, locale completeness, Vue TypeScript
build checking and the production Vite build. This reconciliation does not deploy
the API or publish desktop installation packages.

The integration CI also exposed interference in the existing escaped-content
allocation regression. `testing.AllocsPerRun` counts allocations across the test
process, including background workers started by unrelated service tests. The
same large-input contract now runs in a fresh instance of the test binary with
the original exact zero-allocation assertion, a bounded timeout and the same
compiled production scanner. Thirty repeated precheck test runs passed locally;
the scanner and its allocation limit are unchanged.
