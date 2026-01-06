// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/triedb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/atomictest"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/state"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/message"
	"github.com/ava-labs/avalanchego/graft/coreth/sync/handlers"
	"github.com/ava-labs/avalanchego/graft/evm/sync/synctest"

	syncclient "github.com/ava-labs/avalanchego/graft/coreth/sync/client"
	handlerstats "github.com/ava-labs/avalanchego/graft/coreth/sync/handlers/stats"
)

const (
	testCommitInterval = 1024
	testTargetHeight   = 100
	testNumWorkers     = 4
)

type atomicSyncTestCheckpoint struct {
	expectedNumLeavesSynced int64       // expected number of leaves to have synced at this checkpoint
	leafCutoff              int         // Number of leafs to sync before cutting off responses
	targetRoot              common.Hash // Root of trie to resume syncing from after stopping
	targetHeight            uint64      // Height to sync to after stopping
}

// TestSyncerScenarios is a parameterized test that covers basic syncing scenarios with different worker configurations.
func TestSyncerScenarios(t *testing.T) {
	tests := []struct {
		name        string
		numWorkers  int
		description string
	}{
		{
			name:        "sync with single worker",
			numWorkers:  1,
			description: "should sync correctly with single worker",
		},
		{
			name:        "sync with parallel workers",
			numWorkers:  4,
			description: "should sync correctly with parallel workers",
		},
		{
			name:        "sync with default workers",
			numWorkers:  0, // Will use default
			description: "should sync correctly with default workers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := rand.New(rand.NewSource(1))
			targetHeight := 10 * uint64(testCommitInterval)
			serverTrieDB := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
			root, _, _ := synctest.GenerateIndependentTrie(t, r, serverTrieDB, int(targetHeight), state.TrieKeyLength)

			testSyncer(t, serverTrieDB, targetHeight, root, nil, int64(targetHeight), tt.numWorkers)
		})
	}
}

// TestSyncerResumeScenarios is a parameterized test that covers resume scenarios with different worker configurations.
func TestSyncerResumeScenarios(t *testing.T) {
	tests := []struct {
		name        string
		numWorkers  int
		description string
	}{
		{
			name:        "resume with single worker",
			numWorkers:  1,
			description: "should resume syncing correctly with single worker",
		},
		{
			name:        "resume with parallel workers",
			numWorkers:  4,
			description: "should resume syncing correctly with parallel workers",
		},
		{
			name:        "resume with default workers",
			numWorkers:  0, // Will use default
			description: "should resume syncing correctly with default workers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := rand.New(rand.NewSource(1))
			targetHeight := 10 * uint64(testCommitInterval)
			serverTrieDB := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
			numTrieKeys := int(targetHeight) - 1 // no atomic ops for genesis
			root, _, _ := synctest.GenerateIndependentTrie(t, r, serverTrieDB, numTrieKeys, state.TrieKeyLength)

			testSyncer(t, serverTrieDB, targetHeight, root, []atomicSyncTestCheckpoint{
				{
					targetRoot:              root,
					targetHeight:            targetHeight,
					leafCutoff:              testCommitInterval*5 - 1,
					expectedNumLeavesSynced: testCommitInterval * 4,
				},
			}, int64(targetHeight)+testCommitInterval-1, tt.numWorkers) // we will resync the last commitInterval - 1 leafs
		})
	}
}

// TestSyncerResumeNewRootCheckpointScenarios is a parameterized test that covers resume with new root scenarios with different worker configurations.
func TestSyncerResumeNewRootCheckpointScenarios(t *testing.T) {
	tests := []struct {
		name        string
		numWorkers  int
		description string
	}{
		{
			name:        "resume new root with single worker",
			numWorkers:  1,
			description: "should resume with new root correctly with single worker",
		},
		{
			name:        "resume new root with parallel workers",
			numWorkers:  4,
			description: "should resume with new root correctly with parallel workers",
		},
		{
			name:        "resume new root with default workers",
			numWorkers:  0, // Will use default
			description: "should resume with new root correctly with default workers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := rand.New(rand.NewSource(1))
			targetHeight1 := 10 * uint64(testCommitInterval)
			serverTrieDB := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
			numTrieKeys1 := int(targetHeight1) - 1 // no atomic ops for genesis
			root1, _, _ := synctest.GenerateIndependentTrie(t, r, serverTrieDB, numTrieKeys1, state.TrieKeyLength)

			targetHeight2 := 20 * uint64(testCommitInterval)
			numTrieKeys2 := int(targetHeight2) - 1 // no atomic ops for genesis
			root2, _, _ := synctest.FillIndependentTrie(
				t, r, numTrieKeys1, numTrieKeys2, state.TrieKeyLength, serverTrieDB, root1,
			)

			testSyncer(t, serverTrieDB, targetHeight1, root1, []atomicSyncTestCheckpoint{
				{
					targetRoot:              root2,
					targetHeight:            targetHeight2,
					leafCutoff:              testCommitInterval*5 - 1,
					expectedNumLeavesSynced: testCommitInterval * 4,
				},
			}, int64(targetHeight2)+testCommitInterval-1, tt.numWorkers) // we will resync the last commitInterval - 1 leafs
		})
	}
}

