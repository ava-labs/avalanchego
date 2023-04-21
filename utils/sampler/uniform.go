// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

// Uniform samples values without replacement in the provided range
type Uniform interface {
	Initialize(sampleRange uint64)
	// Sample returns length numbers in the range [0,sampleRange). If there
	// aren't enough numbers in the range, an error is returned. If length is
	// negative the implementation may panic.
	Sample(length int) ([]uint64, error)

	Seed(int64)
	ClearSeed()

	Reset()
	Next() (uint64, error)
}

// NewUniform returns a new sampler
func NewUniform() Uniform {
	return &uniformReplacer{}
}
