//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestDesktopSyncRetirementDeletesOnlyCloudData(t *testing.T) {
	// Upgrade a disposable historical database through the production runner.
	// The shared integration database and real installations are never changed.
	db := newAinoHistoricalDatabase(t, func(name string) bool { return name < "256_" })
	ctx := context.Background()
	const device = "00000000-0000-4000-8000-000000000001"
	var userID, groupID, leaseKeyID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO users(email,password_hash,balance)
		VALUES('sync-retirement@example.test','fixture',37.5) RETURNING id`).Scan(&userID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO groups(name,platform,subscription_type)
		VALUES('Retained subscription','openai','subscription') RETURNING id`).Scan(&groupID))
	_, err := db.ExecContext(ctx, `INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at,daily_usage_usd)
		VALUES($1,$2,now(),now()+interval '30 days',1.25)`, userID, groupID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO api_keys(user_id,key,name,desktop_managed,desktop_credential_type,
		desktop_device_id,desktop_key_version,key_hash_type)
		VALUES($1,$2,'Retained device',true,'device',$3,3,'sha256')`, userID, "sha256:"+strings.Repeat("a", 64), device)
	require.NoError(t, err)
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO api_keys(user_id,key,name,group_id,desktop_managed)
		VALUES($1,'sk-retirement-lease-fixture','Retained legacy lease',$2,true) RETURNING id`, userID, groupID).Scan(&leaseKeyID))
	_, err = db.ExecContext(ctx, `INSERT INTO desktop_model_credentials(user_id,device_id,connection_grant_id,
		session_family_id,token_version,group_id,model_id,api_key_id,expires_at)
		VALUES($1,$2,'grant','family',0,$3,'fixture-model',$4,now()+interval '1 hour')`, userID, device, groupID, leaseKeyID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO desktop_device_info(user_id,device_id,installation_name,os,client_type,client_version)
		VALUES($1,$2,'Retained Mac','macos','desktop_dev','0.23.0')`, userID, device)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO payment_orders(user_id,amount,pay_amount,out_trade_no,status,expires_at)
		VALUES($1,20,20,'retained-order','COMPLETED',now()+interval '1 day')`, userID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO auth_identities(user_id,provider_type,provider_key,provider_subject,verified_at)
		VALUES($1,'email','email','sync-retirement@example.test',now())`, userID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES('desktop.default_model_id','fixture-model')
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`)
	require.NoError(t, err)

	// Both live content and tombstones must disappear, together with receipts
	// and binary blobs; keeping an empty account table would retain the feature.
	_, err = db.ExecContext(ctx, `INSERT INTO desktop_sync_accounts(user_id,revision,used_bytes) VALUES($1,2,100)`, userID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO desktop_sync_documents(user_id,key,revision,deleted,payload,device_id,size_bytes)
		VALUES($1,'session/live',1,false,'{"message":"cloud-only"}',$2,50),($1,'session/deleted',2,true,NULL,$2,10)`, userID, device)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO desktop_sync_operations(user_id,operation_id,fingerprint,result,size_bytes)
		VALUES($1,'00000000-0000-4000-8000-000000000002',$2,'{"status":"applied"}',20)`, userID, strings.Repeat("b", 64))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO desktop_sync_blobs(user_id,sha256,content) VALUES($1,$2,$3)`, userID, strings.Repeat("c", 64), []byte("cloud-only attachment"))
	require.NoError(t, err)

	retained := []string{"users", "groups", "user_subscriptions", "api_keys", "desktop_model_credentials", "desktop_device_info", "payment_orders", "auth_identities", "settings", "auth_cache_invalidation_outbox"}
	// Freeze the historical columns so later additive migrations cannot change
	// the snapshot shape. Every original value and row must still be preserved.
	retainedColumns := make(map[string][]string, len(retained))
	for _, table := range retained {
		rows, err := db.QueryContext(ctx, `SELECT column_name FROM information_schema.columns
			WHERE table_schema=current_schema() AND table_name=$1 ORDER BY ordinal_position`, table)
		require.NoError(t, err, table)
		for rows.Next() {
			var column string
			require.NoError(t, rows.Scan(&column), table)
			retainedColumns[table] = append(retainedColumns[table], pq.QuoteIdentifier(column))
		}
		require.NoError(t, rows.Err(), table)
		require.NoError(t, rows.Close(), table)
		require.NotEmpty(t, retainedColumns[table], table)
	}
	snapshot := func() map[string][32]byte {
		t.Helper()
		result := make(map[string][32]byte, len(retained))
		for _, table := range retained {
			var value string
			query := fmt.Sprintf(`SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text
				FROM (SELECT %s FROM %s) t`, strings.Join(retainedColumns[table], ", "), pq.QuoteIdentifier(table))
			require.NoError(t, db.QueryRowContext(ctx, query).Scan(&value), table)
			// Failed assertions must never print stored credential material.
			result[table] = sha256.Sum256([]byte(value))
		}
		return result
	}
	before := snapshot()
	var apiKeyCount int64
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM api_keys").Scan(&apiKeyCount))
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
	for range 2 {
		require.NoError(t, ApplyMigrations(ctx, db))
	}
	// IF EXISTS also makes the cleanup SQL itself safe to apply repeatedly.
	cleanup, err := migrations.FS.ReadFile("256_remove_desktop_cloud_sync.sql")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(cleanup))
	require.NoError(t, err)
	for _, table := range []string{"desktop_sync_accounts", "desktop_sync_documents", "desktop_sync_operations", "desktop_sync_blobs"} {
		var exists bool
		require.NoError(t, db.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists))
		require.False(t, exists, table)
	}
	require.Equal(t, before, snapshot(), "all account, key, metadata, model and billing rows must remain byte-for-byte identical")
	var defaultBillingPolicyCount int64
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM api_keys WHERE billing_policy=''").Scan(&defaultBillingPolicyCount))
	require.Equal(t, apiKeyCount, defaultBillingPolicyCount, "existing API keys must retain the default empty billing policy")
	for name, checksum := range oldChecksums {
		var got string
		require.NoError(t, db.QueryRowContext(ctx, "SELECT checksum FROM schema_migrations WHERE filename=$1", name).Scan(&got))
		require.Equal(t, checksum, got, "historical migration checksum must remain valid: %s", name)
	}
}
