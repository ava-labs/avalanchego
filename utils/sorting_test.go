// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"cmp"
	"testing"

	"github.com/stretchr/testify/require"
)

var _ Sortable[sortable] = sortable(0)

type sortable int

func (s sortable) Compare(other sortable) int {
	return cmp.Compare(s, other)
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
