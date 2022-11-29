// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package compare

// TODO use a generic bag here once that's merged.
// Returns true iff the slices have the same elements,
// regardless of order.
func UnsortedEquals[T comparable](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	aMap := make(map[T]int, len(a))
	bMap := make(map[T]int, len(b))
	for _, elem := range a {
		aMap[elem]++
	}
	for _, elem := range b {
		bMap[elem]++
	}
	if len(aMap) != len(bMap) {
		return false
	}
	for elem, count := range aMap {
		if bMap[elem] != count {
			return false
		}
	}
	return true
}
