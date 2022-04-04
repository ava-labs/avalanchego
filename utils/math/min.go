// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package math

func Min(min int, nums ...int) int {
	for _, num := range nums {
		if num < min {
			min = num
		}
	}
	return min
}
