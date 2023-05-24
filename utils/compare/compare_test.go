// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package compare

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnsortedEquals(t *testing.T) {
	require := require.New(t)

	require.True(UnsortedEquals([]int{}, []int{}))
	require.True(UnsortedEquals(nil, []int{}))
	require.True(UnsortedEquals([]int{}, nil))
	require.False(UnsortedEquals([]int{1}, nil))
	require.False(UnsortedEquals(nil, []int{1}))
	require.True(UnsortedEquals([]int{1}, []int{1}))
	require.False(UnsortedEquals([]int{1, 2}, []int{}))
	require.False(UnsortedEquals([]int{1, 2}, []int{1}))
	require.False(UnsortedEquals([]int{1}, []int{1, 2}))
	require.True(UnsortedEquals([]int{2, 1}, []int{1, 2}))
	require.True(UnsortedEquals([]int{1, 2}, []int{2, 1}))
}
