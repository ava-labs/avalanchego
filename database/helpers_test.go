// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"golang.org/x/exp/slices"

	"github.com/ava-labs/avalanchego/utils"
)

func TestSortednessUint64(t *testing.T) {
	ints := make([]uint64, 1024)
	for i := range ints {
		ints[i] = rand.Uint64() //#nosec G404
	}
	slices.Sort(ints)

	intBytes := make([][]byte, 1024)
	for i, val := range ints {
		intBytes[i] = PackUInt64(val)
	}
	require.True(t, utils.IsSortedBytes(intBytes))
}

func TestSortednessUint32(t *testing.T) {
	ints := make([]uint32, 1024)
	for i := range ints {
		ints[i] = rand.Uint32() //#nosec G404
	}
	slices.Sort(ints)

	intBytes := make([][]byte, 1024)
	for i, val := range ints {
		intBytes[i] = PackUInt32(val)
	}
	require.True(t, utils.IsSortedBytes(intBytes))
}
