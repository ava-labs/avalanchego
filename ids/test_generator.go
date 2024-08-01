// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"sync"
	"time"

	"gonum.org/v1/gonum/mathext/prng"
)

type uniqueRandUint64 struct {
	generated map[uint64]bool
	rng       *prng.MT19937
	lock      sync.Mutex
}

func (u *uniqueRandUint64) Uint64() uint64 {
	u.lock.Lock()
	defer u.lock.Unlock()

	for {
		n := u.rng.Uint64()
		if !u.generated[n] {
			u.generated[n] = true
			return n
		}
	}
}

var generator = uniqueRandUint64{
	generated: make(map[uint64]bool),
}

func init() {
	source := prng.NewMT19937()
	source.Seed(uint64(time.Now().UnixNano()))
	generator.rng = source
}

// GenerateTestID returns a new ID that should only be used for testing
func GenerateTestID() ID {
	return Empty.Prefix(generator.Uint64())
}

// GenerateTestShortID returns a new ID that should only be used for testing
func GenerateTestShortID() ShortID {
	newID := GenerateTestID()
	newShortID, _ := ToShortID(newID[:20])
	return newShortID
}

// GenerateTestNodeID returns a new ID that should only be used for testing
func GenerateTestNodeID() NodeID {
	return NodeID(GenerateTestShortID())
}

// BuildTestNodeID is an utility to build NodeID from bytes in UTs
// It must not be used in production code. In production code we should
// use ToNodeID, which performs proper length checking.
func BuildTestNodeID(src []byte) NodeID {
	res := NodeID{}
	copy(res[:], src)
	return res
}
