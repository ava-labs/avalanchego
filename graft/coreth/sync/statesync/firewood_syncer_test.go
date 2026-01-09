// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"math/rand"
	"testing"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/triedb"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/firewood/syncer"
	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/message"
	"github.com/ava-labs/avalanchego/graft/coreth/sync/handlers"
	"github.com/ava-labs/avalanchego/graft/evm/firewood"
	"github.com/ava-labs/avalanchego/graft/evm/sync/synctest"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"

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
			_ = synctest.FillAccountsWithStorageAndCode(t, r, clientState, tt.clientSize)

			serverState := createDB(t)
			root := synctest.FillAccountsWithStorageAndCode(t, r, serverState, tt.serverSize)

			testFirewoodSync(t, clientState, serverState, root)
		})
	}
}

func testFirewoodSync(t *testing.T, clientState, serverState state.Database, root common.Hash) {
	// Create the mock P2P client that serves range proofs and change proofs from the server DB.
	var (
		codeRequestHandler = handlers.NewCodeRequestHandler(serverState.DiskDB(), message.Codec, handlerstats.NewNoopHandlerStats())
		mockClient         = statesyncclient.NewTestClient(message.Codec, nil, codeRequestHandler, nil)
		serverDB           = dbFromState(t, serverState)
		rHandler           = p2ptest.NewSelfClient(t, t.Context(), ids.EmptyNodeID, syncer.NewGetRangeProofHandler(serverDB))
		cHandler           = p2ptest.NewSelfClient(t, t.Context(), ids.EmptyNodeID, syncer.NewGetChangeProofHandler(serverDB))
	)

	// Create the code fetcher.
	fetcher, err := NewCodeQueue(clientState.DiskDB().(ethdb.Database), make(chan struct{}))
	require.NoError(t, err, "NewCodeQueue()")

	// Create the consumer code syncer.
	codeSyncer, err := NewCodeSyncer(mockClient, clientState.DiskDB().(ethdb.Database), fetcher.CodeHashes())
	require.NoError(t, err, "NewCodeSyncer()")

	// Create the firewood syncer.
	firewoodSyncer, err := NewFirewoodSyncer(
		syncer.Config{},
		dbFromState(t, clientState),
		root,
		fetcher,
		rHandler,
		cHandler,
	)
	require.NoError(t, err, "NewFirewoodSyncer()")

	eg, egCtx := errgroup.WithContext(t.Context())
	eg.Go(func() error { return codeSyncer.Sync(egCtx) })
	eg.Go(func() error { return firewoodSyncer.Sync(egCtx) })

	require.NoError(t, eg.Wait(), "failure during sync")
}

func createDB(t *testing.T) state.Database {
	diskdb := rawdb.NewMemoryDatabase()
	config := firewood.DefaultConfig(t.TempDir())
	db := extstate.NewDatabaseWithConfig(diskdb, &triedb.Config{DBOverride: config.BackendConstructor})
	t.Cleanup(func() {
		require.NoError(t, db.TrieDB().Close())
	})
	return db
}

func dbFromState(t *testing.T, state state.Database) *ffi.Database {
	t.Helper()
	trieDB, ok := state.TrieDB().Backend().(*firewood.TrieDB)
	require.True(t, ok, "expected firewood TrieDB")
	return trieDB.Firewood
}
