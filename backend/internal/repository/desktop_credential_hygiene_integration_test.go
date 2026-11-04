//go:build integration

package repository_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent/apikey"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDesktopLeaseRetiresExpiredKeysAcrossScopesAndKeepsAudit(t *testing.T) {
	r := newDesktopCredentialRig(t)
	first := r.issue(t, "fixture-a")
	key, err := r.keys.GetByKey(r.ctx, first.APIKey)
	require.NoError(t, err)
	oldGrant := r.grant
	account, err := r.client.Account.Create().SetName("desktop-retirement-usage").SetPlatform(service.PlatformOpenAI).SetType(service.AccountTypeAPIKey).SetCredentials(map[string]any{}).Save(r.ctx)
	require.NoError(t, err)
	usage, err := r.client.UsageLog.Create().SetUserID(r.userID).SetAPIKeyID(key.ID).SetAccountID(account.ID).SetRequestID(uuid.NewString()).SetModel("fixture-a").Save(r.ctx)
	require.NoError(t, err)
	past := time.Now().Add(-time.Minute)
	_, err = repository.GetIntegrationDB().ExecContext(r.ctx, "UPDATE desktop_model_credentials SET created_at=$1, expires_at=$2 WHERE api_key_id=$3", past.Add(-time.Hour), past, key.ID)
	require.NoError(t, err)
	ordinary, err := r.keys.Create(r.ctx, r.userID, service.CreateAPIKeyRequest{Name: "ordinary-history", GroupID: key.GroupID})
	require.NoError(t, err)
	_, err = r.client.APIKey.UpdateOneID(ordinary.ID).SetExpiresAt(past).Save(r.ctx)
	require.NoError(t, err)
	other := r.user(t, service.StatusActive, false)
	otherKey, err := r.client.APIKey.Create().SetUserID(other.ID).SetKey("sk-other-" + uuid.NewString()).SetName("other desktop").SetDesktopManaged(true).SetStatus(service.StatusActive).SetExpiresAt(past).Save(r.ctx)
	require.NoError(t, err)
	r.grant = uuid.NewString()
	live := r.issue(t, "fixture-a")
	retired, err := r.client.APIKey.Query().Where(apikey.IDEQ(key.ID)).Only(mixins.SkipSoftDelete(r.ctx))
	require.NoError(t, err)
	require.NotNil(t, retired.DeletedAt, "new grants must retire expired managed history")
	require.Equal(t, service.StatusAPIKeyDisabled, retired.Status)
	require.NotEqual(t, first.APIKey, retired.Key, "retirement must remove the old secret")
	lease, err := repository.NewDesktopCredentialRepository(r.client).GetByAPIKeyID(r.ctx, key.ID)
	require.NoError(t, err)
	require.NotNil(t, lease.RevokedAt)
	require.Equal(t, "expired", lease.RevokeReason)
	preserved, err := r.client.UsageLog.Get(r.ctx, usage.ID)
	require.NoError(t, err)
	require.Equal(t, key.ID, preserved.APIKeyID, "billing history must retain its original key identity")
	_, err = r.client.APIKey.Get(r.ctx, ordinary.ID)
	require.NoError(t, err, "ordinary expired keys must remain untouched")
	_, err = r.client.APIKey.Get(r.ctx, otherKey.ID)
	require.NoError(t, err, "another user's history must remain untouched")
	require.Equal(t, live.APIKey, r.issue(t, "fixture-a").APIKey)
	require.Equal(t, http.StatusOK, r.request("GET", "/v1/models", "", live.APIKey).Code)
	r.grant = oldGrant
	replacement := r.issue(t, "fixture-a")
	require.NotEqual(t, first.APIKey, replacement.APIKey, "retired same-scope credentials must rotate and recover")
	require.Equal(t, http.StatusUnauthorized, r.request("GET", "/v1/models", "", first.APIKey).Code)
}

