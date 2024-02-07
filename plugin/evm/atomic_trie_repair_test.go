// (c) 2020-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"slices"
	"testing"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/maps"
)

type atomicTrieRepairTest struct {
	setup                   func(a *atomicTrie, db *versiondb.Database)
	expectedHeightsRepaired int
}

func TestAtomicTrieRepair(t *testing.T) {
	require := require.New(t)
	for name, test := range map[string]atomicTrieRepairTest{
		"needs repair": {
			setup:                   func(a *atomicTrie, db *versiondb.Database) {},
			expectedHeightsRepaired: len(mainnetBonusBlocksParsed),
		},
		"should not be repaired twice": {
			setup: func(a *atomicTrie, db *versiondb.Database) {
				_, err := a.repairAtomicTrie(bonusBlockMainnetHeights, mainnetBonusBlocksParsed)
				require.NoError(err)
				require.NoError(db.Commit())
			},
			expectedHeightsRepaired: 0,
		},
		"did not need repair": {
			setup: func(a *atomicTrie, db *versiondb.Database) {
				// simulates a node that has the bonus blocks in the atomic trie
				// but has not yet run the repair
				_, err := a.repairAtomicTrie(bonusBlockMainnetHeights, mainnetBonusBlocksParsed)
				require.NoError(err)
				require.NoError(a.metadataDB.Delete(repairedKey))
				require.NoError(db.Commit())
			},
			expectedHeightsRepaired: len(mainnetBonusBlocksParsed),
		},
	} {
		t.Run(name, test.test)
	}
}

func (test atomicTrieRepairTest) test(t *testing.T) {
	require := require.New(t)
	commitInterval := uint64(4096)

	// create an unrepaired atomic trie for setup
	db := versiondb.New(memdb.New())
	repo, err := NewAtomicTxRepository(db, Codec, 0, nil)
	require.NoError(err)
	atomicBackend, err := NewAtomicBackend(db, testSharedMemory(), nil, repo, 0, common.Hash{}, commitInterval)
	require.NoError(err)
	a := atomicBackend.AtomicTrie().(*atomicTrie)

	// make a commit at a height larger than all bonus blocks
	maxBonusBlockHeight := slices.Max(maps.Keys(mainnetBonusBlocksParsed))
	commitHeight := nearestCommitHeight(maxBonusBlockHeight, commitInterval) + commitInterval
	err = a.commit(commitHeight, types.EmptyRootHash)
	require.NoError(err)
	require.NoError(db.Commit())

	// perform additional setup
	test.setup(a, db)

	// recreate the trie with the repair constructor to test the repair
	var heightsRepaired int
	atomicBackend, heightsRepaired, err = NewAtomicBackendWithBonusBlockRepair(
		db, testSharedMemory(), bonusBlockMainnetHeights, mainnetBonusBlocksParsed,
		repo, commitHeight, common.Hash{}, commitInterval,
	)
	require.NoError(err)
	require.Equal(test.expectedHeightsRepaired, heightsRepaired)

	// call Abort to make sure the repair has called Commit
	db.Abort()
	// verify the trie is repaired
	verifyAtomicTrieIsAlreadyRepaired(require, db, repo, commitHeight, commitInterval)
}

func verifyAtomicTrieIsAlreadyRepaired(
	require *require.Assertions, db *versiondb.Database, repo *atomicTxRepository,
	commitHeight uint64, commitInterval uint64,
) {
	// create a map to track the expected items in the atomic trie.
	// note we serialize the atomic ops to bytes so we can compare nil
	// and empty slices as equal
	expectedKeys := 0
	expected := make(map[uint64]map[ids.ID][]byte)
	for height, block := range mainnetBonusBlocksParsed {
		txs, err := ExtractAtomicTxs(block.ExtData(), false, Codec)
		require.NoError(err)

		requests := make(map[ids.ID][]byte)
		ops, err := mergeAtomicOps(txs)
		require.NoError(err)
		for id, op := range ops {
			bytes, err := Codec.Marshal(codecVersion, op)
			require.NoError(err)
			requests[id] = bytes
			expectedKeys++
		}
		expected[height] = requests
	}

	atomicBackend, heightsRepaired, err := NewAtomicBackendWithBonusBlockRepair(
		db, testSharedMemory(), bonusBlockMainnetHeights, mainnetBonusBlocksParsed,
		repo, commitHeight, common.Hash{}, commitInterval,
	)
	require.NoError(err)
	a := atomicBackend.AtomicTrie().(*atomicTrie)
	require.NoError(err)
	require.Zero(heightsRepaired) // migration should not run a second time

	// iterate over the trie and check it contains the expected items
	root, err := a.Root(commitHeight)
	require.NoError(err)
	it, err := a.Iterator(root, nil)
	require.NoError(err)

	foundKeys := 0
	for it.Next() {
		bytes, err := a.codec.Marshal(codecVersion, it.AtomicOps())
		require.NoError(err)
		require.Equal(expected[it.BlockNumber()][it.BlockchainID()], bytes)
		foundKeys++
	}
	require.Equal(expectedKeys, foundKeys)
}
