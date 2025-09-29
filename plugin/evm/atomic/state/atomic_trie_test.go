// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"encoding/binary"
	"encoding/hex"
	"testing"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ava-labs/coreth/plugin/evm/atomic/atomictest"

	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
)

const testCommitInterval = 100

// indexAtomicTxs updates [tr] with entries in [atomicOps] at height by creating
// a new snapshot, calculating a new root, and calling InsertTrie followed
// by AcceptTrie on the new root.
func indexAtomicTxs(tr *AtomicTrie, height uint64, atomicOps map[ids.ID]*avalancheatomic.Requests) error {
	snapshot, err := tr.OpenTrie(tr.LastAcceptedRoot())
	if err != nil {
		return err
	}
	if err := tr.UpdateTrie(snapshot, height, atomicOps); err != nil {
		return err
	}
	root, nodes, err := snapshot.Commit(false)
	if err != nil {
		return err
	}
	if err := tr.InsertTrie(nodes, root); err != nil {
		return err
	}
	_, err = tr.AcceptTrie(height, root)
	return err
}

func TestNearestCommitHeight(t *testing.T) {
	type test struct {
		height, commitInterval, expectedCommitHeight uint64
	}

	for _, test := range []test{
		{
			height:               4500,
			commitInterval:       4096,
			expectedCommitHeight: 4096,
		},
		{
			height:               8500,
			commitInterval:       4096,
			expectedCommitHeight: 8192,
		},
		{
			height:               950,
			commitInterval:       100,
			expectedCommitHeight: 900,
		},
	} {
		commitHeight := nearestCommitHeight(test.height, test.commitInterval)
		require.Equal(t, test.expectedCommitHeight, commitHeight)
	}
}

