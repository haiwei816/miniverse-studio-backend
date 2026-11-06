package service

import "time"

const maxLimitEntries = 10000

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
