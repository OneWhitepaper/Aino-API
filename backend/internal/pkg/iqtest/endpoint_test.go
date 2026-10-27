package iqtest

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeBaseURL(t *testing.T) {
	for _, value := range []string{"http://example.com/v1", "https://user:pass@example.com/v1", "https://example.com/v1?key=x", "https://example.com/#x", "https://localhost/v1", "https://127.0.0.1/v1", "https://[::1]/v1", "https://169.254.169.254/v1", "https://example.com:8080/v1", "https://example.com/v1/../admin", "https://example.com/v1%2fsecret"} {
		_, err := NormalizeBaseURL(value)
		require.Error(t, err, value)
	}
	base, err := NormalizeBaseURL(" https://api.example.com/v1/ ")
	require.NoError(t, err)
	require.Equal(t, "https://api.example.com/v1", base)
}

func TestResolvePublicRejectsMixedPrivateAndReservedAddresses(t *testing.T) {
	for _, ips := range [][]net.IPAddr{
		{{IP: net.ParseIP("127.0.0.1")}}, {{IP: net.ParseIP("10.0.0.2")}},
		{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("192.168.1.1")}},
		{{IP: net.ParseIP("100.100.100.200")}}, {{IP: net.ParseIP("198.18.0.1")}},
		{{IP: net.ParseIP("2001:db8::1")}},
	} {
		_, err := resolvePublic(context.Background(), "api.example.com", func(context.Context, string) ([]net.IPAddr, error) { return ips, nil })
		require.Error(t, err, ips)
	}
	ips, err := resolvePublic(context.Background(), "api.example.com", func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	})
	require.NoError(t, err)
	require.Equal(t, "8.8.8.8", ips[0].IP.String())
}
