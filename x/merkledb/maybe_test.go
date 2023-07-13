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
		mClone := Clone(m)
		m.value[0] = 0
		require.NotEqual(mClone.value, m.value)
		require.Equal(originalVal, mClone.value)
	}

	// Case: Value is nothing
	{
		m := Nothing[[]byte]()
		mClone := Clone(m)
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

func TestMaybeBytesEquals(t *testing.T) {
	type test struct {
		name     string
		a        Maybe[[]byte]
		b        Maybe[[]byte]
		expected bool
	}

	tests := []test{
		{
			name:     "a and b are both nothing",
			a:        Nothing[[]byte](),
			b:        Nothing[[]byte](),
			expected: true,
		},
		{
			name:     "a is nothing and b is something",
			a:        Nothing[[]byte](),
			b:        Some([]byte{1, 2, 3}),
			expected: false,
		},
		{
			name:     "a is something and b is nothing",
			a:        Some([]byte{1, 2, 3}),
			b:        Nothing[[]byte](),
			expected: false,
		},
		{
			name:     "a and b are the same something",
			a:        Some([]byte{1, 2, 3}),
			b:        Some([]byte{1, 2, 3}),
			expected: true,
		},
		{
			name:     "a and b are different somethings",
			a:        Some([]byte{1, 2, 3}),
			b:        Some([]byte{1, 2, 4}),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			require.Equal(tt.expected, MaybeBytesEquals(tt.a, tt.b))
		})
	}
}
