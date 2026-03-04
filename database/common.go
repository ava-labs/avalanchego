// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

const (
	// If, when a batch is reset, the cap(batch)/len(batch) > MaxExcessCapacityFactor,
	// the underlying array's capacity will be reduced by a factor of capacityReductionFactor.
	// Higher value for MaxExcessCapacityFactor --> less aggressive array downsizing --> less memory allocations
	// but more unnecessary data in the underlying array that can't be garbage collected.
	// Higher value for CapacityReductionFactor --> more aggressive array downsizing --> more memory allocations
	// but less unnecessary data in the underlying array that can't be garbage collected.
	MaxExcessCapacityFactor = 4
	CapacityReductionFactor = 2
)
