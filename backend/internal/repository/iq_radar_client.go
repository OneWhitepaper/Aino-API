package repository

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/pkg/iqradar"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ProvideIQRadarSource reuses the configured outbound update proxy. An invalid
// proxy disables this optional data source without preventing server startup.
func ProvideIQRadarSource(cfg *config.Config) service.IQRadarSource {
	client, err := httpclient.GetClient(httpclient.Options{ProxyURL: cfg.Update.ProxyURL, Timeout: 25 * time.Second, ResponseHeaderTimeout: 20 * time.Second, MaxConnsPerHost: 4, ValidateResolvedIP: true})
	if err != nil {
		slog.Warn("iq_radar.proxy_configuration_invalid")
		return unavailableIQRadarSource{}
	}
	return iqradar.NewClient(client)
}

type unavailableIQRadarSource struct{}

func (unavailableIQRadarSource) Catalog(context.Context) (iqradar.Catalog, error) {
	return iqradar.Catalog{}, errors.New("radar outbound proxy configuration is invalid")
}
func (unavailableIQRadarSource) Score(context.Context, iqradar.Selection, iqradar.Catalog) (iqradar.Point, error) {
	return iqradar.Point{}, errors.New("radar outbound proxy configuration is invalid")
}
func (unavailableIQRadarSource) Community(context.Context) (iqradar.Community, error) {
	return iqradar.Community{}, errors.New("radar outbound proxy configuration is invalid")
}
