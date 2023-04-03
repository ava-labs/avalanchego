// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"fmt"
	"math/rand"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func hashToID(h common.Hash) ids.ID {
	var result ids.ID
	copy(result[:], h[:])
	return result
}

func TestAltRootSimple(t *testing.T) {
	tests := []map[string]string{
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
	}
	for i, test := range tests {
		name := fmt.Sprintf("case %d", i)
		t.Run(name, func(t *testing.T) {
			testAltRoot(t, test)
		})
	}
}

func genMap(nKeys int) map[string]string {
	keyLen := 32
	valLen := 30
	kvs := make(map[string]string, nKeys)
	for len(kvs) < nKeys {
		key := utils.RandomBytes(keyLen)
		val := utils.RandomBytes(valLen)
		fmt.Println("val", common.Bytes2Hex(val))

		kvs[string(key)] = string(val)
	}
	return kvs
}

func TestAltRootRandom(t *testing.T) {
	numKeys := 2
	for i := 0; i < 1; i++ {
		name := fmt.Sprintf("case %d", i)
		rand.Seed(int64(i))
		kvs := genMap(numKeys)
		t.Run(name, func(t *testing.T) {
			testAltRoot(t, kvs)
		})
	}
}

func testAltRoot(t *testing.T, kvs map[string]string) {
	require := require.New(t)
	ctx := context.Background()

	ethTrie := trie.NewEmpty(trie.NewDatabase(rawdb.NewMemoryDatabase()))

	db, err := getBasicDB()
	require.NoError(err)

	trieViewIface, err := db.NewView()
	require.NoError(err)
	trieView := trieViewIface.(*trieView)

	for k, v := range kvs {
		keyBytes, valBytes := []byte(k), []byte(v)
		err := ethTrie.TryUpdate(keyBytes, valBytes)
		require.NoError(err)

		err = trieView.Insert(ctx, keyBytes, valBytes)
		require.NoError(err)
	}

	ethRootID := hashToID(ethTrie.Hash())
	altRootID, err := trieView.GetAltMerkleRoot(ctx)
	require.NoError(err)

	require.Equal(ethRootID, altRootID)
}
