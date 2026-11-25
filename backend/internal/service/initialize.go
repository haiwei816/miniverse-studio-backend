package service

func (s *Service) Initialize() error {
	s.ConcurrentDiagnostics = true
	s.EnforceBans = true
	if err := s.seedGM(); err != nil {
		return err
	}
	if err := s.seedPlayerPlatform(); err != nil {
		return err
	}
	s.loadPersistedOpsAgentConfig()
	return nil
}
