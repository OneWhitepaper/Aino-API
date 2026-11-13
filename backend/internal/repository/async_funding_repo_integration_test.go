//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func asyncFundingFixture(t *testing.T) (service.TeamFundingRepository, *service.TeamFundingService, *service.APIKey, int64) {
	t.Helper()
	repo, owner, actor, team, keyID := teamFundingFixture(t)
	teamFundingCap(t, repo, owner, team, actor, "10")
	reservation, err := repo.Reserve(context.Background(), &service.TeamFundingReserveCommand{RequestID: uuid.NewString(), APIKeyID: keyID, TeamID: team, MemberUserID: actor, PayerUserID: owner, Source: service.TeamFundingSourceBalance, Amount: "10"})
	require.NoError(t, err)
	key := &service.APIKey{ID: keyID, UserID: actor, Group: &service.Group{ID: 123, Name: "accepted group", Platform: service.PlatformGrok}, Funding: &service.FundingDecision{Source: reservation.Source, PayerUserID: owner, TeamID: team, ReservationID: reservation.ID, RequestID: reservation.RequestID, ReservedAmount: reservation.ReservedAmount}}
	return repo, service.NewTeamFundingService(repo), key, owner
}
func TestAsyncFundingQueueOwnsHoldUntilActualDispatch(t *testing.T) {
	ctx := context.Background()
	repo, funding, key, owner := asyncFundingFixture(t)
	require.NoError(t, funding.HandoffFundingToAsync(ctx, key, service.AsyncFundingProviderImage, "imgtask_"+uuid.NewString()))
	require.ErrorIs(t, repo.Release(ctx, key.Funding.ReservationID, key.UserID), service.ErrTeamFundingConflict)
	var balance string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance::text FROM users WHERE id=$1", owner).Scan(&balance))
	require.Equal(t, "490.00000000", balance)
	require.NoError(t, service.BeginFundingDispatch(service.FundingDispatchContext(ctx, key)))
	require.ErrorIs(t, service.BeginFundingDispatch(service.FundingDispatchContext(ctx, key)), service.ErrTeamFundingConflict)
	var status string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT status FROM team_funding_reservations WHERE id=$1", key.Funding.ReservationID).Scan(&status))
	require.Equal(t, "dispatched", status)
}
func TestAsyncFundingQueuedCancellationCannotRefundUnknownDispatch(t *testing.T) {
	ctx := context.Background()
	repo, funding, key, _ := asyncFundingFixture(t)
	registry := repo.(service.AsyncFundingRepository)
	require.NoError(t, funding.HandoffFundingToAsync(ctx, key, service.AsyncFundingProviderImage, uuid.NewString()))
	require.NoError(t, registry.CancelQueuedAsyncFunding(ctx, key.Funding.RequestID, key.ID, key.UserID))
	require.ErrorIs(t, service.BeginFundingDispatch(service.FundingDispatchContext(ctx, key)), service.ErrTeamFundingConflict)
	_, funding, key, _ = asyncFundingFixture(t)
	require.NoError(t, funding.HandoffFundingToAsync(ctx, key, service.AsyncFundingProviderImage, uuid.NewString()))
	require.NoError(t, service.BeginFundingDispatch(service.FundingDispatchContext(ctx, key)))
	require.ErrorIs(t, registry.CancelQueuedAsyncFunding(ctx, key.Funding.RequestID, key.ID, key.UserID), service.ErrTeamFundingConflict)
}
func TestAsyncFundingRestoreAfterRemovalAndCacheLossUsesOriginalPayer(t *testing.T) {
	ctx := context.Background()
	repo, funding, key, owner := asyncFundingFixture(t)
	taskID := "video_" + uuid.NewString()
	require.NoError(t, funding.HandoffFundingToAsync(ctx, key, service.AsyncFundingProviderVideo, key.Funding.RequestID))
	require.NoError(t, service.BeginFundingDispatch(service.FundingDispatchContext(ctx, key)))
	pricing := json.RawMessage(`{"model":"accepted-video","video_duration_seconds":8,"video_resolution":"720p"}`)
	require.NoError(t, funding.BindAsyncFundingTask(ctx, key, service.AsyncFundingProviderVideo, taskID, 77, pricing))
	require.NoError(t, repo.RemoveMember(ctx, owner, key.Funding.TeamID, key.UserID))
	_, err := repo.UpdateFunding(ctx, key.UserID, &service.FundingUpdateCommand{Mode: service.FundingModePersonalOnly, AllowPersonalBalance: true})
	require.NoError(t, err)
	nowKey := *key
	nowKey.Group = &service.Group{ID: 999, Name: "changed group"}
	nowKey.Funding = nil
	restarted := service.NewTeamFundingService(NewTeamFundingRepository(testEntClient(t), integrationDB))
	restored, _, err := restarted.RestoreAsyncFundingRequest(ctx, &nowKey, service.AsyncFundingProviderVideo, taskID)
	require.NoError(t, err)
	require.Equal(t, owner, restored.Funding.PayerUserID)
	require.Equal(t, int64(123), restored.Group.ID)
	require.NotSame(t, key.Group, restored.Group)
	foreign := nowKey
	foreign.UserID++
	_, _, err = restarted.RestoreAsyncFundingRequest(ctx, &foreign, service.AsyncFundingProviderVideo, taskID)
	require.ErrorIs(t, err, service.ErrTeamFundingNotFound)
	task, err := repo.(service.AsyncFundingRepository).GetAsyncFunding(ctx, service.AsyncFundingProviderVideo, taskID, key.UserID, key.ID)
	require.NoError(t, err)
	require.Equal(t, int64(77), task.Snapshot.AccountID)
	require.JSONEq(t, string(pricing), string(task.Snapshot.PricingSnapshot))
}
func TestAsyncFundingTerminalUsageFirstWriteWinsAcrossConcurrentPolls(t *testing.T) {
	ctx := context.Background()
	repo, funding, key, _ := asyncFundingFixture(t)
	taskID := uuid.NewString()
	require.NoError(t, funding.HandoffFundingToAsync(ctx, key, service.AsyncFundingProviderGrokVideo, taskID))
	require.NoError(t, service.BeginFundingDispatch(service.FundingDispatchContext(ctx, key)))
	registry := repo.(service.AsyncFundingRepository)
	first, err := registry.StoreAsyncFundingUsage(ctx, key.Funding.RequestID, key.ID, key.UserID, json.RawMessage(`{"Result":{"VideoCount":1}}`))
	require.NoError(t, err)
	second, err := registry.StoreAsyncFundingUsage(ctx, key.Funding.RequestID, key.ID, key.UserID, json.RawMessage(`{"Result":{"VideoCount":99}}`))
	require.NoError(t, err)
	require.JSONEq(t, string(first), string(second))
	require.NoError(t, registry.MarkAsyncFundingSettled(ctx, key.Funding.RequestID, key.ID, key.UserID))
	task, err := registry.GetAsyncFunding(ctx, service.AsyncFundingProviderGrokVideo, taskID, key.UserID, key.ID)
	require.NoError(t, err)
	require.Equal(t, "settled", task.State)
}
func TestImageTaskPostgresFallbackSurvivesRedisRecordLoss(t *testing.T) {
	ctx := context.Background()
	store := NewImageTaskStore(integrationRedis, integrationDB)
	id := "imgtask_" + uuid.NewString()
	record := &service.ImageTaskRecord{ID: id, UserID: 12, APIKeyID: 34, Status: service.ImageTaskStatusCompleted, Result: json.RawMessage(`{"data":[{"url":"https://fixture.invalid/image.png"}]}`)}
	require.NoError(t, store.Save(ctx, record, time.Hour))
	require.NoError(t, integrationRedis.Del(ctx, imageTaskKey(id)).Err())
	restored, err := NewImageTaskStore(integrationRedis, integrationDB).Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, record.UserID, restored.UserID)
	require.JSONEq(t, string(record.Result), string(restored.Result))
	t.Cleanup(func() { _, _ = integrationDB.ExecContext(ctx, "DELETE FROM image_task_records WHERE task_id=$1", id) })
}

