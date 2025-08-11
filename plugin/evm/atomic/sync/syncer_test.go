// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/ava-labs/coreth/plugin/evm/atomic/atomictest"
	"github.com/ava-labs/coreth/plugin/evm/atomic/state"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"

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

const (
	testCommitInterval = 1024
	testTargetHeight   = 100
	testNumWorkers     = 4
	testRequestSize    = config.DefaultStateSyncRequestSize
)

type atomicSyncTestCheckpoint struct {
	expectedNumLeavesSynced int64       // expected number of leaves to have synced at this checkpoint
	leafCutoff              int         // Number of leafs to sync before cutting off responses
	targetRoot              common.Hash // Root of trie to resume syncing from after stopping
	targetHeight            uint64      // Height to sync to after stopping
}

// TestConfigValidation is a parameterized test that covers all config validation scenarios.
func TestConfigValidation(t *testing.T) {
	_, mockClient, atomicBackend, root := setupParallelizationTest(t, 100)
	clientDB := versiondb.New(memdb.New())

	// Create a valid base config
	validConfig := Config{
		Client:       mockClient,
		Database:     clientDB,
		AtomicTrie:   atomicBackend.AtomicTrie(),
		TargetRoot:   root,
		TargetHeight: 100,
		RequestSize:  config.DefaultStateSyncRequestSize,
		NumWorkers:   defaultNumWorkers,
	}

	tests := []struct {
		name             string
		configModifyFunc func(*Config)
		expectedErr      error
		description      string
	}{
		// Basic validation tests
		{
			name:             "valid config",
			configModifyFunc: func(c *Config) {}, // No modification for valid case.
			expectedErr:      nil,
			description:      "should accept valid configuration",
		},
		{
			name:             "nil client",
			configModifyFunc: func(c *Config) { c.Client = nil },
			expectedErr:      errNilClient,
			description:      "should reject nil client",
		},
		{
			name:             "nil database",
			configModifyFunc: func(c *Config) { c.Database = nil },
			expectedErr:      errNilDatabase,
			description:      "should reject nil database",
		},
		{
			name:             "nil atomic trie",
			configModifyFunc: func(c *Config) { c.AtomicTrie = nil },
			expectedErr:      errNilAtomicTrie,
			description:      "should reject nil atomic trie",
		},
		{
			name:             "empty target root",
			configModifyFunc: func(c *Config) { c.TargetRoot = common.Hash{} },
			expectedErr:      errEmptyTargetRoot,
			description:      "should reject empty target root",
		},
		{
			name:             "zero target height",
			configModifyFunc: func(c *Config) { c.TargetHeight = 0 },
			expectedErr:      errInvalidTargetHeight,
			description:      "should reject zero target height",
		},
		{
			name:             "request size too small",
			configModifyFunc: func(c *Config) { c.RequestSize = 0 }, // This will be set to default, so no error
			expectedErr:      nil,
			description:      "should use default request size when zero",
		},
		{
			name:             "request size too large",
			configModifyFunc: func(c *Config) { c.RequestSize = maxRequestSize + 1 },
			expectedErr:      errInvalidRequestSize,
			description:      "should reject request size above maximum",
		},
		{
			name:             "zero request size (should use default)",
			configModifyFunc: func(c *Config) { c.RequestSize = 0 },
			expectedErr:      nil,
			description:      "should use default request size when zero",
		},
		{
			name:             "num workers too few",
			configModifyFunc: func(c *Config) { c.NumWorkers = 0 }, // This will be set to default, so no error
			expectedErr:      nil,
			description:      "should use default num workers when zero",
		},
		{
			name:             "num workers too many",
			configModifyFunc: func(c *Config) { c.NumWorkers = maxNumWorkers + 1 },
			expectedErr:      errTooManyWorkers,
			description:      "should reject num workers above maximum",
		},
		{
			name:             "zero num workers (should use default)",
			configModifyFunc: func(c *Config) { c.NumWorkers = 0 },
			expectedErr:      nil,
			description:      "should use default num workers when zero",
		},
		// Boundary tests
		{
			name:             "minimum valid request size",
			configModifyFunc: func(c *Config) { c.RequestSize = minRequestSize },
			expectedErr:      nil,
			description:      "should accept minimum valid request size",
		},
		{
			name:             "maximum valid request size",
			configModifyFunc: func(c *Config) { c.RequestSize = maxRequestSize },
			expectedErr:      nil,
			description:      "should accept maximum valid request size",
		},
		{
			name:             "minimum valid num workers",
			configModifyFunc: func(c *Config) { c.NumWorkers = minNumWorkers },
			expectedErr:      nil,
			description:      "should accept minimum valid num workers",
		},
		{
			name:             "maximum valid num workers",
			configModifyFunc: func(c *Config) { c.NumWorkers = maxNumWorkers },
			expectedErr:      nil,
			description:      "should accept maximum valid num workers",
		},
		// Edge cases
		{
			name:             "target height one",
			configModifyFunc: func(c *Config) { c.TargetHeight = 1 },
			expectedErr:      nil,
			description:      "should accept target height of 1",
		},
		{
			name:             "target height max uint64",
			configModifyFunc: func(c *Config) { c.TargetHeight = math.MaxUint64 },
			expectedErr:      nil,
			description:      "should accept maximum target height",
		},
		{
			name:             "request size exactly at bounds",
			configModifyFunc: func(c *Config) { c.RequestSize = minRequestSize },
			expectedErr:      nil,
			description:      "should accept request size at minimum bound",
		},
		{
			name:             "num workers exactly at bounds",
			configModifyFunc: func(c *Config) { c.NumWorkers = minNumWorkers },
			expectedErr:      nil,
			description:      "should accept num workers at minimum bound",
		},
		{
			name:             "negative num workers",
			configModifyFunc: func(c *Config) { c.NumWorkers = -1 },
			expectedErr:      errTooFewWorkers,
			description:      "should reject negative num workers",
		},
		{
			name:             "request size overflow",
			configModifyFunc: func(c *Config) { c.RequestSize = math.MaxUint16 },
			expectedErr:      errInvalidRequestSize,
			description:      "should reject request size that exceeds max allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a copy of the valid config
			config := validConfig

			// Apply the modification
			tt.configModifyFunc(&config)

			// Test validation
			err := config.Validate()
			require.ErrorIs(t, err, tt.expectedErr, tt.description)
		})
	}
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
			rand.Seed(1)
			targetHeight := 10 * uint64(testCommitInterval)
			serverTrieDB := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
			root, _, _ := statesynctest.GenerateTrie(t, serverTrieDB, int(targetHeight), state.TrieKeyLength)

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
			rand.Seed(1)
			targetHeight := 10 * uint64(testCommitInterval)
			serverTrieDB := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
			numTrieKeys := int(targetHeight) - 1 // no atomic ops for genesis
			root, _, _ := statesynctest.GenerateTrie(t, serverTrieDB, numTrieKeys, state.TrieKeyLength)

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
			rand.Seed(1)
			targetHeight1 := 10 * uint64(testCommitInterval)
			serverTrieDB := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
			numTrieKeys1 := int(targetHeight1) - 1 // no atomic ops for genesis
			root1, _, _ := statesynctest.GenerateTrie(t, serverTrieDB, numTrieKeys1, state.TrieKeyLength)

			targetHeight2 := 20 * uint64(testCommitInterval)
			numTrieKeys2 := int(targetHeight2) - 1 // no atomic ops for genesis
			root2, _, _ := statesynctest.FillTrie(
				t, numTrieKeys1, numTrieKeys2, state.TrieKeyLength, serverTrieDB, root1,
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
			name:              "parallelization with 4 workers",
			targetHeight:      2 * uint64(testCommitInterval), // 2,048 leaves for meaningful parallelization
			numWorkers:        4,
			useDefaultWorkers: false,
			description:       "should work correctly with 4 worker goroutines",
		},
		{
			name:              "default parallelization",
			targetHeight:      uint64(testCommitInterval), // 1,024 leaves to test commit boundary
			numWorkers:        0,
			useDefaultWorkers: true,
			description:       "should default to parallelization with default workers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, mockClient, atomicBackend, root := setupParallelizationTest(t, tt.targetHeight)
			runParallelizationTest(t, ctx, mockClient, atomicBackend, root, tt.targetHeight, tt.numWorkers, tt.useDefaultWorkers)
		})
	}
}

