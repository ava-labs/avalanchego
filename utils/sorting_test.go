// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type sortable int

func (s sortable) Less(other sortable) bool {
	return s < other
}

func TestSortSliceSortable(t *testing.T) {
	require := require.New(t)

	var s []sortable
	SortSliceSortable(s)
	require.True(IsSortedAndUniqueSortable(s))
	require.Equal(0, len(s))

	s = []sortable{1}
	SortSliceSortable(s)
	require.True(IsSortedAndUniqueSortable(s))
	require.Equal([]sortable{1}, s)

	s = []sortable{1, 1}
	SortSliceSortable(s)
	require.Equal([]sortable{1, 1}, s)

	s = []sortable{1, 2}
	SortSliceSortable(s)
	require.True(IsSortedAndUniqueSortable(s))
	require.Equal([]sortable{1, 2}, s)

	s = []sortable{2, 1}
	SortSliceSortable(s)
	require.True(IsSortedAndUniqueSortable(s))
	require.Equal([]sortable{1, 2}, s)

	s = []sortable{1, 2, 1}
	SortSliceSortable(s)
	require.Equal([]sortable{1, 1, 2}, s)

	s = []sortable{2, 1, 2}
	SortSliceSortable(s)
	require.Equal([]sortable{1, 2, 2}, s)

	s = []sortable{3, 1, 2}
	SortSliceSortable(s)
	require.Equal([]sortable{1, 2, 3}, s)
}

func TestIsSortedAndUniqueSortable(t *testing.T) {
	require := require.New(t)

	var s []sortable
	require.True(IsSortedAndUniqueSortable(s))

	s = []sortable{}
	require.True(IsSortedAndUniqueSortable(s))

	s = []sortable{1}
	require.True(IsSortedAndUniqueSortable(s))

	s = []sortable{1, 2}
	require.True(IsSortedAndUniqueSortable(s))

	s = []sortable{1, 1}
	require.False(IsSortedAndUniqueSortable(s))

	s = []sortable{2, 1}
	require.False(IsSortedAndUniqueSortable(s))

	s = []sortable{1, 2, 1}
	require.False(IsSortedAndUniqueSortable(s))

	s = []sortable{1, 2, 0}
	require.False(IsSortedAndUniqueSortable(s))
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
