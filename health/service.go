package health

import (
	"time"

	health "github.com/AppsFlyer/go-sundheit"

	"github.com/ava-labs/avalanchego/utils/constants"
)

// Service performs health checks. Other things register health checks
// with Service, which performs them.
type Service interface {
	RegisterCheck(name string, checkFn Check) error
	RegisterMonotonicCheck(name string, checkFn Check) error
	Results() (map[string]interface{}, bool)
}

// NewService returns a new [Service] where the health checks
// run every [checkFreq]
func NewService(checkFreq time.Duration) Service {
	return &service{
		health:    health.New(),
		checkFreq: checkFreq,
	}
}

// service implements Service
type service struct {
	// performs the underlying health checks
	health health.Health
	// Time between health checks
	checkFreq time.Duration
}

// Results returns:
// 1) Name of health check --> health check results
// 2) true iff healthy
func (s *service) Results() (map[string]interface{}, bool) {
	rawResults, healthy := s.health.Results()
	results := make(map[string]interface{}, len(rawResults))
	for name, res := range rawResults {
		results[name] = res
	}
	return results, healthy
}

// RegisterCheckFn adds a check that calls [checkFn] to evaluate health
func (s *service) RegisterCheck(name string, checkFn Check) error {
	check := &check{
		name:    name,
		checkFn: checkFn,
	}

	return s.health.RegisterCheck(&health.Config{
		InitialDelay:    constants.DefaultHealthCheckInitialDelay,
		ExecutionPeriod: s.checkFreq,
		Check:           check,
	})
}

// RegisterMonotonicCheckFn adds a health check that, after it passes once,
// always returns healthy without executing any logic
func (s *service) RegisterMonotonicCheck(name string, checkFn Check) error {
	c := &monotonicCheck{
		check: check{
			name:    name,
			checkFn: checkFn,
		},
	}

	return s.health.RegisterCheck(&health.Config{
		InitialDelay:    constants.DefaultHealthCheckInitialDelay,
		ExecutionPeriod: s.checkFreq,
		Check:           c,
	})
}