func TestGeminiCacheFundingSnapshotPersistsOriginalResourceOwnerAndPayer(t *testing.T) {
	ctx := context.Background()
	repo, funding, key, owner := asyncFundingFixture(t)
	record := &service.GeminiCachedContent{PublicID: "cache" + strings.ReplaceAll(uuid.NewString(), "-", ""), UserID: key.UserID, APIKeyID: key.ID, GroupID: key.Group.ID, AccountID: 77, UpstreamName: "cachedContents/fixture", Model: "gemini-cache-fixture", UpstreamModel: "models/gemini-cache-fixture", TotalTokenCount: 4321, ExpireTime: time.Now().Add(time.Hour), FundingSnapshot: service.AsyncFundingSnapshotFromRequest(key)}
	cacheRepo := NewGeminiCachedContentRepository(integrationDB)
	require.NoError(t, cacheRepo.Create(ctx, record))
	require.NoError(t, repo.RemoveMember(ctx, owner, key.Funding.TeamID, key.UserID))
	live := *key
	live.Group = &service.Group{ID: 999}
	live.Funding = nil
	restored, _, err := funding.RestoreAsyncFundingRequest(ctx, &live, service.AsyncFundingProviderGeminiCache, record.PublicName())
	require.NoError(t, err)
	require.Equal(t, owner, restored.Funding.PayerUserID)
	require.Equal(t, key.Group.ID, restored.Group.ID)
	fetched, err := funding.GetFundingCachedContent(ctx, &live, record.PublicName())
	require.NoError(t, err)
	require.Equal(t, int64(4321), fetched.TotalTokenCount)
	foreign := live
	foreign.UserID++
	_, err = funding.GetFundingCachedContent(ctx, &foreign, record.PublicName())
	require.ErrorIs(t, err, service.ErrGeminiCachedContentNotFound)
	require.NoError(t, cacheRepo.SoftDelete(ctx, record.ID))
	_, _, err = funding.RestoreAsyncFundingRequest(ctx, &live, service.AsyncFundingProviderGeminiCache, record.PublicName())
	require.ErrorIs(t, err, service.ErrTeamFundingNotFound)
}
func TestBatchImageSharedFundingSettlementChargesActorCapAndOriginalPayerOnce(t *testing.T) {
	ctx := context.Background()
	repo, funding, key, owner := asyncFundingFixture(t)
	account := mustCreateAccount(t, testEntClient(t), &service.Account{Name: "batch-shared-" + uuid.NewString(), Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}})
	batchID := "imgbatch_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, funding.HandoffFundingToAsync(ctx, key, service.AsyncFundingProviderBatchImage, batchID))
	require.NoError(t, service.BeginFundingDispatch(service.FundingDispatchContext(ctx, key)))
	require.NoError(t, funding.BindAsyncFundingTask(ctx, key, service.AsyncFundingProviderBatchImage, batchID, account.ID, nil))
	jobs := NewBatchImageRepository(integrationDB)
	hold := 10.0
	_, err := jobs.CreateBatchImageJob(ctx, service.CreateBatchImageJobParams{BatchID: batchID, UserID: key.UserID, APIKeyID: &key.ID, AccountID: &account.ID, Provider: service.BatchImageProviderGeminiAPI, Model: "batch-fixture", Status: service.BatchImageJobStatusSettling, ItemCount: 10, SuccessCount: 3, FailCount: 7, EstimatedCost: 10, HoldAmount: &hold, BillableUnitPrice: 1, GroupRateMultiplier: 1, AccountRateMultiplier: 1, BatchDiscountMultiplier: 1, HoldMultiplier: 0.1, PricingSnapshotVersion: 1, FundingSnapshot: service.AsyncFundingSnapshotFromRequest(key)})
	require.NoError(t, err)
	billing := NewUsageBillingRepository(testEntClient(t), integrationDB)
	storedJob, err := jobs.GetBatchImageJobByBatchID(ctx, batchID)
	require.NoError(t, err)
	command := &service.UsageBillingCommand{RequestID: key.Funding.RequestID, APIKeyID: key.ID, UserID: key.UserID, PayerUserID: owner, FundingReservationID: key.Funding.ReservationID, FundingSource: key.Funding.Source, FundingCost: 3, AccountID: account.ID, Model: storedJob.Model, MediaType: "image", ImageCount: 3, APIKeyQuotaCost: 3, APIKeyRateLimitCost: 3, AccountQuotaCost: 3, RequestPayloadHash: service.BuildBatchImageSettlementManifestHash(storedJob)}
	command.Normalize()
	terminal, err := json.Marshal(map[string]any{"batch_command": command})
	require.NoError(t, err)
	_, err = repo.(service.AsyncFundingRepository).StoreAsyncFundingUsage(ctx, key.Funding.RequestID, key.ID, key.UserID, terminal)
	require.NoError(t, err)
	// A restarted billing-only worker completes known terminal cost without any
	// client polling and without receiving a generation callback.
	keys := service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, &config.Config{})
	keys.SetTeamFundingService(funding)
	recovery := service.NewAsyncFundingRecoveryService(funding, keys, NewUserRepository(testEntClient(t), integrationDB), nil, nil, billing, jobs, nil)
	recovered, recoveryErr := recovery.RunOnce(ctx)
	require.NoError(t, recoveryErr)
	require.GreaterOrEqual(t, recovered, 1)
	settlement := &service.BatchImageSettlementService{Repo: jobs, BillingRepo: billing, Pricing: &service.BatchImageModelPricingResolver{}}
	result, err := settlement.Settle(ctx, batchID)
	require.NoError(t, err)
	require.Equal(t, 3.0, result.ActualCost)
	result, err = settlement.Settle(ctx, batchID)
	require.NoError(t, err)
	require.True(t, result.AlreadySettled)
	var balance, personal, frozen, spent, reserved string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance::text FROM users WHERE id=$1", owner).Scan(&balance))
	require.Equal(t, "497.00000000", balance)
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance::text,frozen_balance::text FROM users WHERE id=$1", key.UserID).Scan(&personal, &frozen))
	require.Equal(t, "0.00000000", personal)
	require.Equal(t, "0.00000000", frozen)
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT spent::text,reserved::text FROM team_members WHERE team_id=$1 AND user_id=$2", key.Funding.TeamID, key.UserID).Scan(&spent, &reserved))
	require.Equal(t, "3.00000000", spent)
	require.Equal(t, "0.00000000", reserved)
	var ledgers int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT count(*) FROM team_funding_ledger WHERE reservation_id=$1", key.Funding.ReservationID).Scan(&ledgers))
	require.Equal(t, 1, ledgers)
	// A second job must still respect the same member cap rather than freezing a personal wallet.
	_, err = repo.Reserve(ctx, &service.TeamFundingReserveCommand{RequestID: uuid.NewString(), APIKeyID: key.ID, TeamID: key.Funding.TeamID, MemberUserID: key.UserID, PayerUserID: owner, Source: service.TeamFundingSourceBalance, Amount: "8"})
	require.Error(t, err)
}

