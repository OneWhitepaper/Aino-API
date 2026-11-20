package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDesktopApplicationNormalizationAndOmission(t *testing.T) {
	for input, expected := range map[string]string{"": "", "   ": "", " AiNo ": "aino", "MoMo": "momo", "future_app2": "future_app2", strings.Repeat("a", 64): strings.Repeat("a", 64)} {
		got, err := NormalizeDesktopApp(input)
		require.NoError(t, err)
		require.Equal(t, expected, got)
	}
	for _, input := range []string{"future-app", "1app", "_app", "app_", "app__name", "app name", "应用", strings.Repeat("a", 65), "app\x00"} {
		_, err := NormalizeDesktopApp(input)
		require.Error(t, err, input)
	}
}

func TestDesktopApplicationCompatibilityAndSnapshot(t *testing.T) {
	require.Empty(t, DesktopAppForKey(&APIKey{}))
	require.Equal(t, "aino", DesktopAppForKey(&APIKey{DesktopManaged: true}))
	key := &APIKey{ID: 7, UserID: 9, DesktopManaged: true, DesktopApp: "momo", BillingPolicy: BillingPolicyBalanceOnly, User: &User{ID: 9}}
	require.NoError(t, ValidateDesktopAppReuse("", key), "omission preserves existing app")
	require.NoError(t, ValidateDesktopAppReuse("momo", key))
	require.ErrorIs(t, ValidateDesktopAppReuse("aino", key), ErrDesktopAppConflict)
	svc := &APIKeyService{}
	snapshot := svc.snapshotFromAPIKey(context.Background(), key)
	require.Equal(t, "momo", snapshot.DesktopApp)
	restored := svc.snapshotToAPIKey("fixture-secret", snapshot)
	require.Equal(t, "momo", restored.DesktopApp)
	require.Equal(t, key.BillingPolicy, restored.BillingPolicy)
}
