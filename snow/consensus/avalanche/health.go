package avalanche

import "time"

// HealthConfig describes parameters for snowman health checks.
type HealthConfig struct {
	// Reports unhealthy if more than this number of requests are outstanding.
	// Must be > 0
	MaxOutstandingRequests int

	// Reports unhealthy if there is at least 1 outstanding not processed
	// before this mark
	MaxRunTimeTxs time.Duration
}
