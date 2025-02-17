// (c) 2020-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"encoding/binary"
	"encoding/hex"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ava-labs/coreth/trie/trienode"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

const testCommitInterval = 100

func mustAtomicOps(tx *atomic.Tx) map[ids.ID]*avalancheatomic.Requests {
	id, reqs, err := tx.AtomicOps()
	if err != nil {
		panic(err)
	}
	return map[ids.ID]*avalancheatomic.Requests{id: reqs}
}

// indexAtomicTxs updates [tr] with entries in [atomicOps] at height by creating
// a new snapshot, calculating a new root, and calling InsertTrie followed
// by AcceptTrie on the new root.
func indexAtomicTxs(tr AtomicTrie, height uint64, atomicOps map[ids.ID]*avalancheatomic.Requests) error {
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
		assert.Equal(t, commitHeight, test.expectedCommitHeight)
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
			codec := atomic.TestTxCodec
			repo, err := NewAtomicTxRepository(db, codec, test.lastAcceptedHeight)
			if err != nil {
				t.Fatal(err)
			}
			operationsMap := make(map[uint64]map[ids.ID]*avalancheatomic.Requests)
			writeTxs(t, repo, 1, test.lastAcceptedHeight+1, test.numTxsPerBlock, nil, operationsMap)

			// Construct the atomic trie for the first time
			atomicBackend1, err := NewAtomicBackend(db, testSharedMemory(), nil, repo, test.lastAcceptedHeight, common.Hash{}, test.commitInterval)
			if err != nil {
				t.Fatal(err)
			}
			atomicTrie1 := atomicBackend1.AtomicTrie()

			rootHash1, commitHeight1 := atomicTrie1.LastCommitted()
			assert.EqualValues(t, test.expectedCommitHeight, commitHeight1)
			if test.expectedCommitHeight != 0 {
				assert.NotEqual(t, common.Hash{}, rootHash1)
			}

			// Verify the operations up to the expected commit height
			verifyOperations(t, atomicTrie1, codec, rootHash1, 1, test.expectedCommitHeight, operationsMap)

			// Construct the atomic trie again (on the same database) and ensure the last accepted root is correct.
			atomicBackend2, err := NewAtomicBackend(db, testSharedMemory(), nil, repo, test.lastAcceptedHeight, common.Hash{}, test.commitInterval)
			if err != nil {
				t.Fatal(err)
			}
			atomicTrie2 := atomicBackend2.AtomicTrie()
			assert.Equal(t, atomicTrie1.LastAcceptedRoot(), atomicTrie2.LastAcceptedRoot())

			// Construct the atomic trie again (on an empty database) and ensure that it produces the same hash.
			atomicBackend3, err := NewAtomicBackend(
				versiondb.New(memdb.New()), testSharedMemory(), nil, repo, test.lastAcceptedHeight, common.Hash{}, test.commitInterval,
			)
			if err != nil {
				t.Fatal(err)
			}
			atomicTrie3 := atomicBackend3.AtomicTrie()

			rootHash3, commitHeight3 := atomicTrie3.LastCommitted()
			assert.EqualValues(t, commitHeight1, commitHeight3)
			assert.EqualValues(t, rootHash1, rootHash3)

			// We now index additional operations up the next commit interval in order to confirm that nothing
			// during the initialization phase will cause an invalid root when indexing continues.
			nextCommitHeight := nearestCommitHeight(test.lastAcceptedHeight+test.commitInterval, test.commitInterval)
			for i := test.lastAcceptedHeight + 1; i <= nextCommitHeight; i++ {
				txs := atomic.NewTestTxs(test.numTxsPerBlock(i))
				if err := repo.Write(i, txs); err != nil {
					t.Fatal(err)
				}

				atomicOps, err := mergeAtomicOps(txs)
				if err != nil {
					t.Fatal(err)
				}
				if err := indexAtomicTxs(atomicTrie1, i, atomicOps); err != nil {
					t.Fatal(err)
				}
				operationsMap[i] = atomicOps
			}
			updatedRoot, updatedLastCommitHeight := atomicTrie1.LastCommitted()
			assert.EqualValues(t, nextCommitHeight, updatedLastCommitHeight)
			assert.NotEqual(t, common.Hash{}, updatedRoot)

			// Verify the operations up to the new expected commit height
			verifyOperations(t, atomicTrie1, codec, updatedRoot, 1, updatedLastCommitHeight, operationsMap)

			// Generate a new atomic trie to compare the root against.
			atomicBackend4, err := NewAtomicBackend(
				versiondb.New(memdb.New()), testSharedMemory(), nil, repo, nextCommitHeight, common.Hash{}, test.commitInterval,
			)
			if err != nil {
				t.Fatal(err)
			}
			atomicTrie4 := atomicBackend4.AtomicTrie()

			rootHash4, commitHeight4 := atomicTrie4.LastCommitted()
			assert.EqualValues(t, updatedRoot, rootHash4)
			assert.EqualValues(t, updatedLastCommitHeight, commitHeight4)
		})
	}
}

