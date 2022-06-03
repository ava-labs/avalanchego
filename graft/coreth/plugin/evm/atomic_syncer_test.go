// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"

	"github.com/ava-labs/coreth/ethdb/memorydb"
	"github.com/ava-labs/coreth/plugin/evm/message"
	syncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/coreth/sync/handlers"
	handlerstats "github.com/ava-labs/coreth/sync/handlers/stats"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
)

const commitInterval = 1024

type atomicSyncTestCheckpoint struct {
	expectedNumLeavesSynced int64       // expected number of leaves to have synced at this checkpoint
	leafCutoff              int         // Number of leafs to sync before cutting off responses
	targetRoot              common.Hash // Root of trie to resume syncing from after stopping
	targetHeight            uint64      // Height to sync to after stopping
}

// testAtomicSyncer creates a leaf handler with [serverTrieDB] and tests to ensure that the atomic syncer can sync correctly
// starting at [targetRoot], and stopping and resuming at each of the [checkpoints].
func testAtomicSyncer(t *testing.T, serverTrieDB *trie.Database, targetHeight uint64, targetRoot common.Hash, checkpoints []atomicSyncTestCheckpoint, finalExpectedNumLeaves int64) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	numLeaves := 0
	mockClient := syncclient.NewMockClient(
		message.Codec,
		handlers.NewLeafsRequestHandler(serverTrieDB, nil, message.Codec, handlerstats.NewNoopHandlerStats()),
		nil,
		nil,
	)

	clientDB := versiondb.New(memdb.New())

	repo, err := NewAtomicTxRepository(clientDB, message.Codec, 0)
	if err != nil {
		t.Fatal("could not initialize atomix tx repository", err)
	}
	atomicTrie, err := newAtomicTrie(clientDB, testSharedMemory(), nil, repo, message.Codec, 0, commitInterval)
	if err != nil {
		t.Fatal("could not initialize atomic trie", err)
	}

	// For each checkpoint, replace the leafsIntercept to shut off the syncer at the correct point and force resume from the checkpoint's
	// next trie.
	for i, checkpoint := range checkpoints {
		// Create syncer targeting the current [syncTrie].
		syncer := newAtomicSyncer(mockClient, atomicTrie, targetRoot, targetHeight)
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
	syncer := newAtomicSyncer(mockClient, atomicTrie, targetRoot, targetHeight)

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
	clientTrieDB := atomicTrie.TrieDB()
	trie.AssertTrieConsistency(t, targetRoot, serverTrieDB, clientTrieDB, nil)

	// check all commit heights are created
	for height := atomicTrie.commitHeightInterval; height <= targetHeight; height += atomicTrie.commitHeightInterval {
		root, err := atomicTrie.Root(height)
		assert.NoError(t, err)
		assert.NotZero(t, root)
	}
}

func TestAtomicSyncer(t *testing.T) {
	rand.Seed(1)
	targetHeight := 10 * uint64(commitInterval)
	serverTrieDB := trie.NewDatabase(memorydb.New())
	root, _, _ := trie.GenerateTrie(t, serverTrieDB, int(targetHeight), atomicKeyLength)

	testAtomicSyncer(t, serverTrieDB, targetHeight, root, nil, int64(targetHeight))
}

func TestAtomicSyncerResume(t *testing.T) {
	rand.Seed(1)
	targetHeight := 10 * uint64(commitInterval)
	serverTrieDB := trie.NewDatabase(memorydb.New())
	numTrieKeys := int(targetHeight) - 1 // no atomic ops for genesis
	root, _, _ := trie.GenerateTrie(t, serverTrieDB, numTrieKeys, atomicKeyLength)

	testAtomicSyncer(t, serverTrieDB, targetHeight, root, []atomicSyncTestCheckpoint{
		{
			targetRoot:              root,
			targetHeight:            targetHeight,
			leafCutoff:              commitInterval*5 - 1,
			expectedNumLeavesSynced: commitInterval * 4,
		},
	}, int64(targetHeight)+commitInterval-1) // we will resync the last commitInterval - 1 leafs
}

func TestAtomicSyncerResumeNewRootCheckpoint(t *testing.T) {
	rand.Seed(1)
	targetHeight1 := 10 * uint64(commitInterval)
	serverTrieDB := trie.NewDatabase(memorydb.New())
	numTrieKeys1 := int(targetHeight1) - 1 // no atomic ops for genesis
	root1, _, _ := trie.GenerateTrie(t, serverTrieDB, numTrieKeys1, atomicKeyLength)

	rand.Seed(1) // seed rand again to get the same leafs in GenerateTrie
	targetHeight2 := 20 * uint64(commitInterval)
	numTrieKeys2 := int(targetHeight2) - 1 // no atomic ops for genesis
	root2, _, _ := trie.GenerateTrie(t, serverTrieDB, numTrieKeys2, atomicKeyLength)

	testAtomicSyncer(t, serverTrieDB, targetHeight1, root1, []atomicSyncTestCheckpoint{
		{
			targetRoot:              root2,
			targetHeight:            targetHeight2,
			leafCutoff:              commitInterval*5 - 1,
			expectedNumLeavesSynced: commitInterval * 4,
		},
	}, int64(targetHeight2)+commitInterval-1) // we will resync the last commitInterval - 1 leafs
}