// TestSyncerParallelizationScenarios is a parameterized test that covers different parallelization scenarios.
func TestSyncerParallelizationScenarios(t *testing.T) {
	tests := []struct {
		name              string
		targetHeight      uint64
		numWorkers        int
		useDefaultWorkers bool
		description       string
	}{
		{
			name:         "parallelization with 4 workers",
			targetHeight: 2 * uint64(testCommitInterval), // 2,048 leaves for meaningful parallelization
			numWorkers:   4,
			description:  "should work correctly with 4 worker goroutines",
		},
		{
			name:         "default parallelization",
			targetHeight: uint64(testCommitInterval), // 1,024 leaves to test commit boundary
			numWorkers:   0,
			description:  "should default to parallelization with default workers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, mockClient, atomicBackend, clientDB, root := setupParallelizationTest(t, tt.targetHeight)
			runParallelizationTest(t, ctx, mockClient, clientDB, atomicBackend, root, tt.targetHeight, tt.numWorkers)
		})
	}
}

// TestSyncerContextCancellation verifies that the syncer properly handles context cancellation.
func TestSyncerContextCancellation(t *testing.T) {
	ctx, mockClient, atomicBackend, clientDB, root := setupParallelizationTest(t, testTargetHeight)
	syncer, err := NewSyncer(mockClient, clientDB, atomicBackend.AtomicTrie(), root, testTargetHeight)
	require.NoError(t, err, "NewSyncer()")

	// Immediately cancel the context to simulate cancellation.
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	err = syncer.Sync(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

// setupParallelizationTest creates the common test infrastructure for parallelization tests.
// It returns the context, mock client, atomic backend, client DB, and root hash for testing.
func setupParallelizationTest(t *testing.T, targetHeight uint64) (context.Context, *syncclient.TestClient, *state.AtomicBackend, *versiondb.Database, common.Hash) {
	// Create a simple test trie with some data.
	r := rand.New(rand.NewSource(1))
	serverTrieDB := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
	root, _, _ := synctest.GenerateIndependentTrie(t, r, serverTrieDB, int(targetHeight), state.TrieKeyLength)

	ctx, mockClient, atomicBackend, clientDB := setupTestInfrastructure(t, serverTrieDB)

	return ctx, mockClient, atomicBackend, clientDB, root
}

// runParallelizationTest executes a parallelization test with the given parameters.
func runParallelizationTest(t *testing.T, ctx context.Context, mockClient *syncclient.TestClient, clientDB *versiondb.Database, atomicBackend *state.AtomicBackend, root common.Hash, targetHeight uint64, numWorkers int) {
	syncer, err := NewSyncer(mockClient, clientDB, atomicBackend.AtomicTrie(), root, targetHeight)
	require.NoError(t, err, "NewSyncer()")

	workerType := "default workers"
	if numWorkers > 0 {
		syncer, err = NewSyncer(mockClient, clientDB, atomicBackend.AtomicTrie(), root, targetHeight, WithNumWorkers(numWorkers))
		require.NoError(t, err, "NewSyncer()")
		workerType = fmt.Sprintf("%d workers", numWorkers)
	}

	// Wait for completion.
	require.NoError(t, syncer.Sync(ctx), "syncer should complete successfully with %s", workerType)
}

// testSyncer creates a leaf handler with [serverTrieDB] and tests to ensure that the atomic syncer can sync correctly
// starting at [targetRoot], and stopping and resuming at each of the [checkpoints].
func testSyncer(t *testing.T, serverTrieDB *triedb.Database, targetHeight uint64, targetRoot common.Hash, checkpoints []atomicSyncTestCheckpoint, finalExpectedNumLeaves int64, numWorkers int) {
	ctx, mockClient, atomicBackend, clientDB := setupTestInfrastructure(t, serverTrieDB)

	numLeaves := 0

	// For each checkpoint, replace the leafsIntercept to shut off the syncer at the correct point and force resume from the checkpoint's
	// next trie.
	for i, checkpoint := range checkpoints {
		syncer, err := NewSyncer(
			mockClient, clientDB, atomicBackend.AtomicTrie(),
			targetRoot,
			targetHeight,
			WithNumWorkers(numWorkers),
		)
		require.NoError(t, err, "NewSyncer()")
		mockClient.GetLeafsIntercept = func(_ message.LeafsRequest, leafsResponse message.LeafsResponse) (message.LeafsResponse, error) {
			// If this request exceeds the desired number of leaves, intercept the request with an error
			if numLeaves+len(leafsResponse.Keys) > checkpoint.leafCutoff {
				return message.LeafsResponse{}, fmt.Errorf("intercept cut off responses after %d leaves", checkpoint.leafCutoff)
			}

			// Increment the number of leaves and return the original response
			numLeaves += len(leafsResponse.Keys)
			return leafsResponse, nil
		}

		err = syncer.Sync(ctx)
		require.ErrorIs(t, err, syncclient.ErrFailedToFetchLeafs)

		require.Equal(t, checkpoint.expectedNumLeavesSynced, int64(numLeaves), "unexpected number of leaves received at checkpoint %d", i)
		// Replace the target root and height for the next checkpoint
		targetRoot = checkpoint.targetRoot
		targetHeight = checkpoint.targetHeight
	}

	// Create syncer targeting the current [targetRoot].
	syncer, err := NewSyncer(
		mockClient, clientDB,
		atomicBackend.AtomicTrie(), targetRoot,
		targetHeight,
		WithRequestSize(defaultRequestSize),
		WithNumWorkers(numWorkers),
	)
	require.NoError(t, err, "NewSyncer()")
	mockClient.GetLeafsIntercept = func(_ message.LeafsRequest, leafsResponse message.LeafsResponse) (message.LeafsResponse, error) {
		// Increment the number of leaves and return the original response
		numLeaves += len(leafsResponse.Keys)
		return leafsResponse, nil
	}

	require.NoError(t, syncer.Sync(ctx), "Expected syncer to finish successfully")
	require.Equal(t, finalExpectedNumLeaves, int64(numLeaves), "unexpected number of leaves received to match")

	// we re-initialise trie DB for asserting the trie to make sure any issues with unflushed writes
	// are caught here as this will only pass if all trie nodes have been written to the underlying DB
	atomicTrie := atomicBackend.AtomicTrie()
	clientTrieDB := atomicTrie.TrieDB()
	synctest.AssertTrieConsistency(t, targetRoot, serverTrieDB, clientTrieDB, nil)

	// check all commit heights are created correctly
	hasher := trie.NewEmpty(triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil))

	serverTrie, err := trie.New(trie.TrieID(targetRoot), serverTrieDB)
	require.NoError(t, err)

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
			if err := hasher.Update(it.Key, it.Value); err != nil {
				return err
			}
		}
		return it.Err
	}

	for height := uint64(0); height <= targetHeight; height++ {
		require.NoErrorf(t, addAllKeysWithPrefix(database.PackUInt64(height)), "failed to add keys for height %d", height)

		if height%testCommitInterval == 0 {
			expected := hasher.Hash()
			root, err := atomicTrie.Root(height)
			require.NoError(t, err)
			require.Equal(t, expected, root)
		}
	}
}

// setupTestInfrastructure creates the common test infrastructure components.
// It returns the context, mock client, atomic backend, and client database.
func setupTestInfrastructure(t *testing.T, serverTrieDB *triedb.Database) (context.Context, *syncclient.TestClient, *state.AtomicBackend, *versiondb.Database) {
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	mockClient := syncclient.NewTestClient(
		message.Codec,
		handlers.NewLeafsRequestHandler(serverTrieDB, state.TrieKeyLength, nil, message.Codec, handlerstats.NewNoopHandlerStats()),
		nil,
		nil,
	)

	clientDB := versiondb.New(memdb.New())
	repo, err := state.NewAtomicTxRepository(clientDB, message.Codec, 0)
	require.NoError(t, err, "NewAtomicTxRepository()")

	atomicBackend, err := state.NewAtomicBackend(atomictest.TestSharedMemory(), nil, repo, 0, common.Hash{}, testCommitInterval)
	require.NoError(t, err, "NewAtomicBackend()")

	return ctx, mockClient, atomicBackend, clientDB
}
