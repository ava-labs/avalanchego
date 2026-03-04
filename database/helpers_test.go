// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database_test

import (
	"math/rand"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/utils"

	. "github.com/ava-labs/avalanchego/database"
)

func TestSortednessUint64(t *testing.T) {
	seed := time.Now().UnixNano()
	t.Log("Seed: ", seed)
	rand := rand.New(rand.NewSource(seed)) //#nosec G404

	ints := make([]uint64, 1024)
	for i := range ints {
		ints[i] = rand.Uint64()
	}
	slices.Sort(ints)

	intBytes := make([][]byte, 1024)
	for i, val := range ints {
		intBytes[i] = PackUInt64(val)
	}
	require.True(t, utils.IsSortedBytes(intBytes))
}

func TestSortednessUint32(t *testing.T) {
	seed := time.Now().UnixNano()
	t.Log("Seed: ", seed)
	rand := rand.New(rand.NewSource(seed)) //#nosec G404

	ints := make([]uint32, 1024)
	for i := range ints {
		ints[i] = rand.Uint32()
	}
	slices.Sort(ints)

	intBytes := make([][]byte, 1024)
	for i, val := range ints {
		intBytes[i] = PackUInt32(val)
	}
	require.True(t, utils.IsSortedBytes(intBytes))
}

func TestOrDefault(t *testing.T) {
	require := require.New(t)

	var (
		db  = memdb.New()
		key = utils.RandomBytes(32)
	)

	// Key doesn't exist
	v, err := WithDefault(GetUInt64, db, key, 1)
	require.NoError(err)
	require.Equal(uint64(1), v)

	require.NoError(PutUInt64(db, key, 2))

	// Key does exist
	v, err = WithDefault(GetUInt64, db, key, 1)
	require.NoError(err)
	require.Equal(uint64(2), v)
}
