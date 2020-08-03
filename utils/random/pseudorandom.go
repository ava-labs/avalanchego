// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package random

import (
	"math/rand"
	"time"
)

func init() { rand.Seed(time.Now().UnixNano()) }

// Rand returns a number inside [min, max). Panics if min >= max
func Rand(min, max int) int {
	return rand.Intn(max-min) + min
}

// Subset creates a list of at most k unique numbers sampled from the sampler.
// Runs in O(k) * O(Sample) time with O(k) space used.
func Subset(s Sampler, k int) []int {
	inds := []int{}
	for i := 0; i < k && s.CanSample(); i++ {
		inds = append(inds, s.Sample())
	}
	return inds
}

// Bernoulli ...
func Bernoulli(p float64) bool {
	return rand.Float64() < p
}
