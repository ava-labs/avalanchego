// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"fmt"
	"math/rand"
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/maybe"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/stretchr/testify/require"
)

func hashToID(h common.Hash) ids.ID {
	var result ids.ID
	copy(result[:], h[:])
	return result
}

func TestAltRootSimple(t *testing.T) {
	tests := []map[string]string{
		{},
		{
			"dog": "food",
		},
		{
			"dog": "food",
			"cat": "meow",
		},
		{
			"dog":   "food",
			"cat":   "meow",
			"ALPHA": "BETA",
		},
		{
			"a":  "abc",
			"c":  "def",
			"cd": "xyz",
		},
	}
	for i, test := range tests {
		name := fmt.Sprintf("case %d", i)
		t.Run(name, func(t *testing.T) {
			testAltRoot(t, test)
		})
	}
}

func genMap(rand *rand.Rand, nKeys int) map[string]string {
	maxKeyLen := 100
	maxValLen := 100
	kvs := make(map[string]string, nKeys)
	for len(kvs) < nKeys {
		keyLen := rand.Intn(maxKeyLen) + 1
		if keyLen == 32 {
			// Don't allow keys of length 32, since that's the length of an account key.
			keyLen += 1
		}
		valLen := rand.Intn(maxValLen) + 1

		key := utils.RandomBytes(keyLen)
		val := utils.RandomBytes(valLen)

		kvs[string(key)] = string(val)
	}
	return kvs
}

func TestAltRootRandom(t *testing.T) {
	maxNumKeys := 2048
	for i := 0; i < 200; i++ {
		numKeys := rand.Intn(maxNumKeys) //nolint:gosec
		name := fmt.Sprintf("case %d", i)
		rand := rand.New(rand.NewSource(int64(i))) //nolint:gosec
		kvs := genMap(rand, numKeys)
		t.Run(name, func(t *testing.T) {
			testAltRoot(t, kvs)
		})
	}
}

func testAltRoot(t *testing.T, kvs map[string]string) {
	require := require.New(t)
	ctx := context.Background()

	ethTrie := trie.NewEmpty(trie.NewDatabase(rawdb.NewMemoryDatabase(), nil))

	kvStore := memdb.New()
	db, err := getBasicDBWithRlpHasher(kvStore)
	require.NoError(err)

	vcs := ViewChanges{
		MapOps: make(map[string]maybe.Maybe[[]byte]),
	}
	for k, v := range kvs {
		keyBytes, valBytes := []byte(k), []byte(v)
		require.NoError(ethTrie.Update(keyBytes, valBytes))

		vcs.MapOps[k] = maybe.Some(valBytes)
	}

	trieView, err := db.NewView(ctx, vcs)
	require.NoError(err)

	ethRootID := hashToID(ethTrie.Hash())
	altRootID, err := trieView.GetMerkleRoot(ctx)
	require.NoError(err)

	require.Equal(ethRootID, altRootID)
	require.NoError(trieView.CommitToDB(ctx))
	dbRoot, err := db.GetMerkleRoot(ctx)
	require.NoError(err)
	require.Equal(ethRootID, dbRoot)

	require.NoError(db.Close())

	// Make sure the database is still correct after closing and reopening it.
	db, err = getBasicDBWithRlpHasher(kvStore)
	require.NoError(err)
	dbRoot, err = db.GetMerkleRoot(ctx)
	require.NoError(err)
	require.Equal(ethRootID, dbRoot)
}

func getBasicDBWithRlpHasher(db database.Database) (*merkleDB, error) {
	config := newDefaultConfig().WithHasher(&rlpHasher{})
	return getBasicDBWithConfig(db, config)
}
