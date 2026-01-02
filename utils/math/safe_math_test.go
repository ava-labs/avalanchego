// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package math

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

const maxUint64 uint64 = math.MaxUint64

func TestMaxUint(t *testing.T) {
	require := require.New(t)

	require.Equal(uint(math.MaxUint), MaxUint[uint]())
	require.Equal(uint8(math.MaxUint8), MaxUint[uint8]())
	require.Equal(uint16(math.MaxUint16), MaxUint[uint16]())
	require.Equal(uint32(math.MaxUint32), MaxUint[uint32]())
	require.Equal(uint64(math.MaxUint64), MaxUint[uint64]())
	require.Equal(uintptr(math.MaxUint), MaxUint[uintptr]())
}

func TestAdd(t *testing.T) {
	require := require.New(t)

	sum, err := Add(0, maxUint64)
	require.NoError(err)
	require.Equal(maxUint64, sum)

	sum, err = Add(maxUint64, 0)
	require.NoError(err)
	require.Equal(maxUint64, sum)

	sum, err = Add(uint64(1<<62), uint64(1<<62))
	require.NoError(err)
	require.Equal(uint64(1<<63), sum)

	_, err = Add(1, maxUint64)
	require.ErrorIs(err, ErrOverflow)

	_, err = Add(maxUint64, 1)
	require.ErrorIs(err, ErrOverflow)

	_, err = Add(maxUint64, maxUint64)
	require.ErrorIs(err, ErrOverflow)
}

func TestSub(t *testing.T) {
	require := require.New(t)

	got, err := Sub(uint64(2), uint64(1))
	require.NoError(err)
	require.Equal(uint64(1), got)

	got, err = Sub(uint64(2), uint64(2))
	require.NoError(err)
	require.Zero(got)

	got, err = Sub(maxUint64, maxUint64)
	require.NoError(err)
	require.Zero(got)

	got, err = Sub(uint64(3), uint64(2))
	require.NoError(err)
	require.Equal(uint64(1), got)

	_, err = Sub(uint64(1), uint64(2))
	require.ErrorIs(err, ErrUnderflow)

	_, err = Sub(maxUint64-1, maxUint64)
	require.ErrorIs(err, ErrUnderflow)
}

func TestMul(t *testing.T) {
	require := require.New(t)

	got, err := Mul(0, maxUint64)
	require.NoError(err)
	require.Zero(got)

	got, err = Mul(maxUint64, 0)
	require.NoError(err)
	require.Zero(got)

	got, err = Mul(uint64(1), uint64(3))
	require.NoError(err)
	require.Equal(uint64(3), got)

	got, err = Mul(uint64(3), uint64(1))
	require.NoError(err)
	require.Equal(uint64(3), got)

	got, err = Mul(uint64(2), uint64(3))
	require.NoError(err)
	require.Equal(uint64(6), got)

	got, err = Mul(maxUint64, 0)
	require.NoError(err)
	require.Zero(got)

	_, err = Mul(maxUint64-1, 2)
	require.ErrorIs(err, ErrOverflow)
}

func TestAbsDiff(t *testing.T) {
	require := require.New(t)

	require.Equal(maxUint64, AbsDiff(0, maxUint64))
	require.Equal(maxUint64, AbsDiff(maxUint64, 0))
	require.Equal(uint64(2), AbsDiff(uint64(3), uint64(1)))
	require.Equal(uint64(2), AbsDiff(uint64(1), uint64(3)))
	require.Zero(AbsDiff(uint64(1), uint64(1)))
	require.Zero(AbsDiff(uint64(0), uint64(0)))
}
