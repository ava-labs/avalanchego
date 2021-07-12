// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"encoding/binary"
	"testing"
)

func TestRollupWindow(t *testing.T) {
	type test struct {
		longs []uint64
		roll  int
	}

	var tests []test = []test{
		{
			[]uint64{1, 2, 3, 4},
			0,
		},
		{
			[]uint64{1, 2, 3, 4},
			1,
		},
		{
			[]uint64{1, 2, 3, 4},
			2,
		},
		{
			[]uint64{1, 2, 3, 4},
			3,
		},
		{
			[]uint64{1, 2, 3, 4},
			4,
		},
		{
			[]uint64{1, 2, 3, 4},
			5,
		},
		{
			[]uint64{121, 232, 432},
			2,
		},
	}

	for _, test := range tests {
		testRollup(t, test.longs, test.roll)
	}
}

func testRollup(t *testing.T, longs []uint64, roll int) {
	slice := make([]byte, len(longs)*8)
	numLongs := len(longs)
	for i := 0; i < numLongs; i++ {
		binary.BigEndian.PutUint64(slice[8*i:], longs[i])
	}

	newSlice := rollWindow(slice, 8, roll)
	// numCopies is the number of longs that should have been copied over from the previous
	// slice as opposed to being left empty.
	numCopies := numLongs - roll
	for i := 0; i < numLongs; i++ {
		// Extract the long value that is encoded at position [i] in [newSlice]
		num := binary.BigEndian.Uint64(newSlice[8*i:])
		// If the current index is past the point where we should have copied the value
		// over from the previous slice, assert that the value encoded in [newSlice]
		// is 0
		if i >= numCopies {
			if num != 0 {
				t.Errorf("Expected num encoded in newSlice at position %d to be 0, but found %d", i, num)
			}
		} else {
			// Otherwise, check that the value was copied over correctly
			prevIndex := i + roll
			prevNum := longs[prevIndex]
			if prevNum != num {
				t.Errorf("Expected num encoded in new slice at position %d to be %d, but found %d", i, prevNum, num)
			}
		}
	}
}
