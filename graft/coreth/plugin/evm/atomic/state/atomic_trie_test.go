// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"encoding/binary"
	"encoding/hex"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/atomictest"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
)

// numTestHeights is the number of heights indexed by tests that simply need to
// process a range of blocks.
const numTestHeights = 100

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
	return tr.AcceptTrie(height, root)
}

func TestAtomicTrieInitialize(t *testing.T) {
	type test struct {
		lastAcceptedHeight uint64
		numTxsPerBlock     func(uint64) int
	}
	for name, test := range map[string]test{
		"genesis": {
			lastAcceptedHeight: 0,
			numTxsPerBlock:     constTxsPerHeight(0),
		},
		"atomic txs at every height": {
			lastAcceptedHeight: 100,
			numTxsPerBlock:     constTxsPerHeight(3),
		},
		"gaps between atomic txs with trailing empty blocks": {
			lastAcceptedHeight: 101,
			// Atomic txs only appear at sparse heights <= 50, so heights 51..101
			// have none and must be back-filled with a committed root.
			numTxsPerBlock: func(height uint64) int {
				if height <= 50 {
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
			atomicBackend1, err := NewAtomicBackend(atomictest.TestSharedMemory(), nil, repo, test.lastAcceptedHeight, common.Hash{})
			require.NoError(t, err)
			atomicTrie1 := atomicBackend1.AtomicTrie()

			rootHash1, commitHeight1 := atomicTrie1.LastCommitted()
			require.Equal(t, test.lastAcceptedHeight, commitHeight1)
			if test.lastAcceptedHeight != 0 {
				require.NotZero(t, rootHash1)
			}

			// A committed root must exist at every height up to the last committed
			// height, including heights with no atomic txs (back-filled during
			// initialization). SAE relies on this property.
			for h := uint64(1); h <= commitHeight1; h++ {
				root, err := atomicTrie1.Root(h)
				require.NoError(t, err)
				require.NotZerof(t, root, "expected committed root at height %d", h)
			}

			verifyOperations(t, atomicTrie1, atomictest.TestTxCodec, rootHash1, 1, test.lastAcceptedHeight, operationsMap)

			// Construct the atomic trie again (on the same database) and ensure the last accepted root is correct.
			atomicBackend2, err := NewAtomicBackend(atomictest.TestSharedMemory(), nil, repo, test.lastAcceptedHeight, common.Hash{})
			require.NoError(t, err)
			atomicTrie2 := atomicBackend2.AtomicTrie()
			require.Equal(t, atomicTrie1.LastAcceptedRoot(), atomicTrie2.LastAcceptedRoot())

			// Construct the atomic trie again (on an empty database) and ensure that it produces the same hash.
			atomicBackend3, err := NewAtomicBackend(atomictest.TestSharedMemory(), nil, repo, test.lastAcceptedHeight, common.Hash{})
			require.NoError(t, err)
			atomicTrie3 := atomicBackend3.AtomicTrie()

			rootHash3, commitHeight3 := atomicTrie3.LastCommitted()
			require.Equal(t, commitHeight1, commitHeight3)
			require.Equal(t, rootHash1, rootHash3)

			// Index some additional blocks to confirm that nothing during the
			// initialization phase causes an invalid root when indexing continues.
			const additionalBlocks = 10
			nextHeight := test.lastAcceptedHeight + additionalBlocks
			for i := test.lastAcceptedHeight + 1; i <= nextHeight; i++ {
				txs := atomictest.NewTestTxs(test.numTxsPerBlock(i))
				require.NoError(t, repo.Write(i, txs))

				atomicOps, err := mergeAtomicOps(txs)
				require.NoError(t, err)
				require.NoError(t, indexAtomicTxs(atomicTrie1, i, atomicOps))
				operationsMap[i] = atomicOps
			}
			updatedRoot, updatedHeight := atomicTrie1.LastCommitted()
			require.Equal(t, nextHeight, updatedHeight)
			require.NotZero(t, updatedRoot)

			// Verify the operations up to the new last committed height.
			verifyOperations(t, atomicTrie1, atomictest.TestTxCodec, updatedRoot, 1, updatedHeight, operationsMap)

			// Generate a new atomic trie to compare the root against.
			atomicBackend4, err := NewAtomicBackend(atomictest.TestSharedMemory(), nil, repo, nextHeight, common.Hash{})
			require.NoError(t, err)
			atomicTrie4 := atomicBackend4.AtomicTrie()

			rootHash4, commitHeight4 := atomicTrie4.LastCommitted()
			require.Equal(t, updatedRoot, rootHash4)
			require.Equal(t, updatedHeight, commitHeight4)
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
	atomicBackend, err := NewAtomicBackend(atomictest.TestSharedMemory(), nil, repo, lastAcceptedHeight, common.Hash{})
	require.NoError(t, err)
	atomicTrie := atomicBackend.AtomicTrie()

	hash, height := atomicTrie.LastCommitted()
	require.NotZero(t, hash)
	require.Equal(t, lastAcceptedHeight, height)

	// We write another tx at a height below the last committed height in the repo and then
	// re-initialize the atomic trie since initialize is not supposed to run again the height
	// at the trie should still be the old height with the old commit hash without any changes.
	// This scenario is not realistic, but is used to test potential double initialization behavior.
	require.NoError(t, repo.Write(15, []*atomic.Tx{atomictest.GenerateTestExportTx()}))

	// Re-initialize the atomic trie
	atomicBackend, err = NewAtomicBackend(atomictest.TestSharedMemory(), nil, repo, lastAcceptedHeight, common.Hash{})
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
	atomicBackend, err := NewAtomicBackend(atomictest.TestSharedMemory(), nil, repo, 0, common.Hash{})
	require.NoError(t, err)
	return atomicBackend.AtomicTrie()
}

func TestAtomicOpsAreNotTxOrderDependent(t *testing.T) {
	atomicTrie1 := newTestAtomicTrie(t)
	atomicTrie2 := newTestAtomicTrie(t)

	for height := uint64(0); height <= numTestHeights; height++ {
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
	require.Equal(t, uint64(numTestHeights), height1)
	require.Equal(t, uint64(numTestHeights), height2)
	require.Equal(t, root1, root2)
}

func TestAtomicTrieDoesNotSkipBonusBlocks(t *testing.T) {
	lastAcceptedHeight := uint64(100)
	numTxsPerBlock := 3
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
	atomicBackend, err := NewAtomicBackend(atomictest.TestSharedMemory(), bonusBlocks, repo, lastAcceptedHeight, common.Hash{})
	require.NoError(t, err)
	atomicTrie := atomicBackend.AtomicTrie()

	rootHash, commitHeight := atomicTrie.LastCommitted()
	require.Equal(t, lastAcceptedHeight, commitHeight)
	require.NotZero(t, rootHash)

	// Verify the operations are as expected
	verifyOperations(t, atomicTrie, atomictest.TestTxCodec, rootHash, 1, lastAcceptedHeight, operationsMap)
}

func TestIndexingNilShouldNotImpactTrie(t *testing.T) {
	// operations to index
	ops := make([]map[ids.ID]*avalancheatomic.Requests, 0)
	for i := 0; i <= numTestHeights; i++ {
		atomicOps, err := atomictest.ConvertToAtomicOps(atomictest.GenerateTestImportTx())
		require.NoError(t, err)
		ops = append(ops, atomicOps)
	}

	// without nils
	a1 := newTestAtomicTrie(t)
	for i := uint64(0); i <= numTestHeights; i++ {
		if i%2 == 0 {
			require.NoError(t, indexAtomicTxs(a1, i, ops[i]))
		}
	}

	root1, height1 := a1.LastCommitted()
	require.NotZero(t, root1)
	require.Equal(t, uint64(numTestHeights), height1)

	// with nils
	a2 := newTestAtomicTrie(t)
	for i := uint64(0); i <= numTestHeights; i++ {
		if i%2 == 0 {
			require.NoError(t, indexAtomicTxs(a2, i, ops[i]))
		} else {
			require.NoError(t, indexAtomicTxs(a2, i, nil))
		}
	}
	root2, height2 := a2.LastCommitted()
	require.NotZero(t, root2)
	require.Equal(t, uint64(numTestHeights), height2)

	// key requirement of the test
	require.Equal(t, root1, root2)
}

func TestApplyToSharedMemory(t *testing.T) {
	type test struct {
		lastAcceptedHeight uint64
		setMarker          func(*AtomicBackend) error
		expectOpsApplied   func(height uint64) bool
		bonusBlockHeights  map[uint64]ids.ID
	}

	for name, test := range map[string]test{
		"marker is set to height": {
			lastAcceptedHeight: 25,
			setMarker:          func(a *AtomicBackend) error { return a.MarkApplyToSharedMemoryCursor(10) },
			expectOpsApplied:   func(height uint64) bool { return height > 10 && height <= 25 },
		},
		"marker is set to height, should skip bonus blocks": {
			lastAcceptedHeight: 25,
			setMarker:          func(a *AtomicBackend) error { return a.MarkApplyToSharedMemoryCursor(10) },
			bonusBlockHeights:  map[uint64]ids.ID{15: {}},
			expectOpsApplied: func(height uint64) bool {
				if height == 15 {
					return false
				}
				return height > 10 && height <= 25
			},
		},
		"marker is set to height + blockchain ID": {
			lastAcceptedHeight: 25,
			setMarker: func(a *AtomicBackend) error {
				cursor := make([]byte, wrappers.LongLen+len(atomictest.TestBlockchainID[:]))
				binary.BigEndian.PutUint64(cursor, 10)
				copy(cursor[wrappers.LongLen:], atomictest.TestBlockchainID[:])
				return a.repo.metadataDB.Put(appliedSharedMemoryCursorKey, cursor)
			},
			expectOpsApplied: func(height uint64) bool { return height > 10 && height <= 25 },
		},
		"marker not set": {
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
			backend, err := NewAtomicBackend(sharedMemories.ThisChain, test.bonusBlockHeights, repo, test.lastAcceptedHeight, common.Hash{})
			require.NoError(t, err)
			atomicTrie := backend.AtomicTrie()

			hash, height := atomicTrie.LastCommitted()
			require.NotZero(t, hash)
			require.Equal(t, test.lastAcceptedHeight, height)

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
			_, err = NewAtomicBackend(sharedMemories.ThisChain, nil, repo, test.lastAcceptedHeight, common.Hash{})
			require.NoError(t, err)
			// require that ops were applied as expected
			testOps()
		})
	}
}

func TestAtomicTrie_AcceptTrie(t *testing.T) {
	t.Parallel()

	// When AcceptTrie is called with a height beyond lastCommittedHeight+1,
	// the gap lastCommittedHeight+1, height-1 is back-filled with the
	// current lastAcceptedRoot, and the supplied root is committed at height.
	testCases := map[string]struct {
		lastAcceptedRoot        common.Hash
		lastCommittedRoot       common.Hash
		lastCommittedHeight     uint64
		height                  uint64
		root                    common.Hash
		wantLastCommittedHeight uint64
		wantLastCommittedRoot   common.Hash
		wantLastAcceptedRoot    common.Hash
		wantMetadataDBKVs       map[string]string // hex to hex
	}{
		"backfill_gap": {
			lastAcceptedRoot:        types.EmptyRootHash,
			lastCommittedRoot:       common.Hash{2},
			lastCommittedHeight:     100,
			height:                  105,
			root:                    common.Hash{3},
			wantLastCommittedHeight: 105,
			wantLastCommittedRoot:   common.Hash{3},
			wantLastAcceptedRoot:    common.Hash{3},
			wantMetadataDBKVs: map[string]string{
				"0000000000000064":                   hex.EncodeToString(common.Hash{2}.Bytes()), // height 100 (pre-existing)
				"0000000000000065":                   hex.EncodeToString(types.EmptyRootHash[:]), // height 101 (back-filled)
				"0000000000000066":                   hex.EncodeToString(types.EmptyRootHash[:]), // height 102 (back-filled)
				"0000000000000067":                   hex.EncodeToString(types.EmptyRootHash[:]), // height 103 (back-filled)
				"0000000000000068":                   hex.EncodeToString(types.EmptyRootHash[:]), // height 104 (back-filled)
				"0000000000000069":                   hex.EncodeToString(common.Hash{3}.Bytes()), // height 105 (committed)
				hex.EncodeToString(lastCommittedKey): "0000000000000069",                         // height 105
			},
		},
		"backfill_gap_with_previous_root": {
			lastAcceptedRoot:        common.Hash{1},
			lastCommittedRoot:       common.Hash{2},
			lastCommittedHeight:     100,
			height:                  105,
			root:                    common.Hash{3},
			wantLastCommittedHeight: 105,
			wantLastCommittedRoot:   common.Hash{3},
			wantLastAcceptedRoot:    common.Hash{3},
			wantMetadataDBKVs: map[string]string{
				"0000000000000064":                   hex.EncodeToString(common.Hash{2}.Bytes()), // height 100 (pre-existing)
				"0000000000000065":                   hex.EncodeToString(common.Hash{1}.Bytes()), // height 101 (back-filled w/ lastAcceptedRoot)
				"0000000000000066":                   hex.EncodeToString(common.Hash{1}.Bytes()), // height 102 (back-filled w/ lastAcceptedRoot)
				"0000000000000067":                   hex.EncodeToString(common.Hash{1}.Bytes()), // height 103 (back-filled w/ lastAcceptedRoot)
				"0000000000000068":                   hex.EncodeToString(common.Hash{1}.Bytes()), // height 104 (back-filled w/ lastAcceptedRoot)
				"0000000000000069":                   hex.EncodeToString(common.Hash{3}.Bytes()), // height 105 (committed)
				hex.EncodeToString(lastCommittedKey): "0000000000000069",                         // height 105
			},
		},
		"no_gap": {
			lastAcceptedRoot:        common.Hash{1},
			lastCommittedRoot:       common.Hash{2},
			lastCommittedHeight:     104,
			height:                  105,
			root:                    common.Hash{3},
			wantLastCommittedHeight: 105,
			wantLastCommittedRoot:   common.Hash{3},
			wantLastAcceptedRoot:    common.Hash{3},
			wantMetadataDBKVs: map[string]string{
				"0000000000000068":                   hex.EncodeToString(common.Hash{2}.Bytes()), // height 104 (pre-existing)
				"0000000000000069":                   hex.EncodeToString(common.Hash{3}.Bytes()), // height 105 (committed)
				hex.EncodeToString(lastCommittedKey): "0000000000000069",                         // height 105
			},
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			versionDB := versiondb.New(memdb.New())
			atomicTrieStorage := prefixdb.New(atomicTrieStoragePrefix, versionDB)
			metadataDB := prefixdb.New(atomicTrieMetaDBPrefix, versionDB)
			const lastAcceptedHeight = 0 // no effect
			atomicTrie, err := newAtomicTrie(atomicTrieStorage, metadataDB, atomictest.TestTxCodec,
				lastAcceptedHeight)
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
			require.NoError(t, atomicTrie.updateLastCommitted(testCase.lastCommittedRoot, testCase.lastCommittedHeight))

			require.NoError(t, atomicTrie.AcceptTrie(testCase.height, testCase.root))

			require.Equal(t, testCase.wantLastCommittedHeight, atomicTrie.lastCommittedHeight)
			require.Equal(t, testCase.wantLastCommittedRoot, atomicTrie.lastCommittedRoot)
			require.Equal(t, testCase.wantLastAcceptedRoot, atomicTrie.lastAcceptedRoot)

			// Acceptance leaves no dirty storage: the inserted root was either
			// committed during back-fill or dereferenced.
			_, storageSize, _ := atomicTrie.trieDB.Size()
			require.Zerof(t, storageSize, "dirty storage size should be zero after accepting the trie but is %s", storageSize)

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
		atomicBackend, err := NewAtomicBackend(sharedMemory, nil, repo, lastAcceptedHeight, common.Hash{})
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

	atomicBackend, err := NewAtomicBackend(atomictest.TestSharedMemory(), nil, repo, lastAcceptedHeight, common.Hash{})
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

	backend, err := NewAtomicBackend(sharedMemory, nil, repo, 0, common.Hash{})
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
