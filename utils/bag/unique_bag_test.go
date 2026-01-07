// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bag

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/set"
)

func TestUniqueBag(t *testing.T) {
	require := require.New(t)

	var ub1 UniqueBag[int]
	ub1.init()
	require.NotNil(ub1)
	require.Empty(ub1)

	elt1 := 1
	elt2 := 2

	ub2 := make(UniqueBag[int])
	ub2.Add(1, elt1, elt2)
	require.True(ub2.GetSet(elt1).Contains(1))
	require.True(ub2.GetSet(elt2).Contains(1))

	var bs1 set.Bits64
	bs1.Add(2)
	bs1.Add(4)

	ub3 := make(UniqueBag[int])

	ub3.UnionSet(elt1, bs1)

	bs1.Clear()
	bs1 = ub3.GetSet(elt1)
	require.Equal(2, bs1.Len())
	require.True(bs1.Contains(2))
	require.True(bs1.Contains(4))

	// Difference test
	bs1.Clear()

	ub4 := make(UniqueBag[int])
	ub4.Add(1, elt1)
	ub4.Add(2, elt1)
	ub4.Add(5, elt2)
	ub4.Add(8, elt2)

	ub5 := make(UniqueBag[int])
	ub5.Add(5, elt2)
	ub5.Add(5, elt1)
	require.Len(ub5.List(), 2)

	ub4.Difference(&ub5)

	ub4elt1 := ub4.GetSet(elt1)
	require.Equal(2, ub4elt1.Len())
	require.True(ub4elt1.Contains(1))
	require.True(ub4elt1.Contains(2))

	ub4elt2 := ub4.GetSet(elt2)
	require.Equal(1, ub4elt2.Len())
	require.True(ub4elt2.Contains(8))

	// DifferenceSet test

	ub6 := make(UniqueBag[int])
	ub6.Add(1, elt1)
	ub6.Add(2, elt1)
	ub6.Add(7, elt1)

	diffBitSet := set.Bits64(0)
	diffBitSet.Add(1)
	diffBitSet.Add(7)

	ub6.DifferenceSet(elt1, diffBitSet)

	ub6elt1 := ub6.GetSet(elt1)
	require.Equal(1, ub6elt1.Len())
	require.True(ub6elt1.Contains(2))
}

func TestUniqueBagClear(t *testing.T) {
	require := require.New(t)

	b := UniqueBag[int]{}
	elt1, elt2 := 0, 1
	b.Add(0, elt1)
	b.Add(1, elt1, elt2)

	b.Clear()
	require.Empty(b.List())

	bs := b.GetSet(elt1)
	require.Zero(bs.Len())

	bs = b.GetSet(elt2)
	require.Zero(bs.Len())
}
