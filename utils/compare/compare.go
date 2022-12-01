// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package compare

import "golang.org/x/exp/maps"

// Returns true iff the slices have the same elements,
// regardless of order.
func UnsortedEquals[T comparable](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	aMap := make(map[T]int, len(a))
	bMap := make(map[T]int, len(b))
	for i := 0; i < len(a); i++ {
		aMap[a[i]]++
		bMap[b[i]]++
	}
	return maps.Equal(aMap, bMap)
}