func TestAtomicTrieInitialize(t *testing.T) {
	type test struct {
		commitInterval, lastAcceptedHeight, expectedCommitHeight uint64
		numTxsPerBlock                                           func(uint64) int
	}
	for name, test := range map[string]test{
		"genesis": {
			commitInterval:       10,
			lastAcceptedHeight:   0,
			expectedCommitHeight: 0,
			numTxsPerBlock:       constTxsPerHeight(0),
		},
		"before first commit": {
			commitInterval:       10,
			lastAcceptedHeight:   5,
			expectedCommitHeight: 0,
			numTxsPerBlock:       constTxsPerHeight(3),
		},
		"first commit": {
			commitInterval:       10,
			lastAcceptedHeight:   10,
			expectedCommitHeight: 10,
			numTxsPerBlock:       constTxsPerHeight(3),
		},
		"past first commit": {
			commitInterval:       10,
			lastAcceptedHeight:   15,
			expectedCommitHeight: 10,
			numTxsPerBlock:       constTxsPerHeight(3),
		},
		"many existing commits": {
			commitInterval:       10,
			lastAcceptedHeight:   1000,
			expectedCommitHeight: 1000,
			numTxsPerBlock:       constTxsPerHeight(3),
		},
		"many existing commits plus 1": {
			commitInterval:       10,
			lastAcceptedHeight:   1001,
			expectedCommitHeight: 1000,
			numTxsPerBlock:       constTxsPerHeight(3),
		},
		"some blocks without atomic tx": {
			commitInterval:       10,
			lastAcceptedHeight:   101,
			expectedCommitHeight: 100,
			numTxsPerBlock: func(height uint64) int {
				if height <= 50 || height == 101 {
					return 1
				}
				return 0
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			db := versiondb.New(memdb.New())
			repo, err := NewAtomicTxRepository(db, atomictest.TestTxCodec, test.lastAcceptedHeight)
			require.NoError(t, err)
			operationsMap := make(map[uint64]map[ids.ID]*avalancheatomic.Requests)
			writeTxs(t, repo, 1, test.lastAcceptedHeight+1, test.numTxsPerBlock, nil, operationsMap)

			// Construct the atomic trie for the first time
			atomicBackend1, err := NewAtomicBackend(atomictest.TestSharedMemory(), nil, repo, test.lastAcceptedHeight, common.Hash{}, test.commitInterval)
			require.NoError(t, err)
			atomicTrie1 := atomicBackend1.AtomicTrie()

			rootHash1, commitHeight1 := atomicTrie1.LastCommitted()
			require.Equal(t, test.expectedCommitHeight, commitHeight1)
			if test.expectedCommitHeight != 0 {
				require.NotZero(t, rootHash1)
			}

			// Verify the operations up to the expected commit height
			verifyOperations(t, atomicTrie1, atomictest.TestTxCodec, rootHash1, 1, test.expectedCommitHeight, operationsMap)

			// Construct the atomic trie again (on the same database) and ensure the last accepted root is correct.
			atomicBackend2, err := NewAtomicBackend(atomictest.TestSharedMemory(), nil, repo, test.lastAcceptedHeight, common.Hash{}, test.commitInterval)
			require.NoError(t, err)
			atomicTrie2 := atomicBackend2.AtomicTrie()
			require.Equal(t, atomicTrie1.LastAcceptedRoot(), atomicTrie2.LastAcceptedRoot())

			// Construct the atomic trie again (on an empty database) and ensure that it produces the same hash.
			atomicBackend3, err := NewAtomicBackend(atomictest.TestSharedMemory(), nil, repo, test.lastAcceptedHeight, common.Hash{}, test.commitInterval)
			require.NoError(t, err)
			atomicTrie3 := atomicBackend3.AtomicTrie()

			rootHash3, commitHeight3 := atomicTrie3.LastCommitted()
			require.Equal(t, commitHeight1, commitHeight3)
			require.Equal(t, rootHash1, rootHash3)

			// We now index additional operations up the next commit interval in order to confirm that nothing
			// during the initialization phase will cause an invalid root when indexing continues.
			nextCommitHeight := nearestCommitHeight(test.lastAcceptedHeight+test.commitInterval, test.commitInterval)
			for i := test.lastAcceptedHeight + 1; i <= nextCommitHeight; i++ {
				txs := atomictest.NewTestTxs(test.numTxsPerBlock(i))
				require.NoError(t, repo.Write(i, txs))

				atomicOps, err := mergeAtomicOps(txs)
				require.NoError(t, err)
				require.NoError(t, indexAtomicTxs(atomicTrie1, i, atomicOps))
				operationsMap[i] = atomicOps
			}
			updatedRoot, updatedLastCommitHeight := atomicTrie1.LastCommitted()
			require.Equal(t, nextCommitHeight, updatedLastCommitHeight)
			require.NotZero(t, updatedRoot)

			// Verify the operations up to the new expected commit height
			verifyOperations(t, atomicTrie1, atomictest.TestTxCodec, updatedRoot, 1, updatedLastCommitHeight, operationsMap)

			// Generate a new atomic trie to compare the root against.
			atomicBackend4, err := NewAtomicBackend(atomictest.TestSharedMemory(), nil, repo, nextCommitHeight, common.Hash{}, test.commitInterval)
			require.NoError(t, err)
			atomicTrie4 := atomicBackend4.AtomicTrie()

			rootHash4, commitHeight4 := atomicTrie4.LastCommitted()
			require.Equal(t, updatedRoot, rootHash4)
			require.Equal(t, updatedLastCommitHeight, commitHeight4)
		})
	}
}

func TestIndexerInitializesOnlyOnce(t *testing.T) {
	lastAcceptedHeight := uint64(25)
	db := versiondb.New(memdb.New())
	repo, err := NewAtomicTxRepository(db, atomictest.TestTxCodec, lastAcceptedHeight)
	require.NoError(t, err)
	operationsMap := make(map[uint64]map[ids.ID]*avalancheatomic.Requests)
	writeTxs(t, repo, 1, lastAcceptedHeight+1, constTxsPerHeight(2), nil, operationsMap)

	// Initialize atomic repository
	atomicBackend, err := NewAtomicBackend(atomictest.TestSharedMemory(), nil, repo, lastAcceptedHeight, common.Hash{}, 10 /* commitInterval*/)
	require.NoError(t, err)
	atomicTrie := atomicBackend.AtomicTrie()

	hash, height := atomicTrie.LastCommitted()
	require.NotZero(t, hash)
	require.Equal(t, uint64(20), height)

	// We write another tx at a height below the last committed height in the repo and then
	// re-initialize the atomic trie since initialize is not supposed to run again the height
	// at the trie should still be the old height with the old commit hash without any changes.
	// This scenario is not realistic, but is used to test potential double initialization behavior.
	require.NoError(t, repo.Write(15, []*atomic.Tx{atomictest.GenerateTestExportTx()}))

	// Re-initialize the atomic trie
	atomicBackend, err = NewAtomicBackend(atomictest.TestSharedMemory(), nil, repo, lastAcceptedHeight, common.Hash{}, 10 /* commitInterval */)
	require.NoError(t, err)
	atomicTrie = atomicBackend.AtomicTrie()

	newHash, newHeight := atomicTrie.LastCommitted()
	require.Equal(t, height, newHeight, "height should not have changed")
	require.Equal(t, hash, newHash, "hash should be the same")
}

