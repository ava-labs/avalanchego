// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
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
	require := require.New(t)
	ctx := context.Background()

	kvs := map[string]string{
		"dog":   "food",
		"cat":   "meow",
		"ALPHA": "BETA",
	}

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
