package iqtest

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

var errEndpoint = errors.New("invalid_endpoint")

func NormalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) > 2048 || strings.ContainsAny(raw, "\\\r\n\t") {
		return "", errEndpoint
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" || u.RawPath != "" {
		return "", errEndpoint
	}
	if u.Port() != "" && u.Port() != "443" {
		return "", errEndpoint
	}
	if urlvalidator.IsBlockedHost(u.Hostname()) {
		return "", errEndpoint
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == "." || part == ".." {
			return "", errEndpoint
		}
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if u.Path == "" {
		u.Path = "/v1"
	}
	return u.String(), nil
}

var blockedNetworks = func() []netip.Prefix {
	ranges := []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4", "2001:db8::/32", "2001::/32", "2002::/16", "64:ff9b::/96", "64:ff9b:1::/48"}
	result := make([]netip.Prefix, 0, len(ranges))
	for _, value := range ranges {
		result = append(result, netip.MustParsePrefix(value))
	}
	return result
}()

func publicIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return false
	}
	if addr.Is6() && !netip.MustParsePrefix("2000::/3").Contains(addr) {
		return false
	}
	for _, network := range blockedNetworks {
		if network.Contains(addr) {
			return false
		}
	}
	return true
}

type lookupIPFunc func(context.Context, string) ([]net.IPAddr, error)

func resolvePublic(ctx context.Context, host string, lookup lookupIPFunc) ([]net.IPAddr, error) {
	ips, err := lookup(ctx, host)
	if err != nil || len(ips) == 0 || len(ips) > 16 {
		return nil, errEndpoint
	}
	for _, addr := range ips {
		if addr.Zone != "" || !publicIP(addr.IP) {
			return nil, errEndpoint
		}
	}
	return ips, nil
}

// Resolve at the actual dial and connect to the validated literal IP, keeping
// the original hostname for TLS verification. No proxy or redirect can bypass it.
func newPublicClient() *http.Client {
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		MaxConnsPerHost: 8, MaxIdleConns: 16, MaxIdleConnsPerHost: 2,
		IdleConnTimeout: 60 * time.Second, ResponseHeaderTimeout: 45 * time.Second,
		TLSHandshakeTimeout: 15 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil || port != "443" {
				return nil, errEndpoint
			}
			ips, err := resolvePublic(ctx, host, net.DefaultResolver.LookupIPAddr)
			if err != nil {
				return nil, err
			}
			var lastErr error
			for _, addr := range ips {
				conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(addr.IP.String(), port))
				if dialErr == nil {
					return conn, nil
				}
				lastErr = dialErr
			}
			return nil, lastErr
		},
	}
	return &http.Client{Transport: transport, Timeout: 10 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