func newTestAtomicTrie(t *testing.T) *AtomicTrie {
	db := versiondb.New(memdb.New())
	repo, err := NewAtomicTxRepository(db, atomictest.TestTxCodec, 0)
	require.NoError(t, err)
	atomicBackend, err := NewAtomicBackend(atomictest.TestSharedMemory(), nil, repo, 0, common.Hash{}, testCommitInterval)
	require.NoError(t, err)
	return atomicBackend.AtomicTrie()
}

func TestIndexerWriteAndRead(t *testing.T) {
	atomicTrie := newTestAtomicTrie(t)

	blockRootMap := make(map[uint64]common.Hash)
	lastCommittedBlockHeight := uint64(0)
	var lastCommittedBlockHash common.Hash

	// process 305 blocks so that we get three commits (100, 200, 300)
	for height := uint64(1); height <= testCommitInterval*3+5; /*=305*/ height++ {
		atomicRequests, err := atomictest.ConvertToAtomicOps(atomictest.GenerateTestImportTx())
		require.NoError(t, err)
		require.NoError(t, indexAtomicTxs(atomicTrie, height, atomicRequests))
		if height%testCommitInterval == 0 {
			lastCommittedBlockHash, lastCommittedBlockHeight = atomicTrie.LastCommitted()
			require.NotZero(t, lastCommittedBlockHash)
			blockRootMap[lastCommittedBlockHeight] = lastCommittedBlockHash
		}
	}

	// ensure we have 3 roots
	require.Len(t, blockRootMap, 3)

	hash, height := atomicTrie.LastCommitted()
	require.Equal(t, lastCommittedBlockHeight, height)
	require.Equal(t, lastCommittedBlockHash, hash)

	// Verify that [atomicTrie] can access each of the expected roots
	for height, hash := range blockRootMap {
		root, err := atomicTrie.Root(height)
		require.NoError(t, err)
		require.Equal(t, hash, root)
	}
}

func TestAtomicOpsAreNotTxOrderDependent(t *testing.T) {
	atomicTrie1 := newTestAtomicTrie(t)
	atomicTrie2 := newTestAtomicTrie(t)

	for height := uint64(0); height <= testCommitInterval; /*=205*/ height++ {
		tx1 := atomictest.GenerateTestImportTx()
		tx2 := atomictest.GenerateTestImportTx()
		atomicRequests1, err := mergeAtomicOps([]*atomic.Tx{tx1, tx2})
		require.NoError(t, err)
		atomicRequests2, err := mergeAtomicOps([]*atomic.Tx{tx2, tx1})
		require.NoError(t, err)

		require.NoError(t, indexAtomicTxs(atomicTrie1, height, atomicRequests1))
		require.NoError(t, indexAtomicTxs(atomicTrie2, height, atomicRequests2))
	}
	root1, height1 := atomicTrie1.LastCommitted()
	root2, height2 := atomicTrie2.LastCommitted()
	require.NotZero(t, root1)
	require.Equal(t, uint64(testCommitInterval), height1)
	require.Equal(t, uint64(testCommitInterval), height2)
	require.Equal(t, root1, root2)
}

