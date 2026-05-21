// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

// Uniform samples values without replacement in the provided range
type Uniform interface {
	Initialize(sampleRange uint64)
	// Sample returns length numbers in the range [0,sampleRange). If there
	// aren't enough numbers in the range, false is returned. If length is
	// negative the implementation may panic.
	Sample(length int) ([]uint64, bool)

	Next() (uint64, bool)
	Reset()
}

// NewUniform returns a new sampler
func NewUniform() Uniform {
	return &uniformReplacer{
		rng: globalRNG,
	}
}

// NewDeterministicUniform returns a new sampler
func NewDeterministicUniform(source Source) Uniform {
	return &uniformReplacer{
		rng: &rng{
			rng: source,
		},
	}
}
