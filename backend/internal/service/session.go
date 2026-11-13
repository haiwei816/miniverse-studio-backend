package service

// sessionHash keeps issuance, validation and revocation in the same environment.
// Existing sandbox sessions retain their hashes and cannot enter production.
func (s *Service) sessionHash(token string) string {
	if !s.Sandbox {
		token = "production:" + token
	}
	return hash(token)
}
