# Task 1: Team Funding Domain

Implemented the team funding management API/domain, PostgreSQL persistence, database migration, and durable shared funding reservation and settlement support.

## Contracts

- `NewTeamFundingRepository(_ *ent.Client, db *sql.DB) service.TeamFundingRepository`
- `NewTeamFundingService(repo service.TeamFundingRepository) *service.TeamFundingService`
- `NewTeamFundingHandler(funding *service.TeamFundingService) *handler.TeamFundingHandler`
- `(*TeamFundingService).GetFundingContext(ctx context.Context, userID int64) (*FundingContext, error)`
- `(*TeamFundingService).Reserve(ctx context.Context, cmd *TeamFundingReserveCommand) (*TeamFundingReservation, error)`
- `(*TeamFundingService).Release(ctx context.Context, reservationID, memberUserID int64) error`; only reservations still `reserved` can be released.
- `(*TeamFundingService).MarkDispatched(ctx context.Context, reservationID, memberUserID int64) error`; dispatched holds are retained until settlement.
- `repository.SettleTeamFundingReservationTx(ctx context.Context, tx *sql.Tx, reservationID int64, requestID string, apiKeyID int64, actualAmount string) (*service.TeamFundingSettlement, error)` joins the existing usage billing transaction. Callers skip the ordinary payer balance/subscription debit for shared decisions.

Funding amounts are decimal strings normalized to eight places and persisted in `NUMERIC(20,8)`. Reservation atomically escrows owner balance or occupies selected subscription windows, while member `reserved` and cap accounting serialize in PostgreSQL. Settlement is idempotent, refunds unused escrow, records actual ledger cost and unauthorized overage, and blocks further shared reservations after overage. No automatic dispatched-hold expiry is present.

The management handler implements the specified `/api/v1/user` funding, team, invitation, member, and ledger operations. It derives the actor exclusively from the JWT auth subject and rejects unknown JSON fields. Team-member views limit membership and ledger rows to the caller; owners can view team-wide ledger entries. Personal preferences remain member-controlled, team settings are owner-controlled, invitations require the target's acceptance and expire after seven days, and rejoining retains prior spend and cap history.

## Verification

- `go test -tags=unit ./internal/service ./internal/handler -run 'TeamFunding|FundingDecision' -count=1` passed (service and handler packages).
- `go test -tags=unit ./internal/server/routes -run TeamFunding -count=1` passed.
- `go test -tags=integration ./internal/repository -run TeamFunding -count=1` passed against isolated PostgreSQL/Testcontainers for all 11 cases: invitation ownership, target acceptance/idempotence/expiry/rejoin, concurrent reservations, settlement idempotence/refund/rollback/overage/replay rejection, independent member caps and scoped ledger, cross-source caps, pool exhaustion, paused membership, selected subscription/group/window accounting/refunds, legacy preference defaults, and the zero-cap/zero-cost boundary.
- `git diff --check` passed for Task 1 files.

No production deployment or paid model request was performed.
