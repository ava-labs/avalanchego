// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"

	"github.com/ava-labs/coreth/plugin/evm/atomic/atomictest"
	"github.com/ava-labs/coreth/plugin/evm/atomic/state"
	atomicstate "github.com/ava-labs/coreth/plugin/evm/atomic/state"
	"github.com/ava-labs/coreth/plugin/evm/config"
	"github.com/ava-labs/coreth/plugin/evm/message"
	syncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/coreth/sync/handlers"
	handlerstats "github.com/ava-labs/coreth/sync/handlers/stats"
	"github.com/ava-labs/coreth/sync/statesync/statesynctest"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/triedb"
)

const commitInterval = 1024

type atomicSyncTestCheckpoint struct {
	expectedNumLeavesSynced int64       // expected number of leaves to have synced at this checkpoint
	leafCutoff              int         // Number of leafs to sync before cutting off responses
	targetRoot              common.Hash // Root of trie to resume syncing from after stopping
	targetHeight            uint64      // Height to sync to after stopping
}

// testSyncer creates a leaf handler with [serverTrieDB] and tests to ensure that the atomic syncer can sync correctly
// starting at [targetRoot], and stopping and resuming at each of the [checkpoints].
func testSyncer(t *testing.T, serverTrieDB *triedb.Database, targetHeight uint64, targetRoot common.Hash, checkpoints []atomicSyncTestCheckpoint, finalExpectedNumLeaves int64) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	numLeaves := 0
	mockClient := syncclient.NewTestClient(
		message.Codec,
		handlers.NewLeafsRequestHandler(serverTrieDB, state.TrieKeyLength, nil, message.Codec, handlerstats.NewNoopHandlerStats()),
		nil,
		nil,
	)

	clientDB := versiondb.New(memdb.New())
	repo, err := state.NewAtomicTxRepository(clientDB, message.Codec, 0)
	if err != nil {
		t.Fatal("could not initialize atomix tx repository", err)
	}
	atomicBackend, err := state.NewAtomicBackend(atomictest.TestSharedMemory(), nil, repo, 0, common.Hash{}, commitInterval)
	if err != nil {
		t.Fatal("could not initialize atomic backend", err)
	}

	// For each checkpoint, replace the leafsIntercept to shut off the syncer at the correct point and force resume from the checkpoint's
	// next trie.
	for i, checkpoint := range checkpoints {
		// Create syncer targeting the current [syncTrie].
		syncer, err := newSyncer(mockClient, clientDB, atomicBackend.AtomicTrie(), targetRoot, targetHeight, config.DefaultStateSyncRequestSize)
		if err != nil {
			t.Fatal(err)
		}
		mockClient.GetLeafsIntercept = func(_ message.LeafsRequest, leafsResponse message.LeafsResponse) (message.LeafsResponse, error) {
			// If this request exceeds the desired number of leaves, intercept the request with an error
			if numLeaves+len(leafsResponse.Keys) > checkpoint.leafCutoff {
				return message.LeafsResponse{}, fmt.Errorf("intercept cut off responses after %d leaves", checkpoint.leafCutoff)
			}

			// Increment the number of leaves and return the original response
			numLeaves += len(leafsResponse.Keys)
			return leafsResponse, nil
		}

		syncer.Start(ctx)
		if err := <-syncer.Done(); err == nil {
			t.Fatalf("Expected syncer to fail at checkpoint with numLeaves %d", numLeaves)
		}

		assert.Equal(t, checkpoint.expectedNumLeavesSynced, int64(numLeaves), "unexpected number of leaves received at checkpoint %d", i)
		// Replace the target root and height for the next checkpoint
		targetRoot = checkpoint.targetRoot
		targetHeight = checkpoint.targetHeight
	}

	// Create syncer targeting the current [targetRoot].
	syncer, err := newSyncer(mockClient, clientDB, atomicBackend.AtomicTrie(), targetRoot, targetHeight, config.DefaultStateSyncRequestSize)
	if err != nil {
		t.Fatal(err)
	}

	// Update intercept to only count the leaves
	mockClient.GetLeafsIntercept = func(_ message.LeafsRequest, leafsResponse message.LeafsResponse) (message.LeafsResponse, error) {
		// Increment the number of leaves and return the original response
		numLeaves += len(leafsResponse.Keys)
		return leafsResponse, nil
	}

	syncer.Start(ctx)
	if err := <-syncer.Done(); err != nil {
		t.Fatalf("Expected syncer to finish successfully but failed due to %s", err)
	}

	assert.Equal(t, finalExpectedNumLeaves, int64(numLeaves), "unexpected number of leaves received to match")

	// we re-initialise trie DB for asserting the trie to make sure any issues with unflushed writes
	// are caught here as this will only pass if all trie nodes have been written to the underlying DB
	atomicTrie := atomicBackend.AtomicTrie()
	clientTrieDB := atomicTrie.TrieDB()
	statesynctest.AssertTrieConsistency(t, targetRoot, serverTrieDB, clientTrieDB, nil)

	// check all commit heights are created correctly
	hasher := trie.NewEmpty(triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil))
	assert.NoError(t, err)

	serverTrie, err := trie.New(trie.TrieID(targetRoot), serverTrieDB)
	assert.NoError(t, err)
	addAllKeysWithPrefix := func(prefix []byte) error {
		nodeIt, err := serverTrie.NodeIterator(prefix)
		if err != nil {
			return err
		}
		it := trie.NewIterator(nodeIt)
		for it.Next() {
			if !bytes.HasPrefix(it.Key, prefix) {
				return it.Err
			}
			err := hasher.Update(it.Key, it.Value)
			assert.NoError(t, err)
		}
		return it.Err
	}

	for height := uint64(0); height <= targetHeight; height++ {
		err := addAllKeysWithPrefix(database.PackUInt64(height))
		assert.NoError(t, err)

		if height%commitInterval == 0 {
			expected := hasher.Hash()
			root, err := atomicTrie.Root(height)
			assert.NoError(t, err)
			assert.Equal(t, expected, root)
		}
	}
}

