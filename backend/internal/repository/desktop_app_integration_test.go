//go:build integration

package repository_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDesktopApplicationConcurrentIssuanceCannotRelabelAnInstallation(t *testing.T) {
	r := newDesktopCredentialRig(t)
	devices := service.NewDesktopDeviceKeyService(repository.NewDesktopDeviceKeyRepository(r.client), r.keys)
	deviceID := uuid.NewString()
	type outcome struct {
		app string
		key *service.DesktopDeviceKeyResponse
		err error
	}
	results := make(chan outcome, 4)
	var wg sync.WaitGroup
	for _, app := range []string{"aino", "momo", "aino", "momo"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key, err := devices.Ensure(r.ctx, r.userID, service.DesktopDeviceKeyRequest{DeviceID: deviceID, DesktopApp: app})
			results <- outcome{app, key, err}
		}()
	}
	wg.Wait()
	close(results)
	winner, keyID, successes, conflicts, secrets := "", "", 0, 0, 0
	for result := range results {
		if result.err != nil {
			require.ErrorIs(t, result.err, service.ErrDesktopAppConflict)
			conflicts++
			continue
		}
		if winner == "" {
			winner, keyID = result.app, result.key.KeyID
		}
		require.Equal(t, winner, result.key.DesktopApp)
		require.Equal(t, keyID, result.key.KeyID)
		successes++
		if result.key.APIKey != "" {
			secrets++
		}
	}
	require.Equal(t, 2, successes)
	require.Equal(t, 2, conflicts)
	require.Equal(t, 1, secrets)
}

