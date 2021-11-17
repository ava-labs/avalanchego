// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"math/rand"
	"sync"
	"time"

	"gonum.org/v1/gonum/mathext/prng"
)

var (
	int63Mask uint64 = 1<<63 - 1
	globalRNG        = newRNG()
)

func newRNG() rng {
	source := prng.NewMT19937()
	source.Seed(uint64(time.Now().UnixNano()))
	// We don't use a cryptographically secure source of randomness here, as
	// there's no need to ensure a truly random sampling.
	return rand.New(&syncSource{rng: source}) // #nosec G404
}

func Seed(seed int64) {
	globalRNG.Seed(seed)
}

type rng interface {
	// Seed uses the provided seed value to initialize the generator to a
	// deterministic state.
	Seed(seed int64)

	// Int63n returns, as an int64, a non-negative pseudo-random number in
	// [0,n). It panics if n <= 0.
	Int63n(n int64) int64
}

type syncSource struct {
	lock sync.Mutex
	rng  *prng.MT19937
}

func (s *syncSource) Seed(seed int64) {
	s.lock.Lock()
	s.rng.Seed(uint64(seed))
	s.lock.Unlock()
}

func (s *syncSource) Int63() int64 {
	return int64(s.Uint64() & int63Mask)
}

func (s *syncSource) Uint64() uint64 {
	s.lock.Lock()
	n := s.rng.Uint64()
	s.lock.Unlock()
	return n
}