func TestIndexerInitializesOnlyOnce(t *testing.T) {
	lastAcceptedHeight := uint64(25)
	db := versiondb.New(memdb.New())
	codec := atomic.TestTxCodec
	repo, err := NewAtomicTxRepository(db, codec, lastAcceptedHeight)
	assert.NoError(t, err)
	operationsMap := make(map[uint64]map[ids.ID]*avalancheatomic.Requests)
	writeTxs(t, repo, 1, lastAcceptedHeight+1, constTxsPerHeight(2), nil, operationsMap)

	// Initialize atomic repository
	atomicBackend, err := NewAtomicBackend(db, testSharedMemory(), nil, repo, lastAcceptedHeight, common.Hash{}, 10 /* commitInterval*/)
	assert.NoError(t, err)
	atomicTrie := atomicBackend.AtomicTrie()

	hash, height := atomicTrie.LastCommitted()
	assert.NotEqual(t, common.Hash{}, hash)
	assert.Equal(t, uint64(20), height)

	// We write another tx at a height below the last committed height in the repo and then
	// re-initialize the atomic trie since initialize is not supposed to run again the height
	// at the trie should still be the old height with the old commit hash without any changes.
	// This scenario is not realistic, but is used to test potential double initialization behavior.
	err = repo.Write(15, []*atomic.Tx{atomic.GenerateTestExportTx()})
	assert.NoError(t, err)

	// Re-initialize the atomic trie
	atomicBackend, err = NewAtomicBackend(db, testSharedMemory(), nil, repo, lastAcceptedHeight, common.Hash{}, 10 /* commitInterval */)
	assert.NoError(t, err)
	atomicTrie = atomicBackend.AtomicTrie()

	newHash, newHeight := atomicTrie.LastCommitted()
	assert.Equal(t, height, newHeight, "height should not have changed")
	assert.Equal(t, hash, newHash, "hash should be the same")
}

func newTestAtomicTrie(t *testing.T) AtomicTrie {
	db := versiondb.New(memdb.New())
	repo, err := NewAtomicTxRepository(db, atomic.TestTxCodec, 0)
	if err != nil {
		t.Fatal(err)
	}
	atomicBackend, err := NewAtomicBackend(db, testSharedMemory(), nil, repo, 0, common.Hash{}, testCommitInterval)
	if err != nil {
		t.Fatal(err)
	}
	return atomicBackend.AtomicTrie()
}

func TestIndexerWriteAndRead(t *testing.T) {
	atomicTrie := newTestAtomicTrie(t)

	blockRootMap := make(map[uint64]common.Hash)
	lastCommittedBlockHeight := uint64(0)
	var lastCommittedBlockHash common.Hash

	// process 305 blocks so that we get three commits (100, 200, 300)
	for height := uint64(1); height <= testCommitInterval*3+5; /*=305*/ height++ {
		atomicRequests := mustAtomicOps(atomic.GenerateTestImportTx())
		err := indexAtomicTxs(atomicTrie, height, atomicRequests)
		assert.NoError(t, err)
		if height%testCommitInterval == 0 {
			lastCommittedBlockHash, lastCommittedBlockHeight = atomicTrie.LastCommitted()
			assert.NoError(t, err)
			assert.NotEqual(t, common.Hash{}, lastCommittedBlockHash)
			blockRootMap[lastCommittedBlockHeight] = lastCommittedBlockHash
		}
	}

	// ensure we have 3 roots
	assert.Len(t, blockRootMap, 3)

	hash, height := atomicTrie.LastCommitted()
	assert.EqualValues(t, lastCommittedBlockHeight, height)
	assert.Equal(t, lastCommittedBlockHash, hash)

	// Verify that [atomicTrie] can access each of the expected roots
	for height, hash := range blockRootMap {
		root, err := atomicTrie.Root(height)
		assert.NoError(t, err)
		assert.Equal(t, hash, root)
	}
}

