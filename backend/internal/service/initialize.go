package service

func (s *Service) Initialize() error {
	if err := s.seedGM(); err != nil {
		return err
	}
	return s.seedPlayerPlatform()
}
