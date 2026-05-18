// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/ethdb/memorydb"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/message/messagetest"
	"github.com/ava-labs/avalanchego/graft/evm/sync/client"
	"github.com/ava-labs/avalanchego/graft/evm/sync/handlers"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"

	handlerstats "github.com/ava-labs/avalanchego/graft/evm/sync/handlers/stats"
)

type codeSyncerTest struct {
	clientDB          ethdb.Database
	queueCapacity     int
	codeRequestHashes [][]common.Hash
	codeByteSlices    [][]byte
	getCodeIntercept  func(hashes []common.Hash, codeBytes [][]byte) ([][]byte, error)
	err               error
}

func testCodeSyncer(t *testing.T, test codeSyncerTest, c codec.Manager) {
	// Set up serverDB
	serverDB := memorydb.New()

	codeHashes := make([]common.Hash, 0, len(test.codeByteSlices))
	for _, codeBytes := range test.codeByteSlices {
		codeHash := crypto.Keccak256Hash(codeBytes)
		rawdb.WriteCode(serverDB, codeHash, codeBytes)
		codeHashes = append(codeHashes, codeHash)
	}

	// Set up mockClient
	codeRequestHandler := handlers.NewCodeRequestHandler(serverDB, c, handlerstats.NewNoopHandlerStats())
	mockClient := client.NewTestClient(c, nil, codeRequestHandler, nil)
	mockClient.GetCodeIntercept = test.getCodeIntercept

	clientDB := test.clientDB
	if clientDB == nil {
		clientDB = rawdb.NewMemoryDatabase()
	}

	codeQueue, err := NewQueue(
		clientDB,
		WithCapacity(test.queueCapacity),
	)
	require.NoError(t, err)

	codeSyncer, err := NewSyncer(
		mockClient,
		clientDB,
		codeQueue.CodeHashes(),
	)
	require.NoError(t, err)
	go func() {
		for _, codeHashes := range test.codeRequestHashes {
			if err := codeQueue.AddCode(t.Context(), codeHashes); err != nil {
				require.ErrorIs(t, err, test.err)
			}
		}
		if err := codeQueue.Finalize(); err != nil {
			require.ErrorIs(t, err, test.err)
		}
	}()

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	// Run the sync and handle expected error.
	err = codeSyncer.Sync(ctx)
	require.ErrorIs(t, err, test.err)
	if err != nil {
		return // don't check the state
	}

	// Assert that the client synced the code correctly.
	for i, codeHash := range codeHashes {
		codeBytes := rawdb.ReadCode(clientDB, codeHash)
		require.Equal(t, test.codeByteSlices[i], codeBytes)
	}
}

func TestCodeSyncerSingleCodeHash(t *testing.T) {
	t.Parallel()
	codeBytes := utils.RandomBytes(100)
	codeHash := crypto.Keccak256Hash(codeBytes)
	messagetest.ForEachCodec(t, func(c codec.Manager, _ message.LeafsRequestType) {
		testCodeSyncer(t, codeSyncerTest{
			codeRequestHashes: [][]common.Hash{{codeHash}},
			codeByteSlices:    [][]byte{codeBytes},
		}, c)
	})
}

func TestCodeSyncerManyCodeHashes(t *testing.T) {
	t.Parallel()
	numCodeSlices := 5000
	codeHashes := make([]common.Hash, 0, numCodeSlices)
	codeByteSlices := make([][]byte, 0, numCodeSlices)
	for i := 0; i < numCodeSlices; i++ {
		codeBytes := utils.RandomBytes(100)
		codeHash := crypto.Keccak256Hash(codeBytes)
		codeHashes = append(codeHashes, codeHash)
		codeByteSlices = append(codeByteSlices, codeBytes)
	}

	messagetest.ForEachCodec(t, func(c codec.Manager, _ message.LeafsRequestType) {
		testCodeSyncer(t, codeSyncerTest{
			queueCapacity:     10,
			codeRequestHashes: [][]common.Hash{codeHashes[0:100], codeHashes[100:2000], codeHashes[2000:2005], codeHashes[2005:]},
			codeByteSlices:    codeByteSlices,
		}, c)
	})
}

