// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"sort"
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

func TestSortSliceOrdered(t *testing.T) {
	require := require.New(t)

	var s []int
	SortSliceOrdered(s)
	require.True(IsSortedAndUniqueOrdered(s))
	require.Equal(0, len(s))

	s = []int{1}
	SortSliceOrdered(s)
	require.True(IsSortedAndUniqueOrdered(s))
	require.Equal([]int{1}, s)

	s = []int{1, 2}
	SortSliceOrdered(s)
	require.True(IsSortedAndUniqueOrdered(s))
	require.Equal([]int{1, 2}, s)

	s = []int{2, 1}
	SortSliceOrdered(s)
	require.True(IsSortedAndUniqueOrdered(s))
	require.Equal([]int{1, 2}, s)

	s = []int{1, 2, 1}
	SortSliceOrdered(s)
	require.Equal([]int{1, 1, 2}, s)

	s = []int{2, 1, 3}
	SortSliceOrdered(s)
	require.Equal([]int{1, 2, 3}, s)
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

func TestSort2DByteSliceAndIsSorted2DByteSlice(t *testing.T) {
	require := require.New(t)

	var s [][]byte
	Sort2DByteSlice(s)
	require.True(IsSorted2DByteSlice(s))

	s = [][]byte{{1}}
	Sort2DByteSlice(s)
	require.True(IsSorted2DByteSlice(s))
	require.Equal([]byte{1}, s[0])

	s = [][]byte{{1}, {2}}
	Sort2DByteSlice(s)
	require.True(IsSorted2DByteSlice(s))
	require.Equal([]byte{1}, s[0])
	require.Equal([]byte{2}, s[1])

	s = [][]byte{{1}, {1}}
	Sort2DByteSlice(s)
	require.True(IsSorted2DByteSlice(s))
	require.Equal([]byte{1}, s[0])
	require.Equal([]byte{1}, s[1])

	s = [][]byte{{2}, {1}}
	Sort2DByteSlice(s)
	require.True(IsSorted2DByteSlice(s))
	require.Equal([]byte{1}, s[0])
	require.Equal([]byte{2}, s[1])

	s = [][]byte{{1}, {2}, {3}}
	Sort2DByteSlice(s)
	require.True(IsSorted2DByteSlice(s))
	require.Equal([]byte{1}, s[0])
	require.Equal([]byte{2}, s[1])
	require.Equal([]byte{3}, s[2])

	s = [][]byte{{2}, {1}, {2}}
	Sort2DByteSlice(s)
	require.True(IsSorted2DByteSlice(s))
	require.Equal([]byte{1}, s[0])
	require.Equal([]byte{2}, s[1])
	require.Equal([]byte{2}, s[2])
}

func TestSortByHashAndIsSortedAndUniqueByHash(t *testing.T) {
	require := require.New(t)

	var s [][]byte
	SortByHash(s)
	require.True(IsSortedAndUniqueByHash(s))
	require.Len(s, 0)

	s = [][]byte{{1}}
	SortByHash(s)
	require.True(IsSortedAndUniqueByHash(s))
	require.Equal([]byte{1}, s[0])

	s = [][]byte{{1}, {1}}
	SortByHash(s)
	require.False(IsSortedAndUniqueByHash(s))
	require.Equal([]byte{1}, s[0])
	require.Equal([]byte{1}, s[1])

	s = [][]byte{{1}, {2}}
	SortByHash(s)
	require.True(IsSortedAndUniqueByHash(s))
	require.Equal([]byte{1}, s[0])
	require.Equal([]byte{2}, s[1])

	s = [][]byte{{2}, {1}}
	SortByHash(s)
	require.True(IsSortedAndUniqueByHash(s))
	require.Equal([]byte{1}, s[0])
	require.Equal([]byte{2}, s[1])

	s = [][]byte{{2}, {2}}
	SortByHash(s)
	require.False(IsSortedAndUniqueByHash(s))
	require.Equal([]byte{2}, s[0])
	require.Equal([]byte{2}, s[1])

	s = [][]byte{{1}, {2}, {3}}
	SortByHash(s)
	require.True(IsSortedAndUniqueByHash(s))
	require.Equal([]byte{3}, s[0])
	require.Equal([]byte{1}, s[1])
	require.Equal([]byte{2}, s[2])

	s = [][]byte{{3}, {1}, {2}}
	SortByHash(s)
	require.True(IsSortedAndUniqueByHash(s))
	require.Equal([]byte{3}, s[0])
	require.Equal([]byte{1}, s[1])
	require.Equal([]byte{2}, s[2])

	s = [][]byte{{2}, {1}, {3}}
	SortByHash(s)
	require.True(IsSortedAndUniqueByHash(s))
	require.Equal([]byte{3}, s[0])
	require.Equal([]byte{1}, s[1])
	require.Equal([]byte{2}, s[2])

	s = [][]byte{{2}, {1}, {3}, {1}}
	SortByHash(s)
	require.False(IsSortedAndUniqueByHash(s))
	require.Equal([]byte{3}, s[0])
	require.Equal([]byte{1}, s[1])
	require.Equal([]byte{1}, s[2])
	require.Equal([]byte{2}, s[3])
}

func TestIsSortedAndUniqueOrdered(t *testing.T) {
	require := require.New(t)

	var s []int
	require.True(IsSortedAndUniqueOrdered(s))

	s = []int{}
	require.True(IsSortedAndUniqueOrdered(s))

	s = []int{1}
	require.True(IsSortedAndUniqueOrdered(s))

	s = []int{1, 2}
	require.True(IsSortedAndUniqueOrdered(s))

	s = []int{1, 1}
	require.False(IsSortedAndUniqueOrdered(s))

	s = []int{2, 1}
	require.False(IsSortedAndUniqueOrdered(s))

	s = []int{1, 2, 1}
	require.False(IsSortedAndUniqueOrdered(s))

	s = []int{1, 2, 0}
	require.False(IsSortedAndUniqueOrdered(s))

	s = []int{1, 2, 2}
	require.False(IsSortedAndUniqueOrdered(s))

	s = []int{2, 1, 2}
	require.False(IsSortedAndUniqueOrdered(s))
}

var _ sort.Interface = &sortableSlice{}

type sortableSlice []int

func (s sortableSlice) Len() int {
	return len(s)
}

func (s sortableSlice) Less(i, j int) bool {
	return s[i] < s[j]
}

func (s sortableSlice) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func TestIsSortedAndUnique(t *testing.T) {
	require := require.New(t)

	var s sortableSlice
	require.True(IsSortedAndUnique(s))

	s = sortableSlice{}
	require.True(IsSortedAndUnique(s))

	s = sortableSlice{1}
	require.True(IsSortedAndUnique(s))

	s = sortableSlice{1, 2}
	require.True(IsSortedAndUnique(s))

	s = sortableSlice{1, 1}
	require.False(IsSortedAndUnique(s))

	s = sortableSlice{2, 1}
	require.False(IsSortedAndUnique(s))

	s = sortableSlice{1, 2, 1}
	require.False(IsSortedAndUnique(s))

	s = sortableSlice{1, 2, 0}
	require.False(IsSortedAndUnique(s))

	s = sortableSlice{1, 2, 2}
	require.False(IsSortedAndUnique(s))

	s = sortableSlice{2, 1, 2}
	require.False(IsSortedAndUnique(s))

	s = sortableSlice{1, 2, 3}
	require.True(IsSortedAndUnique(s))
}
