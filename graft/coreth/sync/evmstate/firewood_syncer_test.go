// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"context"
	"math/rand"
	"testing"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/triedb"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/database/merkle/firewood/syncer"
	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/graft/coreth/sync/code"
	"github.com/ava-labs/avalanchego/graft/coreth/sync/handlers"
	"github.com/ava-labs/avalanchego/graft/evm/firewood"
	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/sync/synctest"
	"github.com/ava-labs/avalanchego/graft/evm/utils/utilstest"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"

	statesyncclient "github.com/ava-labs/avalanchego/graft/coreth/sync/client"
	handlerstats "github.com/ava-labs/avalanchego/graft/coreth/sync/handlers/stats"
)

func TestFirewoodSync(t *testing.T) {
	tests := []struct {
		name       string
		clientSize int
		serverSize int
	}{
		{
			name:       "both empty",
			clientSize: 0,
			serverSize: 0,
		},
		{
			name:       "one request from empty",
			clientSize: 0,
			serverSize: 1000,
		},
		{
			name:       "replace all",
			clientSize: 10_000,
			serverSize: 10_000,
		},
		{
			name:       "10,000 keys from empty",
			clientSize: 0,
			serverSize: 10_000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := rand.New(rand.NewSource(1))

			clientState := createDB(t)
			_, _ = synctest.FillAccountsWithStorageAndCode(t, r, clientState, types.EmptyRootHash, tt.clientSize)

			serverState := createDB(t)
			// Store the expected accounts to verify after sync.
			root, accounts := synctest.FillAccountsWithStorageAndCode(t, r, serverState, types.EmptyRootHash, tt.serverSize)

			firewoodSyncer, codeSyncer, codeQueue := createSyncers(t, clientState, serverState, root)
			require.NoError(t, runFirewoodSync(t.Context(), codeSyncer, firewoodSyncer), "failure during sync")

			// Finalization is not necessary, as no errors were encountered during sync.
			assertFirewoodConsistency(t, root, clientState, accounts)

			// Code queue should be closed.
			err := codeQueue.AddCode(t.Context(), []common.Hash{{1}})
			require.ErrorIs(t, err, code.ErrFailedToAddCodeHashesToQueue)
		})
	}
}

func TestFirewoodSyncerFinalizeScenarios(t *testing.T) {
	tests := []struct {
		name   string
		cancel bool
	}{
		{
			name:   "success finalizes queue",
			cancel: false,
		},
		{
			name:   "cancel then finalize",
			cancel: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := rand.New(rand.NewSource(1))

			clientState := createDB(t)
			_, _ = synctest.FillAccountsWithStorageAndCode(t, r, clientState, types.EmptyRootHash, 0)

			serverState := createDB(t)
			root, _ := synctest.FillAccountsWithStorageAndCode(t, r, serverState, types.EmptyRootHash, 10)

			firewoodSyncer, codeSyncer, codeQueue := createSyncers(t, clientState, serverState, root)

			ctx := t.Context()
			var cancel context.CancelFunc
			if tt.cancel {
				ctx, cancel = context.WithCancel(ctx)
				t.Cleanup(cancel)
			}

			if tt.cancel {
				// Trigger cancellation and wait for both syncers to exit.
				cancel()
				err := runFirewoodSync(ctx, codeSyncer, firewoodSyncer)
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.NoError(t, runFirewoodSync(ctx, codeSyncer, firewoodSyncer), "failure during sync")
			}

			// Ensure Finalize is idempotent and that the queue is closed.
			require.NoError(t, firewoodSyncer.Finalize())
			require.NoError(t, firewoodSyncer.Finalize())

			// After finalize, the queue should reject new code additions.
			err := codeQueue.AddCode(t.Context(), []common.Hash{{1}})
			require.ErrorIs(t, err, code.ErrFailedToAddCodeHashesToQueue)
		})
	}
}

