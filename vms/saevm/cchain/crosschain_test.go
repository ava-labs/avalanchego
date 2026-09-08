// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/crosschain"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	ethparams "github.com/ava-labs/libevm/params"
)

func utxoIDs(utxos ...*avax.UTXO) []crosschain.UTXOID {
	ids := make([]crosschain.UTXOID, len(utxos))
	for i, u := range utxos {
		ids[i] = crosschain.UTXOID{TxID: u.TxID, OutputIndex: u.OutputIndex}
	}
	return ids
}

// importCall is the owner's own import, credited to `to`.
func importCall(tb testing.TB, to common.Address, utxos ...*avax.UTXO) []byte {
	tb.Helper()
	data, err := crosschain.ABI.Pack("importUTXOs", utxoIDs(utxos...), to)
	require.NoError(tb, err)
	return data
}

// remoteImportCall imports on behalf of owners that allowed it.
func remoteImportCall(tb testing.TB, utxos ...*avax.UTXO) []byte {
	tb.Helper()
	data, err := crosschain.ABI.Pack("remoteImportUTXOs", utxoIDs(utxos...))
	require.NoError(tb, err)
	return data
}

func newAVAXUTXO(owner common.Address, amount uint64, assetID ids.ID) *avax.UTXO {
	return &avax.UTXO{
		UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
		Asset:  avax.Asset{ID: assetID},
		Out: &secp256k1fx.TransferOutput{Amt: amount, OutputOwners: secp256k1fx.OutputOwners{
			Threshold: 1, Addrs: []ids.ShortID{ids.ShortID(owner)},
		}},
	}
}

