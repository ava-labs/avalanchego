// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package math

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

const maxUint64 uint64 = math.MaxUint64

func TestMax(t *testing.T) {
	require := require.New(t)

	require.Equal(maxUint64, Max(0, maxUint64))
	require.Equal(maxUint64, Max(maxUint64, 0))
	require.Equal(1, Max(1, 0))
	require.Equal(1, Max(0, 1))
	require.Equal(0, Max(0, 0))
	require.Equal(2, Max(2, 2))
}

func TestMin(t *testing.T) {
	require := require.New(t)

	require.Equal(uint64(0), Min(uint64(0), maxUint64))
	require.Equal(uint64(0), Min(maxUint64, uint64(0)))
	require.Equal(0, Min(1, 0))
	require.Equal(0, Min(0, 1))
	require.Equal(0, Min(0, 0))
	require.Equal(2, Min(2, 2))
	require.Equal(1, Min(1, 2))
}

func TestAdd64(t *testing.T) {
	require := require.New(t)

	sum, err := Add64(0, maxUint64)
	require.NoError(err)
	require.Equal(maxUint64, sum)

	sum, err = Add64(maxUint64, 0)
	require.NoError(err)
	require.Equal(maxUint64, sum)

	sum, err = Add64(uint64(1<<62), uint64(1<<62))
	require.NoError(err)
	require.Equal(uint64(1<<63), sum)

	_, err = Add64(1, maxUint64)
	require.ErrorIs(err, ErrOverflow)

	_, err = Add64(maxUint64, 1)
	require.ErrorIs(err, ErrOverflow)

	_, err = Add64(maxUint64, maxUint64)
	require.ErrorIs(err, ErrOverflow)
}

func TestSub(t *testing.T) {
	require := require.New(t)

	got, err := Sub(uint64(2), uint64(1))
	require.NoError(err)
	require.Equal(uint64(1), got)

	got, err = Sub(uint64(2), uint64(2))
	require.NoError(err)
	require.Equal(uint64(0), got)

	got, err = Sub(maxUint64, maxUint64)
	require.NoError(err)
	require.Equal(uint64(0), got)

	got, err = Sub(uint64(3), uint64(2))
	require.NoError(err)
	require.Equal(uint64(1), got)

	_, err = Sub(uint64(1), uint64(2))
	require.ErrorIs(err, ErrUnderflow)

	_, err = Sub(maxUint64-1, maxUint64)
	require.ErrorIs(err, ErrUnderflow)
}

func TestMul64(t *testing.T) {
	require := require.New(t)

	got, err := Mul64(0, maxUint64)
	require.NoError(err)
	require.Equal(uint64(0), got)

	got, err = Mul64(maxUint64, 0)
	require.NoError(err)
	require.Equal(uint64(0), got)

	got, err = Mul64(uint64(1), uint64(3))
	require.NoError(err)
	require.Equal(uint64(3), got)

	got, err = Mul64(uint64(3), uint64(1))
	require.NoError(err)
	require.Equal(uint64(3), got)

	got, err = Mul64(uint64(2), uint64(3))
	require.NoError(err)
	require.Equal(uint64(6), got)

	got, err = Mul64(maxUint64, 0)
	require.NoError(err)
	require.Equal(uint64(0), got)

	_, err = Mul64(maxUint64-1, 2)
	require.ErrorIs(err, ErrOverflow)
}

func TestAbsDiff(t *testing.T) {
	require := require.New(t)

	require.Equal(maxUint64, AbsDiff(0, maxUint64))
	require.Equal(maxUint64, AbsDiff(maxUint64, 0))
	require.Equal(uint64(2), AbsDiff(uint64(3), uint64(1)))
	require.Equal(uint64(2), AbsDiff(uint64(1), uint64(3)))
	require.Equal(uint64(0), AbsDiff(uint64(1), uint64(1)))
	require.Equal(uint64(0), AbsDiff(uint64(0), uint64(0)))
}