func TestDesktopLeaseKeyListRetiresHistoryAndKeepsFamilyFence(t *testing.T) {
	for _, sortBy := range []string{"created_at", "current_concurrency"} {
		t.Run(sortBy, func(t *testing.T) {
			r := newDesktopCredentialRig(t)
			first := r.issue(t, "fixture-a")
			key, err := r.keys.GetByKey(r.ctx, first.APIKey)
			require.NoError(t, err)
			repo := repository.NewDesktopCredentialRepository(r.client)
			require.NoError(t, repo.RevokeFamily(r.ctx, r.family))
			keys, page, err := r.keys.List(r.ctx, r.userID, pagination.PaginationParams{Page: 1, PageSize: 20, SortBy: sortBy}, service.APIKeyListFilters{})
			require.NoError(t, err)
			require.Empty(t, keys, "opening key management must retire revoked managed history")
			require.Zero(t, page.Total)
			retired, err := r.client.APIKey.Query().Where(apikey.IDEQ(key.ID)).Only(mixins.SkipSoftDelete(r.ctx))
			require.NoError(t, err)
			require.NotNil(t, retired.DeletedAt)
			lease, err := repo.GetByAPIKeyID(r.ctx, key.ID)
			require.NoError(t, err)
			require.Equal(t, "parent_session_revoked", lease.RevokeReason)
			_, err = r.credentials.IssueOrReuseLease(r.ctx, service.DesktopCredentialRequest{UserID: r.userID, DeviceID: r.device, ConnectionGrantID: uuid.NewString(), SessionFamilyID: r.family, ModelID: "fixture-a"})
			require.ErrorIs(t, err, service.ErrDesktopCredentialRevoked, "cleanup must preserve the revoked-family fence")
		})
	}
}

