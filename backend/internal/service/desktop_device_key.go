package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
)

const DesktopCredentialTypeDevice = "device"
const desktopDeviceHashPrefix = "sha256:"

var (
	ErrDesktopDeviceKeyNotFound = infraerrors.NotFound("DESKTOP_DEVICE_KEY_NOT_FOUND", "device key not found")
	ErrDesktopDeviceKeyRevoked  = infraerrors.Forbidden("DESKTOP_DEVICE_KEY_REVOKED", "device key has been revoked; explicit reauthorization is required")
	ErrDesktopDeviceKeyVersion  = infraerrors.Conflict("DESKTOP_DEVICE_KEY_VERSION_CONFLICT", "device key changed; reload its current status")
)

// Device identities live on the existing API Key, including after soft deletion.
type DesktopDeviceKeyRepository interface {
	EnsureDeviceKey(context.Context, *APIKey, *int, bool, bool) (*APIKey, bool, error)
	GetDeviceKey(context.Context, int64, string) (*APIKey, error)
	ListDeviceKeys(context.Context, int64) ([]APIKey, error)
	RevokeDeviceKey(context.Context, int64, string) error
}

type DesktopDeviceKeyRequest struct {
	DeviceID        string `json:"device_id"`
	Rotate          bool   `json:"rotate,omitempty"`
	ExpectedVersion *int   `json:"expected_version,omitempty"`
	Reauthorize     bool   `json:"reauthorize,omitempty"`
}

type DesktopDeviceKeyResponse struct {
	KeyID     string     `json:"key_id"`
	DeviceID  string     `json:"device_id"`
	Version   int        `json:"version"`
	Status    string     `json:"status"`
	ExpiresAt *time.Time `json:"expires_at"`
	APIKey    string     `json:"api_key,omitempty"`
}

type DesktopDeviceKeyService struct {
	repo DesktopDeviceKeyRepository
	keys *APIKeyService
}

func NewDesktopDeviceKeyService(repo DesktopDeviceKeyRepository, keys *APIKeyService) *DesktopDeviceKeyService {
	return &DesktopDeviceKeyService{repo: repo, keys: keys}
}

func HashDesktopDeviceSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return desktopDeviceHashPrefix + hex.EncodeToString(sum[:])
}

// Stored hashes already ARE the cache index. Never hash them a second time.
func APIKeyAuthCacheIndex(key, hashType string) string {
	if hashType == "sha256" && strings.HasPrefix(key, desktopDeviceHashPrefix) && len(key) == len(desktopDeviceHashPrefix)+64 {
		return strings.TrimPrefix(key, desktopDeviceHashPrefix)
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func validateDesktopDeviceID(deviceID string) error {
	id, err := uuid.Parse(deviceID)
	if err != nil || id == uuid.Nil || id.String() != deviceID {
		return infraerrors.BadRequest("INVALID_DEVICE", "device ID must be a canonical UUID")
	}
	return nil
}

func deviceKeyResponse(key *APIKey) *DesktopDeviceKeyResponse {
	status := "active"
	if !key.IsActive() || key.DeletedAt != nil {
		status = "revoked"
	}
	return &DesktopDeviceKeyResponse{KeyID: strconv.FormatInt(key.ID, 10), DeviceID: key.DesktopDeviceID, Version: key.DesktopKeyVersion, Status: status}
}

func (s *DesktopDeviceKeyService) Ensure(ctx context.Context, userID int64, req DesktopDeviceKeyRequest) (*DesktopDeviceKeyResponse, error) {
	if err := validateDesktopDeviceID(req.DeviceID); err != nil {
		return nil, err
	}
	if (req.Rotate || req.Reauthorize) && (req.ExpectedVersion == nil || *req.ExpectedVersion < 1) || req.Rotate && req.Reauthorize {
		return nil, infraerrors.BadRequest("INVALID_REQUEST", "rotation or reauthorization requires the current version and cannot be combined")
	}
	user, err := s.keys.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !user.IsActive() {
		return nil, ErrUserNotActive
	}
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return nil, err
	}
	secret := "sk-aino-device-" + base64.RawURLEncoding.EncodeToString(secretBytes)
	candidate := &APIKey{UserID: userID, Key: HashDesktopDeviceSecret(secret), Name: "Aino desktop", Status: StatusActive, DesktopManaged: true, DesktopCredentialType: DesktopCredentialTypeDevice, DesktopDeviceID: req.DeviceID, DesktopKeyVersion: 1, KeyHashType: "sha256"}
	var previous *APIKey
	if req.Rotate || req.Reauthorize {
		previous, err = s.repo.GetDeviceKey(ctx, userID, req.DeviceID)
		if err != nil {
			return nil, err
		}
	}
	key, changed, err := s.repo.EnsureDeviceKey(ctx, candidate, req.ExpectedVersion, req.Rotate, req.Reauthorize)
	if err != nil {
		return nil, err
	}
	result := deviceKeyResponse(key)
	if changed {
		if previous != nil {
			s.keys.InvalidateAuthCacheByStoredKey(ctx, previous.Key, previous.KeyHashType)
		}
		s.keys.InvalidateAuthCacheByStoredKey(ctx, key.Key, key.KeyHashType)
		result.APIKey = secret
	}
	return result, nil
}

func (s *DesktopDeviceKeyService) Get(ctx context.Context, userID int64, deviceID string) (*DesktopDeviceKeyResponse, error) {
	if err := validateDesktopDeviceID(deviceID); err != nil {
		return nil, err
	}
	key, err := s.repo.GetDeviceKey(ctx, userID, deviceID)
	if err != nil {
		return nil, err
	}
	return deviceKeyResponse(key), nil
}

func (s *DesktopDeviceKeyService) Revoke(ctx context.Context, userID int64, deviceID string) error {
	key, err := s.repo.GetDeviceKey(ctx, userID, deviceID)
	if err != nil {
		return err
	}
	if err := s.repo.RevokeDeviceKey(ctx, userID, deviceID); err != nil {
		return err
	}
	s.keys.InvalidateAuthCacheByStoredKey(ctx, key.Key, key.KeyHashType)
	return nil
}

func (s *DesktopDeviceKeyService) MergeDevices(ctx context.Context, userID int64, devices []DesktopDevice) ([]DesktopDevice, error) {
	keys, err := s.repo.ListDeviceKeys(ctx, userID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]int, len(devices))
	for i, device := range devices {
		byID[device.DeviceID] = i
	}
	for _, key := range keys {
		state := deviceKeyResponse(&key)
		lastUsed := key.UpdatedAt
		if key.LastUsedAt != nil {
			lastUsed = *key.LastUsedAt
		}
		device := DesktopDevice{DeviceID: key.DesktopDeviceID, LastUsedAt: lastUsed, Status: state.Status, Revoked: state.Status != "active"}
		if i, ok := byID[device.DeviceID]; ok {
			devices[i] = device
		} else {
			devices = append(devices, device)
		}
	}
	return devices, nil
}