func TestAtomicTrieDoesNotSkipBonusBlocks(t *testing.T) {
	lastAcceptedHeight := uint64(100)
	numTxsPerBlock := 3
	commitInterval := uint64(10)
	expectedCommitHeight := uint64(100)
	db := versiondb.New(memdb.New())
	repo, err := NewAtomicTxRepository(db, atomictest.TestTxCodec, lastAcceptedHeight)
	require.NoError(t, err)
	operationsMap := make(map[uint64]map[ids.ID]*avalancheatomic.Requests)
	writeTxs(t, repo, 1, lastAcceptedHeight, constTxsPerHeight(numTxsPerBlock), nil, operationsMap)

	bonusBlocks := map[uint64]ids.ID{
		10: {},
		13: {},
		14: {},
	}
	// Construct the atomic trie for the first time
	atomicBackend, err := NewAtomicBackend(atomictest.TestSharedMemory(), bonusBlocks, repo, lastAcceptedHeight, common.Hash{}, commitInterval)
	require.NoError(t, err)
	atomicTrie := atomicBackend.AtomicTrie()

	rootHash, commitHeight := atomicTrie.LastCommitted()
	require.Equal(t, expectedCommitHeight, commitHeight)
	require.NotZero(t, rootHash)

	// Verify the operations are as expected
	verifyOperations(t, atomicTrie, atomictest.TestTxCodec, rootHash, 1, expectedCommitHeight, operationsMap)
}

func TestIndexingNilShouldNotImpactTrie(t *testing.T) {
	// operations to index
	ops := make([]map[ids.ID]*avalancheatomic.Requests, 0)
	for i := 0; i <= testCommitInterval; i++ {
		atomicOps, err := atomictest.ConvertToAtomicOps(atomictest.GenerateTestImportTx())
		require.NoError(t, err)
		ops = append(ops, atomicOps)
	}

	// without nils
	a1 := newTestAtomicTrie(t)
	for i := uint64(0); i <= testCommitInterval; i++ {
		if i%2 == 0 {
			require.NoError(t, indexAtomicTxs(a1, i, ops[i]))
		}
	}

	root1, height1 := a1.LastCommitted()
	require.NotZero(t, root1)
	require.Equal(t, uint64(testCommitInterval), height1)

	// with nils
	a2 := newTestAtomicTrie(t)
	for i := uint64(0); i <= testCommitInterval; i++ {
		if i%2 == 0 {
			require.NoError(t, indexAtomicTxs(a2, i, ops[i]))
		} else {
			require.NoError(t, indexAtomicTxs(a2, i, nil))
		}
	}
	root2, height2 := a2.LastCommitted()
	require.NotZero(t, root2)
	require.Equal(t, uint64(testCommitInterval), height2)

	// key requirement of the test
	require.Equal(t, root1, root2)
}

func TestApplyToSharedMemory(t *testing.T) {
	type test struct {
		commitInterval, lastAcceptedHeight uint64
		setMarker                          func(*AtomicBackend) error
		expectOpsApplied                   func(height uint64) bool
		bonusBlockHeights                  map[uint64]ids.ID
	}

	for name, test := range map[string]test{
		"marker is set to height": {
			commitInterval:     10,
			lastAcceptedHeight: 25,
			setMarker:          func(a *AtomicBackend) error { return a.MarkApplyToSharedMemoryCursor(10) },
			expectOpsApplied:   func(height uint64) bool { return height > 10 && height <= 20 },
		},
		"marker is set to height, should skip bonus blocks": {
			commitInterval:     10,
			lastAcceptedHeight: 25,
			setMarker:          func(a *AtomicBackend) error { return a.MarkApplyToSharedMemoryCursor(10) },
			bonusBlockHeights:  map[uint64]ids.ID{15: {}},
			expectOpsApplied: func(height uint64) bool {
				if height == 15 {
					return false
				}
				return height > 10 && height <= 20
			},
		},
		"marker is set to height + blockchain ID": {
			commitInterval:     10,
			lastAcceptedHeight: 25,
			setMarker: func(a *AtomicBackend) error {
				cursor := make([]byte, wrappers.LongLen+len(atomictest.TestBlockchainID[:]))
				binary.BigEndian.PutUint64(cursor, 10)
				copy(cursor[wrappers.LongLen:], atomictest.TestBlockchainID[:])
				return a.repo.metadataDB.Put(appliedSharedMemoryCursorKey, cursor)
			},
			expectOpsApplied: func(height uint64) bool { return height > 10 && height <= 20 },
		},
		"marker not set": {
			commitInterval:     10,
			lastAcceptedHeight: 25,
			setMarker:          func(*AtomicBackend) error { return nil },
			expectOpsApplied:   func(uint64) bool { return false },
		},
	} {
		t.Run(name, func(t *testing.T) {
			db := versiondb.New(memdb.New())
			repo, err := NewAtomicTxRepository(db, atomictest.TestTxCodec, test.lastAcceptedHeight)
			require.NoError(t, err)
			operationsMap := make(map[uint64]map[ids.ID]*avalancheatomic.Requests)
			writeTxs(t, repo, 1, test.lastAcceptedHeight+1, constTxsPerHeight(2), nil, operationsMap)

			// Initialize atomic repository
			m := avalancheatomic.NewMemory(db)
			sharedMemories := atomictest.NewSharedMemories(m, snowtest.CChainID, atomictest.TestBlockchainID)
			backend, err := NewAtomicBackend(sharedMemories.ThisChain, test.bonusBlockHeights, repo, test.lastAcceptedHeight, common.Hash{}, test.commitInterval)
			require.NoError(t, err)
			atomicTrie := backend.AtomicTrie()

			hash, height := atomicTrie.LastCommitted()
			require.NotZero(t, hash)
			require.Equal(t, uint64(20), height)

			// prepare peer chain's shared memory by applying items we expect to remove as puts
			for _, ops := range operationsMap {
				require.NoError(t, sharedMemories.AddItemsToBeRemovedToPeerChain(ops))
			}

			require.NoError(t, test.setMarker(backend))
			require.NoError(t, db.Commit())
			require.NoError(t, backend.ApplyToSharedMemory(test.lastAcceptedHeight))

			testOps := func() {
				// require that ops were applied as expected
				for height, ops := range operationsMap {
					if test.expectOpsApplied(height) {
						sharedMemories.AssertOpsApplied(t, ops)
					} else {
						sharedMemories.AssertOpsNotApplied(t, ops)
					}
				}

				hasMarker, err := atomicTrie.metadataDB.Has(appliedSharedMemoryCursorKey)
				require.NoError(t, err)
				require.False(t, hasMarker)
			}

			// marker should be removed after ApplyToSharedMemory is complete
			testOps()
			// reinitialize the atomic trie
			_, err = NewAtomicBackend(sharedMemories.ThisChain, nil, repo, test.lastAcceptedHeight, common.Hash{}, test.commitInterval)
			require.NoError(t, err)
			// require that ops were applied as expected
			testOps()
		})
	}
}

