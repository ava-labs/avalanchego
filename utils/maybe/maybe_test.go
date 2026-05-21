// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package maybe

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMaybeClone(t *testing.T) {
	require := require.New(t)

	// Case: Value is maybe
	{
		val := []byte{1, 2, 3}
		originalVal := slices.Clone(val)
		m := Some(val)
		mClone := Bind(m, slices.Clone[[]byte])
		m.value[0] = 0
		require.NotEqual(mClone.value, m.value)
		require.Equal(originalVal, mClone.value)
	}

	// Case: Value is nothing
	{
		m := Nothing[[]byte]()
		mClone := Bind(m, slices.Clone[[]byte])
		require.True(mClone.IsNothing())
	}
}

func TestMaybeString(t *testing.T) {
	require := require.New(t)

	// Case: Value is maybe
	{
		val := []int{1, 2, 3}
		m := Some(val)
		require.Equal("Some[[]int]{[1 2 3]}", m.String())
	}

	// Case: Value is nothing
	{
		m := Nothing[int]()
		require.Equal("Nothing[int]", m.String())
	}
}

func TestMaybeEquality(t *testing.T) {
	require := require.New(t)
	require.True(Equal(Nothing[int](), Nothing[int](), func(i int, i2 int) bool {
		return i == i2
	}))
	require.False(Equal(Nothing[int](), Some(1), func(i int, i2 int) bool {
		return i == i2
	}))
	require.False(Equal(Some(1), Nothing[int](), func(i int, i2 int) bool {
		return i == i2
	}))
	require.True(Equal(Some(1), Some(1), func(i int, i2 int) bool {
		return i == i2
	}))
}
