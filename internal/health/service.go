package health

import (
	"context"
	"time"
)

type Pinger interface {
	Ping(context.Context) error
}

type Service struct {
	database Pinger
	timeout  time.Duration
}

type Check struct {
	Status       string            `json:"status"`
	Dependencies map[string]string `json:"dependencies,omitempty"`
}

func NewService(database Pinger, timeout time.Duration) *Service {
	return &Service{database: database, timeout: timeout}
}

func (s *Service) Live() Check {
	return Check{Status: "ok"}
}

func (s *Service) Ready(ctx context.Context) (Check, bool) {
	checkCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	if err := s.database.Ping(checkCtx); err != nil {
		return Check{Status: "not_ready", Dependencies: map[string]string{"postgres": "unavailable"}}, false
	}
	return Check{Status: "ready", Dependencies: map[string]string{"postgres": "ok"}}, true
}
