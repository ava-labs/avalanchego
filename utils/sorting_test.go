// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var _ Sortable[sortable] = sortable(0)

type sortable int

func (s sortable) Less(other sortable) bool {
	return s < other
}

func TestSortSliceSortable(t *testing.T) {
	require := require.New(t)

	var s []sortable
	Sort(s)
	require.True(IsSortedAndUnique(s))
	require.Empty(s)

	s = []sortable{1}
	Sort(s)
	require.True(IsSortedAndUnique(s))
	require.Equal([]sortable{1}, s)

	s = []sortable{1, 1}
	Sort(s)
	require.Equal([]sortable{1, 1}, s)

	s = []sortable{1, 2}
	Sort(s)
	require.True(IsSortedAndUnique(s))
	require.Equal([]sortable{1, 2}, s)

	s = []sortable{2, 1}
	Sort(s)
	require.True(IsSortedAndUnique(s))
	require.Equal([]sortable{1, 2}, s)

	s = []sortable{1, 2, 1}
	Sort(s)
	require.Equal([]sortable{1, 1, 2}, s)

	s = []sortable{2, 1, 2}
	Sort(s)
	require.Equal([]sortable{1, 2, 2}, s)

	s = []sortable{3, 1, 2}
	Sort(s)
	require.Equal([]sortable{1, 2, 3}, s)
}

func TestSortBytesIsSortedBytes(t *testing.T) {
	require := require.New(t)

	seed := time.Now().UnixNano()
	t.Log("Seed: ", seed)
	rand := rand.New(rand.NewSource(seed)) //#nosec G404

	slices := make([][]byte, 1024)
	for j := 0; j < len(slices); j++ {
		slices[j] = make([]byte, 32)
		_, _ = rand.Read(slices[j])
	}
	require.False(IsSortedBytes(slices))
	SortBytes(slices)
	require.True(IsSortedBytes(slices))
}

func TestIsSortedAndUniqueSortable(t *testing.T) {
	require := require.New(t)

	var s []sortable
	require.True(IsSortedAndUnique(s))

	s = []sortable{}
	require.True(IsSortedAndUnique(s))

	s = []sortable{1}
	require.True(IsSortedAndUnique(s))

	s = []sortable{1, 2}
	require.True(IsSortedAndUnique(s))

	s = []sortable{1, 1}
	require.False(IsSortedAndUnique(s))

	s = []sortable{2, 1}
	require.False(IsSortedAndUnique(s))

	s = []sortable{1, 2, 1}
	require.False(IsSortedAndUnique(s))

	s = []sortable{1, 2, 0}
	require.False(IsSortedAndUnique(s))
}

func TestIsUnique(t *testing.T) {
	require := require.New(t)

	var s []int
	require.True(IsUnique(s))

	s = []int{}
	require.True(IsUnique(s))

	s = []int{1}
	require.True(IsUnique(s))

	s = []int{1, 2}
	require.True(IsUnique(s))

	s = []int{1, 1}
	require.False(IsUnique(s))

	s = []int{2, 1}
	require.True(IsUnique(s))

	s = []int{1, 2, 1}
	require.False(IsUnique(s))
}

func TestSortByHash(t *testing.T) {
	require := require.New(t)

	s := [][]byte{}
	SortByHash(s)
	require.Empty(s)

	s = [][]byte{{1}}
	SortByHash(s)
	require.Len(s, 1)
	require.Equal([]byte{1}, s[0])

	s = [][]byte{{1}, {2}}
	SortByHash(s)
	require.Len(s, 2)
	require.Equal([]byte{1}, s[0])
	require.Equal([]byte{2}, s[1])

	for i := byte(0); i < 100; i++ {
		s = [][]byte{{i}, {i + 1}, {i + 2}}
		SortByHash(s)
		require.Len(s, 3)
		require.True(IsSortedAndUniqueByHash(s))
	}
}