func TestAtomicOpsAreNotTxOrderDependent(t *testing.T) {
	atomicTrie1 := newTestAtomicTrie(t)
	atomicTrie2 := newTestAtomicTrie(t)

	for height := uint64(0); height <= testCommitInterval; /*=205*/ height++ {
		tx1 := atomic.GenerateTestImportTx()
		tx2 := atomic.GenerateTestImportTx()
		atomicRequests1, err := mergeAtomicOps([]*atomic.Tx{tx1, tx2})
		assert.NoError(t, err)
		atomicRequests2, err := mergeAtomicOps([]*atomic.Tx{tx2, tx1})
		assert.NoError(t, err)

		err = indexAtomicTxs(atomicTrie1, height, atomicRequests1)
		assert.NoError(t, err)
		err = indexAtomicTxs(atomicTrie2, height, atomicRequests2)
		assert.NoError(t, err)
	}
	root1, height1 := atomicTrie1.LastCommitted()
	root2, height2 := atomicTrie2.LastCommitted()
	assert.NotEqual(t, common.Hash{}, root1)
	assert.Equal(t, uint64(testCommitInterval), height1)
	assert.Equal(t, uint64(testCommitInterval), height2)
	assert.Equal(t, root1, root2)
}

func TestAtomicTrieDoesNotSkipBonusBlocks(t *testing.T) {
	lastAcceptedHeight := uint64(100)
	numTxsPerBlock := 3
	commitInterval := uint64(10)
	expectedCommitHeight := uint64(100)
	db := versiondb.New(memdb.New())
	codec := atomic.TestTxCodec
	repo, err := NewAtomicTxRepository(db, codec, lastAcceptedHeight)
	if err != nil {
		t.Fatal(err)
	}
	operationsMap := make(map[uint64]map[ids.ID]*avalancheatomic.Requests)
	writeTxs(t, repo, 1, lastAcceptedHeight, constTxsPerHeight(numTxsPerBlock), nil, operationsMap)

	bonusBlocks := map[uint64]ids.ID{
		10: {},
		13: {},
		14: {},
	}
	// Construct the atomic trie for the first time
	atomicBackend, err := NewAtomicBackend(db, testSharedMemory(), bonusBlocks, repo, lastAcceptedHeight, common.Hash{}, commitInterval)
	if err != nil {
		t.Fatal(err)
	}
	atomicTrie := atomicBackend.AtomicTrie()

	rootHash, commitHeight := atomicTrie.LastCommitted()
	assert.EqualValues(t, expectedCommitHeight, commitHeight)
	assert.NotEqual(t, common.Hash{}, rootHash)

	// Verify the operations are as expected
	verifyOperations(t, atomicTrie, codec, rootHash, 1, expectedCommitHeight, operationsMap)
}

