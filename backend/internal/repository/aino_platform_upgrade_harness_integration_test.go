//go:build integration

package repository

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// NewAinoPrePlatformDatabase is test-only: it builds the actual pre-phone
// schema in a separate disposable database rather than undoing current tables.
func NewAinoPrePlatformDatabase(t *testing.T) *sql.DB {
	return newAinoHistoricalDatabase(t, func(name string) bool { return name < "239_" })
}

func newAinoHistoricalDatabase(t *testing.T, include func(string) bool) *sql.DB {
	t.Helper()
	ctx := context.Background()
	container, err := tcpostgres.Run(ctx, postgresImageTag, tcpostgres.WithDatabase("fixture_upgrade"), tcpostgres.WithUsername("fixture"), tcpostgres.WithPassword("fixture"), tcpostgres.BasicWaitStrategies())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, container.Terminate(ctx)) })
	dsn, err := container.ConnectionString(ctx, "sslmode=disable", "TimeZone=UTC")
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	entries, err := migrations.FS.ReadDir(".")
	require.NoError(t, err)
	before := fstest.MapFS{}
	for _, entry := range entries {
		if !include(entry.Name()) || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		data, err := migrations.FS.ReadFile(entry.Name())
		require.NoError(t, err)
		before[entry.Name()] = &fstest.MapFile{Data: data}
	}
	require.NoError(t, applyMigrationsFS(ctx, db, before))
	return db
}

func TestAinoV028UpgradePreservesManagedCredentialsAndClientOrders(t *testing.T) {
	upstreamAdditions := map[string]bool{
		"241_add_payment_order_bonus_amount.sql":        true,
		"241_add_typesafe_platform.sql":                 true,
		"242_drop_platform_check_constraints.sql":       true,
		"242_group_i18n.sql":                            true,
		"243_gemini_cached_contents.sql":                true,
		"244_gemini_cached_contents_channel_usage.sql":  true,
		"245_gemini_cached_contents_mutation_lease.sql": true,
		"246_enable_gemini_image_generation_groups.sql": true,
	}
	db := newAinoHistoricalDatabase(t, func(name string) bool { return !upstreamAdditions[name] })
	ctx := context.Background()
	var userID, groupID, keyID int64
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO users(email,password_hash,balance) VALUES('upgrade@example.test','fixture',37.5) RETURNING id").Scan(&userID))
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO groups(name,platform) VALUES('Aino upgrade','openai') RETURNING id").Scan(&groupID))
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO api_keys(user_id,key,name,group_id,desktop_managed) VALUES($1,'sk-upgrade-fixture','Aino desktop',$2,true) RETURNING id", userID, groupID).Scan(&keyID))
	_, err := db.ExecContext(ctx, "INSERT INTO auth_identities(user_id,provider_type,provider_key,provider_subject,verified_at) VALUES($1,'phone','phone','+8613900000099',now())", userID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "INSERT INTO desktop_model_credentials(user_id,device_id,connection_grant_id,session_family_id,token_version,group_id,model_id,api_key_id,expires_at,revoked_at,revoke_reason) VALUES($1,'device','grant','family',0,$2,'aino-fixture',$3,now()+interval '1 hour',now(),'fixture_revoked')", userID, groupID, keyID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "INSERT INTO payment_orders(user_id,amount,pay_amount,out_trade_no,status,expires_at,client_order_id,creation_state) VALUES($1,20,20,'aino-upgrade-order','COMPLETED',now()+interval '1 day','00000000-0000-4000-8000-000000000001','complete')", userID)
	require.NoError(t, err)

	oldChecksums := map[string]string{}
	rows, err := db.QueryContext(ctx, "SELECT filename,checksum FROM schema_migrations")
	require.NoError(t, err)
	for rows.Next() {
		var name, checksum string
		require.NoError(t, rows.Scan(&name, &checksum))
		oldChecksums[name] = checksum
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	require.Contains(t, oldChecksums, "244_payment_client_order_id.sql")
	for range 2 {
		require.NoError(t, ApplyMigrations(ctx, db))
	}
	for name, checksum := range oldChecksums {
		var got string
		require.NoError(t, db.QueryRowContext(ctx, "SELECT checksum FROM schema_migrations WHERE filename=$1", name).Scan(&got))
		require.Equal(t, checksum, got, name)
	}
	for name := range upstreamAdditions {
		var count int
		require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE filename=$1", name).Scan(&count))
		require.Equal(t, 1, count, name)
	}
	var balance, phone, model, reason, status, state, i18n string
	var managed, revoked bool
	var bonus float64
	require.NoError(t, db.QueryRowContext(ctx, "SELECT balance::text FROM users WHERE id=$1", userID).Scan(&balance))
	require.Equal(t, "37.50000000", balance)
	require.NoError(t, db.QueryRowContext(ctx, "SELECT provider_subject FROM auth_identities WHERE user_id=$1 AND provider_type='phone'", userID).Scan(&phone))
	require.Equal(t, "+8613900000099", phone)
	require.NoError(t, db.QueryRowContext(ctx, "SELECT k.desktop_managed,c.model_id,c.revoked_at IS NOT NULL,c.revoke_reason FROM api_keys k JOIN desktop_model_credentials c ON c.api_key_id=k.id WHERE k.id=$1", keyID).Scan(&managed, &model, &revoked, &reason))
	require.True(t, managed)
	require.Equal(t, "aino-fixture", model)
	require.True(t, revoked)
	require.Equal(t, "fixture_revoked", reason)
	require.NoError(t, db.QueryRowContext(ctx, "SELECT status,creation_state,bonus_amount FROM payment_orders WHERE client_order_id='00000000-0000-4000-8000-000000000001'").Scan(&status, &state, &bonus))
	require.Equal(t, "COMPLETED", status)
	require.Equal(t, "complete", state)
	require.Zero(t, bonus)
	require.NoError(t, db.QueryRowContext(ctx, "SELECT i18n::text FROM groups WHERE id=$1", groupID).Scan(&i18n))
	require.Equal(t, "{}", i18n)
}
