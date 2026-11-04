package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadDesktopDeviceKeyPolicyRejectsMalformedConfiguration(t *testing.T) {
	for name, policy := range map[string]string{
		"null enabled":            "  enabled: null\n  rollout_allowlist: []\n",
		"null allowlist":          "  enabled: true\n  rollout_allowlist: null\n",
		"unknown field":           "  enabled: true\n  rollout_allowlisst: []\n",
		"fractional user":         "  enabled: true\n  rollout_allowlist:\n    - user_id: 7.5\n      device_id: 00000000-0000-4000-8000-000000000001\n",
		"rounded fractional user": "  enabled: true\n  rollout_allowlist:\n    - user_id: 7.0000000000000001\n      device_id: 00000000-0000-4000-8000-000000000001\n",
		"decimal notation user":   "  enabled: true\n  rollout_allowlist:\n    - user_id: 7.0\n      device_id: 00000000-0000-4000-8000-000000000001\n",
		"exponent notation user":  "  enabled: true\n  rollout_allowlist:\n    - user_id: 7e0\n      device_id: 00000000-0000-4000-8000-000000000001\n",
		"nonpositive user":        "  enabled: false\n  rollout_allowlist:\n    - user_id: 0\n      device_id: 00000000-0000-4000-8000-000000000001\n",
		"noncanonical device":     "  enabled: false\n  rollout_allowlist:\n    - user_id: 7\n      device_id: 00000000000040008000000000000001\n",
		"nil device":              "  enabled: false\n  rollout_allowlist:\n    - user_id: 7\n      device_id: 00000000-0000-0000-0000-000000000000\n",
		"unknown entry field":     "  enabled: true\n  rollout_allowlist:\n    - user_id: 7\n      device_id: 00000000-0000-4000-8000-000000000001\n      other: true\n",
		"duplicate exact pair":    "  enabled: false\n  rollout_allowlist:\n    - user_id: 7\n      device_id: 00000000-0000-4000-8000-000000000001\n    - user_id: 7\n      device_id: 00000000-0000-4000-8000-000000000001\n",
		"wrong allowlist shape":   "  enabled: true\n  rollout_allowlist: allow-everyone\n",
	} {
		t.Run(name, func(t *testing.T) {
			resetViperWithJWTSecret(t)
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte("desktop_device_keys:\n"+policy), 0600))
			t.Setenv("CONFIG_FILE", path)
			_, err := Load()
			require.ErrorContains(t, err, "desktop_device_keys")
		})
	}
}

func TestDesktopDeviceKeyPolicyRequiresExplicitEnableAndExactPair(t *testing.T) {
	resetViperWithJWTSecret(t)
	cfg, err := Load()
	require.NoError(t, err)
	require.False(t, cfg.DesktopDeviceKeys.AllowsMutation(7, "00000000-0000-4000-8000-000000000001"))
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("desktop_device_keys:\n  enabled: true\n  rollout_allowlist:\n    - user_id: 7\n      device_id: 00000000-0000-4000-8000-000000000001\n    - user_id: 8\n      device_id: 00000000-0000-4000-8000-000000000002\n"), 0600))
	t.Setenv("CONFIG_FILE", path)
	cfg, err = Load()
	require.NoError(t, err)
	require.True(t, cfg.DesktopDeviceKeys.AllowsMutation(7, "00000000-0000-4000-8000-000000000001"))
	require.True(t, cfg.DesktopDeviceKeys.AllowsMutation(8, "00000000-0000-4000-8000-000000000002"))
	require.False(t, cfg.DesktopDeviceKeys.AllowsMutation(7, "00000000-0000-4000-8000-000000000002"))
	require.False(t, cfg.DesktopDeviceKeys.AllowsMutation(8, "00000000-0000-4000-8000-000000000001"))
	cfg.DesktopDeviceKeys.Enabled = false
	require.False(t, cfg.DesktopDeviceKeys.AllowsMutation(7, "00000000-0000-4000-8000-000000000001"))
	cfg.DesktopDeviceKeys.Enabled = true
	cfg.DesktopDeviceKeys.RolloutAllowlist = nil
	require.True(t, cfg.DesktopDeviceKeys.AllowsMutation(7, "00000000-0000-4000-8000-000000000001"))
}

func TestDesktopDeviceKeyRolloutOnlyReadsDeploymentConfiguration(t *testing.T) {
	resetViperWithJWTSecret(t)
	t.Setenv("DESKTOP_DEVICE_KEYS_ENABLED", "true")
	cfg, err := Load()
	require.NoError(t, err)
	require.False(t, cfg.DesktopDeviceKeys.Enabled, "the deployment-only gate must not acquire a new environment override")
}

func TestDesktopDeviceKeyPolicyAcceptsOmittedOptionalFields(t *testing.T) {
	for _, policy := range []string{"{}", "\n  enabled: false\n", "\n  rollout_allowlist: []\n"} {
		t.Run(policy, func(t *testing.T) {
			resetViperWithJWTSecret(t)
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte("desktop_device_keys: "+policy), 0600))
			t.Setenv("CONFIG_FILE", path)
			cfg, err := Load()
			require.NoError(t, err)
			require.False(t, cfg.DesktopDeviceKeys.Enabled)
		})
	}
}
