// Package health provides liveness and readiness state evaluations for the service.
package health

import (
	"context"
	"errors"
	"sync/atomic"
)

// Pinger defines the capability to verify dependency connectivity (e.g. database).
type Pinger interface {
	Ping(ctx context.Context) error
}

// Service monitors application liveness and readiness.
type Service struct {
	shuttingDown atomic.Bool
	pinger       Pinger
}

// New creates a new health Service. Pinger can be nil if no downstream
// dependency check is required.
func New(pinger Pinger) *Service {
	return &Service{
		pinger: pinger,
	}
}

// IsLive reports whether the process is alive. Liveness indicates the HTTP process
// is running and able to respond to requests, and never depends on external dependencies.
func (s *Service) IsLive() bool {
	return true
}

// CheckReady evaluates whether the service is ready to accept traffic.
// It returns an error if the service is shutting down or if the downstream dependency check fails.
func (s *Service) CheckReady(ctx context.Context) error {
	if s.shuttingDown.Load() {
		return errors.New("service is shutting down")
	}

	if s.pinger != nil {
		if err := s.pinger.Ping(ctx); err != nil {
			return err
		}
	}

	return nil
}

// SetShuttingDown marks the service as shutting down, causing subsequent readiness
// checks to fail so load balancers/proxies stop routing traffic to this instance.
func (s *Service) SetShuttingDown() {
	s.shuttingDown.Store(true)
}
