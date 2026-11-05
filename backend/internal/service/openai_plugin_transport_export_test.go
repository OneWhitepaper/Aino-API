package service

import (
	"net/http"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	hcplugin "github.com/hashicorp/go-plugin"
)

// OpenAIPluginTransportForTest exposes the production dispatch seam to external
// tests, which can import repository.HTTPUpstream without creating a cycle.
func OpenAIPluginTransportForTest(upstream HTTPUpstream, api pluginv1.TransportPluginClient) func(*http.Request, string, *Account) (*http.Response, error) {
	manager := &PluginManager{}
	manager.route.Store(&pluginRoute{
		pluginID:       1,
		rolloutPercent: 100,
		runtime:        &pluginRuntime{client: &hcplugin.Client{}, api: api},
	})
	gateway := &OpenAIGatewayService{pluginManager: manager, httpUpstream: upstream}
	return gateway.doOpenAIUpstream
}