// TestSyncerContextCancellation verifies that the syncer properly handles context cancellation.
func TestSyncerContextCancellation(t *testing.T) {
	ctx, mockClient, atomicBackend, root := setupParallelizationTest(t, testTargetHeight)
	config := createTestConfig(mockClient, atomicBackend, root, testTargetHeight)
	syncer := createTestSyncer(t, config)

	// Immediately cancel the context to simulate cancellation.
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	err := syncer.Sync(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

// setupParallelizationTest creates the common test infrastructure for parallelization tests.
// It returns the context, mock client, atomic backend, and root hash for testing.
func setupParallelizationTest(t *testing.T, targetHeight uint64) (context.Context, *syncclient.TestClient, *state.AtomicBackend, common.Hash) {
	// Create a simple test trie with some data.
	serverTrieDB := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
	root, _, _ := statesynctest.GenerateTrie(t, serverTrieDB, int(targetHeight), state.TrieKeyLength)

	ctx, mockClient, atomicBackend, _ := setupTestInfrastructure(t, serverTrieDB)

	return ctx, mockClient, atomicBackend, root
}

// runParallelizationTest executes a parallelization test with the given parameters.
func runParallelizationTest(t *testing.T, ctx context.Context, mockClient *syncclient.TestClient, atomicBackend *state.AtomicBackend, root common.Hash, targetHeight uint64, numWorkers int, useDefaultWorkers bool) {
	config := createTestConfig(mockClient, atomicBackend, root, targetHeight)

	// Set worker count based on test type
	if useDefaultWorkers {
		config.NumWorkers = defaultNumWorkers
	} else {
		config.NumWorkers = numWorkers
	}

	syncer := createTestSyncer(t, config)
	workerType := "default workers"
	if !useDefaultWorkers {
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
		// Create syncer targeting the current [syncTrie].
		syncerConfig := Config{
			Client:       mockClient,
			Database:     clientDB,
			AtomicTrie:   atomicBackend.AtomicTrie(),
			TargetRoot:   targetRoot,
			TargetHeight: targetHeight,
			RequestSize:  config.DefaultStateSyncRequestSize,
			NumWorkers:   numWorkers,
		}
		syncer, err := newSyncer(&syncerConfig)
		require.NoError(t, err, "could not create syncer")
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
	syncerConfig := Config{
		Client:       mockClient,
		Database:     clientDB,
		AtomicTrie:   atomicBackend.AtomicTrie(),
		TargetRoot:   targetRoot,
		TargetHeight: targetHeight,
		RequestSize:  config.DefaultStateSyncRequestSize,
		NumWorkers:   numWorkers,
	}
	syncer, err := newSyncer(&syncerConfig)
	require.NoError(t, err, "could not create syncer")
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
	statesynctest.AssertTrieConsistency(t, targetRoot, serverTrieDB, clientTrieDB, nil)

	// check all commit heights are created correctly
	hasher := trie.NewEmpty(triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil))

	serverTrie, err := trie.New(trie.TrieID(targetRoot), serverTrieDB)
	if err != nil {
		t.Fatalf("failed to create server trie: %v", err)
	}
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
		if err := addAllKeysWithPrefix(database.PackUInt64(height)); err != nil {
			t.Fatalf("failed to add keys for height %d: %v", height, err)
		}

		if height%testCommitInterval == 0 {
			expected := hasher.Hash()
			root, err := atomicTrie.Root(height)
			if err != nil {
				t.Fatalf("failed to get root for height %d: %v", height, err)
			}
			require.Equal(t, expected, root)
		}
	}
}

// setupTestInfrastructure creates the common test infrastructure components.
// It returns the context, mock client, atomic backend, and client database.
func setupTestInfrastructure(t *testing.T, serverTrieDB *triedb.Database) (context.Context, *syncclient.TestClient, *state.AtomicBackend, *versiondb.Database) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	mockClient := syncclient.NewTestClient(
		message.Codec,
		handlers.NewLeafsRequestHandler(serverTrieDB, state.TrieKeyLength, nil, message.Codec, handlerstats.NewNoopHandlerStats()),
		nil,
		nil,
	)

	clientDB := versiondb.New(memdb.New())
	repo, err := state.NewAtomicTxRepository(clientDB, message.Codec, 0)
	require.NoError(t, err, "could not initialize atomic tx repository")

	atomicBackend, err := state.NewAtomicBackend(atomictest.TestSharedMemory(), nil, repo, 0, common.Hash{}, testCommitInterval)
	require.NoError(t, err, "could not initialize atomic backend")

	return ctx, mockClient, atomicBackend, clientDB
}

// createTestConfig creates a test configuration with default values.
func createTestConfig(mockClient *syncclient.TestClient, atomicBackend *state.AtomicBackend, root common.Hash, targetHeight uint64) Config {
	return Config{
		Client:       mockClient,
		Database:     versiondb.New(memdb.New()),
		AtomicTrie:   atomicBackend.AtomicTrie(),
		TargetRoot:   root,
		TargetHeight: targetHeight,
		RequestSize:  testRequestSize,
		NumWorkers:   0, // Will use default
	}
}

// createTestSyncer creates a test syncer with the given configuration.
func createTestSyncer(t *testing.T, config Config) *syncer {
	syncer, err := newSyncer(&config)
	require.NoError(t, err, "could not create syncer")
	return syncer
}
