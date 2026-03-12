// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/crypto"
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

// newTestCodeSyncClient creates a server DB populated with the given code slices
// and returns a test client backed by it, along with the corresponding hashes.
func newTestCodeSyncClient(t *testing.T, c codec.Manager, codeSlices [][]byte) (*client.TestClient, []common.Hash) {
	t.Helper()
	serverDB := memorydb.New()
	hashes := make([]common.Hash, len(codeSlices))
	for i, code := range codeSlices {
		hashes[i] = crypto.Keccak256Hash(code)
		rawdb.WriteCode(serverDB, hashes[i], code)
	}
	handler := handlers.NewCodeRequestHandler(serverDB, c, handlerstats.NewNoopHandlerStats())
	return client.NewTestClient(c, nil, handler, nil), hashes
}

func TestCodeSyncer_BasicScenarios(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		numCodes      int
		queueCapacity int
		preFetchAll   bool // hashes come from DB markers instead of AddCode
	}{
		{
			name:     "single code hash",
			numCodes: 1,
		},
		{
			name:          "many code hashes with small queue",
			numCodes:      5000,
			queueCapacity: 10,
		},
		{
			name:        "resumes from prefetch markers",
			numCodes:    1,
			preFetchAll: true,
		},
		{
			name:        "prefetch overflow exceeds default queue capacity",
			numCodes:    100,
			preFetchAll: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			codeSlices := make([][]byte, tt.numCodes)
			for i := range codeSlices {
				codeSlices[i] = utils.RandomBytes(100)
			}

			messagetest.ForEachCodec(t, func(c codec.Manager, _ message.LeafsRequestType) {
				mockClient, codeHashes := newTestCodeSyncClient(t, c, codeSlices)
				clientDB := rawdb.NewMemoryDatabase()

				if tt.preFetchAll {
					for _, h := range codeHashes {
						require.NoError(t, customrawdb.WriteCodeToFetch(clientDB, h))
					}
				}

				var qopts []QueueOption
				if tt.queueCapacity > 0 {
					qopts = append(qopts, WithCapacity(tt.queueCapacity))
				}
				queue, err := NewQueue(clientDB, make(chan struct{}), qopts...)
				require.NoError(t, err)

				syncer, err := NewSyncer(mockClient, clientDB, queue.CodeHashes())
				require.NoError(t, err)

				go func() {
					if !tt.preFetchAll {
						_ = queue.AddCode(t.Context(), codeHashes)
					}
					_ = queue.Finalize()
				}()

				require.NoError(t, syncer.Sync(t.Context()))
				for i, h := range codeHashes {
					require.Equal(t, codeSlices[i], rawdb.ReadCode(clientDB, h))
				}
			})
		})
	}
}

func TestCodeSyncer_RequestError(t *testing.T) {
	t.Parallel()

	codeBytes := utils.RandomBytes(100)
	codeHash := crypto.Keccak256Hash(codeBytes)
	errDummy := errors.New("dummy error")

	messagetest.ForEachCodec(t, func(c codec.Manager, _ message.LeafsRequestType) {
		mockClient, _ := newTestCodeSyncClient(t, c, [][]byte{codeBytes})
		mockClient.GetCodeIntercept = func([]common.Hash, [][]byte) ([][]byte, error) {
			return nil, errDummy
		}

		clientDB := rawdb.NewMemoryDatabase()
		queue, err := NewQueue(clientDB, make(chan struct{}), WithCapacity(1))
		require.NoError(t, err)

		syncer, err := NewSyncer(mockClient, clientDB, queue.CodeHashes(), WithNumWorkers(1), WithCodeHashesPerRequest(1))
		require.NoError(t, err)

		go func() {
			_ = queue.AddCode(t.Context(), []common.Hash{codeHash})
			_ = queue.Finalize()
		}()

		err = syncer.Sync(t.Context())
		require.ErrorIs(t, err, errDummy)

		// Verify in-flight entries are cleaned up after error.
		_, loaded := syncer.inFlight.Load(codeHash)
		require.False(t, loaded)
	})
}

