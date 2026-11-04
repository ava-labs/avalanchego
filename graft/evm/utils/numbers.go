// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"math/big"
	"time"
)

func TimeToNewUint64(time time.Time) *uint64 {
	return PointerTo(uint64(time.Unix()))
}

func Uint64ToTime(val *uint64) time.Time {
	timestamp := int64(*val)
	return time.Unix(timestamp, 0)
}

// BigEqual returns true if a is equal to b. If a and b are nil, it returns
// true.
func BigEqual(a, b *big.Int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Cmp(b) == 0
}
