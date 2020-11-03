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
	arr := []uint32{}
	if !IsSortedAndUniqueUint32(arr) {
		t.Fatal("is sorted and unique")
	}

	arr = []uint32{0}
	if !IsSortedAndUniqueUint32(arr) {
		t.Fatal("is sorted and unique")
	}

	arr = []uint32{0, 0}
	if IsSortedAndUniqueUint32(arr) {
		t.Fatal("is not sorted and unique")
	}

	arr = []uint32{0, 1}
	if !IsSortedAndUniqueUint32(arr) {
		t.Fatal("is sorted and unique")
	}

	arr = []uint32{1, 0}
	if IsSortedAndUniqueUint32(arr) {
		t.Fatal("is not sorted and unique")
	}

	arr = []uint32{0, 1, 2}
	if !IsSortedAndUniqueUint32(arr) {
		t.Fatal("is sorted and unique")
	}

	arr = []uint32{0, 0, 1}
	if IsSortedAndUniqueUint32(arr) {
		t.Fatal("is not sorted and unique")
	}

	arr = []uint32{0, 1, 1}
	if IsSortedAndUniqueUint32(arr) {
		t.Fatal("is not sorted and unique")
	}

	arr = []uint32{2, 1, 0}
	if IsSortedAndUniqueUint32(arr) {
		t.Fatal("is not sorted and unique")
	}

	arr = []uint32{2, 1, 2}
	if IsSortedAndUniqueUint32(arr) {
		t.Fatal("is not sorted and unique")
	}

	arr = []uint32{2, 1, 3}
	if IsSortedAndUniqueUint32(arr) {
		t.Fatal("is not sorted and unique")
	}

	arr = []uint32{0, 10, 20}
	if !IsSortedAndUniqueUint32(arr) {
		t.Fatal("is sorted and unique")
	}

	arr = []uint32{10, 20, 25}
	if !IsSortedAndUniqueUint32(arr) {
		t.Fatal("is sorted and unique")
	}
}