// TestCodeSyncer_SessionedWorkerErrorDoesNotHang verifies that a worker failure
// in session mode unblocks sendHash instead of hanging forever. Without the
// runner-context cancellation in startSession, sendHash blocks on a full hashes
// channel because runner.ctx (parent of the errgroup context) stays alive.
func TestCodeSyncer_SessionedWorkerErrorDoesNotHang(t *testing.T) {
	t.Parallel()

	errDummy := errors.New("worker failure")

	messagetest.ForEachCodec(t, func(c codec.Manager, _ message.LeafsRequestType) {
		// Create enough distinct codes so the hashes overflow the worker's
		// channel buffer (size 1 with NumWorkers=1, CodeHashesPerReq=1).
		const numCodes = 50
		codeSlices := make([][]byte, numCodes)
		for i := range codeSlices {
			codeSlices[i] = utils.RandomBytes(100)
		}

		mockClient, codeHashes := newTestCodeSyncClient(t, c, codeSlices)
		mockClient.GetCodeIntercept = func([]common.Hash, [][]byte) ([][]byte, error) {
			return nil, errDummy
		}

		clientDB := rawdb.NewMemoryDatabase()
		quit := make(chan struct{})
		codeQueue, err := NewSessionedQueue(clientDB, quit, WithSessionedCapacity(100))
		require.NoError(t, err)

		codeSyncer, err := NewDynamicSyncer(
			mockClient, clientDB, codeQueue,
			WithNumWorkers(1), WithCodeHashesPerRequest(1),
		)
		require.NoError(t, err)

		// Use a short timeout so the test fails fast if the syncer hangs.
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		t.Cleanup(cancel)

		syncDone := make(chan error, 1)
		go func() { syncDone <- codeSyncer.Sync(ctx) }()

		_, err = codeQueue.Start(common.Hash{1})
		require.NoError(t, err)

		for _, h := range codeHashes {
			_ = codeQueue.AddCode(t.Context(), []common.Hash{h})
		}

		require.NoError(t, codeQueue.Finalize())
		close(quit)

		select {
		case err := <-syncDone:
			require.ErrorIs(t, err, errDummy)
		case <-ctx.Done():
			require.FailNow(t, "sync hung after worker error - runner.ctx was not canceled")
		}
	})
}

func TestCodeSyncer_SessionedQueuePivotIgnoresStaleHashes(t *testing.T) {
	t.Parallel()

	messagetest.ForEachCodec(t, func(c codec.Manager, _ message.LeafsRequestType) {
		codeBytes1 := utils.RandomBytes(100)
		codeBytes2 := utils.RandomBytes(100)
		h1 := crypto.Keccak256Hash(codeBytes1)
		h2 := crypto.Keccak256Hash(codeBytes2)

		mockClient, _ := newTestCodeSyncClient(t, c, [][]byte{codeBytes1, codeBytes2})

		clientDB := rawdb.NewMemoryDatabase()
		quit := make(chan struct{})
		codeQueue, err := NewSessionedQueue(clientDB, quit, WithSessionedCapacity(10))
		require.NoError(t, err)

		codeSyncer, err := NewDynamicSyncer(
			mockClient,
			clientDB,
			codeQueue,
			WithNumWorkers(1),
			WithCodeHashesPerRequest(1),
		)
		require.NoError(t, err)

		syncDone := make(chan error, 1)
		go func() { syncDone <- codeSyncer.Sync(t.Context()) }()

		sid1, err := codeQueue.Start(common.Hash{1})
		require.NoError(t, err)
		sid2, changed, err := codeQueue.PivotTo(common.Hash{2})
		require.NoError(t, err)
		require.True(t, changed)
		require.NotEqual(t, sid1, sid2)

		// Verify pivot cleared markers.
		it := customrawdb.NewCodeToFetchIterator(clientDB)
		defer it.Release()
		require.False(t, it.Next(), "expected pivot to clear code-to-fetch markers")
		require.NoError(t, it.Error())

		// Inject a stale event from the old session — syncer should ignore it.
		require.NoError(t, codeQueue.sendEventLocked(t.Context(), Event{
			Type:      EventCodeHash,
			SessionID: sid1,
			Hash:      h1,
		}))

		// Enqueue a valid hash for the current session.
		require.NoError(t, codeQueue.AddCode(t.Context(), []common.Hash{h2}))

		require.NoError(t, codeQueue.Finalize())
		close(quit)
		require.NoError(t, <-syncDone)

		require.Empty(t, rawdb.ReadCode(clientDB, h1), "stale session hash should be ignored")
		require.Equal(t, codeBytes2, rawdb.ReadCode(clientDB, h2))
	})
}
