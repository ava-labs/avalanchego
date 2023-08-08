// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/exp/slices"
)

func TestMaybeClone(t *testing.T) {
	require := require.New(t)

	// Case: Value is maybe
	{
		val := []byte{1, 2, 3}
		originalVal := slices.Clone(val)
		m := Some(val)
		mClone := MaybeBind(m, slices.Clone[[]byte])
		m.value[0] = 0
		require.NotEqual(mClone.value, m.value)
		require.Equal(originalVal, mClone.value)
	}

	// Case: Value is nothing
	{
		m := Nothing[[]byte]()
		mClone := MaybeBind(m, slices.Clone[[]byte])
		require.True(mClone.IsNothing())
	}
}

func TestMaybeEquality(t *testing.T) {
	require := require.New(t)
	require.True(MaybeEqual(Nothing[int](), Nothing[int](), func(i int, i2 int) bool {
		return i == i2
	}))
	require.False(MaybeEqual(Nothing[int](), Some(1), func(i int, i2 int) bool {
		return i == i2
	}))
	require.False(MaybeEqual(Some(1), Nothing[int](), func(i int, i2 int) bool {
		return i == i2
	}))
	require.True(MaybeEqual(Some(1), Some(1), func(i int, i2 int) bool {
		return i == i2
	}))
}
