package service

import "net/http"

// sessionHash keeps issuance, validation and revocation in the same environment.
// Existing sandbox sessions retain their hashes and cannot enter production.
func (s *Service) sessionHash(token string) string {
	if !s.Sandbox {
		token = "production:" + token
	}
	return hash(token)
}

func playerSessionCookie(token string, maxAge int, sandbox bool) *http.Cookie {
	return &http.Cookie{
		Name:     "mv_player_session",
		Value:    token,
		Path:     "/v1",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   !sandbox,
		SameSite: http.SameSiteStrictMode,
	}
}

func gmSessionCookie(token string, maxAge int, sandbox bool) *http.Cookie {
	return &http.Cookie{
		Name:     "mv_gm_session",
		Value:    token,
		Path:     "/gm",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   !sandbox,
		SameSite: http.SameSiteStrictMode,
	}
}
