//go:build integration

package repository

import (
	"context"
	"testing"

	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestMigration263OmniFlowBranding(t *testing.T) {
	migration, err := dbmigrations.FS.ReadFile("263_omniflow_branding.sql")
	require.NoError(t, err)

	for _, tc := range []struct {
		name         string
		siteName     string
		subtitle     string
		wantName     string
		wantSubtitle string
		wantLogo     string
	}{
		{"aino", "Aino", "Aino Subscription to API Conversion Platform", "OmniFlow API", "AI Model Gateway", "/omniflow-logo.png"},
		{"aino_api", "Aino API", "Subscription to API Conversion Platform", "OmniFlow API", "AI Model Gateway", "/omniflow-logo.png"},
		{"aino_hyphen", "AINO-API", "", "OmniFlow API", "AI Model Gateway", "/omniflow-logo.png"},
		{"upstream_default", "Sub2API", "Subscription to API Conversion Platform", "OmniFlow API", "AI Model Gateway", "/omniflow-logo.png"},
		{"trimmed_case", " aInO ", " Aino Subscription to API Conversion Platform ", "OmniFlow API", "AI Model Gateway", "/omniflow-logo.png"},
		{"custom_subtitle", "Aino", "My model gateway", "OmniFlow API", "My model gateway", "/omniflow-logo.png"},
		{"custom_brand", "My Gateway", "Aino Subscription to API Conversion Platform", "My Gateway", "Aino Subscription to API Conversion Platform", "data:image/png;base64,old-logo"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := testTx(t)
			ctx := context.Background()
			_, err := tx.ExecContext(ctx, `
INSERT INTO settings (key, value) VALUES
    ('site_name', $1), ('site_logo', 'data:image/png;base64,old-logo'), ('site_subtitle', $2)
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, tc.siteName, tc.subtitle)
			require.NoError(t, err)

			for range 2 {
				_, err = tx.ExecContext(ctx, string(migration))
				require.NoError(t, err)
				for key, want := range map[string]string{
					"site_name": tc.wantName, "site_logo": tc.wantLogo, "site_subtitle": tc.wantSubtitle,
				} {
					var got string
					require.NoError(t, tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = $1`, key).Scan(&got))
					require.Equal(t, want, got, key)
				}
			}

			// A logo customized after the upgrade must not be overwritten on replay.
			_, err = tx.ExecContext(ctx, `UPDATE settings SET value = 'https://example.com/new-logo.png' WHERE key = 'site_logo'`)
			require.NoError(t, err)
			_, err = tx.ExecContext(ctx, string(migration))
			require.NoError(t, err)
			var logo string
			require.NoError(t, tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'site_logo'`).Scan(&logo))
			require.Equal(t, "https://example.com/new-logo.png", logo)
		})
	}
}