func TestAtomicTrie_AcceptTrie(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		lastAcceptedRoot        common.Hash
		lastCommittedRoot       common.Hash
		lastCommittedHeight     uint64
		commitInterval          uint64
		height                  uint64
		root                    common.Hash
		wantHasCommitted        bool
		wantLastCommittedHeight uint64
		wantLastCommittedRoot   common.Hash
		wantLastAcceptedRoot    common.Hash
		wantTipBufferRoot       common.Hash
		wantMetadataDBKVs       map[string]string // hex to hex
	}{
		"no_committing": {
			lastAcceptedRoot:        types.EmptyRootHash,
			lastCommittedRoot:       common.Hash{2},
			lastCommittedHeight:     100,
			commitInterval:          10,
			height:                  105,
			root:                    common.Hash{3},
			wantLastCommittedHeight: 100,
			wantLastCommittedRoot:   common.Hash{2},
			wantLastAcceptedRoot:    common.Hash{3},
			wantTipBufferRoot:       common.Hash{3},
			wantMetadataDBKVs: map[string]string{
				"0000000000000064":                   hex.EncodeToString(common.Hash{2}.Bytes()), // height 100
				hex.EncodeToString(lastCommittedKey): "0000000000000064",                         // height 100
			},
		},
		"no_committing_with_previous_root": {
			lastAcceptedRoot:        common.Hash{1},
			lastCommittedRoot:       common.Hash{2},
			lastCommittedHeight:     100,
			commitInterval:          10,
			height:                  105,
			root:                    common.Hash{3},
			wantLastCommittedHeight: 100,
			wantLastCommittedRoot:   common.Hash{2},
			wantLastAcceptedRoot:    common.Hash{3},
			wantTipBufferRoot:       common.Hash{3},
			wantMetadataDBKVs: map[string]string{
				"0000000000000064":                   hex.EncodeToString(common.Hash{2}.Bytes()), // height 100
				hex.EncodeToString(lastCommittedKey): "0000000000000064",                         // height 100
			},
		},
		"commit_all_up_to_height_without_height": {
			lastAcceptedRoot:        types.EmptyRootHash,
			lastCommittedRoot:       common.Hash{2},
			lastCommittedHeight:     60,
			commitInterval:          10,
			height:                  105,
			root:                    common.Hash{3},
			wantHasCommitted:        true,
			wantLastCommittedHeight: 100,
			wantLastCommittedRoot:   types.EmptyRootHash,
			wantLastAcceptedRoot:    common.Hash{3},
			wantTipBufferRoot:       common.Hash{3},
			wantMetadataDBKVs: map[string]string{
				"000000000000003c":                   hex.EncodeToString(common.Hash{2}.Bytes()), // height 60
				"0000000000000046":                   hex.EncodeToString(types.EmptyRootHash[:]), // height 70
				"0000000000000050":                   hex.EncodeToString(types.EmptyRootHash[:]), // height 80
				"000000000000005a":                   hex.EncodeToString(types.EmptyRootHash[:]), // height 90
				"0000000000000064":                   hex.EncodeToString(types.EmptyRootHash[:]), // height 100
				hex.EncodeToString(lastCommittedKey): "0000000000000064",                         // height 100
			},
		},
		"commit_root": {
			lastAcceptedRoot:        types.EmptyRootHash,
			lastCommittedRoot:       common.Hash{2},
			lastCommittedHeight:     100,
			commitInterval:          10,
			height:                  110,
			root:                    common.Hash{3},
			wantHasCommitted:        true,
			wantLastCommittedHeight: 110,
			wantLastCommittedRoot:   common.Hash{3},
			wantLastAcceptedRoot:    common.Hash{3},
			wantTipBufferRoot:       common.Hash{3},
			wantMetadataDBKVs: map[string]string{
				"0000000000000064":                   hex.EncodeToString(common.Hash{2}.Bytes()), // height 100
				"000000000000006e":                   hex.EncodeToString(common.Hash{3}.Bytes()), // height 110
				hex.EncodeToString(lastCommittedKey): "000000000000006e",                         // height 110
			},
		},
		"commit_root_with_previous_root": {
			lastAcceptedRoot:        common.Hash{1},
			lastCommittedRoot:       common.Hash{2},
			lastCommittedHeight:     100,
			commitInterval:          10,
			height:                  110,
			root:                    common.Hash{3},
			wantHasCommitted:        true,
			wantLastCommittedHeight: 110,
			wantLastCommittedRoot:   common.Hash{3},
			wantLastAcceptedRoot:    common.Hash{3},
			wantTipBufferRoot:       common.Hash{3},
			wantMetadataDBKVs: map[string]string{
				"0000000000000064":                   hex.EncodeToString(common.Hash{2}.Bytes()), // height 100
				"000000000000006e":                   hex.EncodeToString(common.Hash{3}.Bytes()), // height 110
				hex.EncodeToString(lastCommittedKey): "000000000000006e",                         // height 110
			},
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			versionDB := versiondb.New(memdb.New())
			atomicTrieDB := prefixdb.New(atomicTrieDBPrefix, versionDB)
			metadataDB := prefixdb.New(atomicTrieMetaDBPrefix, versionDB)
			const lastAcceptedHeight = 0 // no effect
			atomicTrie, err := newAtomicTrie(atomicTrieDB, metadataDB, atomictest.TestTxCodec,
				lastAcceptedHeight, testCase.commitInterval)
			require.NoError(t, err)
			atomicTrie.lastAcceptedRoot = testCase.lastAcceptedRoot
			if testCase.lastAcceptedRoot != types.EmptyRootHash {
				// Generate trie node test blob
				encoder := rlp.NewEncoderBuffer(nil)
				offset := encoder.List()
				encoder.WriteBytes([]byte{1})        // key
				encoder.WriteBytes(make([]byte, 32)) // value
				encoder.ListEnd(offset)
				testBlob := encoder.ToBytes()
				require.NoError(t, encoder.Flush())

				nodeSet := trienode.NewNodeSet(testCase.lastAcceptedRoot)
				nodeSet.AddNode([]byte("any"), trienode.New(testCase.lastAcceptedRoot, testBlob)) // dirty node
				require.NoError(t, atomicTrie.InsertTrie(nodeSet, testCase.lastAcceptedRoot))

				_, storageSize, _ := atomicTrie.trieDB.Size()
				require.NotZero(t, storageSize, "there should be a dirty node taking up storage space")
			}
			atomicTrie.updateLastCommitted(testCase.lastCommittedRoot, testCase.lastCommittedHeight)

			hasCommitted, err := atomicTrie.AcceptTrie(testCase.height, testCase.root)
			require.NoError(t, err)

			require.Equal(t, testCase.wantHasCommitted, hasCommitted)
			require.Equal(t, testCase.wantLastCommittedHeight, atomicTrie.lastCommittedHeight)
			require.Equal(t, testCase.wantLastCommittedRoot, atomicTrie.lastCommittedRoot)
			require.Equal(t, testCase.wantLastAcceptedRoot, atomicTrie.lastAcceptedRoot)

			// Check dereferencing previous dirty root inserted occurred
			_, storageSize, _ := atomicTrie.trieDB.Size()
			require.Zerof(t, storageSize, "storage size should be zero after accepting the trie due to the dirty nodes derefencing but is %s", storageSize)

			for wantKeyHex, wantValueHex := range testCase.wantMetadataDBKVs {
				wantKey, err := hex.DecodeString(wantKeyHex)
				require.NoError(t, err)
				value, err := metadataDB.Get(wantKey)
				require.NoErrorf(t, err, "getting key %s from metadata database", wantKeyHex)
				require.Equalf(t, wantValueHex, hex.EncodeToString(value), "value for key %s", wantKeyHex)
			}
		})
	}
}

