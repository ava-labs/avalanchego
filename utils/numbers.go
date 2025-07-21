// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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

// BigEqual returns true if a is equal to b. If a and b are nil, it returns
// true.
func BigEqual(a, b *big.Int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Cmp(b) == 0
}

// BigEqualUint64 returns true if a is equal to b. If a is nil or not a uint64,
// it returns false.
func BigEqualUint64(a *big.Int, b uint64) bool {
	return a != nil &&
		a.IsUint64() &&
		a.Uint64() == b
}

// BigLessOrEqualUint64 returns true if a is less than or equal to b. If a is
// nil or not a uint64, it returns false.
func BigLessOrEqualUint64(a *big.Int, b uint64) bool {
	return a != nil &&
		a.IsUint64() &&
		a.Uint64() <= b
}
