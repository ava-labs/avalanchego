// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"context"
	"math/rand"
	"sync/atomic"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/message/messagetest"
	"github.com/ava-labs/avalanchego/graft/evm/sync/client"
	"github.com/ava-labs/avalanchego/graft/evm/sync/code"
	"github.com/ava-labs/avalanchego/graft/evm/sync/handlers"
	"github.com/ava-labs/avalanchego/graft/evm/sync/synctest"

	handlerstats "github.com/ava-labs/avalanchego/graft/evm/sync/handlers/stats"
	synctypes "github.com/ava-labs/avalanchego/graft/evm/sync/types"
)

// dynamicSyncTestEnv holds shared infrastructure for dynamic sync tests.
type dynamicSyncTestEnv struct {
	mockClient  *client.TestClient
	clientEthDB ethdb.Database
	clientDB    state.Database
	serverDB    state.Database
	codeQueue   *code.SessionedQueue
	codeSyncer  interface{ Sync(context.Context) error }
}

func newDynamicSyncTestEnv(t *testing.T, serverDB state.Database, c codec.Manager) *dynamicSyncTestEnv {
	t.Helper()
	clientEthDB := rawdb.NewMemoryDatabase()

	leafsHandler := handlers.NewLeafsRequestHandler(serverDB.TrieDB(), message.StateTrieKeyLength, nil, c, handlerstats.NewNoopHandlerStats())
	codeHandler := handlers.NewCodeRequestHandler(serverDB.DiskDB(), c, handlerstats.NewNoopHandlerStats())
	mockClient := client.NewTestClient(c, leafsHandler, codeHandler, nil)

	quit := make(chan struct{})
	t.Cleanup(func() { close(quit) })

	codeQueue, err := code.NewSessionedQueue(clientEthDB, quit)
	require.NoError(t, err)

	codeSyncer, err := code.NewDynamicSyncer(mockClient, clientEthDB, codeQueue)
	require.NoError(t, err)

	return &dynamicSyncTestEnv{
		mockClient:  mockClient,
		clientEthDB: clientEthDB,
		clientDB:    state.NewDatabase(clientEthDB),
		serverDB:    serverDB,
		codeQueue:   codeQueue,
		codeSyncer:  codeSyncer,
	}
}

// runSync creates a dynamic syncer for root, runs it alongside the code
// syncer to completion, verifies DB consistency, and returns the syncer.
func (e *dynamicSyncTestEnv) runSync(t *testing.T, root common.Hash, leafReqType message.LeafsRequestType) *synctypes.DynamicSyncer {
	t.Helper()
	stateSyncer, err := NewHashDBDynamicSyncer(
		e.mockClient, e.clientEthDB, root, e.codeQueue,
		testRequestSize, leafReqType, WithBatchSize(1000),
	)
	require.NoError(t, err)

	eg, egCtx := errgroup.WithContext(t.Context())
	eg.Go(func() error { return e.codeSyncer.Sync(egCtx) })
	eg.Go(func() error { return stateSyncer.Sync(egCtx) })
	require.NoError(t, eg.Wait())

	synctest.AssertDBConsistency(t, root, e.clientDB, e.serverDB)

	return stateSyncer
}

func TestDynamicSync_CompletesWithoutPivot(t *testing.T) {
	t.Parallel()
	messagetest.ForEachCodec(t, func(c codec.Manager, leafReqType message.LeafsRequestType) {
		r := rand.New(rand.NewSource(1))
		serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
		root, _ := synctest.FillAccounts(t, r, serverDB, common.Hash{}, 250, nil)

		env := newDynamicSyncTestEnv(t, serverDB, c)
		env.runSync(t, root, leafReqType)
	})
}

func TestDynamicSync_WithCodeAndStorage(t *testing.T) {
	t.Parallel()
	messagetest.ForEachCodec(t, func(c codec.Manager, leafReqType message.LeafsRequestType) {
		r := rand.New(rand.NewSource(1))
		serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
		root, _ := synctest.FillAccounts(t, r, serverDB, common.Hash{}, 100, func(t *testing.T, index int, addr common.Address, account types.StateAccount, storageTr state.Trie) types.StateAccount {
			if index%3 == 0 {
				codeBytes := make([]byte, 256)
				_, err := r.Read(codeBytes)
				require.NoError(t, err)
				codeHash := crypto.Keccak256Hash(codeBytes)
				rawdb.WriteCode(serverDB.DiskDB(), codeHash, codeBytes)
				account.CodeHash = codeHash[:]
			}
			if index%5 == 0 {
				synctest.FillStorageForAccount(t, r, 16, addr, storageTr)
			}
			return account
		})

		env := newDynamicSyncTestEnv(t, serverDB, c)
		env.runSync(t, root, leafReqType)
	})
}

