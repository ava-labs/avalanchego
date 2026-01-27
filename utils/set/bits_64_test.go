// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package set

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBits64(t *testing.T) {
	require := require.New(t)

	var bs1 Bits64
	require.Empty(bs1)

	bs1.Add(5)
	require.Equal(1, bs1.Len())
	require.True(bs1.Contains(5))

	bs1.Add(10)
	require.Equal(2, bs1.Len())
	require.True(bs1.Contains(5))
	require.True(bs1.Contains(10))

	bs1.Add(10)
	require.Equal(2, bs1.Len())
	require.True(bs1.Contains(5))
	require.True(bs1.Contains(10))

	var bs2 Bits64
	require.Empty(bs2)

	bs2.Add(0)
	require.Equal(1, bs2.Len())
	require.True(bs2.Contains(0))

	bs2.Union(bs1)
	require.Equal(2, bs1.Len())
	require.True(bs1.Contains(5))
	require.True(bs1.Contains(10))
	require.Equal(3, bs2.Len())
	require.True(bs2.Contains(0))
	require.True(bs2.Contains(5))
	require.True(bs2.Contains(10))

	bs1.Clear()
	require.Empty(bs1)
	require.Equal(3, bs2.Len())
	require.True(bs2.Contains(0))
	require.True(bs2.Contains(5))
	require.True(bs2.Contains(10))

	bs1.Add(63)
	require.Equal(1, bs1.Len())
	require.True(bs1.Contains(63))

	bs1.Add(1)
	require.Equal(2, bs1.Len())
	require.True(bs1.Contains(1))
	require.True(bs1.Contains(63))

	bs1.Remove(63)
	require.Equal(1, bs1.Len())
	require.True(bs1.Contains(1))

	var bs3 Bits64
	require.Empty(bs3)

	bs3.Add(0)
	bs3.Add(2)
	bs3.Add(5)

	var bs4 Bits64
	require.Empty(bs4)

	bs4.Add(2)
	bs4.Add(5)

	bs3.Intersection(bs4)

	require.Equal(2, bs3.Len())
	require.True(bs3.Contains(2))
	require.True(bs3.Contains(5))
	require.Equal(2, bs4.Len())
	require.True(bs4.Contains(2))
	require.True(bs4.Contains(5))

	var bs5 Bits64
	require.Empty(bs5)

	bs5.Add(7)
	bs5.Add(11)
	bs5.Add(9)

	var bs6 Bits64
	require.Empty(bs6)

	bs6.Add(9)
	bs6.Add(11)

	bs5.Difference(bs6)
	require.Equal(1, bs5.Len())
	require.True(bs5.Contains(7))
	require.Equal(2, bs6.Len())
	require.True(bs6.Contains(9))
	require.True(bs6.Contains(11))
}

func TestBits64String(t *testing.T) {
	var bs Bits64

	bs.Add(17)

	require.Equal(t, "0000000000020000", bs.String())
}
