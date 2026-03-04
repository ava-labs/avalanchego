// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

// WeightedWithoutReplacement defines how to sample weight without replacement.
// Note that the behavior is to sample the weight without replacement, not the
// indices. So duplicate indices can be returned.
type WeightedWithoutReplacement interface {
	Initialize(weights []uint64) error
	Sample(count int) ([]int, bool)
}

// NewDeterministicWeightedWithoutReplacement returns a new sampler
func NewDeterministicWeightedWithoutReplacement(source Source) WeightedWithoutReplacement {
	return &weightedWithoutReplacementGeneric{
		u: NewDeterministicUniform(source),
		w: NewWeighted(),
	}
}

// NewWeightedWithoutReplacement returns a new sampler
func NewWeightedWithoutReplacement() WeightedWithoutReplacement {
	return &weightedWithoutReplacementGeneric{
		u: NewUniform(),
		w: NewWeighted(),
	}
}
