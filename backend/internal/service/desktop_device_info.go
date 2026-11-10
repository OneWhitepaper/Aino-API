package service

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var ErrDesktopDeviceInfoInvalid = infraerrors.BadRequest("INVALID_DEVICE_INFO", "device information must contain a valid installation name, operating system, client type and version")

// Device info is untrusted display metadata. It never grants authorization or
// changes the permanent identity, model selection, subscription or billing.
type DesktopDeviceInfo struct {
	InstallationName string `json:"installation_name"`
	OS               string `json:"os"`
	ClientType       string `json:"client_type"`
	ClientVersion    string `json:"client_version"`
}

type DesktopDeviceInfoRepository interface {
	UpsertDeviceInfo(context.Context, int64, string, DesktopDeviceInfo) (*DesktopDeviceInfo, error)
	ListDeviceInfo(context.Context, int64) (map[string]DesktopDeviceInfo, error)
}

func validDeviceDisplayText(value string, limit int) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > limit || strings.TrimSpace(value) == "" {
		return false
	}
	for _, char := range value {
		if unicode.IsControl(char) || unicode.In(char, unicode.Cf) {
			return false
		}
	}
	return true
}

func (s *DesktopCredentialService) UpdateDeviceInfo(ctx context.Context, userID int64, deviceID string, info DesktopDeviceInfo) (*DesktopDeviceInfo, error) {
	if err := validateDesktopDeviceID(deviceID); err != nil {
		return nil, err
	}
	if !validDeviceDisplayText(info.InstallationName, 80) || !validDeviceDisplayText(info.ClientVersion, 64) {
		return nil, ErrDesktopDeviceInfoInvalid
	}
	if info.OS != "macos" && info.OS != "windows" && info.OS != "linux" {
		return nil, ErrDesktopDeviceInfoInvalid
	}
	if info.ClientType != "desktop" && info.ClientType != "desktop_dev" && info.ClientType != "browser" {
		return nil, ErrDesktopDeviceInfoInvalid
	}
	if s == nil || userID <= 0 {
		return nil, ErrServiceUnavailable
	}
	repo, ok := s.repo.(DesktopDeviceInfoRepository)
	if !ok {
		return nil, ErrServiceUnavailable
	}
	info.InstallationName = strings.TrimSpace(info.InstallationName)
	info.ClientVersion = strings.TrimSpace(info.ClientVersion)
	return repo.UpsertDeviceInfo(ctx, userID, deviceID, info)
}

func (s *DesktopCredentialService) AttachDeviceInfo(ctx context.Context, userID int64, devices []DesktopDevice) ([]DesktopDevice, error) {
	repo, ok := s.repo.(DesktopDeviceInfoRepository)
	if !ok || len(devices) == 0 {
		return devices, nil
	}
	metadata, err := repo.ListDeviceInfo(ctx, userID)
	if err != nil {
		return nil, err
	}
	result := append([]DesktopDevice(nil), devices...)
	for i := range result {
		result[i].DeviceInfo = nil
		if info, exists := metadata[result[i].DeviceID]; exists {
			result[i].DeviceInfo = &info
		}
	}
	return result, nil
}
