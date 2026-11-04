package config

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// Deployment policy only controls issuance; existing device identities remain
// usable and revocable when an operator closes the rollout.
type DesktopDeviceKeysConfig struct {
	Enabled          bool                           `mapstructure:"enabled" json:"enabled"`
	RolloutAllowlist []DesktopDeviceKeyRolloutEntry `mapstructure:"rollout_allowlist" json:"rollout_allowlist"`
}

type DesktopDeviceKeyRolloutEntry struct {
	UserID   int64  `mapstructure:"user_id" json:"user_id"`
	DeviceID string `mapstructure:"device_id" json:"device_id"`
}

func (c DesktopDeviceKeysConfig) Validate() error {
	seen := make(map[DesktopDeviceKeyRolloutEntry]struct{}, len(c.RolloutAllowlist))
	for i, entry := range c.RolloutAllowlist {
		id, err := uuid.Parse(entry.DeviceID)
		if entry.UserID <= 0 || err != nil || id == uuid.Nil || id.String() != entry.DeviceID {
			return fmt.Errorf("desktop_device_keys.rollout_allowlist entry %d requires a positive user_id and canonical nonzero device UUID", i+1)
		}
		if _, exists := seen[entry]; exists {
			return fmt.Errorf("desktop_device_keys.rollout_allowlist entry %d duplicates a user/device pair", i+1)
		}
		seen[entry] = struct{}{}
	}
	return nil
}

func (c DesktopDeviceKeysConfig) AllowsMutation(userID int64, deviceID string) bool {
	if !c.Enabled {
		return false
	}
	if len(c.RolloutAllowlist) == 0 {
		return true
	}
	for _, entry := range c.RolloutAllowlist {
		if entry.UserID == userID && entry.DeviceID == deviceID {
			return true
		}
	}
	return false
}

func loadDesktopDeviceKeysConfig(raw any) (DesktopDeviceKeysConfig, error) {
	var policy DesktopDeviceKeysConfig
	if raw == nil {
		return policy, nil
	}
	if err := validateDesktopDeviceKeyRawIDs(raw); err != nil {
		return policy, err
	}
	// Viper's weak coercion can turn a fractional ID into another account ID,
	// or ignore a misspelled allowlist and accidentally open the whole rollout.
	encoded, err := json.Marshal(raw)
	if err != nil {
		return policy, fmt.Errorf("desktop_device_keys contains invalid configuration")
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return policy, fmt.Errorf("desktop_device_keys requires a boolean enabled and a valid rollout_allowlist")
	}
	return policy, nil
}

func validateDesktopDeviceKeyRawIDs(raw any) error {
	section, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("desktop_device_keys must be a configuration object")
	}
	if section["rollout_allowlist"] == nil {
		return nil
	}
	entries, ok := section["rollout_allowlist"].([]any)
	if !ok {
		return fmt.Errorf("desktop_device_keys.rollout_allowlist must be a list")
	}
	for i, rawEntry := range entries {
		entry, ok := rawEntry.(map[string]any)
		if !ok {
			return fmt.Errorf("desktop_device_keys.rollout_allowlist entry %d must be an object", i+1)
		}
		// Even an integral float may already have lost source precision in YAML.
		// Inspect the original type before JSON can turn that value into an ID.
		switch entry["user_id"].(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		default:
			return fmt.Errorf("desktop_device_keys.rollout_allowlist entry %d user_id must be an integer", i+1)
		}
	}
	return nil
}
