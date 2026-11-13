package service

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

const maxLimitEntries = 10000

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	peer = peer.Unmap()
	// Only trust proxy forwarding headers from local/private reverse proxies (e.g. Docker nginx).
	if peer.IsLoopback() || peer.IsPrivate() {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			for i := len(parts) - 1; i >= 0; i-- {
				candidate := strings.TrimSpace(parts[i])
				if addr, err := netip.ParseAddr(candidate); err == nil {
					addr = addr.Unmap()
					if !addr.IsPrivate() && !addr.IsLoopback() || i == 0 {
						return addr.String()
					}
				}
			}
		}
		if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
			if addr, err := netip.ParseAddr(xri); err == nil {
				return addr.Unmap().String()
			}
		}
	}
	return peer.String()
}

// No worker goroutine is needed: active traffic sweeps expired entries once a minute.
// A full table rejects new sources instead of growing or evicting active counters.
func (s *Service) allowRequest(host string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !now.Before(s.nextLimitSweep) {
		for key, b := range s.limits {
			if now.Sub(b.start) >= time.Minute {
				delete(s.limits, key)
			}
		}
		s.nextLimitSweep = now.Add(time.Minute)
	}
	b, exists := s.limits[host]
	if !exists {
		if len(s.limits) >= maxLimitEntries {
			return false
		}
		b = bucket{start: now}
	} else if now.Sub(b.start) >= time.Minute {
		b = bucket{start: now}
	}
	if b.n >= 600 {
		return false
	}
	b.n++
	s.limits[host] = b
	return true
}
