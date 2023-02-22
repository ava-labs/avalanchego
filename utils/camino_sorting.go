// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import "math"

// Returns true if the elements in [s] are unique and sorted.
// math.MaxUint32 is skipped because they are used as wildcard
func IsSortedAndUniqueOrderedSigIndices(s []uint32) bool {
	var last uint32
	for i := 0; i < len(s)-1; i++ {
		if s[i] != math.MaxUint32 {
			last = s[i]
		}
		if s[i+1] != math.MaxUint32 {
			if last >= s[i+1] {
				return false
			}
		}
	}
	return true
}
