// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

// WeightedWithoutReplacement defines how to sample weight without replacement.
// Note that the behavior is to sample the weight without replacement, not the
// indices. So duplicate indices can be returned.
type WeightedWithoutReplacement interface {
	Initialize(weights []uint64) error
	Sample(count int) ([]int, error)
}
