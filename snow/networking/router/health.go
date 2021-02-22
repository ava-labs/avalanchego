package router

import "time"

// HealthConfig describes parameters for router health checks.
type HealthConfig struct {
	// Reports unhealthy if we drop more than [MaxPercentDropped] of messages
	// between health checks
	MaxDropRate float64

	// Reports unhealthy if more than this number of requests are outstanding.
	// Must be > 0
	MaxOutstandingRequests int

	// Reports unhealthy if there is at least 1 outstanding request continuously
	// for longer than this
	MaxTimeSinceNoOutstandingRequests time.Duration
}