func TestCodeSyncerRequestErrors(t *testing.T) {
	t.Parallel()
	codeBytes := utils.RandomBytes(100)
	codeHash := crypto.Keccak256Hash(codeBytes)
	errDummy := errors.New("dummy error")
	messagetest.ForEachCodec(t, func(c codec.Manager, _ message.LeafsRequestType) {
		testCodeSyncer(t, codeSyncerTest{
			codeRequestHashes: [][]common.Hash{{codeHash}},
			codeByteSlices:    [][]byte{codeBytes},
			getCodeIntercept: func([]common.Hash, [][]byte) ([][]byte, error) {
				return nil, errDummy
			},
			err: errDummy,
		}, c)
	})
}

func TestCodeSyncerAddsInProgressCodeHashes(t *testing.T) {
	t.Parallel()
	codeBytes := utils.RandomBytes(100)
	codeHash := crypto.Keccak256Hash(codeBytes)
	messagetest.ForEachCodec(t, func(c codec.Manager, _ message.LeafsRequestType) {
		clientDB := rawdb.NewMemoryDatabase()
		require.NoError(t, customrawdb.WriteCodeToFetch(clientDB, codeHash))
		testCodeSyncer(t, codeSyncerTest{
			clientDB:          clientDB,
			codeRequestHashes: nil,
			codeByteSlices:    [][]byte{codeBytes},
		}, c)
	})
}

// TestCodeSyncerCleansMarkerRewrittenMidCleanup is a deterministic
// regression test for an orphan code-to-fetch marker. A wrapped client
// DB pauses the first Batch.Write after the delete commits. The test
// then rewrites the marker and enqueues a duplicate, forcing a sibling
// worker to handle a marker recreated mid-cleanup.
func TestCodeSyncerCleansMarkerRewrittenMidCleanup(t *testing.T) {
	t.Parallel()

	messagetest.ForEachCodec(t, func(c codec.Manager, _ message.LeafsRequestType) {
		codeBytes := utils.RandomBytes(100)
		codeHash := crypto.Keccak256Hash(codeBytes)

		// probeHash is a synchronization barrier. Its code is on disk so
		// dequeueing it does not affect codeHash's marker.
		probeBytes := utils.RandomBytes(100)
		probeHash := crypto.Keccak256Hash(probeBytes)

		rawDB := rawdb.NewMemoryDatabase()
		clientDB := newBlockingBatchDB(rawDB)

		rawdb.WriteCode(rawDB, codeHash, codeBytes)
		require.NoError(t, customrawdb.WriteCodeToFetch(rawDB, codeHash))
		rawdb.WriteCode(rawDB, probeHash, probeBytes)

		serverDB := memorydb.New()
		rawdb.WriteCode(serverDB, codeHash, codeBytes)
		rawdb.WriteCode(serverDB, probeHash, probeBytes)
		handler := handlers.NewCodeRequestHandler(serverDB, c, handlerstats.NewNoopHandlerStats())
		mockClient := client.NewTestClient(c, nil, handler, nil)

		ch := make(chan common.Hash)
		codeSyncer, err := NewSyncer(mockClient, clientDB, ch, WithNumWorkers(2))
		require.NoError(t, err)

		syncErrCh := make(chan error, 1)
		go func() { syncErrCh <- codeSyncer.Sync(t.Context()) }()

		// A worker takes codeHash, commits the marker delete, then pauses
		// inside the wrapped Batch.Write.
		ch <- codeHash
		<-clientDB.blocked

		// Rewrite the marker (modelling a concurrent AddCode) and enqueue
		// a duplicate for a sibling worker.
		require.NoError(t, customrawdb.WriteCodeToFetch(rawDB, codeHash))
		ch <- codeHash

		// Barrier! This send returns only after the sibling has processed
		// the duplicate and is back at the channel receive, so its decision
		// is committed before we release the paused worker.
		ch <- probeHash

		close(clientDB.release)
		close(ch)

		require.NoError(t, <-syncErrCh)

		it := customrawdb.NewCodeToFetchIterator(rawDB)
		defer it.Release()
		require.False(t, it.Next(), "stale code-to-fetch marker remained after sync")
		require.NoError(t, it.Error())
	})
}

// blockingBatchDB pauses inside the first Batch.Write after the commit
// lands. Subsequent writes are not blocked.
type blockingBatchDB struct {
	ethdb.Database
	primed  atomic.Bool
	blocked chan struct{}
	release chan struct{}
}

func newBlockingBatchDB(inner ethdb.Database) *blockingBatchDB {
	return &blockingBatchDB{
		Database: inner,
		blocked:  make(chan struct{}),
		release:  make(chan struct{}),
	}
}

