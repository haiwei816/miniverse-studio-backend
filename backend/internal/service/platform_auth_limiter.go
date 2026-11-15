package service

import "time"

const maxPlatformAuthEntries = 10000

// Both dimensions are charged before any password derivation or database work.
// The maps never evict live quotas to make room for attacker-chosen identities.
func (s *Service) allowPlatformAuth(ip, username string, now time.Time) bool {
	s.authMu.Lock()
	defer s.authMu.Unlock()
	if !now.Before(s.nextAuthSweep) {
		for key, v := range s.authLimits {
			if now.Sub(v.start) >= time.Minute {
				delete(s.authLimits, key)
			}
		}
		s.nextAuthSweep = now.Add(time.Minute)
	}
	keys := [2]string{"ip:" + ip, "user:" + username}
	quotas := [2]int{30, 10}
	values := [2]bucket{}
	missing := 0
	for i, key := range keys {
		v, exists := s.authLimits[key]
		if !exists {
			missing++
		}
		if !exists || now.Sub(v.start) >= time.Minute {
			v = bucket{start: now}
		}
		if v.n >= quotas[i] {
			return false
		}
		values[i] = v
	}
	if len(s.authLimits)+missing > maxPlatformAuthEntries {
		return false
	}
	for i, key := range keys {
		v := values[i]
		v.n++
		s.authLimits[key] = v
	}
	return true
}

func (s *Service) platformPasswordHash(password, salt string) (string, error) {
	select {
	case s.authHashes <- struct{}{}:
		defer func() { <-s.authHashes }()
		return passwordHash(password, salt), nil
	default:
		return "", bad(503, "auth_busy")
	}
}
