// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"math/rand"
	"sync"
	"time"
)

var globalRNG = newRNG()

func newRNG() rng {
	source := rand.NewSource(time.Now().UnixNano())

	// We don't use a cryptographically secure source of randomness here, as
	// there's no need to ensure a truly random sampling.
	return rand.New(&syncSource{Source: source}) // #nosec G404
}

func Seed(seed int64) {
	globalRNG.Seed(seed)
}

type rng interface {
	// Seed uses the provided seed value to initialize the generator to a
	// deterministic state.
	Seed(int64)

	// Int63n returns, as an int64, a non-negative pseudo-random number in
	// [0,n). It panics if n <= 0.
	Int63n(int64) int64
}

type syncSource struct {
	lock sync.Mutex
	rand.Source
}

func (s *syncSource) Seed(seed int64) {
	s.lock.Lock()
	s.Source.Seed(seed)
	s.lock.Unlock()
}

func (s *syncSource) Int63() int64 {
	s.lock.Lock()
	n := s.Source.Int63()
	s.lock.Unlock()
	return n
}
