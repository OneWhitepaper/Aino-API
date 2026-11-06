//go:build integration

package repository_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/google/uuid"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestDesktopSyncHTTPContract(t *testing.T) {
	r := newDesktopModelRig(t)
	r.desktopHandler.SetSyncService(service.NewDesktopSyncService(repository.NewDesktopSyncRepository(repository.GetIntegrationDB())))
	owner := r.user(t, service.StatusActive, false)
	token, err := r.auth.GenerateToken(r.ctx, owner)
	require.NoError(t, err)
	body := `{"version":1,"device_id":"device-a","after":"0","limit":50,"operations":[{"operation_id":"00000000-0000-4000-8000-000000000001","key":"conversation/default/one","base_revision":"0","deleted":false,"payload":{"schema":1}}]}`
	w := r.request(http.MethodPost, "/api/v1/desktop/sync/exchange", body, token)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"status":"applied"`)
}

func syncInput(key, base, value string) service.DesktopSyncRequest {
	return service.DesktopSyncRequest{Version: 1, DeviceID: "device-a", After: "0", Limit: 50, Operations: []service.DesktopSyncOperation{{OperationID: uuid.NewString(), Key: key, BaseRevision: base, Payload: json.RawMessage(value)}}}
}

func TestDesktopSyncDurableReceiptsIsolationAndTombstones(t *testing.T) {
	r := newDesktopModelRig(t)
	owner, other := r.user(t, service.StatusActive, false), r.user(t, service.StatusActive, false)
	svc := service.NewDesktopSyncService(repository.NewDesktopSyncRepository(repository.GetIntegrationDB()))
	in := syncInput("conversation/default/one", "0", `{"n":1}`)
	first, err := svc.Exchange(r.ctx, owner.ID, in)
	require.NoError(t, err)
	require.Equal(t, "1", first.Results[0].Record.Revision)
	replacement := syncInput(in.Operations[0].Key, "1", `{"n":2}`)
	second, err := svc.Exchange(r.ctx, owner.ID, replacement)
	require.NoError(t, err)
	require.Equal(t, "2", second.Cursor)
	// Recreate the service/repository to prove retry state survives processes.
	svc = service.NewDesktopSyncService(repository.NewDesktopSyncRepository(repository.GetIntegrationDB()))
	replay, err := svc.Exchange(r.ctx, owner.ID, in)
	require.NoError(t, err)
	require.Equal(t, first.Results, replay.Results)
	hidden, err := svc.Exchange(r.ctx, other.ID, service.DesktopSyncRequest{Version: 1, DeviceID: "b", After: "0", Limit: 50})
	require.NoError(t, err)
	require.Empty(t, hidden.Changes)
	require.Equal(t, "0", hidden.Cursor)
	stale := syncInput(in.Operations[0].Key, "0", `{"n":3}`)
	conflict, err := svc.Exchange(r.ctx, owner.ID, stale)
	require.NoError(t, err)
	require.Equal(t, "conflict", conflict.Results[0].Status)
	require.Equal(t, "2", conflict.Results[0].Record.Revision)
	deletion := syncInput(in.Operations[0].Key, "2", "")
	deletion.Operations[0].Deleted = true
	deleted, err := svc.Exchange(r.ctx, owner.ID, deletion)
	require.NoError(t, err)
	require.True(t, deleted.Results[0].Record.Deleted)
	resurrection := syncInput(in.Operations[0].Key, "3", `{"n":4}`)
	rejected, err := svc.Exchange(r.ctx, owner.ID, resurrection)
	require.NoError(t, err)
	require.Equal(t, "conflict", rejected.Results[0].Status)
	require.True(t, rejected.Results[0].Record.Deleted)
	oldConflict, err := svc.Exchange(r.ctx, owner.ID, stale)
	require.NoError(t, err)
	require.Equal(t, conflict.Results, oldConflict.Results, "conflict receipts keep the original record")
	in.Operations[0].Payload = json.RawMessage(`{"n":99}`)
	_, err = svc.Exchange(r.ctx, owner.ID, in)
	require.ErrorIs(t, err, service.ErrDesktopSyncOperationReuse)
}

func TestDesktopSyncConcurrentCASAndPagination(t *testing.T) {
	r := newDesktopModelRig(t)
	owner := r.user(t, service.StatusActive, false)
	svc := service.NewDesktopSyncService(repository.NewDesktopSyncRepository(repository.GetIntegrationDB()))
	const workers = 12
	replies := make(chan *service.DesktopSyncResponse, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := svc.Exchange(context.Background(), owner.ID, syncInput("same", "0", `{"n":1}`))
			replies <- out
			errs <- err
		}()
	}
	wg.Wait()
	close(replies)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	applied := 0
	for reply := range replies {
		if reply.Results[0].Status == "applied" {
			applied++
		}
		require.Equal(t, "1", reply.Cursor)
	}
	require.Equal(t, 1, applied)
	for i := 0; i < 3; i++ {
		_, err := svc.Exchange(r.ctx, owner.ID, syncInput(fmt.Sprint("key", i), "0", `{"n":1}`))
		require.NoError(t, err)
	}
	page := service.DesktopSyncRequest{Version: 1, DeviceID: "reader", After: "0", Limit: 2}
	a, err := svc.Exchange(r.ctx, owner.ID, page)
	require.NoError(t, err)
	require.True(t, a.HasMore)
	require.Equal(t, "2", a.Cursor)
	require.Len(t, a.Changes, 2)
	page.After = a.Cursor
	b, err := svc.Exchange(r.ctx, owner.ID, page)
	require.NoError(t, err)
	require.False(t, b.HasMore)
	require.Equal(t, "4", b.Cursor)
	require.Len(t, b.Changes, 2)
	page.After = "5"
	_, err = svc.Exchange(r.ctx, owner.ID, page)
	require.ErrorIs(t, err, service.ErrDesktopSyncInvalid)
}

func TestDesktopSyncQuotaRollbackReplacementAndPrivateBlobs(t *testing.T) {
	r := newDesktopModelRig(t)
	owner, other := r.user(t, service.StatusActive, false), r.user(t, service.StatusActive, false)
	repo := repository.NewDesktopSyncRepository(repository.GetIntegrationDB())
	svc := service.NewDesktopSyncService(repo)
	data := []byte("private attachment")
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	require.ErrorIs(t, svc.PutBlob(r.ctx, owner.ID, strings.Repeat("0", 64), data), service.ErrDesktopSyncDigest)
	require.NoError(t, svc.PutBlob(r.ctx, owner.ID, hash, data))
	var before int64
	require.NoError(t, repository.GetIntegrationDB().QueryRow("SELECT used_bytes FROM desktop_sync_accounts WHERE user_id=$1", owner.ID).Scan(&before))
	require.NoError(t, svc.PutBlob(r.ctx, owner.ID, hash, data))
	var after int64
	require.NoError(t, repository.GetIntegrationDB().QueryRow("SELECT used_bytes FROM desktop_sync_accounts WHERE user_id=$1", owner.ID).Scan(&after))
	require.Equal(t, before, after)
	_, err := svc.GetBlob(r.ctx, other.ID, hash)
	require.ErrorIs(t, err, service.ErrDesktopSyncNotFound)
	got, err := svc.GetBlob(r.ctx, owner.ID, hash)
	require.NoError(t, err)
	require.Equal(t, data, got)
	// A per-account DB quota permits deterministic small fixtures, and is enforced inside the lock.
	_, err = repository.GetIntegrationDB().Exec("UPDATE desktop_sync_accounts SET quota_bytes=4096 WHERE user_id=$1", owner.ID)
	require.NoError(t, err)
	in := syncInput("a", "0", `{"s":"`+strings.Repeat("x", 1000)+`"}`)
	first, err := svc.Exchange(r.ctx, owner.ID, in)
	require.NoError(t, err)
	replace := syncInput("a", first.Results[0].Record.Revision, `{"s":"small"}`)
	_, err = svc.Exchange(r.ctx, owner.ID, replace)
	require.NoError(t, err)
	require.NoError(t, repository.GetIntegrationDB().QueryRow("SELECT used_bytes FROM desktop_sync_accounts WHERE user_id=$1", owner.ID).Scan(&after))
	require.Less(t, after, int64(2000), "replacing the document frees its old payload; applied receipts are compact")
	batch := syncInput("b", "0", `{"s":"small"}`)
	batch.Operations = append(batch.Operations, syncInput("c", "0", `{"s":"`+strings.Repeat("x", 4096)+`"}`).Operations[0])
	_, err = svc.Exchange(r.ctx, owner.ID, batch)
	require.ErrorIs(t, err, service.ErrDesktopSyncQuota)
	read, err := svc.Exchange(r.ctx, owner.ID, service.DesktopSyncRequest{Version: 1, DeviceID: "read", After: "0", Limit: 50})
	require.NoError(t, err)
	require.Len(t, read.Changes, 1)
	require.Equal(t, "2", read.Cursor)
	// Rejected batches publish no operation receipts either.
	batch.Operations = batch.Operations[:1]
	retried, err := svc.Exchange(r.ctx, owner.ID, batch)
	require.NoError(t, err)
	require.Equal(t, "3", retried.Results[0].Record.Revision)
	require.ErrorIs(t, svc.PutBlob(r.ctx, owner.ID, fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Repeat("b", 4096)))), []byte(strings.Repeat("b", 4096))), service.ErrDesktopSyncQuota)
}

func TestDesktopSyncResponseByteBudgetAndBlobHTTP(t *testing.T) {
	r := newDesktopModelRig(t)
	owner, other := r.user(t, service.StatusActive, false), r.user(t, service.StatusActive, false)
	svc := service.NewDesktopSyncService(repository.NewDesktopSyncRepository(repository.GetIntegrationDB()))
	r.desktopHandler.SetSyncService(svc)
	token, err := r.auth.GenerateToken(r.ctx, owner)
	require.NoError(t, err)
	otherToken, err := r.auth.GenerateToken(r.ctx, other)
	require.NoError(t, err)
	// HTML-like payloads exercise the actual no-escape transport bound.
	payload := `{"text":"` + strings.Repeat("<", 6*1024*1024) + `"}`
	for i := 0; i < 3; i++ {
		_, err = svc.Exchange(r.ctx, owner.ID, syncInput(fmt.Sprint("large", i), "0", payload))
		require.NoError(t, err)
	}
	hidden := r.request(http.MethodPost, "/api/v1/desktop/sync/exchange", `{"version":1,"device_id":"other","after":"0","limit":50,"operations":[]}`, otherToken)
	require.Equal(t, 200, hidden.Code)
	require.Contains(t, hidden.Body.String(), `"changes":[]`)
	first := r.request(http.MethodPost, "/api/v1/desktop/sync/exchange", `{"version":1,"device_id":"b","after":"0","limit":50,"operations":[]}`, token)
	require.Equal(t, 200, first.Code, first.Body.String()[:100])
	require.LessOrEqual(t, first.Body.Len(), 16*1024*1024)
	var envelope struct {
		Data service.DesktopSyncResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &envelope))
	require.Len(t, envelope.Data.Changes, 2)
	require.True(t, envelope.Data.HasMore)
	require.Equal(t, "2", envelope.Data.Cursor)
	second, err := svc.Exchange(r.ctx, owner.ID, service.DesktopSyncRequest{Version: 1, DeviceID: "b", After: "2", Limit: 50})
	require.NoError(t, err)
	require.Len(t, second.Changes, 1)
	require.False(t, second.HasMore)
	require.Equal(t, "3", second.Cursor)
	data := "private-raw-attachment"
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(data)))
	path := "/api/v1/desktop/sync/blobs/" + hash
	require.Equal(t, 401, r.request(http.MethodPut, path, data, "").Code)
	require.Equal(t, 200, r.request(http.MethodPut, path, data, token).Code)
	require.Equal(t, 404, r.request(http.MethodGet, path, "", otherToken).Code)
	downloaded := r.request(http.MethodGet, path, "", token)
	require.Equal(t, 200, downloaded.Code)
	require.Equal(t, data, downloaded.Body.String())
	require.Equal(t, "application/octet-stream", downloaded.Header().Get("Content-Type"))
	require.Equal(t, 400, r.request(http.MethodPut, path, "wrong", token).Code)
	require.Equal(t, 400, r.request(http.MethodPut, path, strings.Repeat("x", 8*1024*1024+1), token).Code)
	// Corrupt storage is never returned as a valid content-addressed blob.
	_, err = repository.GetIntegrationDB().Exec("UPDATE desktop_sync_blobs SET content=$3 WHERE user_id=$1 AND sha256=$2", owner.ID, hash, []byte("bad"))
	require.NoError(t, err)
	require.Equal(t, 400, r.request(http.MethodGet, path, "", token).Code)
}

func TestDesktopSyncMalformedJSONBDoesNotPublishEarlierOperations(t *testing.T) {
	r := newDesktopModelRig(t)
	owner := r.user(t, service.StatusActive, false)
	svc := service.NewDesktopSyncService(repository.NewDesktopSyncRepository(repository.GetIntegrationDB()))
	for _, bad := range []string{`{"bad":"\u0000"}`, `{"bad":1e1000000}`} {
		in := syncInput("first", "0", `{"ok":true}`)
		in.Operations = append(in.Operations, syncInput("bad", "0", bad).Operations[0])
		_, err := svc.Exchange(r.ctx, owner.ID, in)
		require.ErrorIs(t, err, service.ErrDesktopSyncInvalid)
		out, err := svc.Exchange(r.ctx, owner.ID, service.DesktopSyncRequest{Version: 1, DeviceID: "read", After: "0", Limit: 50})
		require.NoError(t, err)
		require.Empty(t, out.Changes)
		require.Equal(t, "0", out.Cursor)
	}
}

func TestDesktopSyncConcurrentDistinctWritesCommitContiguousCursors(t *testing.T) {
	r := newDesktopModelRig(t)
	owner := r.user(t, service.StatusActive, false)
	const writes = 12
	errors := make(chan error, writes)
	var wg sync.WaitGroup
	for i := 0; i < writes; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			svc := service.NewDesktopSyncService(repository.NewDesktopSyncRepository(repository.GetIntegrationDB()))
			_, err := svc.Exchange(context.Background(), owner.ID, syncInput(fmt.Sprint("key", i), "0", `{"ok":true}`))
			errors <- err
		}(i)
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	svc := service.NewDesktopSyncService(repository.NewDesktopSyncRepository(repository.GetIntegrationDB()))
	out, err := svc.Exchange(r.ctx, owner.ID, service.DesktopSyncRequest{Version: 1, DeviceID: "reader", After: "0", Limit: 50})
	require.NoError(t, err)
	require.Equal(t, "12", out.Cursor)
	require.False(t, out.HasMore)
	require.Len(t, out.Changes, writes)
	for i, record := range out.Changes {
		require.Equal(t, fmt.Sprint(i+1), record.Revision)
	}
}