func TestDesktopApplicationDeviceAndLeaseIdentityIsStable(t *testing.T) {
	r := newDesktopCredentialRig(t)
	deviceRepo := repository.NewDesktopDeviceKeyRepository(r.client)
	devices := service.NewDesktopDeviceKeyService(deviceRepo, r.keys)
	deviceID := uuid.NewString()
	issued, err := devices.Ensure(r.ctx, r.userID, service.DesktopDeviceKeyRequest{DeviceID: deviceID, DesktopApp: " MoMo ", BillingPolicy: service.BillingPolicyBalanceOnly})
	require.NoError(t, err)
	require.Equal(t, "momo", issued.DesktopApp)
	stored, err := deviceRepo.GetDeviceKey(r.ctx, r.userID, deviceID)
	require.NoError(t, err)
	require.Equal(t, "MoMo desktop", stored.Name)
	_, err = devices.Ensure(r.ctx, r.userID, service.DesktopDeviceKeyRequest{DeviceID: deviceID, DesktopApp: "aino"})
	require.ErrorIs(t, err, service.ErrDesktopAppConflict)
	rotated, err := devices.Ensure(r.ctx, r.userID, service.DesktopDeviceKeyRequest{DeviceID: deviceID, Rotate: true, ExpectedVersion: &issued.Version})
	require.NoError(t, err)
	require.Equal(t, issued.KeyID, rotated.KeyID)
	require.Equal(t, "momo", rotated.DesktopApp)
	require.Equal(t, service.BillingPolicyBalanceOnly, rotated.BillingPolicy)
	require.NoError(t, devices.Revoke(r.ctx, r.userID, deviceID))
	state, err := devices.Get(r.ctx, r.userID, deviceID)
	require.NoError(t, err)
	reauthorized, err := devices.Ensure(r.ctx, r.userID, service.DesktopDeviceKeyRequest{DeviceID: deviceID, Reauthorize: true, ExpectedVersion: &state.Version})
	require.NoError(t, err)
	require.Equal(t, "momo", reauthorized.DesktopApp)
	legacy, err := devices.Ensure(r.ctx, r.userID, service.DesktopDeviceKeyRequest{DeviceID: uuid.NewString()})
	require.NoError(t, err)
	require.Equal(t, "aino", legacy.DesktopApp)

	req := service.DesktopCredentialRequest{UserID: r.userID, DeviceID: r.device, ConnectionGrantID: r.grant, SessionFamilyID: r.family, ModelID: "fixture-a", DesktopApp: "momo"}
	body, err := json.Marshal(map[string]string{"device_id": r.device, "connection_grant_id": r.grant, "model_id": "fixture-a", "desktop_app": " MoMo "})
	require.NoError(t, err)
	w := r.request(http.MethodPost, "/api/v1/desktop/credentials", string(body), r.token)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var provisioned struct {
		Data service.DesktopCredentialResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &provisioned))
	lease := &provisioned.Data
	require.Equal(t, "momo", lease.DesktopApp)
	key, err := r.keys.GetByKey(r.ctx, lease.APIKey)
	require.NoError(t, err)
	require.Equal(t, "momo", key.DesktopApp)
	req.DesktopApp = ""
	reused, err := r.credentials.IssueOrReuseLease(r.ctx, req)
	require.NoError(t, err)
	require.Equal(t, lease.CredentialID, reused.CredentialID)
	require.Equal(t, "momo", reused.DesktopApp)
	req.DesktopApp = "aino"
	_, err = r.credentials.IssueOrReuseLease(r.ctx, req)
	require.ErrorIs(t, err, service.ErrDesktopAppConflict)
	req.ConnectionGrantID = uuid.NewString()
	_, err = r.credentials.IssueOrReuseLease(r.ctx, req)
	require.ErrorIs(t, err, service.ErrDesktopAppConflict, "another grant cannot relabel the installation")
	_, err = devices.Ensure(r.ctx, r.userID, service.DesktopDeviceKeyRequest{DeviceID: r.device, DesktopApp: "aino"})
	require.ErrorIs(t, err, service.ErrDesktopAppConflict, "permanent and temporary credentials share application provenance")
	_, err = repository.GetIntegrationDB().ExecContext(r.ctx, "UPDATE desktop_model_credentials SET created_at=$1,expires_at=$2 WHERE api_key_id=$3", time.Now().Add(-time.Hour), time.Now().Add(-time.Minute), key.ID)
	require.NoError(t, err)
	req.DesktopApp, req.ConnectionGrantID = "", r.grant
	renewed, err := r.credentials.IssueOrReuseLease(r.ctx, req)
	require.NoError(t, err)
	require.NotEqual(t, lease.CredentialID, renewed.CredentialID)
	require.Equal(t, "momo", renewed.DesktopApp)
	metadata, err := r.keys.GetUsageMetadata(r.ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, "momo", metadata.DesktopApp, "retirement preserves attribution")

	r.router.GET("/app-keys", gin.HandlerFunc(middleware.NewJWTAuthMiddleware(r.auth, r.users, r.settings, nil)), handler.NewAPIKeyHandler(r.keys).List)
	w = r.request(http.MethodGet, "/app-keys?desktop_managed=true&desktop_app=momo&include_archived=true&page_size=1", "", r.token)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var envelope struct {
		Data struct {
			Items []struct {
				DesktopApp string `json:"desktop_app"`
				Key        string `json:"key"`
			} `json:"items"`
			Total int `json:"total"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.Equal(t, 3, envelope.Data.Total)
	require.Len(t, envelope.Data.Items, 1)
	require.Equal(t, "momo", envelope.Data.Items[0].DesktopApp)
	require.Empty(t, envelope.Data.Items[0].Key)
	for _, filter := range []string{"desktop_app=bad-app", "desktop_managed=false&desktop_app=momo"} {
		require.Equal(t, http.StatusBadRequest, r.request(http.MethodGet, "/app-keys?"+filter, "", r.token).Code)
	}
}

func TestDesktopApplicationMigrationBackfillAndImmutability(t *testing.T) {
	ctx := t.Context()
	tx, err := repository.GetIntegrationDB().BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	schema := "app_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = tx.ExecContext(ctx, fmt.Sprintf(`CREATE SCHEMA %s; SET LOCAL search_path TO %s, public;
CREATE TABLE api_keys(id BIGSERIAL PRIMARY KEY,user_id BIGINT,desktop_managed BOOLEAN NOT NULL DEFAULT FALSE,deleted_at TIMESTAMPTZ,quota NUMERIC NOT NULL DEFAULT 0);
INSERT INTO api_keys(user_id,desktop_managed,quota) VALUES (1,true,12),(1,false,34);
INSERT INTO api_keys(user_id,desktop_managed,deleted_at,quota) VALUES (1,true,NOW(),56);`, schema, schema))
	require.NoError(t, err)
	content, err := migrations.FS.ReadFile("264_desktop_app_classification.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(content))
	require.NoError(t, err)
	var apps string
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT string_agg(desktop_app,',' ORDER BY id) FROM api_keys").Scan(&apps))
	require.Equal(t, "aino,,aino", apps)
	var quota int
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT SUM(quota)::int FROM api_keys").Scan(&quota))
	require.Equal(t, 102, quota)
	var app string
	require.NoError(t, tx.QueryRowContext(ctx, "INSERT INTO api_keys(user_id,desktop_managed) VALUES(1,true) RETURNING desktop_app").Scan(&app))
	require.Equal(t, "aino", app, "old binary omitting the column remains compatible")
	require.NoError(t, tx.QueryRowContext(ctx, "INSERT INTO api_keys(user_id,desktop_managed,desktop_app) VALUES(1,true,'future_app') RETURNING desktop_app").Scan(&app))
	require.Equal(t, "future_app", app)
	for _, invalid := range []string{"UPDATE api_keys SET desktop_app='momo' WHERE id=1", "INSERT INTO api_keys(user_id,desktop_managed,desktop_app) VALUES(1,true,'bad-app')", "INSERT INTO api_keys(user_id,desktop_managed,desktop_app) VALUES(1,false,'momo')"} {
		_, err = tx.ExecContext(ctx, "SAVEPOINT app_shape")
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, invalid)
		require.Error(t, err)
		_, err = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT app_shape")
		require.NoError(t, err)
	}
	_, err = tx.ExecContext(ctx, "UPDATE api_keys SET quota=quota+1 WHERE id=1")
	require.NoError(t, err, "classification must not block existing quota updates")
}
