// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"sort"
)

// IsSortedAndUnique returns true if the elements in the data are unique and sorted.
func IsSortedAndUnique(data sort.Interface) bool {
	for i := data.Len() - 2; i >= 0; i-- {
		if !data.Less(i, i+1) {
			return false
		}
	}
	return true
}

type innerSortUint32 []uint32

func (su32 innerSortUint32) Less(i, j int) bool { return su32[i] < su32[j] }
func (su32 innerSortUint32) Len() int           { return len(su32) }
func (su32 innerSortUint32) Swap(i, j int)      { su32[j], su32[i] = su32[i], su32[j] }

// SortUint32 sorts an uint32 array
func SortUint32(u32 []uint32) { sort.Sort(innerSortUint32(u32)) }

// IsSortedAndUniqueUint32 returns true if the array of uint32s are sorted and unique
func IsSortedAndUniqueUint32(u32 []uint32) bool { return IsSortedAndUnique(innerSortUint32(u32)) }