func createSyncers(t *testing.T, clientState, serverState state.Database, root common.Hash) (*FirewoodSyncer, *code.Syncer, *code.Queue) {
	t.Helper()
	// Create the mock P2P client that serves range proofs and change proofs from the server DB.
	var (
		codeRequestHandler = handlers.NewCodeRequestHandler(serverState.DiskDB(), message.CorethCodec, handlerstats.NewNoopHandlerStats())
		mockClient         = statesyncclient.NewTestClient(message.CorethCodec, nil, codeRequestHandler, nil)
		serverDB           = dbFromState(t, serverState)
		rHandler           = p2ptest.NewSelfClient(t, t.Context(), ids.EmptyNodeID, syncer.NewGetRangeProofHandler(serverDB))
		cHandler           = p2ptest.NewSelfClient(t, t.Context(), ids.EmptyNodeID, syncer.NewGetChangeProofHandler(serverDB))
	)

	// Create the producer code queue.
	codeQueue, err := code.NewQueue(clientState.DiskDB().(ethdb.Database), make(chan struct{}))
	require.NoError(t, err, "NewCodeQueue()")

	// Create the consumer code syncer.
	codeSyncer, err := code.NewSyncer(mockClient, clientState.DiskDB().(ethdb.Database), codeQueue.CodeHashes())
	require.NoError(t, err, "NewCodeSyncer()")

	// Create the firewood syncer.
	firewoodSyncer, err := NewFirewoodSyncer(
		syncer.Config{},
		dbFromState(t, clientState),
		root,
		codeQueue,
		rHandler,
		cHandler,
	)
	require.NoError(t, err, "NewFirewoodSyncer()")
	return firewoodSyncer, codeSyncer, codeQueue
}

func runFirewoodSync(ctx context.Context, codeSyncer *code.Syncer, firewoodSyncer *FirewoodSyncer) error {
	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error { return codeSyncer.Sync(egCtx) })
	eg.Go(func() error { return firewoodSyncer.Sync(egCtx) })
	return eg.Wait()
}

func createDB(t *testing.T) state.Database {
	t.Helper()
	diskdb := rawdb.NewMemoryDatabase()
	config := firewood.DefaultConfig(t.TempDir())
	db := extstate.NewDatabaseWithConfig(diskdb, &triedb.Config{DBOverride: config.BackendConstructor})
	t.Cleanup(func() {
		require.NoError(t, db.TrieDB().Close())
	})
	return db
}

func assertFirewoodConsistency(t *testing.T, root common.Hash, clientState state.Database, accounts map[*utilstest.Key]*types.StateAccount) {
	t.Helper()

	db := dbFromState(t, clientState)
	gotRoot, err := db.Root()
	require.NoErrorf(t, err, "%T.Root()", db)
	require.Equal(t, root, common.Hash(gotRoot), "client DB root does not match expected root")

	for k, acc := range accounts {
		codeHash := common.BytesToHash(acc.CodeHash)
		if codeHash == (common.Hash{}) || codeHash == types.EmptyCodeHash {
			continue
		}
		codeBytes := rawdb.ReadCode(clientState.DiskDB(), codeHash)
		require.NotEmptyf(t, codeBytes, "no code found for code hash %s", codeHash.Hex())
		require.Equalf(t, codeHash, crypto.Keccak256Hash(codeBytes), "incorrect code for account %+x", k.Address)
	}

	it := customrawdb.NewCodeToFetchIterator(clientState.DiskDB())
	defer it.Release()
	require.False(t, it.Next(), "expected no remaining code-to-fetch markers after successful sync")
	require.NoError(t, it.Error())
}

func dbFromState(t *testing.T, state state.Database) *ffi.Database {
	t.Helper()
	trieDB, ok := state.TrieDB().Backend().(*firewood.TrieDB)
	require.True(t, ok, "expected firewood TrieDB")
	return trieDB.Firewood
}
