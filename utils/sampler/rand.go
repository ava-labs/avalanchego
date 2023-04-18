// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"math"
	"sync"
	"time"

	"gonum.org/v1/gonum/mathext/prng"
)

var globalRNG = newRNG()

func newRNG() *rng {
	source := prng.NewMT19937()
	source.Seed(uint64(time.Now().UnixNano()))
	return &rng{rng: source}
}

func Seed(seed int64) {
	globalRNG.Seed(seed)
}

type rngSource interface {
	Seed(uint64)
	Uint64() uint64
}

type rng struct {
	lock sync.Mutex
	rng  rngSource
}

// Seed uses the provided seed value to initialize the generator to a
// deterministic state.
func (r *rng) Seed(seed int64) {
	r.lock.Lock()
	r.rng.Seed(uint64(seed))
	r.lock.Unlock()
}

// Uint64n returns a pseudo-random number in [0,n].
//
// Invariant: The result of this function is stored in chain state, so any
// modifications are considered breaking.
func (r *rng) Uint64n(n uint64) uint64 {
	switch {
	// n+1 is power of two, so we can just mask
	//
	// Note: This does work for MaxUint64 as overflow is explicitly part of the
	// compiler specification: https://go.dev/ref/spec#Integer_overflow
	case n&(n+1) == 0:
		return r.uint64() & n

	// n is greater than MaxUint64/2 so we need to just iterate until we get a
	// number in the requested range.
	case n > math.MaxInt64:
		v := r.uint64()
		for v > n {
			v = r.uint64()
		}
		return v

	// n is less than MaxUint64/2 so we can generate a number in the range
	// [0, k*n] where k is the largest integer such that k*n is still less than
	// MaxUint64/2. We can't easily find k such that k*n is still less than
	// MaxUint64 because the calculation would overflow.
	//
	// ref: https://github.com/golang/go/blob/ce10e9d84574112b224eae88dc4e0f43710808de/src/math/rand/rand.go#L127-L132
	default:
		max := (1 << 63) - 1 - (1<<63)%(n+1)
		v := r.uint63()
		for v > max {
			v = r.uint63()
		}
		return v % (n + 1)
	}
}

// uint63 returns a random number in [0, MaxInt64]
func (r *rng) uint63() uint64 {
	return r.uint64() & math.MaxInt64
}

// uint64 returns a random number in [0, MaxUint64]
func (r *rng) uint64() uint64 {
	// Note: We must grab a write lock here because rng.Uint64 internally
	// modifies state.
	r.lock.Lock()
	n := r.rng.Uint64()
	r.lock.Unlock()
	return n
}