func TestAsyncFundingHTTPFollowupAfterRemovalAndZeroWalletUsesAcceptedSnapshot(t *testing.T) {
	ctx := context.Background()
	repo, funding, key, owner := asyncFundingFixture(t)
	taskID := "video_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, funding.HandoffFundingToAsync(ctx, key, service.AsyncFundingProviderVideo, taskID))
	require.NoError(t, service.BeginFundingDispatch(service.FundingDispatchContext(ctx, key)))
	require.NoError(t, funding.BindAsyncFundingTask(ctx, key, service.AsyncFundingProviderVideo, taskID, 77, json.RawMessage(`{"model":"fixture"}`)))
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = repo.SettleTeamFundingReservationTx(ctx, tx, key.Funding.ReservationID, key.Funding.RequestID, key.ID, "10")
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	require.NoError(t, repo.RemoveMember(ctx, owner, key.Funding.TeamID, key.UserID))
	users := NewUserRepository(testEntClient(t), integrationDB)
	_, err = users.SetBalance(ctx, owner, 0)
	require.NoError(t, err)
	keyRepo := NewAPIKeyRepository(testEntClient(t), integrationDB)
	actual, err := keyRepo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	cfg := &config.Config{RunMode: config.RunModeStandard}
	keys := service.NewAPIKeyService(keyRepo, users, NewGroupRepository(testEntClient(t), integrationDB), NewUserSubscriptionRepository(testEntClient(t)), NewUserGroupRateRepository(integrationDB), nil, cfg)
	keys.SetTeamFundingService(funding)
	router := gin.New()
	router.Use(gin.HandlerFunc(middleware.NewAPIKeyAuthMiddleware(keys, nil, cfg)))
	var observed int
	router.GET("/v1/videos/:request_id", func(c *gin.Context) {
		restored, ok := middleware.GetAPIKeyFromContext(c)
		require.True(t, ok)
		require.Equal(t, owner, restored.Funding.PayerUserID)
		require.Equal(t, int64(123), restored.Group.ID)
		require.True(t, service.IsFundingFollowup(c.Request.Context()))
		observed++
		c.Status(http.StatusOK)
	})
	request := httptest.NewRequest(http.MethodGet, "/v1/videos/"+taskID, nil)
	request.Header.Set("Authorization", "Bearer "+actual.Key)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, 1, observed)
	var personal string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance::text FROM users WHERE id=$1", key.UserID).Scan(&personal))
	require.Equal(t, "0.00000000", personal)
}
