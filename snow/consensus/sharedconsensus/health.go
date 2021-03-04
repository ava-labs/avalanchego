package sharedconsensus

import "time"

// HealthConfig describes parameters for consensus health checks.
type HealthConfig struct {
	// Reports unhealthy if more than this number of items are outstanding.
	// Must be > 0
	MaxOutstandingItems int

	// Reports unhealthy if there is at least 1 outstanding item not processed
	// before this mark
	MaxRunTimeItems time.Duration
}
