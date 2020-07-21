// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package random

import (
	"math"
	"math/rand"
)

// Weighted implements the Sampler interface by sampling based on a heap
// structure.
//
// Node weight is defined as the node's given weight along with it's
// children's recursive weights. Once sampled, a nodes given weight is set to 0.
//
// Replacing runs in O(n) time while sampling runs in O(log(n)) time.
type Weighted struct {
	Weights []uint64

	// The reason this is separated from Weights, is because it is set to 0
	// after being sampled.
	weights    []int64
	cumWeights []int64
}

func (s *Weighted) init() {
	if len(s.Weights) != len(s.weights) {
		s.Replace()
	}
}

// Sample returns a number in [0, len(weights)) with probability proportional to
// the weight of the item at that index. Assumes Len > 0. Sample takes
// O(log(len(weights))) time.
func (s *Weighted) Sample() int {
	i := s.SampleReplace()
	s.changeWeight(i, 0)
	return i
}

// SampleReplace returns a number in [0, len(weights)) with probability
// proportional to the weight of the item at that index. Assumes CanSample
// returns true. Sample takes O(log(len(weights))) time. The returned index is
// not removed.
func (s *Weighted) SampleReplace() int {
	s.init()
	// Weak randomness is acceptable for sampling
	// #nosec G404
	for w, i := rand.Int63n(s.cumWeights[0]), 0; ; {
		w -= s.weights[i]
		if w < 0 {
			return i
		}

		i = i*2 + 1 // We shouldn't return the root, so check the left child

		if lw := s.cumWeights[i]; lw <= w {
			// If the weight is greater than the left weight, you should move to
			// the right child
			w -= lw
			i++
		}
	}
}

// CanSample returns the number of items left that can be sampled
func (s *Weighted) CanSample() bool {
	s.init()
	return len(s.cumWeights) > 0 && s.cumWeights[0] > 0
}

// Replace all the sampled elements. Takes O(len(weights)) time.
func (s *Weighted) Replace() {
	// Attempt to malloc as few times as possible
	if s.weights == nil || cap(s.weights) < len(s.Weights) {
		s.weights = make([]int64, len(s.Weights))
	} else {
		s.weights = s.weights[:len(s.Weights)]
	}
	if s.cumWeights == nil || cap(s.cumWeights) < len(s.Weights) {
		s.cumWeights = make([]int64, len(s.Weights))
	} else {
		s.cumWeights = s.cumWeights[:len(s.Weights)]
	}

	for i, w := range s.Weights {
		if w > math.MaxInt64 {
			panic("Weight too large")
		}
		s.weights[i] = int64(w)
	}

	copy(s.cumWeights, s.weights)

	// Initialize the heap
	for i := len(s.cumWeights) - 1; i > 0; i-- {
		parent := (i - 1) / 2
		w := uint64(s.cumWeights[parent]) + uint64(s.cumWeights[i])
		if w > math.MaxInt64 {
			panic("Weight too large")
		}
		s.cumWeights[parent] = int64(w)
	}
}

func (s *Weighted) changeWeight(i int, newWeight int64) {
	change := s.weights[i] - newWeight

	s.weights[i] = newWeight

	// Decrease my weight and all my parents weights.
	s.cumWeights[i] -= change
	for i > 0 {
		i = (i - 1) / 2
		s.cumWeights[i] -= change
	}
}
