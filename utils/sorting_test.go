// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"math/rand"
	"testing"
)

func TestSort2dByteArray(t *testing.T) {
	numSubArrs := 20
	maxLength := 100

	// Create a random 2D array
	arr := [][]byte{}
	for i := 0; i < numSubArrs; i++ {
		subArrLen := rand.Intn(maxLength) // #nosec G404
		subArr := make([]byte, subArrLen)
		_, err := rand.Read(subArr) // #nosec G404
		if err != nil {
			t.Fatal(err)
		}
		arr = append(arr, subArr)
	}

	// In the unlikely event the random array is sorted, unsort it
	if IsSorted2DBytes(arr) {
		arr[0], arr[len(arr)-1] = arr[len(arr)-1], arr[0]
	}
	Sort2DBytes(arr) // sort it
	if !IsSorted2DBytes(arr) {
		t.Fatal("should be sorted")
	}
}

func TestSortUint32Array(t *testing.T) {
	tests := []struct {
		name     string
		arr      []uint32
		isSorted bool
	}{
		{
			name:     "nil",
			arr:      nil,
			isSorted: true,
		},
		{
			name:     "[]",
			arr:      []uint32{},
			isSorted: true,
		},
		{
			name:     "[0]",
			arr:      []uint32{0},
			isSorted: true,
		},
		{
			name:     "[0,0]",
			arr:      []uint32{0, 0},
			isSorted: false,
		},
		{
			name:     "[0,1]",
			arr:      []uint32{0, 1},
			isSorted: true,
		},
		{
			name:     "[1,0]",
			arr:      []uint32{1, 0},
			isSorted: false,
		},
		{
			name:     "[0,1,2]",
			arr:      []uint32{0, 1, 2},
			isSorted: true,
		},
		{
			name:     "[0,0,1]",
			arr:      []uint32{0, 0, 1},
			isSorted: false,
		},
		{
			name:     "[0,1,1]",
			arr:      []uint32{0, 1, 1},
			isSorted: false,
		},
		{
			name:     "[2,1,2]",
			arr:      []uint32{2, 1, 2},
			isSorted: false,
		},
		{
			name:     "[2,1,3]",
			arr:      []uint32{2, 1, 3},
			isSorted: false,
		},
		{
			name:     "[0,10,20]",
			arr:      []uint32{0, 10, 20},
			isSorted: true,
		},
		{
			name:     "[10,20,25]",
			arr:      []uint32{10, 20, 25},
			isSorted: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.isSorted {
				if !IsSortedAndUniqueUint32(test.arr) {
					t.Fatal("should have been marked as sorted and unique")
				}
			} else if IsSortedAndUniqueUint32(test.arr) {
				t.Fatal("shouldn't have been marked as sorted and unique")
			}
		})
	}
}