func TestSyncer(t *testing.T) {
	rand.Seed(1)
	targetHeight := 10 * uint64(commitInterval)
	serverTrieDB := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
	root, _, _ := statesynctest.GenerateTrie(t, serverTrieDB, int(targetHeight), atomicstate.TrieKeyLength)

	testSyncer(t, serverTrieDB, targetHeight, root, nil, int64(targetHeight))
}

func TestSyncerResume(t *testing.T) {
	rand.Seed(1)
	targetHeight := 10 * uint64(commitInterval)
	serverTrieDB := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
	numTrieKeys := int(targetHeight) - 1 // no atomic ops for genesis
	root, _, _ := statesynctest.GenerateTrie(t, serverTrieDB, numTrieKeys, atomicstate.TrieKeyLength)

	testSyncer(t, serverTrieDB, targetHeight, root, []atomicSyncTestCheckpoint{
		{
			targetRoot:              root,
			targetHeight:            targetHeight,
			leafCutoff:              commitInterval*5 - 1,
			expectedNumLeavesSynced: commitInterval * 4,
		},
	}, int64(targetHeight)+commitInterval-1) // we will resync the last commitInterval - 1 leafs
}

func TestSyncerResumeNewRootCheckpoint(t *testing.T) {
	rand.Seed(1)
	targetHeight1 := 10 * uint64(commitInterval)
	serverTrieDB := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
	numTrieKeys1 := int(targetHeight1) - 1 // no atomic ops for genesis
	root1, _, _ := statesynctest.GenerateTrie(t, serverTrieDB, numTrieKeys1, atomicstate.TrieKeyLength)

	targetHeight2 := 20 * uint64(commitInterval)
	numTrieKeys2 := int(targetHeight2) - 1 // no atomic ops for genesis
	root2, _, _ := statesynctest.FillTrie(
		t, numTrieKeys1, numTrieKeys2, atomicstate.TrieKeyLength, serverTrieDB, root1,
	)

	testSyncer(t, serverTrieDB, targetHeight1, root1, []atomicSyncTestCheckpoint{
		{
			targetRoot:              root2,
			targetHeight:            targetHeight2,
			leafCutoff:              commitInterval*5 - 1,
			expectedNumLeavesSynced: commitInterval * 4,
		},
	}, int64(targetHeight2)+commitInterval-1) // we will resync the last commitInterval - 1 leafs
}
