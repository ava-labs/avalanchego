// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

// Uniform samples values without replacement in the provided range
type Uniform interface {
	Initialize(sampleRange uint64) error
	Sample(length int) ([]uint64, error)

	Seed(int64)
	ClearSeed()

	Reset()
	Next() (uint64, error)
}

// NewUniform returns a new sampler
func NewUniform() Uniform { return &uniformReplacer{} }