func (db *blockingBatchDB) NewBatch() ethdb.Batch {
	return &blockingBatch{Batch: db.Database.NewBatch(), db: db}
}

type blockingBatch struct {
	ethdb.Batch
	db *blockingBatchDB
}

func (b *blockingBatch) Write() error {
	err := b.Batch.Write()
	if b.db.primed.CompareAndSwap(false, true) {
		close(b.db.blocked)
		<-b.db.release
	}
	return err
}

// TestCodeSyncerDuplicateAddCodeNoMarkerLeak stresses the same invariant
// end-to-end: many producers hammer AddCode for one hash, and after sync
// no code-to-fetch markers may remain. Two variants:
//
//   - "code-already-on-disk": code is pre-written, so each dequeue only
//     needs to delete the marker.
//   - "code-fetched-during-sync": the disk starts empty, the first dequeue
//     fetches the code from the network and later duplicates only delete.
func TestCodeSyncerDuplicateAddCodeNoMarkerLeak(t *testing.T) {
	tests := []struct {
		name         string
		preWriteCode bool
	}{
		{name: "code-already-on-disk", preWriteCode: true},
		{name: "code-fetched-during-sync", preWriteCode: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			messagetest.ForEachCodec(t, func(c codec.Manager, _ message.LeafsRequestType) {
				codeBytes := utils.RandomBytes(100)
				codeHash := crypto.Keccak256Hash(codeBytes)

				clientDB := rawdb.NewMemoryDatabase()
				if tt.preWriteCode {
					rawdb.WriteCode(clientDB, codeHash, codeBytes)
				}

				serverDB := memorydb.New()
				rawdb.WriteCode(serverDB, codeHash, codeBytes)
				handler := handlers.NewCodeRequestHandler(serverDB, c, handlerstats.NewNoopHandlerStats())
				mockClient := client.NewTestClient(c, nil, handler, nil)

				codeQueue, err := NewQueue(clientDB)
				require.NoError(t, err)

				const (
					numWorkers   = 8
					numProducers = 8
					iterations   = 50_000
				)
				codeSyncer, err := NewSyncer(mockClient, clientDB, codeQueue.CodeHashes(), WithNumWorkers(numWorkers))
				require.NoError(t, err)

				// Run the syncer in a goroutine so the main test can drive
				// producers and Finalize from the foreground.
				syncErrCh := make(chan error, 1)
				go func() {
					syncErrCh <- codeSyncer.Sync(t.Context())
				}()

				// Tight-loop AddCode from many producers maximises the chance
				// of landing inside a worker's marker-cleanup window.
				var producers errgroup.Group
				for range numProducers {
					producers.Go(func() error {
						for range iterations {
							if err := codeQueue.AddCode(t.Context(), []common.Hash{codeHash}); err != nil {
								return err
							}
						}
						return nil
					})
				}
				require.NoError(t, producers.Wait())
				require.NoError(t, codeQueue.Finalize())
				require.NoError(t, <-syncErrCh)

				require.Equal(t, codeBytes, rawdb.ReadCode(clientDB, codeHash))

				it := customrawdb.NewCodeToFetchIterator(clientDB)
				defer it.Release()
				require.False(t, it.Next(), "stale code-to-fetch marker remained after sync")
				require.NoError(t, it.Error())
			})
		})
	}
}

func TestCodeSyncerAddsMoreInProgressThanQueueSize(t *testing.T) {
	t.Parallel()
	numCodeSlices := 100
	codeHashes := make([]common.Hash, 0, numCodeSlices)
	codeByteSlices := make([][]byte, 0, numCodeSlices)
	for i := 0; i < numCodeSlices; i++ {
		codeBytes := utils.RandomBytes(100)
		codeHash := crypto.Keccak256Hash(codeBytes)
		codeHashes = append(codeHashes, codeHash)
		codeByteSlices = append(codeByteSlices, codeBytes)
	}

	messagetest.ForEachCodec(t, func(c codec.Manager, _ message.LeafsRequestType) {
		db := rawdb.NewMemoryDatabase()
		for _, codeHash := range codeHashes {
			require.NoError(t, customrawdb.WriteCodeToFetch(db, codeHash))
		}

		testCodeSyncer(t, codeSyncerTest{
			clientDB:          db,
			codeRequestHashes: nil,
			codeByteSlices:    codeByteSlices,
		}, c)
	})
}