// TestPrecompileExportAndImport drives the cross-chain transfer precompile
// through export, owner import, remote import, and replay on a fresh node.
func TestPrecompileExportAndImport(t *testing.T) {
	wallet := saetest.NewUNSAFEWallet(t, 2, types.LatestSigner(saetest.ChainConfig()))
	owner, stranger := wallet.Addresses()[0], wallet.Addresses()[1]
	db := memdb.New()
	dataDir := t.TempDir()
	timeOpt, clock := withVMTime(testStartTime)
	// A max allocation would overflow when the import credits the owner.
	balance := types.Account{Balance: new(big.Int).Exp(big.NewInt(10), big.NewInt(24), nil)}
	funded := options.Func[sutConfig](func(c *sutConfig) {
		c.genesis.Alloc[owner] = balance
		c.genesis.Alloc[stranger] = balance
	})
	opts := []sutOption{timeOpt, withDB(db), withChainDataDir(dataDir), funded}
	ctx, node := newSUT(t, opts...)
	precompile := crosschain.ContractAddress

	// Export 5 nAVAX to a P-Chain owner. The value leaves the sender and the
	// UTXO appears in shared memory after execution.
	pOwner := ids.ShortID{0xaa}
	exportData, err := crosschain.ABI.Pack("exportAVAX", [32]byte(constants.PlatformChainID), common.Address(pOwner))
	require.NoError(t, err)
	const exported = 5
	exportTx := wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
		To: &precompile, Gas: 100_000, GasFeeCap: big.NewInt(1), Data: exportData,
		Value: new(big.Int).Mul(big.NewInt(exported), big.NewInt(ethparams.GWei)),
	})
	require.NoError(t, node.ethclient.SendTransaction(ctx, exportTx))
	node.waitForPendingEthTxs(ctx, t, exportTx)
	exportedBlk := node.runConsensusLoop(ctx, t)
	receipt := exportedBlk.Receipts()[0]
	require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status)
	exports, _, err := crosschain.FromReceipts(exportedBlk.Receipts())
	require.NoError(t, err)
	require.Len(t, exports, 1)
	require.Equal(t, uint64(exported), exports[0].Amount)
	require.Equal(t, pOwner, exports[0].To)
	precompileBalance := node.balance(t, precompile)
	require.True(t, precompileBalance.IsZero(), "export value must not stay at the precompile")
	node.assertUTXOsExist(t, constants.PlatformChainID, node.ctx.ChainID, &avax.UTXO{
		UTXOID: exports[0].UTXOID, Asset: avax.Asset{ID: node.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{Amt: exported, OutputOwners: secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{pOwner}}},
	})

	// A bad export reverts: not a whole nAVAX.
	badExport := wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
		To: &precompile, Gas: 100_000, GasFeeCap: big.NewInt(1), Data: exportData, Value: big.NewInt(1),
	})
	require.NoError(t, node.ethclient.SendTransaction(ctx, badExport))
	node.waitForPendingEthTxs(ctx, t, badExport)
	badBlk := node.runConsensusLoop(ctx, t)
	require.Equal(t, types.ReceiptStatusFailed, badBlk.Receipts()[0].Status)
	precompileBalance = node.balance(t, precompile)
	require.True(t, precompileBalance.IsZero(), "a reverted export returns the value to the sender")

	// An import of a UTXO that is not in shared memory builds no block.
	const amount = uint64(100_000)
	utxo := newAVAXUTXO(owner, amount, node.ctx.AVAXAssetID)
	recipient := common.Address{0xbb}
	importTx := wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
		To: &precompile, Gas: 100_000, GasFeeCap: big.NewInt(1), Data: importCall(t, recipient, utxo),
	})
	require.NoError(t, node.ethclient.SendTransaction(ctx, importTx))
	node.waitForPendingEthTxs(ctx, t, importTx)
	node.unblockWaitForEvent()
	_, err = node.BuildBlock(ctx, nil)
	require.ErrorIs(t, err, errEmptyBlock, "an import without its UTXO must not be included")

	// Once the P-Chain data arrives, the same transaction imports. The owner
	// chose the recipient, which is credited the full amount, and the UTXO is
	// consumed.
	node.addUTXOs(t, node.ctx.ChainID, constants.PlatformChainID, utxo)
	clock.Set(clock.Now().Add(time.Second))
	importedBlk := node.runConsensusLoop(ctx, t)
	require.Equal(t, types.ReceiptStatusSuccessful, importedBlk.Receipts()[0].Status)
	_, records, err := tx.ParseExtData(customtypes.BlockExtData(importedBlk.EthBlock()))
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, utxo.InputID(), records[0].UTXOID.InputID())
	require.Equal(t, constants.PlatformChainID, records[0].SourceChain)
	require.Equal(t, ids.ShortID(owner), records[0].Owner)
	require.Equal(t, amount, records[0].Amount)
	require.Equal(t, tx.ScaleAVAX(amount), node.balance(t, recipient))
	node.assertUTXOsMissing(t, node.ctx.ChainID, constants.PlatformChainID, utxo)

	// A stranger cannot import the owner's UTXO to itself, and cannot import
	// for the owner until the owner allows it.
	utxo2 := newAVAXUTXO(owner, amount, node.ctx.AVAXAssetID)
	node.addUTXOs(t, node.ctx.ChainID, constants.PlatformChainID, utxo2)
	theft := wallet.SetNonceAndSign(t, 1, &types.DynamicFeeTx{
		To: &precompile, Gas: 100_000, GasFeeCap: big.NewInt(1), Data: importCall(t, stranger, utxo2),
	})
	strangerImport := wallet.SetNonceAndSign(t, 1, &types.DynamicFeeTx{
		To: &precompile, Gas: 100_000, GasFeeCap: big.NewInt(1), Data: remoteImportCall(t, utxo2),
	})
	require.NoError(t, node.ethclient.SendTransaction(ctx, theft))
	node.waitForPendingEthTxs(ctx, t, theft)
	theftBlk := node.runConsensusLoop(ctx, t)
	require.Equal(t, types.ReceiptStatusFailed, theftBlk.Receipts()[0].Status)
	node.assertUTXOsExist(t, node.ctx.ChainID, constants.PlatformChainID, utxo2)
	clock.Set(clock.Now().Add(time.Minute))
	require.NoError(t, node.ethclient.SendTransaction(ctx, strangerImport))
	node.waitForPendingEthTxs(ctx, t, strangerImport)
	refusedBlk := node.runConsensusLoop(ctx, t)
	require.Equal(t, types.ReceiptStatusFailed, refusedBlk.Receipts()[0].Status)
	node.assertUTXOsExist(t, node.ctx.ChainID, constants.PlatformChainID, utxo2)

	allow, err := crosschain.ABI.Pack("allowRemoteImport", true)
	require.NoError(t, err)
	allowTx := wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
		To: &precompile, Gas: 100_000, GasFeeCap: big.NewInt(1), Data: allow,
	})
	require.NoError(t, node.ethclient.SendTransaction(ctx, allowTx))
	node.waitForPendingEthTxs(ctx, t, allowTx)
	allowedBlk := node.runConsensusLoop(ctx, t)
	require.Equal(t, types.ReceiptStatusSuccessful, allowedBlk.Receipts()[0].Status)

	// The refused block still names utxo2, so the retry waits until that
	// block settles and leaves the processing range.
	clock.Set(clock.Now().Add(time.Minute))
	strangerImport2 := wallet.SetNonceAndSign(t, 1, &types.DynamicFeeTx{
		To: &precompile, Gas: 100_000, GasFeeCap: big.NewInt(1), Data: remoteImportCall(t, utxo2),
	})
	require.NoError(t, node.ethclient.SendTransaction(ctx, strangerImport2))
	node.waitForPendingEthTxs(ctx, t, strangerImport2)
	before := node.balance(t, owner)
	remoteBlk := node.runConsensusLoop(ctx, t)
	require.Equal(t, types.ReceiptStatusSuccessful, remoteBlk.Receipts()[0].Status)
	after := node.balance(t, owner)
	require.Equal(t, tx.ScaleAVAX(amount), *new(uint256.Int).Sub(&after, &before), "the owner receives the full amount, the stranger paid gas")
	node.assertUTXOsMissing(t, node.ctx.ChainID, constants.PlatformChainID, utxo2)

	// Fresh nodes replay the same blocks. A live node needs the UTXOs to
	// verify; a bootstrapping node executes without them and reaches the
	// same state roots.
	all := []*blocks.Block{exportedBlk, badBlk, importedBlk, theftBlk, refusedBlk, allowedBlk, remoteBlk}
	for _, mode := range []snow.State{snow.NormalOp, snow.Bootstrapping} {
		t.Run(mode.String(), func(t *testing.T) {
			ctx, replay := newSUT(t, timeOpt, withState(mode), funded)
			for _, original := range all {
				b, err := replay.ParseBlock(ctx, original.Bytes())
				require.NoError(t, err)
				if mode == snow.NormalOp {
					switch original.ID() {
					case importedBlk.ID():
						require.Error(t, replay.VerifyBlock(ctx, nil, b), "import block must not verify without the UTXO")
						replay.addUTXOs(t, replay.ctx.ChainID, constants.PlatformChainID, utxo)
					case theftBlk.ID():
						replay.addUTXOs(t, replay.ctx.ChainID, constants.PlatformChainID, utxo2)
					}
				}
				require.NoError(t, replay.VerifyBlock(ctx, nil, b))
				require.NoError(t, replay.AcceptBlock(ctx, b))
				require.NoError(t, b.WaitUntilExecuted(ctx))
				require.Equal(t, original.PostExecutionStateRoot(), b.PostExecutionStateRoot())
			}
			require.Equal(t, node.balance(t, owner), replay.balance(t, owner))
			require.Equal(t, node.balance(t, recipient), replay.balance(t, recipient))
			replay.assertUTXOsMissing(t, replay.ctx.ChainID, constants.PlatformChainID, utxo, utxo2)
			if mode == snow.Bootstrapping {
				// A delayed P export clears the removal marker without
				// creating another spendable input.
				replay.addUTXOs(t, replay.ctx.ChainID, constants.PlatformChainID, utxo)
				replay.assertUTXOsMissing(t, replay.ctx.ChainID, constants.PlatformChainID, utxo)
			}
		})
	}
}
