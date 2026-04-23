// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"math/big"
	"testing"

	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	avalanchedb "github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/vmtest"
	"github.com/ava-labs/avalanchego/graft/evm/rpc"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"

	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
)

// TestFirewoodHistoricalReplayAcrossAtomicImport reproduces a Firewood archive
// query that must replay across an accepted atomic import block.
//
// Before the historical replay fix, this query fails because replay re-enters
// normal atomic semantic verification and consults live shared memory for the
// imported UTXO, which has already been consumed.
func TestFirewoodHistoricalReplayAcrossAtomicImport(t *testing.T) {
	require := require.New(t)
	ctx := t.Context()

	const (
		importAmount      = uint64(50_000_000)
		importBlockHeight = 16
		targetBlockHeight = 17
		totalBlocks       = 31
	)

	configJSON := `{
		"pruning-enabled": false,
		"commit-interval": 10,
		"state-history": 11
	}`

	recipient := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	var importedInputID ids.ID

	vm := newAtomicTestVM()
	fork := upgradetest.ApricotPhase2
	tvm := vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{
		Fork:       &fork,
		ConfigJSON: configJSON,
		Scheme:     customrawdb.FirewoodScheme,
	})
	defer func() {
		require.NoError(vm.Shutdown(ctx))
	}()

	require.NoError(addUTXOs(tvm.AtomicMemory, vm.Ctx, map[ids.ShortID]uint64{
		vmtest.TestShortIDAddrs[0]: importAmount,
	}))

	for height := 1; height <= totalBlocks; height++ {
		if height == importBlockHeight {
			importTx, err := vm.newImportTx(vm.Ctx.XChainID, recipient, vmtest.InitialBaseFee, vmtest.TestKeys[0:1])
			require.NoError(err)
			inputUTXOs := importTx.InputUTXOs()
			require.Len(inputUTXOs, 1)
			for inputID := range inputUTXOs {
				importedInputID = inputID
			}
			require.NoError(vm.AtomicMempool.AddLocalTx(importTx))

			msg, err := vm.WaitForEvent(ctx)
			require.NoError(err)
			require.Equal(commonEng.PendingTxs, msg)

			blk, err := vm.BuildBlock(ctx)
			require.NoError(err)
			require.NoError(blk.Verify(ctx))
			require.NoError(vm.SetPreference(ctx, blk.ID()))
			require.NoError(blk.Accept(ctx))
			continue
		}

		nonce := vm.Ethereum().TxPool().Nonce(vmtest.TestEthAddrs[0])
		tx := types.NewTransaction(
			nonce,
			common.Address{},
			big.NewInt(0),
			21_000,
			vmtest.InitialBaseFee,
			nil,
		)
		signedTx, err := types.SignTx(
			tx,
			types.LatestSignerForChainID(vm.Ethereum().BlockChain().Config().ChainID),
			vmtest.TestKeys[0].ToECDSA(),
		)
		require.NoError(err)

		blk, err := vmtest.IssueTxsAndSetPreference([]*types.Transaction{signedTx}, vm)
		require.NoErrorf(err, "failed to build regular block at height %d", height)
		require.NoErrorf(blk.Accept(ctx), "failed to accept regular block at height %d", height)
	}

	vm.Ethereum().BlockChain().DrainAcceptorQueue()
	_, err := vm.Ctx.SharedMemory.Get(vm.Ctx.XChainID, [][]byte{importedInputID[:]})
	require.ErrorIs(err, avalanchedb.ErrNotFound)

	stateDB, header, err := vm.Ethereum().APIBackend.StateAndHeaderByNumber(ctx, rpc.BlockNumber(targetBlockHeight))
	require.NoErrorf(err, "historical query at block %d should replay successfully", targetBlockHeight)
	require.Equal(uint64(targetBlockHeight), header.Number.Uint64())

	balance := stateDB.GetBalance(recipient)
	require.Equalf(
		1,
		balance.Cmp(uint256.NewInt(0)),
		"recipient %s should have a positive imported balance at block %d",
		recipient,
		targetBlockHeight,
	)
}
