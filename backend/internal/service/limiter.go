package service

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

var apiRedisRateScript = redis.NewScript(`
local current = redis.call("INCR", KEYS[1])
if current == 1 then
	redis.call("PEXPIRE", KEYS[1], ARGV[2])
end
if current > tonumber(ARGV[1]) then
	return 0
end
return 1
`)

const maxLimitEntries = 10000

func clientIP(r *http.Request, trusted []netip.Prefix) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	peer = peer.Unmap()
	isTrusted := func(addr netip.Addr) bool {
		for _, prefix := range trusted {
			if prefix.Contains(addr) {
				return true
			}
		}
		return false
	}
	if !isTrusted(peer) {
		return peer.String()
	}
	// Walk from the connection towards the client, stopping at the first
	// untrusted hop. Malformed chains are ignored rather than partially trusted.
	if values := r.Header.Values("X-Forwarded-For"); len(values) > 0 {
		parts := strings.Split(strings.Join(values, ","), ",")
		current := peer
		for i := len(parts) - 1; i >= 0 && isTrusted(current); i-- {
			addr, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
			if err != nil || addr.Zone() != "" {
				return peer.String()
			}
			current = addr.Unmap()
		}
		return current.String()
	}
	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		if addr, err := netip.ParseAddr(xri); err == nil && addr.Zone() == "" {
			return addr.Unmap().String()
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
	if s.Redis != nil && s.Redis.Raw() != nil {
		return s.allowRequestRedis(host, now)
	}
	return true
}

func (s *Service) allowRequestRedis(host string, now time.Time) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	minuteSlot := now.UTC().Format("200601021504")
	key := "mv:rl:api:" + host + ":" + minuteSlot
	res, err := apiRedisRateScript.Run(ctx, s.Redis.Raw(), []string{key}, 600, int64(70000)).Int64()
	if err != nil {
		// Degrade gracefully to local bucket enforcement if Redis times out.
		return true
	}
	return res == 1
}