func BenchmarkAtomicTrieInit(b *testing.B) {
	db := versiondb.New(memdb.New())

	operationsMap := make(map[uint64]map[ids.ID]*avalancheatomic.Requests)

	lastAcceptedHeight := uint64(25000)
	// add 25000 * 3 = 75000 transactions
	repo, err := NewAtomicTxRepository(db, atomictest.TestTxCodec, lastAcceptedHeight)
	require.NoError(b, err)
	writeTxs(b, repo, 1, lastAcceptedHeight, constTxsPerHeight(3), nil, operationsMap)

	var (
		atomicTrie *AtomicTrie
		hash       common.Hash
		height     uint64
	)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sharedMemory := atomictest.TestSharedMemory()
		atomicBackend, err := NewAtomicBackend(sharedMemory, nil, repo, lastAcceptedHeight, common.Hash{}, 5000)
		require.NoError(b, err)
		atomicTrie = atomicBackend.AtomicTrie()

		hash, height = atomicTrie.LastCommitted()
		require.Equal(b, lastAcceptedHeight, height)
		require.NotEqual(b, common.Hash{}, hash)
	}
	b.StopTimer()

	// Verify operations
	verifyOperations(b, atomicTrie, atomictest.TestTxCodec, hash, 1, lastAcceptedHeight, operationsMap)
}

