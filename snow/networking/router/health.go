package router

// HealthConfig describes parameters for router health checks.
type HealthConfig struct {
	// Reports unhealthy if we drop more than [MaxPercentDropped] of messages
	// between health checks
	MaxDropRate float64
}
