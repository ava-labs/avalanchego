// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/ethdb/memorydb"
	"github.com/stretchr/testify/require"

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

// TestCodeSyncerCleansMarkerWhenCodeOnDiskAndInFlightHeld is a deterministic
// regression test for an orphan code-to-fetch marker. When the code is
// already on disk, the worker must delete the marker even if inFlight is
// already held by another worker.
func TestCodeSyncerCleansMarkerWhenCodeOnDiskAndInFlightHeld(t *testing.T) {
	t.Parallel()

	messagetest.ForEachCodec(t, func(c codec.Manager, _ message.LeafsRequestType) {
		codeBytes := utils.RandomBytes(100)
		codeHash := crypto.Keccak256Hash(codeBytes)

		clientDB := rawdb.NewMemoryDatabase()
		rawdb.WriteCode(clientDB, codeHash, codeBytes)
		require.NoError(t, customrawdb.WriteCodeToFetch(clientDB, codeHash))

		serverDB := memorydb.New()
		rawdb.WriteCode(serverDB, codeHash, codeBytes)
		handler := handlers.NewCodeRequestHandler(serverDB, c, handlerstats.NewNoopHandlerStats())
		mockClient := client.NewTestClient(c, nil, handler, nil)

		ch := make(chan common.Hash, 1)
		codeSyncer, err := NewSyncer(mockClient, clientDB, ch)
		require.NoError(t, err)

		// Pretend another worker is already processing this hash.
		codeSyncer.inFlight.Store(codeHash, struct{}{})

		ch <- codeHash
		close(ch)

		require.NoError(t, codeSyncer.Sync(t.Context()))

		it := customrawdb.NewCodeToFetchIterator(clientDB)
		defer it.Release()
		require.False(t, it.Next(), "stale code-to-fetch marker remained after sync")
		require.NoError(t, it.Error())
	})
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

				codeSyncer, err := NewSyncer(mockClient, clientDB, codeQueue.CodeHashes())
				require.NoError(t, err)

				// Tight-loop AddCode from many producers maximises the chance of
				// landing inside a worker's marker-cleanup window.
				const (
					numProducers = 8
					iterations   = 50_000
				)
				var wg sync.WaitGroup
				wg.Add(numProducers)
				for range numProducers {
					go func() {
						defer wg.Done()
						for range iterations {
							if err := codeQueue.AddCode(t.Context(), []common.Hash{codeHash}); err != nil {
								return
							}
						}
					}()
				}
				go func() {
					wg.Wait()
					_ = codeQueue.Finalize()
				}()

				require.NoError(t, codeSyncer.Sync(t.Context()))
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
