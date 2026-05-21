// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package resource

import "math"

// NoUsage implements Usage() by always returning 0.
var NoUsage User = noUsage{}

type noUsage struct{}

func (noUsage) AvailableDiskPercentage() uint64 {
	return 100
}

func (noUsage) CPUUsage() float64 {
	return 0
}

func (noUsage) DiskUsage() (float64, float64) {
	return 0, 0
}

func (noUsage) AvailableDiskBytes() uint64 {
	return math.MaxUint64
}
