package health

import (
	"encoding/json"
	"time"

	health "github.com/AppsFlyer/go-sundheit"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// Service performs health checks. Other things register health checks
// with Service, which performs them.
type Service interface {
	RegisterCheck(name string, checkFn Check) error
	RegisterMonotonicCheck(name string, checkFn Check) error
	Results() (map[string]health.Result, bool)
}

// NewService returns a new [Service] where the health checks
// run every [checkFreq]
func NewService(checkFreq time.Duration, log logging.Logger) Service {
	healthChecker := health.New()
	healthChecker.WithCheckListener(&checkListener{
		log:    log,
		checks: make(map[string]bool),
	})
	return &service{
		Health:    healthChecker,
		checkFreq: checkFreq,
	}
}

// service implements Service
type service struct {
	// performs the underlying health checks
	health.Health
	// Time between health checks
	checkFreq time.Duration
}

// RegisterCheckFn adds a check that calls [checkFn] to evaluate health
func (s *service) RegisterCheck(name string, checkFn Check) error {
	check := &check{
		name:    name,
		checkFn: checkFn,
	}

	return s.Health.RegisterCheck(&health.Config{
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

	return s.Health.RegisterCheck(&health.Config{
		InitialDelay:    constants.DefaultHealthCheckInitialDelay,
		ExecutionPeriod: s.checkFreq,
		Check:           c,
	})
}

type checkListener struct {
	log    logging.Logger
	checks map[string]bool
}

func (c *checkListener) OnCheckStarted(name string) {
	c.log.Debug("starting to run health check %s", name)
}
func (c *checkListener) OnCheckCompleted(name string, result health.Result) {
	resultJSON, err := json.Marshal(result)
	if err != nil {
		c.log.Error("failed to encode health check %q when it was failing due to: %s", name, err)
		return
	}

	isHealthy := result.IsHealthy()
	previouslyHealthy, exists := c.checks[name]
	c.checks[name] = isHealthy

	if !exists || isHealthy == previouslyHealthy {
		if isHealthy {
			c.log.Debug("health check %q returned healthy with: %s", name, string(resultJSON))
		} else {
			c.log.Debug("health check %q returned unhealthy with: %s", name, string(resultJSON))
		}
		return
	}

	if isHealthy {
		c.log.Info("health check %q became healthy with: %s", name, string(resultJSON))
	} else {
		c.log.Warn("health check %q became unhealthy with: %s", name, string(resultJSON))
	}
}
