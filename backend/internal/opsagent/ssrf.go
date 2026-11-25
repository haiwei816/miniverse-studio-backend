package opsagent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

// ErrSSRFBlocked indicates an upstream connection attempt to a restricted network address.
var ErrSSRFBlocked = errors.New("ops_agent_ssrf_blocked")

var (
	cgnatPrefix       = netip.MustParsePrefix("100.64.0.0/10")
	thisNetPrefix     = netip.MustParsePrefix("0.0.0.0/8")
	reservedV4Prefix  = netip.MustParsePrefix("240.0.0.0/4")
	ietfProtoPrefix   = netip.MustParsePrefix("192.0.0.0/24")
	benchmarkV4Prefix = netip.MustParsePrefix("198.18.0.0/15")
)

// IsBlockedUpstreamIP checks whether a resolved IP is disallowed for outbound model calls.
func IsBlockedUpstreamIP(ip netip.Addr, host string, sandbox bool) bool {
	if !ip.IsValid() {
		return true
	}
	ip = ip.Unmap()
	if ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if ip.IsLoopback() {
		return !sandbox
	}
	if sandbox && strings.EqualFold(strings.TrimSpace(host), "host.docker.internal") {
		return false
	}
	if ip.IsPrivate() {
		return true
	}
	if ip.Is4() {
		if cgnatPrefix.Contains(ip) || thisNetPrefix.Contains(ip) || reservedV4Prefix.Contains(ip) || ietfProtoPrefix.Contains(ip) || benchmarkV4Prefix.Contains(ip) {
			return true
		}
	}
	return false
}

// NewSafeHTTPClient builds an HTTP client that prevents redirects and blocks SSRF / DNS rebinding.
func NewSafeHTTPClient(timeout time.Duration, sandbox bool) *http.Client {
	dialer := &net.Dialer{Timeout: timeout}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			if ip, parseErr := netip.ParseAddr(host); parseErr == nil {
				if IsBlockedUpstreamIP(ip, host, sandbox) {
					return nil, fmt.Errorf("%w: %s", ErrSSRFBlocked, ip.String())
				}
				return dialer.DialContext(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
			}
			ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			if len(ips) == 0 {
				return nil, fmt.Errorf("%w: empty dns result", ErrSSRFBlocked)
			}
			for _, ip := range ips {
				if IsBlockedUpstreamIP(ip, host, sandbox) {
					return nil, fmt.Errorf("%w: %s", ErrSSRFBlocked, ip.Unmap().String())
				}
			}
			var lastErr error
			for _, ip := range ips {
				conn, derr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
				if derr == nil {
					return conn, nil
				}
				lastErr = derr
			}
			return nil, lastErr
		},
	}
	return &http.Client{
		Timeout:       timeout,
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
