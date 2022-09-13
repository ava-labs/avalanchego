// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
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
	sum, err := Add64(0, maxUint64)
	if err != nil {
		t.Fatalf("Add64 failed unexpectedly")
	}
	if sum != maxUint64 {
		t.Fatalf("Expected %d, got %d", maxUint64, sum)
	}

	sum, err = Add64(maxUint64, 0)
	if err != nil {
		t.Fatalf("Add64 failed unexpectedly")
	}
	if sum != math.MaxUint64 {
		t.Fatalf("Expected %d, got %d", maxUint64, sum)
	}

	sum, err = Add64(uint64(1<<62), uint64(1<<62))
	if err != nil {
		t.Fatalf("Add64 failed unexpectedly")
	}
	if sum != uint64(1<<63) {
		t.Fatalf("Expected %d, got %d", uint64(1<<63), sum)
	}

	_, err = Add64(1, maxUint64)
	if err == nil {
		t.Fatalf("Add64 succeeded unexpectedly")
	}

	_, err = Add64(maxUint64, 1)
	if err == nil {
		t.Fatalf("Add64 succeeded unexpectedly")
	}

	_, err = Add64(maxUint64, maxUint64)
	if err == nil {
		t.Fatalf("Add64 succeeded unexpectedly")
	}
}

func TestSub(t *testing.T) {
	actual, err := Sub(uint64(2), uint64(1))
	if err != nil {
		t.Fatalf("Sub64 failed unexpectedly")
	} else if actual != 1 {
		t.Fatalf("Expected %d, got %d", 1, actual)
	}

	_, err = Sub(uint64(1), uint64(2))
	if err == nil {
		t.Fatalf("Sub64 did not fail in the manner expected")
	}
}

func TestMul64(t *testing.T) {
	if prod, err := Mul64(maxUint64, 0); err != nil {
		t.Fatalf("Mul64 failed unexpectedly")
	} else if prod != 0 {
		t.Fatalf("Mul64 returned wrong value")
	}

	if prod, err := Mul64(maxUint64, 1); err != nil {
		t.Fatalf("Mul64 failed unexpectedly")
	} else if prod != maxUint64 {
		t.Fatalf("Mul64 returned wrong value")
	}

	if _, err := Mul64(maxUint64-1, 2); err == nil {
		t.Fatalf("Mul64 overflowed")
	}
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
