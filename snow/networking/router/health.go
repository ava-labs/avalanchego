package router

import "time"

// HealthConfig describes parameters for router health checks.
type HealthConfig struct {
	// Reports unhealthy if we drop more than [MaxPercentDropped]
	// in the last [MaxDroppedMessagesDuration]
	MaxPercentDropped         float64
	MaxPercentDroppedDuration time.Duration
}
