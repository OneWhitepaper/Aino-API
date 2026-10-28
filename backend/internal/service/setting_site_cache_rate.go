package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
)

const SettingKeySiteCacheRateVisibleToUsers = "usage_site_cache_rate_visible_to_users"

func (s *SettingService) GetSiteCacheRateVisibility(ctx context.Context) (bool, error) {
	if s == nil || s.settingRepo == nil {
		return false, nil
	}
	value, err := s.settingRepo.GetValue(ctx, SettingKeySiteCacheRateVisibleToUsers)
	if err != nil && !errors.Is(err, ErrSettingNotFound) {
		return false, fmt.Errorf("get site cache rate visibility: %w", err)
	}
	return value == "true", nil
}

func (s *SettingService) SetSiteCacheRateVisibility(ctx context.Context, visible bool) error {
	if err := s.settingRepo.Set(ctx, SettingKeySiteCacheRateVisibleToUsers, strconv.FormatBool(visible)); err != nil {
		return fmt.Errorf("save site cache rate visibility: %w", err)
	}
	return nil
}
