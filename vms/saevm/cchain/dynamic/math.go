// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dynamic

import "github.com/ava-labs/avalanchego/utils/math"

// toward moves current at most maxDiff steps toward desired.
func toward[T ~uint64](current T, desired *T, maxDiff T) T {
	if desired == nil {
		return current
	}

	d := *desired
	change := math.AbsDiff(current, d)
	change = min(change, maxDiff)
	if current < d {
		return current + change
	}
	return current - change
}

// search returns the smallest v in [0, n) such that f(v) is true, assuming f
// is monotonic: if f(v) is true, then f(w) is true for all w >= v. If no such
// v exists, search returns n.
func search[T ~uint64](n T, f func(T) bool) T {
	lo, hi := T(0), n
	for lo < hi {
		mid := lo + (hi-lo)/2
		if !f(mid) {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}
