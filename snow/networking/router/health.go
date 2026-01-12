// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import "time"

// HealthConfig describes parameters for router health checks.
type HealthConfig struct {
	// Reports unhealthy if we drop more than [MaxDropRate] of messages
	MaxDropRate float64 `json:"maxDropRate"`

	// Halflife of averager used to calculate the message drop rate
	// Must be > 0.
	// Larger value --> Drop rate affected less by recent messages
	MaxDropRateHalflife time.Duration `json:"maxDropRateHalflife"`

	// Reports unhealthy if more than this number of requests are outstanding.
	// Must be > 0
	MaxOutstandingRequests int `json:"maxOutstandingRequests"`

	// Reports unhealthy if there is a request outstanding for longer than this
	MaxOutstandingDuration time.Duration `json:"maxOutstandingDuration"`

	// Reports unhealthy if there is at least 1 outstanding not processed
	// before this mark
	MaxRunTimeRequests time.Duration `json:"maxRunTimeRequests"`
}
