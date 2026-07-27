// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"math/big"
	"testing"

	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/vmtest"
	"github.com/ava-labs/avalanchego/graft/evm/firewood"
	"github.com/ava-labs/avalanchego/graft/evm/firewood/statehistory"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"

	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
)

// historyStateSnapshot is the live state of the test address right after a
// block was accepted, later compared against what the flat history overlay
// serves for the same height.
type historyStateSnapshot struct {
	avax  *uint256.Int
	asset *big.Int
	multi bool
	nonce uint64
}

// TestStateHistoryServesAtomicTxWrites pins the C-chain specific state
// history surface: atomic import/export txs mutate state through
// EVMStateTransfer (AddBalance, AddBalanceMultiCoin, SubBalanceMultiCoin),
// including the isMultiCoin account flag that rides inside the encoded
// account leaf as a libevm extra and the ANT balances stored as storage
// slots. All of it must be captured per block and served back verbatim by the
// overlay at every historical height.
func TestStateHistoryServesAtomicTxWrites(t *testing.T) {
	require := require.New(t)

	fork := upgradetest.NoUpgrades
	vm := newAtomicTestVM()
	tvm := vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork:       &fork,
		Scheme:     customrawdb.FirewoodScheme,
		ConfigJSON: `{"pruning-enabled":true,"state-sync-enabled":false,"state-history-enabled":true}`,
	})
	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
	}()

	var (
		key       = vmtest.TestKeys[0]
		ethAddr   = key.EthAddress()
		shortAddr = key.Address()
		assetID   = ids.GenerateTestID()

		// Enough AVAX (in nAVAX) to pay the fixed pre-AP3 export fee later.
		importAmount = uint64(10_000_000)
	)

	bc := vm.Ethereum().BlockChain()

	snapshotAt := func(root common.Hash) historyStateSnapshot {
		statedb, err := bc.StateAt(root)
		require.NoError(err)
		wrapped := extstate.New(statedb)
		return historyStateSnapshot{
			avax:  wrapped.GetBalance(ethAddr),
			asset: wrapped.GetBalanceMultiCoin(ethAddr, common.Hash(assetID)),
			multi: customtypes.IsMultiCoin(statedb, ethAddr),
			nonce: wrapped.GetNonce(ethAddr),
		}
	}

	buildAndAccept := func(tx *atomic.Tx) {
		require.NoError(vm.AtomicMempool.AddLocalTx(tx))
		msg, err := vm.WaitForEvent(t.Context())
		require.NoError(err)
		require.Equal(commonEng.PendingTxs, msg)

		blk, err := vm.BuildBlock(t.Context())
		require.NoError(err)
		require.NoError(blk.Verify(t.Context()))
		require.NoError(vm.SetPreference(t.Context(), blk.ID()))
		require.NoError(blk.Accept(t.Context()))
		bc.DrainAcceptorQueue()
	}

	snapshots := make(map[uint64]historyStateSnapshot)
	snapshots[0] = snapshotAt(bc.GetHeaderByNumber(0).Root)

	// Block 1: import AVAX (AddBalance).
	_, err := addUTXO(tvm.AtomicMemory, vm.Ctx, ids.GenerateTestID(), 0, vm.Ctx.AVAXAssetID, importAmount, shortAddr)
	require.NoError(err)
	importTx, err := vm.newImportTx(vm.Ctx.XChainID, ethAddr, vmtest.InitialBaseFee, vmtest.TestKeys[0:1])
	require.NoError(err)
	buildAndAccept(importTx)
	snapshots[1] = snapshotAt(vm.LastAcceptedExtendedBlock().GetEthBlock().Root())

	// Block 2: import a non-AVAX asset (AddBalanceMultiCoin sets the
	// isMultiCoin flag and writes the balance into a storage slot).
	_, err = addUTXO(tvm.AtomicMemory, vm.Ctx, ids.GenerateTestID(), 0, assetID, 5, shortAddr)
	require.NoError(err)
	importTx2, err := vm.newImportTx(vm.Ctx.XChainID, ethAddr, vmtest.InitialBaseFee, vmtest.TestKeys[0:1])
	require.NoError(err)
	buildAndAccept(importTx2)
	snapshots[2] = snapshotAt(vm.LastAcceptedExtendedBlock().GetEthBlock().Root())

	// Block 3: export part of the asset back (SubBalanceMultiCoin rewrites
	// the same storage slot at a later height, plus the AVAX fee and nonce).
	statedb, err := bc.State()
	require.NoError(err)
	exportTx, err := atomic.NewExportTx(vm.Ctx, vm.CurrentRules(), extstate.New(statedb), assetID, 2, vm.Ctx.XChainID, shortAddr, vmtest.InitialBaseFee, vmtest.TestKeys[0:1])
	require.NoError(err)
	buildAndAccept(exportTx)
	snapshots[3] = snapshotAt(vm.LastAcceptedExtendedBlock().GetEthBlock().Root())

	// The imports and export must have moved the balances the test relies on;
	// otherwise the overlay comparison below would vacuously pass on zeros.
	require.Positive(snapshots[1].avax.Cmp(snapshots[0].avax))
	require.Equal(big.NewInt(5), snapshots[2].asset)
	require.Equal(big.NewInt(3), snapshots[3].asset)
	require.False(snapshots[1].multi)
	require.True(snapshots[2].multi)
	require.True(snapshots[3].multi)

	// The history store must cover [genesis, lastAccepted].
	store := bc.TrieDB().Backend().(*firewood.TrieDB).HistoryStore()
	require.NotNil(store)
	first, ok, err := store.FirstBlock()
	require.NoError(err)
	require.True(ok)
	require.Zero(first)
	head, ok, err := store.Head()
	require.NoError(err)
	require.True(ok)
	require.Equal(uint64(3), head)

	// The overlay must serve every height exactly as the live state had it.
	for target, want := range snapshots {
		header := bc.GetHeaderByNumber(target)
		require.NotNil(header)
		overlay := statehistory.NewOverlay(store, bc.StateCache(), target, header.Root)
		sdb, err := state.New(header.Root, overlay, nil)
		require.NoError(err)
		wrapped := extstate.New(sdb)
		require.Equal(want.avax, wrapped.GetBalance(ethAddr), "avax balance at height %d", target)
		require.Equal(want.asset, wrapped.GetBalanceMultiCoin(ethAddr, common.Hash(assetID)), "asset balance at height %d", target)
		require.Equal(want.multi, customtypes.IsMultiCoin(sdb, ethAddr), "isMultiCoin flag at height %d", target)
		require.Equal(want.nonce, wrapped.GetNonce(ethAddr), "nonce at height %d", target)
	}
}