func TestIndexingNilShouldNotImpactTrie(t *testing.T) {
	// operations to index
	ops := make([]map[ids.ID]*avalancheatomic.Requests, 0)
	for i := 0; i <= testCommitInterval; i++ {
		ops = append(ops, mustAtomicOps(atomic.GenerateTestImportTx()))
	}

	// without nils
	a1 := newTestAtomicTrie(t)
	for i := uint64(0); i <= testCommitInterval; i++ {
		if i%2 == 0 {
			if err := indexAtomicTxs(a1, i, ops[i]); err != nil {
				t.Fatal(err)
			}
		} else {
			// do nothing
		}
	}

	root1, height1 := a1.LastCommitted()
	assert.NotEqual(t, common.Hash{}, root1)
	assert.Equal(t, uint64(testCommitInterval), height1)

	// with nils
	a2 := newTestAtomicTrie(t)
	for i := uint64(0); i <= testCommitInterval; i++ {
		if i%2 == 0 {
			if err := indexAtomicTxs(a2, i, ops[i]); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := indexAtomicTxs(a2, i, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	root2, height2 := a2.LastCommitted()
	assert.NotEqual(t, common.Hash{}, root2)
	assert.Equal(t, uint64(testCommitInterval), height2)

	// key assertion of the test
	assert.Equal(t, root1, root2)
}

type sharedMemories struct {
	thisChain   avalancheatomic.SharedMemory
	peerChain   avalancheatomic.SharedMemory
	thisChainID ids.ID
	peerChainID ids.ID
}

func (s *sharedMemories) addItemsToBeRemovedToPeerChain(ops map[ids.ID]*avalancheatomic.Requests) error {
	for _, reqs := range ops {
		puts := make(map[ids.ID]*avalancheatomic.Requests)
		puts[s.thisChainID] = &avalancheatomic.Requests{}
		for _, key := range reqs.RemoveRequests {
			val := []byte{0x1}
			puts[s.thisChainID].PutRequests = append(puts[s.thisChainID].PutRequests, &avalancheatomic.Element{Key: key, Value: val})
		}
		if err := s.peerChain.Apply(puts); err != nil {
			return err
		}
	}
	return nil
}

func (s *sharedMemories) assertOpsApplied(t *testing.T, ops map[ids.ID]*avalancheatomic.Requests) {
	t.Helper()
	for _, reqs := range ops {
		// should be able to get put requests
		for _, elem := range reqs.PutRequests {
			val, err := s.peerChain.Get(s.thisChainID, [][]byte{elem.Key})
			if err != nil {
				t.Fatalf("error finding puts in peerChainMemory: %s", err)
			}
			assert.Equal(t, elem.Value, val[0])
		}

		// should not be able to get remove requests
		for _, key := range reqs.RemoveRequests {
			_, err := s.thisChain.Get(s.peerChainID, [][]byte{key})
			assert.EqualError(t, err, "not found")
		}
	}
}

func (s *sharedMemories) assertOpsNotApplied(t *testing.T, ops map[ids.ID]*avalancheatomic.Requests) {
	t.Helper()
	for _, reqs := range ops {
		// should not be able to get put requests
		for _, elem := range reqs.PutRequests {
			_, err := s.peerChain.Get(s.thisChainID, [][]byte{elem.Key})
			assert.EqualError(t, err, "not found")
		}

		// should be able to get remove requests (these were previously added as puts on peerChain)
		for _, key := range reqs.RemoveRequests {
			val, err := s.thisChain.Get(s.peerChainID, [][]byte{key})
			assert.NoError(t, err)
			assert.Equal(t, []byte{0x1}, val[0])
		}
	}
}

func newSharedMemories(atomicMemory *avalancheatomic.Memory, thisChainID, peerChainID ids.ID) *sharedMemories {
	return &sharedMemories{
		thisChain:   atomicMemory.NewSharedMemory(thisChainID),
		peerChain:   atomicMemory.NewSharedMemory(peerChainID),
		thisChainID: thisChainID,
		peerChainID: peerChainID,
	}
}

func TestApplyToSharedMemory(t *testing.T) {
	type test struct {
		commitInterval, lastAcceptedHeight uint64
		setMarker                          func(*atomicBackend) error
		expectOpsApplied                   func(height uint64) bool
		bonusBlockHeights                  map[uint64]ids.ID
	}

	for name, test := range map[string]test{
		"marker is set to height": {
			commitInterval:     10,
			lastAcceptedHeight: 25,
			setMarker:          func(a *atomicBackend) error { return a.MarkApplyToSharedMemoryCursor(10) },
			expectOpsApplied:   func(height uint64) bool { return height > 10 && height <= 20 },
		},
		"marker is set to height, should skip bonus blocks": {
			commitInterval:     10,
			lastAcceptedHeight: 25,
			setMarker:          func(a *atomicBackend) error { return a.MarkApplyToSharedMemoryCursor(10) },
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
			setMarker: func(a *atomicBackend) error {
				cursor := make([]byte, wrappers.LongLen+len(atomic.TestBlockchainID[:]))
				binary.BigEndian.PutUint64(cursor, 10)
				copy(cursor[wrappers.LongLen:], atomic.TestBlockchainID[:])
				return a.metadataDB.Put(appliedSharedMemoryCursorKey, cursor)
			},
			expectOpsApplied: func(height uint64) bool { return height > 10 && height <= 20 },
		},
		"marker not set": {
			commitInterval:     10,
			lastAcceptedHeight: 25,
			setMarker:          func(*atomicBackend) error { return nil },
			expectOpsApplied:   func(uint64) bool { return false },
		},
	} {
		t.Run(name, func(t *testing.T) {
			db := versiondb.New(memdb.New())
			codec := atomic.TestTxCodec
			repo, err := NewAtomicTxRepository(db, codec, test.lastAcceptedHeight)
			assert.NoError(t, err)
			operationsMap := make(map[uint64]map[ids.ID]*avalancheatomic.Requests)
			writeTxs(t, repo, 1, test.lastAcceptedHeight+1, constTxsPerHeight(2), nil, operationsMap)

			// Initialize atomic repository
			m := avalancheatomic.NewMemory(db)
			sharedMemories := newSharedMemories(m, testCChainID, atomic.TestBlockchainID)
			backend, err := NewAtomicBackend(db, sharedMemories.thisChain, test.bonusBlockHeights, repo, test.lastAcceptedHeight, common.Hash{}, test.commitInterval)
			assert.NoError(t, err)
			atomicTrie := backend.AtomicTrie().(*atomicTrie)

			hash, height := atomicTrie.LastCommitted()
			assert.NotEqual(t, common.Hash{}, hash)
			assert.Equal(t, uint64(20), height)

			// prepare peer chain's shared memory by applying items we expect to remove as puts
			for _, ops := range operationsMap {
				if err := sharedMemories.addItemsToBeRemovedToPeerChain(ops); err != nil {
					t.Fatal(err)
				}
			}

			assert.NoError(t, test.setMarker(backend.(*atomicBackend)))
			assert.NoError(t, db.Commit())
			assert.NoError(t, backend.ApplyToSharedMemory(test.lastAcceptedHeight))

			// assert ops were applied as expected
			for height, ops := range operationsMap {
				if test.expectOpsApplied(height) {
					sharedMemories.assertOpsApplied(t, ops)
				} else {
					sharedMemories.assertOpsNotApplied(t, ops)
				}
			}

			// marker should be removed after ApplyToSharedMemory is complete
			hasMarker, err := atomicTrie.metadataDB.Has(appliedSharedMemoryCursorKey)
			assert.NoError(t, err)
			assert.False(t, hasMarker)
			// reinitialize the atomic trie
			backend, err = NewAtomicBackend(
				db, sharedMemories.thisChain, nil, repo, test.lastAcceptedHeight, common.Hash{}, test.commitInterval,
			)
			assert.NoError(t, err)
			// no further changes should have occurred in shared memory
			// assert they are as they were prior to reinitializing
			for height, ops := range operationsMap {
				if test.expectOpsApplied(height) {
					sharedMemories.assertOpsApplied(t, ops)
				} else {
					sharedMemories.assertOpsNotApplied(t, ops)
				}
			}

			// marker should be removed after ApplyToSharedMemory is complete
			hasMarker, err = atomicTrie.metadataDB.Has(appliedSharedMemoryCursorKey)
			assert.NoError(t, err)
			assert.False(t, hasMarker)
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
			atomicTrie, err := newAtomicTrie(atomicTrieDB, metadataDB, atomic.TestTxCodec,
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
				err := encoder.Flush()
				require.NoError(t, err)

				nodeSet := trienode.NewNodeSet(testCase.lastAcceptedRoot)
				nodeSet.AddNode([]byte("any"), trienode.New(testCase.lastAcceptedRoot, testBlob)) // dirty node
				err = atomicTrie.InsertTrie(nodeSet, testCase.lastAcceptedRoot)
				require.NoError(t, err)

				_, storageSize, _ := atomicTrie.trieDB.Size()
				require.NotZero(t, storageSize, "there should be a dirty node taking up storage space")
			}
			atomicTrie.updateLastCommitted(testCase.lastCommittedRoot, testCase.lastCommittedHeight)

			hasCommitted, err := atomicTrie.AcceptTrie(testCase.height, testCase.root)
			require.NoError(t, err)

			assert.Equal(t, testCase.wantHasCommitted, hasCommitted)
			assert.Equal(t, testCase.wantLastCommittedHeight, atomicTrie.lastCommittedHeight)
			assert.Equal(t, testCase.wantLastCommittedRoot, atomicTrie.lastCommittedRoot)
			assert.Equal(t, testCase.wantLastAcceptedRoot, atomicTrie.lastAcceptedRoot)

			// Check dereferencing previous dirty root inserted occurred
			_, storageSize, _ := atomicTrie.trieDB.Size()
			assert.Zerof(t, storageSize, "storage size should be zero after accepting the trie due to the dirty nodes derefencing but is %s", storageSize)

			for wantKeyHex, wantValueHex := range testCase.wantMetadataDBKVs {
				wantKey, err := hex.DecodeString(wantKeyHex)
				require.NoError(t, err)
				value, err := metadataDB.Get(wantKey)
				assert.NoErrorf(t, err, "getting key %s from metadata database", wantKeyHex)
				assert.Equalf(t, wantValueHex, hex.EncodeToString(value), "value for key %s", wantKeyHex)
			}
		})
	}
}

func BenchmarkAtomicTrieInit(b *testing.B) {
	db := versiondb.New(memdb.New())
	codec := atomic.TestTxCodec

	operationsMap := make(map[uint64]map[ids.ID]*avalancheatomic.Requests)

	lastAcceptedHeight := uint64(25000)
	// add 25000 * 3 = 75000 transactions
	repo, err := NewAtomicTxRepository(db, codec, lastAcceptedHeight)
	assert.NoError(b, err)
	writeTxs(b, repo, 1, lastAcceptedHeight, constTxsPerHeight(3), nil, operationsMap)

	var (
		atomicTrie AtomicTrie
		hash       common.Hash
		height     uint64
	)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sharedMemory := testSharedMemory()
		atomicBackend, err := NewAtomicBackend(db, sharedMemory, nil, repo, lastAcceptedHeight, common.Hash{}, 5000)
		assert.NoError(b, err)
		atomicTrie = atomicBackend.AtomicTrie()

		hash, height = atomicTrie.LastCommitted()
		assert.Equal(b, lastAcceptedHeight, height)
		assert.NotEqual(b, common.Hash{}, hash)
	}
	b.StopTimer()

	// Verify operations
	verifyOperations(b, atomicTrie, codec, hash, 1, lastAcceptedHeight, operationsMap)
}

func BenchmarkAtomicTrieIterate(b *testing.B) {
	db := versiondb.New(memdb.New())
	codec := atomic.TestTxCodec

	operationsMap := make(map[uint64]map[ids.ID]*avalancheatomic.Requests)

	lastAcceptedHeight := uint64(25_000)
	// add 25000 * 3 = 75000 transactions
	repo, err := NewAtomicTxRepository(db, codec, lastAcceptedHeight)
	assert.NoError(b, err)
	writeTxs(b, repo, 1, lastAcceptedHeight, constTxsPerHeight(3), nil, operationsMap)

	atomicBackend, err := NewAtomicBackend(db, testSharedMemory(), nil, repo, lastAcceptedHeight, common.Hash{}, 5000)
	assert.NoError(b, err)
	atomicTrie := atomicBackend.AtomicTrie()

	hash, height := atomicTrie.LastCommitted()
	assert.Equal(b, lastAcceptedHeight, height)
	assert.NotEqual(b, common.Hash{}, hash)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		it, err := atomicTrie.Iterator(hash, nil)
		if err != nil {
			b.Fatal("could not initialize atomic trie iterator")
		}
		for it.Next() {
			assert.NotZero(b, it.BlockNumber())
			assert.NotZero(b, it.BlockchainID())
		}
		assert.NoError(b, it.Error())
	}
}

func levelDB(t testing.TB) database.Database {
	db, err := leveldb.New(t.TempDir(), nil, logging.NoLog{}, prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
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
	codec := atomic.TestTxCodec
	sharedMemory := testSharedMemory()

	lastAcceptedHeight := blocks
	repo, err := NewAtomicTxRepository(db, codec, lastAcceptedHeight)
	assert.NoError(b, err)

	backend, err := NewAtomicBackend(db, sharedMemory, nil, repo, 0, common.Hash{}, 5000)
	if err != nil {
		b.Fatal(err)
	}
	trie := backend.AtomicTrie()
	for height := uint64(1); height <= lastAcceptedHeight; height++ {
		txs := atomic.NewTestTxs(constTxsPerHeight(3)(height))
		ops, err := mergeAtomicOps(txs)
		assert.NoError(b, err)
		assert.NoError(b, indexAtomicTxs(trie, height, ops))
	}

	hash, height := trie.LastCommitted()
	assert.Equal(b, lastAcceptedHeight, height)
	assert.NotEqual(b, common.Hash{}, hash)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		backend.(*atomicBackend).sharedMemory = testSharedMemory()
		assert.NoError(b, backend.MarkApplyToSharedMemoryCursor(0))
		assert.NoError(b, db.Commit())
		assert.NoError(b, backend.ApplyToSharedMemory(lastAcceptedHeight))
	}
}
