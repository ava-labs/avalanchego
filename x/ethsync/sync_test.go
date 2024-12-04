// (c) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ethsync

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"testing"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/x/merkledb"
	"github.com/ava-labs/avalanchego/x/sync"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/plugin/evm"
	"github.com/ava-labs/coreth/triedb"
	"github.com/ava-labs/coreth/triedb/pathdb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/sha3"
)

const defaultSimultaneousWorkLimit = 5

var lastKeyStoredAt = common.Hash{'p', 'r', 'e', 'f', 'i', 'x'}

func getVal(addr common.Address, i uint64) common.Hash {
	bytes := binary.BigEndian.AppendUint64(nil, i)
	h := sha3.NewLegacyKeccak256()
	h.Write(addr.Bytes())
	h.Write(bytes)
	return common.BytesToHash(h.Sum(nil))
}

func addState(statedb *state.StateDB, addr common.Address, kvs int) {
	amount := uint256.NewInt(100)
	statedb.AddBalance(addr, amount)
	statedb.SetNonce(addr, statedb.GetNonce(addr)+1)

	lastKeyHash := statedb.GetState(addr, lastKeyStoredAt)
	lastKey := binary.BigEndian.Uint64(lastKeyHash.Bytes()[24:32])
	for i := lastKey; i < lastKey+uint64(kvs); i++ {
		bytes := binary.BigEndian.AppendUint64(nil, i)
		statedb.SetState(addr, common.BytesToHash(bytes), getVal(addr, i))
	}
	bytes := binary.BigEndian.AppendUint64(nil, lastKey+uint64(kvs))
	statedb.SetState(addr, lastKeyStoredAt, common.BytesToHash(bytes))

	fmt.Println("--> Address: ", addr, " KVs: ", lastKey+uint64(kvs))
}

func verifyState(t *testing.T, statedb *state.StateDB, addr common.Address) {
	require := require.New(t)

	balance := statedb.GetBalance(addr)
	nonce := statedb.GetNonce(addr)

	lastKeyHash := statedb.GetState(addr, lastKeyStoredAt)
	lastKey := binary.BigEndian.Uint64(lastKeyHash.Bytes()[24:32])
	for i := uint64(0); i < lastKey; i++ {
		bytes := binary.BigEndian.AppendUint64(nil, i)
		require.Equal(
			getVal(addr, i),
			statedb.GetState(addr, common.BytesToHash(bytes)))
	}

	t.Logf("Address: %s, KVs: %d, Balance: %s, Nonce: %d", addr, lastKey, balance, nonce)
}

func TestSync(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	// Create a server database, fill with some state
	serverDisk := rawdb.NewMemoryDatabase()
	serverDB := state.NewDatabaseWithConfig(serverDisk, &triedb.Config{PathDB: pathdb.Defaults})

	accounts := make([]common.Address, 100)
	for i := 0; i < len(accounts); i++ {
		bytes := binary.BigEndian.AppendUint64(nil, uint64(i))
		accounts[i] = common.BytesToAddress(bytes)
	}

	accountsPerState := 10
	serverStates := 100
	serverRoot := types.EmptyRootHash
	for i := 0; i < serverStates; i++ {
		statedb, err := state.New(serverRoot, serverDB, nil)
		require.NoError(err)

		for j := 0; j < accountsPerState; j++ {
			accIdx := (i*accountsPerState + j) % len(accounts)
			kvs := 0
			if j%5 == 0 {
				kvs = 1000
			}
			addState(statedb, accounts[accIdx], kvs)
		}

		serverRoot, err = statedb.Commit(uint64(i), true)
		require.NoError(err)

		t.Logf("Server state %d: %s", i, serverRoot)
	}

	// Create a client database
	clientDisk := memdb.New()
	client, err := New(ctx, clientDisk, merkledb.Config{})
	require.NoError(err)

	clientNodeID, serverNodeID := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()

	server := &db{
		triedb: serverDB.TrieDB(),
		root:   serverRoot,
	}

	rangeProofs := sync.NewGetRangeProofHandler(logging.NoLog{}, server)
	changeProofs := sync.NewGetChangeProofHandler(logging.NoLog{}, server)

	log := logging.NewWrappedCore(
		logging.Info,
		os.Stdout,
		logging.Auto.ConsoleEncoder(),
	)
	managerConfig := sync.ManagerConfig{
		DB:                    client,
		Log:                   logging.NewLogger("sync", log),
		BranchFactor:          merkledb.BranchFactor16,
		TargetRoot:            ids.ID(serverRoot),
		SimultaneousWorkLimit: defaultSimultaneousWorkLimit,
		RangeProofClient:      p2ptest.NewClient(t, ctx, rangeProofs, clientNodeID, serverNodeID),
		ChangeProofClient:     p2ptest.NewClient(t, ctx, changeProofs, clientNodeID, serverNodeID),
		KVCallback:            client.KVCallback,
	}
	manager, err := sync.NewManager(managerConfig, prometheus.NewRegistry())
	require.NoError(err)
	client.manager = manager // Needed to hook-up KVCallback

	require.NoError(manager.Start(ctx))
	require.NoError(manager.Wait(ctx))

	// Verify the client database
	ethdb := rawdb.NewDatabase(evm.Database{Database: clientDisk})
	clientDB := state.NewDatabaseWithConfig(ethdb, &triedb.Config{PathDB: pathdb.Defaults})
	clientState, err := state.New(serverRoot, clientDB, nil)
	require.NoError(err)
	for _, account := range accounts {
		verifyState(t, clientState, account)
	}
}
