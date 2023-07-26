// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"math/big"
	"time"
)

func NewUint64(val uint64) *uint64 { return &val }

func TimeToNewUint64(time time.Time) *uint64 {
	unix := uint64(time.Unix())
	return NewUint64(unix)
}

func Uint64ToTime(val *uint64) time.Time {
	timestamp := int64(*val)
	return time.Unix(timestamp, 0)
}

// IsBlockForked returns whether a fork scheduled at block s is active at the given head block.
// Note: [s] and [head] can be either a block number or a block timestamp.
func IsBlockForked(s, head *big.Int) bool {
	if s == nil || head == nil {
		return false
	}
	return s.Cmp(head) <= 0
}

// IsTimestampForked returns whether a fork scheduled at timestamp s is active
// at the given head timestamp. Whilst this method is the same as isBlockForked,
// they are explicitly separate for clearer reading.
func IsTimestampForked(s *uint64, head uint64) bool {
	if s == nil {
		return false
	}
	return *s <= head
}

// IsForkTransition returns true if [fork] activates during the transition from
// [parent] to [current].
// Taking [parent] as a pointer allows for us to pass nil when checking forks
// that activate during genesis.
// Note: this works for both block number and timestamp activated forks.
func IsForkTransition(fork *uint64, parent *uint64, current uint64) bool {
	var parentForked bool
	if parent != nil {
		parentForked = IsTimestampForked(fork, *parent)
	}
	currentForked := IsTimestampForked(fork, current)
	return !parentForked && currentForked
}

// BigNumEqual returns true if x and y are equivalent ie. both nil or both
// contain the same value.
func BigNumEqual(x, y *big.Int) bool {
	if x == nil || y == nil {
		return x == y
	}
	return x.Cmp(y) == 0
}

// Uint64PtrEqual returns true if x and y pointers are equivalent ie. both nil or both
// contain the same value.
func Uint64PtrEqual(x, y *uint64) bool {
	if x == nil || y == nil {
		return x == y
	}
	return *x == *y
}