func BenchmarkAtomicTrieIterate(b *testing.B) {
	db := versiondb.New(memdb.New())

	operationsMap := make(map[uint64]map[ids.ID]*avalancheatomic.Requests)

	lastAcceptedHeight := uint64(25_000)
	// add 25000 * 3 = 75000 transactions
	repo, err := NewAtomicTxRepository(db, atomictest.TestTxCodec, lastAcceptedHeight)
	require.NoError(b, err)
	writeTxs(b, repo, 1, lastAcceptedHeight, constTxsPerHeight(3), nil, operationsMap)

	atomicBackend, err := NewAtomicBackend(atomictest.TestSharedMemory(), nil, repo, lastAcceptedHeight, common.Hash{}, 5000)
	require.NoError(b, err)
	atomicTrie := atomicBackend.AtomicTrie()

	hash, height := atomicTrie.LastCommitted()
	require.Equal(b, lastAcceptedHeight, height)
	require.NotZero(b, hash)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		it, err := atomicTrie.Iterator(hash, nil)
		require.NoError(b, err)
		for it.Next() {
			require.NotZero(b, it.BlockNumber())
			require.NotZero(b, it.BlockchainID())
		}
		require.NoError(b, it.Error())
	}
}

func levelDB(t testing.TB) database.Database {
	db, err := leveldb.New(t.TempDir(), nil, logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(t, err)
	return db
}

func BenchmarkApplyToSharedMemory(b *testing.B) {
	tests := []struct {
		name   string
		newDB  func() database.Database
		blocks uint64
	}{
		{
			name:   "memdb-25k",
			newDB:  func() database.Database { return memdb.New() },
			blocks: 25_000,
		},
		{
			name:   "memdb-250k",
			newDB:  func() database.Database { return memdb.New() },
			blocks: 250_000,
		},
		{
			name:   "leveldb-25k",
			newDB:  func() database.Database { return levelDB(b) },
			blocks: 25_000,
		},
		{
			name:   "leveldb-250k",
			newDB:  func() database.Database { return levelDB(b) },
			blocks: 250_000,
		},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			disk := test.newDB()
			defer disk.Close()
			benchmarkApplyToSharedMemory(b, disk, test.blocks)
		})
	}
}

