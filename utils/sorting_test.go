// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"math/rand"
	"testing"
)

// TODO add tests

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
	if IsSorted2DByteSlice(arr) {
		arr[0], arr[len(arr)-1] = arr[len(arr)-1], arr[0]
	}
	Sort2DByteSlice(arr) // sort it
	if !IsSorted2DByteSlice(arr) {
		t.Fatal("should be sorted")
	}
}
