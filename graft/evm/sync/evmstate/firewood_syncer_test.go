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
	"github.com/ava-labs/avalanchego/graft/evm/firewood"
	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/sync/code"
	"github.com/ava-labs/avalanchego/graft/evm/sync/handlers"
	"github.com/ava-labs/avalanchego/graft/evm/sync/synctest"
	"github.com/ava-labs/avalanchego/graft/evm/utils/utilstest"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"

	statesyncclient "github.com/ava-labs/avalanchego/graft/evm/sync/client"
	handlerstats "github.com/ava-labs/avalanchego/graft/evm/sync/handlers/stats"
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
			assertFirewoodConsistency(t, root, clientState, serverState, accounts)

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
	// Use CorethCodec as the default - the firewood syncer uses p2p directly, not the message codec,
	// so the codec choice only affects the code request handler which is auxiliary to these tests.
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
	triedbConfig := &triedb.Config{
		DBOverride: config.BackendConstructor,
	}
	// Create the state database using libevm's state package, then wrap it with
	// firewood.NewStateAccessor to avoid an import cycle between firewood and state packages.
	internalState := state.NewDatabaseWithConfig(diskdb, triedbConfig)
	tdb := internalState.TrieDB().Backend().(*firewood.TrieDB)
	t.Cleanup(func() {
		require.NoError(t, tdb.Close())
	})
	return firewood.NewStateAccessor(internalState, tdb)
}

func assertFirewoodConsistency(t *testing.T, root common.Hash, clientState, serverState state.Database, accounts map[*utilstest.Key]*types.StateAccount) {
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

	// Verify all key-value pairs (accounts + storage) match between client
	// and server using a raw FFI iterator, bypassing the 32-byte account
	// filter that would otherwise hide storage entries.
	assertFirewoodAllKeysConsistency(t, clientState, serverState)

	it := customrawdb.NewCodeToFetchIterator(clientState.DiskDB())
	defer it.Release()
	require.False(t, it.Next(), "expected no remaining code-to-fetch markers after successful sync")
	require.NoError(t, it.Error())
}

// assertFirewoodAllKeysConsistency uses the raw FFI iterator to verify that
// every key-value pair in the client database matches the server database.
// This covers both account entries (32-byte keys) and storage entries
// (64-byte combined keys) in Firewood's flat key-value model.
func assertFirewoodAllKeysConsistency(t *testing.T, clientState, serverState state.Database) {
	t.Helper()

	clientDB := dbFromState(t, clientState)
	serverDB := dbFromState(t, serverState)

	clientRoot, err := clientDB.Root()
	require.NoError(t, err, "client Root()")
	serverRoot, err := serverDB.Root()
	require.NoError(t, err, "server Root()")
	require.Equal(t, clientRoot, serverRoot, "client and server roots must match")

	// Empty databases have EmptyRoot and cannot create revisions.
	if clientRoot == ffi.EmptyRoot {
		return
	}

	clientRev, err := clientDB.Revision(clientRoot)
	require.NoError(t, err, "client Revision()")
	defer func() { require.NoError(t, clientRev.Drop()) }()
	serverRev, err := serverDB.Revision(serverRoot)
	require.NoError(t, err, "server Revision()")
	defer func() { require.NoError(t, serverRev.Drop()) }()

	clientIt, err := clientRev.Iter(nil)
	require.NoError(t, err, "client Iter()")
	defer func() { require.NoError(t, clientIt.Drop()) }()

	serverIt, err := serverRev.Iter(nil)
	require.NoError(t, err, "server Iter()")
	defer func() { require.NoError(t, serverIt.Drop()) }()

	clientNext, serverNext := clientIt.Next(), serverIt.Next()
	for clientNext && serverNext {
		require.Equal(t, clientIt.Key(), serverIt.Key(), "key mismatch (key=%x)", serverIt.Key())
		require.Equal(t, clientIt.Value(), serverIt.Value(), "value mismatch (key=%x)", clientIt.Key())
		clientNext, serverNext = clientIt.Next(), serverIt.Next()
	}
	require.NoError(t, clientIt.Err(), "client iterator error")
	require.NoError(t, serverIt.Err(), "server iterator error")
	require.False(t, clientNext, "client has extra entries beyond server")
	require.False(t, serverNext, "server has extra entries beyond client")
}

func dbFromState(t *testing.T, state state.Database) *ffi.Database {
	t.Helper()
	trieDB, ok := state.TrieDB().Backend().(*firewood.TrieDB)
	require.True(t, ok, "expected firewood TrieDB")
	return trieDB.Firewood
}