func TestDynamicSync_PivotMidSync(t *testing.T) {
	t.Parallel()
	messagetest.ForEachCodec(t, func(c codec.Manager, leafReqType message.LeafsRequestType) {
		r := rand.New(rand.NewSource(1))
		serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
		root1, _ := synctest.FillAccountsWithStorageAndCode(t, r, serverDB, types.EmptyRootHash, 500)
		root2, _ := synctest.FillAccountsWithStorageAndCode(t, r, serverDB, root1, 200)
		require.NotEqual(t, root1, root2)

		env := newDynamicSyncTestEnv(t, serverDB, c)

		// Start syncing root1, then pivot to root2 mid-sync.
		stateSyncer, err := NewHashDBDynamicSyncer(
			env.mockClient, env.clientEthDB, root1, env.codeQueue,
			testRequestSize, leafReqType, WithBatchSize(1000),
		)
		require.NoError(t, err)

		// Trigger pivot to root2 on the first leafs response.
		var pivotTriggered atomic.Bool
		env.mockClient.GetLeafsIntercept = func(_ message.LeafsRequest, resp message.LeafsResponse) (message.LeafsResponse, error) {
			if pivotTriggered.CompareAndSwap(false, true) {
				_ = stateSyncer.UpdateTarget(&synctest.SyncTarget{BlockRoot: root2, BlockHeight: 200})
			}
			return resp, nil
		}

		eg, egCtx := errgroup.WithContext(t.Context())
		eg.Go(func() error { return env.codeSyncer.Sync(egCtx) })
		eg.Go(func() error { return stateSyncer.Sync(egCtx) })
		require.NoError(t, eg.Wait())
		require.True(t, pivotTriggered.Load(), "pivot should have been triggered")

		// The final state should match root2, not root1.
		synctest.AssertDBConsistency(t, root2, env.clientDB, serverDB)
	})
}

func TestDynamicSync_UpdateTarget_StaleIgnored(t *testing.T) {
	t.Parallel()
	messagetest.ForEachCodec(t, func(c codec.Manager, leafReqType message.LeafsRequestType) {
		r := rand.New(rand.NewSource(1))
		serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
		root, _ := synctest.FillAccounts(t, r, serverDB, common.Hash{}, 50, nil)

		env := newDynamicSyncTestEnv(t, serverDB, c)
		ds := env.runSync(t, root, leafReqType)

		// After sync completes, calling UpdateTarget with a lower height is a no-op.
		require.NoError(t, ds.UpdateTarget(&synctest.SyncTarget{
			BlockRoot:   common.HexToHash("0xdead"),
			BlockHeight: 0,
		}))
	})
}

func TestDynamicSync_UpdateTarget_SameRootDifferentHeight(t *testing.T) {
	t.Parallel()
	messagetest.ForEachCodec(t, func(c codec.Manager, leafReqType message.LeafsRequestType) {
		r := rand.New(rand.NewSource(1))
		serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
		root, _ := synctest.FillAccounts(t, r, serverDB, common.Hash{}, 50, nil)

		env := newDynamicSyncTestEnv(t, serverDB, c)
		ds := env.runSync(t, root, leafReqType)

		// Same root but higher height should update height without triggering a pivot.
		prevHeight := ds.TargetHeight()
		newHeight := prevHeight + 100
		require.NoError(t, ds.UpdateTarget(&synctest.SyncTarget{
			BlockRoot:   ds.DesiredRoot(),
			BlockHeight: newHeight,
		}))
		require.Equal(t, newHeight, ds.TargetHeight())
	})
}

func TestDynamicSync_UpdateTarget_StaticNoop(t *testing.T) {
	t.Parallel()
	messagetest.ForEachCodec(t, func(c codec.Manager, leafReqType message.LeafsRequestType) {
		r := rand.New(rand.NewSource(1))
		serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
		root, _ := synctest.FillAccounts(t, r, serverDB, common.Hash{}, 50, nil)
		clientEthDB := rawdb.NewMemoryDatabase()

		leafsHandler := handlers.NewLeafsRequestHandler(serverDB.TrieDB(), message.StateTrieKeyLength, nil, c, handlerstats.NewNoopHandlerStats())
		codeHandler := handlers.NewCodeRequestHandler(serverDB.DiskDB(), c, handlerstats.NewNoopHandlerStats())
		mockClient := client.NewTestClient(c, leafsHandler, codeHandler, nil)

		codeQueue, err := code.NewQueue(clientEthDB)
		require.NoError(t, err)

		stateSyncer, err := NewHashDBSyncer(mockClient, clientEthDB, root, codeQueue, testRequestSize, leafReqType, WithFinalizeCodeQueue(codeQueue.Finalize))
		require.NoError(t, err)

		// UpdateTarget on a static syncer is a no-op.
		require.NoError(t, stateSyncer.UpdateTarget(&synctest.SyncTarget{
			BlockRoot:   common.HexToHash("0xbeef"),
			BlockHeight: 999,
		}))
	})
}

func TestDynamicSync_ContextCancellation(t *testing.T) {
	t.Parallel()
	messagetest.ForEachCodec(t, func(c codec.Manager, leafReqType message.LeafsRequestType) {
		r := rand.New(rand.NewSource(1))
		serverDB := state.NewDatabase(rawdb.NewMemoryDatabase())
		root, _ := synctest.FillAccountsWithStorageAndCode(t, r, serverDB, types.EmptyRootHash, 2000)

		env := newDynamicSyncTestEnv(t, serverDB, c)

		stateSyncer, err := NewHashDBDynamicSyncer(
			env.mockClient, env.clientEthDB, root, env.codeQueue,
			testRequestSize, leafReqType,
		)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		t.Cleanup(cancel)

		// Cancel context on first leafs response.
		env.mockClient.GetLeafsIntercept = func(_ message.LeafsRequest, resp message.LeafsResponse) (message.LeafsResponse, error) {
			cancel()
			return resp, nil
		}

		eg, egCtx := errgroup.WithContext(ctx)
		eg.Go(func() error { return env.codeSyncer.Sync(egCtx) })
		eg.Go(func() error { return stateSyncer.Sync(egCtx) })
		err = eg.Wait()
		require.ErrorIs(t, err, context.Canceled)
	})
}
