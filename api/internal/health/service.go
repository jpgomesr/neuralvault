package health

type HealthService struct{}

func (s HealthService) AllHealth() (ConnectionHealth, error) {
	err := s.GetDatabaseHealth()
	if err != nil {
		return ConnectionHealth{}, err
	}

	return ConnectionHealth{
		Server:   "OK",
		Database: "OK",
	}, nil
}

func (s HealthService) GetDatabaseHealth() error {
	// TODO after database integration need to make something to try to reach database and see if its running
	return nil
}
