// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bag

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBagOf(t *testing.T) {
	tests := []struct {
		name           string
		elements       []int
		expectedCounts map[int]int
	}{
		{
			name:           "nil",
			elements:       nil,
			expectedCounts: map[int]int{},
		},
		{
			name:           "empty",
			elements:       []int{},
			expectedCounts: map[int]int{},
		},
		{
			name:     "unique elements",
			elements: []int{1, 2, 3},
			expectedCounts: map[int]int{
				1: 1,
				2: 1,
				3: 1,
			},
		},
		{
			name:     "duplicate elements",
			elements: []int{1, 2, 3, 1, 2, 3},
			expectedCounts: map[int]int{
				1: 2,
				2: 2,
				3: 2,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			b := Of(tt.elements...)

			require.Equal(len(tt.elements), b.Len())
			for entry, count := range tt.expectedCounts {
				require.Equal(count, b.Count(entry))
			}
		})
	}
}

func TestBagAdd(t *testing.T) {
	require := require.New(t)

	elt0 := 0
	elt1 := 1

	bag := Bag[int]{}

	require.Zero(bag.Count(elt0))
	require.Zero(bag.Count(elt1))
	require.Zero(bag.Len())
	require.Empty(bag.List())
	mode, freq := bag.Mode()
	require.Equal(elt0, mode)
	require.Zero(freq)
	require.Empty(bag.Threshold())

	bag.Add(elt0)

	require.Equal(1, bag.Count(elt0))
	require.Zero(bag.Count(elt1))
	require.Equal(1, bag.Len())
	require.Len(bag.List(), 1)
	mode, freq = bag.Mode()
	require.Equal(elt0, mode)
	require.Equal(1, freq)
	require.Len(bag.Threshold(), 1)

	bag.Add(elt0)

	require.Equal(2, bag.Count(elt0))
	require.Zero(bag.Count(elt1))
	require.Equal(2, bag.Len())
	require.Len(bag.List(), 1)
	mode, freq = bag.Mode()
	require.Equal(elt0, mode)
	require.Equal(2, freq)
	require.Len(bag.Threshold(), 1)

	bag.AddCount(elt1, 3)

	require.Equal(2, bag.Count(elt0))
	require.Equal(3, bag.Count(elt1))
	require.Equal(5, bag.Len())
	require.Len(bag.List(), 2)
	mode, freq = bag.Mode()
	require.Equal(elt1, mode)
	require.Equal(3, freq)
	require.Len(bag.Threshold(), 2)
}

func TestBagSetThreshold(t *testing.T) {
	require := require.New(t)

	elt0 := 0
	elt1 := 1

	bag := Bag[int]{}

	bag.AddCount(elt0, 2)
	bag.AddCount(elt1, 3)

	bag.SetThreshold(0)

	require.Equal(2, bag.Count(elt0))
	require.Equal(3, bag.Count(elt1))
	require.Equal(5, bag.Len())
	require.Len(bag.List(), 2)
	mode, freq := bag.Mode()
	require.Equal(elt1, mode)
	require.Equal(3, freq)
	require.Len(bag.Threshold(), 2)

	bag.SetThreshold(3)

	require.Equal(2, bag.Count(elt0))
	require.Equal(3, bag.Count(elt1))
	require.Equal(5, bag.Len())
	require.Len(bag.List(), 2)
	mode, freq = bag.Mode()
	require.Equal(elt1, mode)
	require.Equal(3, freq)
	require.Len(bag.Threshold(), 1)
}

func TestBagFilter(t *testing.T) {
	require := require.New(t)

	elt0 := 0
	elt1 := 1
	elt2 := 2

	bag := Bag[int]{}

	bag.AddCount(elt0, 1)
	bag.AddCount(elt1, 3)
	bag.AddCount(elt2, 5)

	filterFunc := func(elt int) bool {
		return elt%2 == 0
	}
	even := bag.Filter(filterFunc)

	require.Equal(1, even.Count(elt0))
	require.Zero(even.Count(elt1))
	require.Equal(5, even.Count(elt2))
}

func TestBagSplit(t *testing.T) {
	require := require.New(t)

	elt0 := 0
	elt1 := 1
	elt2 := 2

	bag := Bag[int]{}

	bag.AddCount(elt0, 1)
	bag.AddCount(elt1, 3)
	bag.AddCount(elt2, 5)

	bags := bag.Split(func(i int) bool {
		return i%2 != 0
	})

	evens := bags[0]
	odds := bags[1]

	require.Equal(1, evens.Count(elt0))
	require.Zero(evens.Count(elt1))
	require.Equal(5, evens.Count(elt2))
	require.Zero(odds.Count(elt0))
	require.Equal(3, odds.Count(elt1))
	require.Zero(odds.Count(elt2))
}

func TestBagString(t *testing.T) {
	elt0 := 123

	bag := Bag[int]{}

	bag.AddCount(elt0, 1337)

	expected := `Bag[int]: (Size = 1337)
    123: 1337`

	require.Equal(t, expected, bag.String())
}

func TestBagRemove(t *testing.T) {
	require := require.New(t)

	elt0 := 0
	elt1 := 1
	elt2 := 2

	bag := Bag[int]{}

	bag.Remove(elt0)
	require.Zero(bag.Len())

	bag.AddCount(elt0, 3)
	bag.AddCount(elt1, 2)
	bag.Add(elt2)
	require.Equal(6, bag.Len())
	require.Len(bag.counts, 3)
	mode, freq := bag.Mode()
	require.Equal(elt0, mode)
	require.Equal(3, freq)

	bag.Remove(elt0)

	require.Zero(bag.Count(elt0))
	require.Equal(2, bag.Count(elt1))
	require.Equal(1, bag.Count(elt2))
	require.Equal(3, bag.Len())
	require.Len(bag.counts, 2)
	mode, freq = bag.Mode()
	require.Equal(elt1, mode)
	require.Equal(2, freq)

	bag.Remove(elt1)
	require.Zero(bag.Count(elt0))
	require.Zero(bag.Count(elt1))
	require.Equal(1, bag.Count(elt2))
	require.Equal(1, bag.Len())
	require.Len(bag.counts, 1)
	mode, freq = bag.Mode()
	require.Equal(elt2, mode)
	require.Equal(1, freq)
}

func TestBagEquals(t *testing.T) {
	require := require.New(t)

	bag1 := Bag[int]{}
	bag2 := Bag[int]{}

	// Case: both empty
	require.True(bag1.Equals(bag2))
	require.True(bag2.Equals(bag1))

	// Case: one empty, one not
	bag1.Add(0)
	require.False(bag1.Equals(bag2))
	require.False(bag2.Equals(bag1))

	bag2.Add(0)
	require.True(bag1.Equals(bag2))
	require.True(bag2.Equals(bag1))

	// Case: both non-empty, different elements
	bag1.Add(1)
	require.False(bag1.Equals(bag2))
	require.False(bag2.Equals(bag1))

	bag2.Add(1)
	require.True(bag1.Equals(bag2))
	require.True(bag2.Equals(bag1))

	// Case: both non-empty, different counts
	bag1.Add(0)
	require.False(bag1.Equals(bag2))
	require.False(bag2.Equals(bag1))

	bag2.Add(0)
	require.True(bag1.Equals(bag2))
	require.True(bag2.Equals(bag1))
}

func TestBagClone(t *testing.T) {
	require := require.New(t)
	bag := Bag[int]{}
	bag.AddCount(100, 1)
	bag.AddCount(200, 2)
	bag.AddCount(300, 3)

	clonedBag := bag.Clone()

	require.Equal(bag, clonedBag)
}
