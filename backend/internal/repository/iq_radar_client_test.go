package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestIQRadarInvalidProxyDisablesSourceWithoutBlockingApplication(t *testing.T) {
	cfg := &config.Config{}
	cfg.Update.ProxyURL = "://invalid-proxy"
	source := ProvideIQRadarSource(cfg)
	if source == nil {
		t.Fatal("optional radar source must remain constructible")
	}
	if _, err := source.Catalog(context.Background()); err == nil {
		t.Fatal("invalid proxy must not fall back to a direct request")
	}
}