func benchmarkApplyToSharedMemory(b *testing.B, disk database.Database, blocks uint64) {
	db := versiondb.New(disk)
	sharedMemory := atomictest.TestSharedMemory()

	lastAcceptedHeight := blocks
	repo, err := NewAtomicTxRepository(db, atomictest.TestTxCodec, lastAcceptedHeight)
	require.NoError(b, err)

	backend, err := NewAtomicBackend(sharedMemory, nil, repo, 0, common.Hash{}, 5000)
	require.NoError(b, err)
	trie := backend.AtomicTrie()
	for height := uint64(1); height <= lastAcceptedHeight; height++ {
		txs := atomictest.NewTestTxs(constTxsPerHeight(3)(height))
		ops, err := mergeAtomicOps(txs)
		require.NoError(b, err)
		require.NoError(b, indexAtomicTxs(trie, height, ops))
	}

	hash, height := trie.LastCommitted()
	require.Equal(b, lastAcceptedHeight, height)
	require.NotEqual(b, common.Hash{}, hash)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		backend.sharedMemory = atomictest.TestSharedMemory()
		require.NoError(b, backend.MarkApplyToSharedMemoryCursor(0))
		require.NoError(b, db.Commit())
		require.NoError(b, backend.ApplyToSharedMemory(lastAcceptedHeight))
	}
}

// verifyOperations creates an iterator over the atomicTrie at [rootHash] and verifies that the all of the operations in the trie in the interval [from, to] are identical to
// the atomic operations contained in [operationsMap] on the same interval.
//
//nolint:unparam // it's reasonable to provide a `from` height for this function.
func verifyOperations(t testing.TB, atomicTrie *AtomicTrie, codec codec.Manager, rootHash common.Hash, from, to uint64, operationsMap map[uint64]map[ids.ID]*avalancheatomic.Requests) {
	t.Helper()

	// Start the iterator at `from`
	fromBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(fromBytes, from)
	iter, err := atomicTrie.Iterator(rootHash, fromBytes)
	require.NoError(t, err, "creating iterator")

	// Generate map of the marshalled atomic operations on the interval [from, to]
	// based on `operationsMap`.
	marshalledOperationsMap := make(map[uint64]map[ids.ID][]byte)
	for height, blockRequests := range operationsMap {
		if height < from || height > to {
			continue
		}
		for blockchainID, atomicRequests := range blockRequests {
			b, err := codec.Marshal(0, atomicRequests)
			require.NoError(t, err, "marshaling atomic requests")
			if requestsMap, exists := marshalledOperationsMap[height]; exists {
				requestsMap[blockchainID] = b
			} else {
				requestsMap = make(map[ids.ID][]byte)
				requestsMap[blockchainID] = b
				marshalledOperationsMap[height] = requestsMap
			}
		}
	}

	// Generate map of marshalled atomic operations on the interval [from, to]
	// based on the contents of the trie.
	iteratorMarshalledOperationsMap := make(map[uint64]map[ids.ID][]byte)
	for iter.Next() {
		height := iter.BlockNumber()
		require.GreaterOrEqualf(t, height, from, "Iterator starting at (%d) found value at block height (%d)", from, height)
		if height > to {
			continue
		}

		blockchainID := iter.BlockchainID()
		b, err := codec.Marshal(0, iter.AtomicOps())
		require.NoError(t, err, "marshaling atomic operations")
		if requestsMap, exists := iteratorMarshalledOperationsMap[height]; exists {
			requestsMap[blockchainID] = b
		} else {
			requestsMap = make(map[ids.ID][]byte)
			requestsMap[blockchainID] = b
			iteratorMarshalledOperationsMap[height] = requestsMap
		}
	}
	require.NoError(t, iter.Error(), "iterator error")

	require.Equal(t, marshalledOperationsMap, iteratorMarshalledOperationsMap)
}