func TestDesktopLeaseKeyManagementFiltersAndEffectiveExpiry(t *testing.T) {
	r := newDesktopCredentialRig(t)
	lease := r.issue(t, "fixture-a")
	managed, err := r.keys.GetByKey(r.ctx, lease.APIKey)
	require.NoError(t, err)
	live, err := r.keys.Create(r.ctx, r.userID, service.CreateAPIKeyRequest{Name: "manual-live", GroupID: managed.GroupID})
	require.NoError(t, err)
	expired, err := r.keys.Create(r.ctx, r.userID, service.CreateAPIKeyRequest{Name: "manual-expired", GroupID: managed.GroupID})
	require.NoError(t, err)
	_, err = r.client.APIKey.UpdateOneID(expired.ID).SetExpiresAt(time.Now().Add(-time.Minute)).Save(r.ctx)
	require.NoError(t, err)
	r.router.GET("/managed-key-list", gin.HandlerFunc(middleware.NewJWTAuthMiddleware(r.auth, r.users, r.settings, nil)), handler.NewAPIKeyHandler(r.keys).List)
	for _, tc := range []struct {
		query string
		ids   []int64
	}{
		{"", []int64{managed.ID, live.ID, expired.ID}},
		{"?desktop_managed=false", []int64{live.ID, expired.ID}},
		{"?desktop_managed=true", []int64{managed.ID}},
		{"?desktop_managed=false&status=expired", []int64{expired.ID}},
		{"?desktop_managed=false&status=active", []int64{live.ID}},
		{"?desktop_managed=false&sort_by=current_concurrency&status=expired", []int64{expired.ID}},
	} {
		w := r.request("GET", "/managed-key-list"+tc.query, "", r.token)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var data struct {
			Data struct {
				Items []struct {
					ID     int64  `json:"id"`
					Status string `json:"status"`
				} `json:"items"`
				Total int64 `json:"total"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &data))
		ids := []int64{}
		for _, item := range data.Data.Items {
			ids = append(ids, item.ID)
			if item.ID == expired.ID {
				require.Equal(t, "expired", item.Status)
			}
		}
		require.ElementsMatch(t, tc.ids, ids, tc.query)
		require.EqualValues(t, len(tc.ids), data.Data.Total, tc.query)
	}
	require.Equal(t, http.StatusBadRequest, r.request("GET", "/managed-key-list?desktop_managed=invalid", "", r.token).Code)
}

func TestDesktopLeaseDevicesDistinguishExpiryAndRevocation(t *testing.T) {
	r := newDesktopCredentialRig(t)
	lease := r.issue(t, "fixture-a")
	key, err := r.keys.GetByKey(r.ctx, lease.APIKey)
	require.NoError(t, err)
	check := func(status string, revoked bool) {
		t.Helper()
		w := r.request("GET", "/api/v1/desktop/devices", "", r.token)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var result struct {
			Data []struct {
				Status  string `json:"status"`
				Revoked bool   `json:"revoked"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		require.Len(t, result.Data, 1)
		require.Equal(t, status, result.Data[0].Status)
		require.Equal(t, revoked, result.Data[0].Revoked)
	}
	check("active", false)
	_, err = repository.GetIntegrationDB().ExecContext(r.ctx, "UPDATE desktop_model_credentials SET created_at=$1, expires_at=$2 WHERE api_key_id=$3", time.Now().Add(-time.Hour), time.Now().Add(-time.Minute), key.ID)
	require.NoError(t, err)
	check("expired", false)
	_, _, err = r.keys.List(r.ctx, r.userID, pagination.DefaultPagination(), service.APIKeyListFilters{})
	require.NoError(t, err)
	check("expired", false)
	r.issue(t, "fixture-a")
	check("active", false)
	require.NoError(t, repository.NewDesktopCredentialRepository(r.client).RevokeFamily(r.ctx, r.family))
	check("revoked", true)
}

func TestDesktopLeaseRetiredExpiryCanBeExplicitlyRevoked(t *testing.T) {
	r := newDesktopCredentialRig(t)
	first := r.issue(t, "fixture-a")
	key, err := r.keys.GetByKey(r.ctx, first.APIKey)
	require.NoError(t, err)
	_, err = repository.GetIntegrationDB().ExecContext(r.ctx, "UPDATE desktop_model_credentials SET created_at=$1, expires_at=$2 WHERE api_key_id=$3", time.Now().Add(-time.Hour), time.Now().Add(-time.Minute), key.ID)
	require.NoError(t, err)
	keys, _, err := r.keys.List(r.ctx, r.userID, pagination.DefaultPagination(), service.APIKeyListFilters{})
	require.NoError(t, err)
	require.Empty(t, keys)
	require.NoError(t, r.credentials.RevokeDevice(r.ctx, r.userID, r.device))
	devices, err := r.credentials.ListDevices(r.ctx, r.userID)
	require.NoError(t, err)
	require.Len(t, devices, 1)
	require.Equal(t, "revoked", devices[0].Status, "explicit revocation must supersede natural expiry")
	require.True(t, devices[0].Revoked)
	repo := repository.NewDesktopCredentialRepository(r.client)
	lease, err := repo.GetByAPIKeyID(r.ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, "parent_session_revoked", lease.RevokeReason)
	lease.ConnectionGrantID = uuid.NewString()
	lease.ExpiresAt = time.Now().Add(time.Hour)
	lease.RevokedAt, lease.RevokeReason = nil, ""
	candidate := &service.APIKey{UserID: r.userID, Key: "sk-fixture-" + uuid.NewString(), Name: "Aino desktop", GroupID: key.GroupID, Status: service.StatusActive, ExpiresAt: &lease.ExpiresAt, DesktopManaged: true}
	_, _, err = repo.IssueOrReuse(r.ctx, lease, candidate, time.Hour)
	require.ErrorIs(t, err, service.ErrDesktopCredentialRevoked, "retired history must retain an explicit family fence")
}

func TestDesktopLeaseDeviceKeepsExpiredFamilyRevocableAfterNewerFamilyRevoked(t *testing.T) {
	r := newDesktopCredentialRig(t)
	olderToken := r.token
	older := r.issue(t, "fixture-a")
	key, err := r.keys.GetByKey(r.ctx, older.APIKey)
	require.NoError(t, err)
	_, err = repository.GetIntegrationDB().ExecContext(r.ctx, "UPDATE desktop_model_credentials SET created_at=$1, expires_at=$2 WHERE api_key_id=$3", time.Now().Add(-time.Hour), time.Now().Add(-time.Minute), key.ID)
	require.NoError(t, err)
	user, err := r.userRepo.GetByID(r.ctx, r.userID)
	require.NoError(t, err)
	pair, err := r.auth.GenerateTokenPair(r.ctx, user, "")
	require.NoError(t, err)
	claims, err := r.auth.ValidateToken(pair.AccessToken)
	require.NoError(t, err)
	r.token, r.grant = pair.AccessToken, uuid.NewString()
	r.issue(t, "fixture-a")
	require.NoError(t, repository.NewDesktopCredentialRepository(r.client).RevokeFamily(r.ctx, claims.SessionID))
	devices, err := r.credentials.ListDevices(r.ctx, r.userID)
	require.NoError(t, err)
	require.Len(t, devices, 1)
	require.Equal(t, "expired", devices[0].Status, "a newer revoked family must not hide another family's renewable authorization")
	require.False(t, devices[0].Revoked)
	r.token, r.grant = olderToken, uuid.NewString()
	recovered := r.issue(t, "fixture-a")
	require.NotEqual(t, older.APIKey, recovered.APIKey)
	require.NoError(t, r.credentials.RevokeDevice(r.ctx, r.userID, r.device))
	devices, err = r.credentials.ListDevices(r.ctx, r.userID)
	require.NoError(t, err)
	require.Equal(t, "revoked", devices[0].Status)
	require.True(t, devices[0].Revoked)
}
