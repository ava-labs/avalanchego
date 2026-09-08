// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"encoding/hex"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ava-labs/libevm/accounts/abi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/tests/cchainhelper"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/warp"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestContractImportSettlementAndReplay(t *testing.T) {
	wallet := saetest.NewUNSAFEWallet(t, 1, types.LatestSigner(saetest.ChainConfig()))
	owner := wallet.Addresses()[0]
	helper := crypto.CreateAddress(owner, 0)
	db := memdb.New()
	dataDir := t.TempDir()
	timeOpt, clock := withVMTime(testStartTime)
	funded := withAccount(owner, types.Account{Balance: new(big.Int).Exp(big.NewInt(10), big.NewInt(24), nil)})
	trusted := options.Func[sutConfig](func(c *sutConfig) { c.vmConfig.HelperAddress = helper })
	opts := []sutOption{timeOpt, withDB(db), withChainDataDir(dataDir), funded, trusted}
	ctx, node := newSUT(t, opts...)
	parsed, err := abi.JSON(strings.NewReader(cchainhelper.ABI))
	require.NoError(t, err)
	bin, err := hex.DecodeString(cchainhelper.Bin)
	require.NoError(t, err)
		deploy := wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
		Gas: 2_000_000, GasFeeCap: big.NewInt(1), Data: bin,
	})
	require.NoError(t, node.ethclient.SendTransaction(ctx, deploy))
	node.waitForPendingEthTxs(ctx, t, deploy)
	deployed := node.runConsensusLoop(ctx, t)
	require.Equal(t, types.ReceiptStatusSuccessful, deployed.Receipts()[0].Status)

	const amount, fee = uint64(100_000), uint64(7)
	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
		Asset:  avax.Asset{ID: node.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{Amt: amount, OutputOwners: secp256k1fx.OutputOwners{
			Threshold: 1, Addrs: []ids.ShortID{ids.ShortID(owner)},
		}},
	}
	inputs := []struct {
		TxID        [32]byte
		OutputIndex uint32
		Amount      uint64
	}{{utxo.TxID, utxo.OutputIndex, amount}}
	callData, err := parsed.Pack("importFromP", node.ctx.NetworkID, node.ctx.AVAXAssetID, inputs, fee)
	require.NoError(t, err)
	signed := wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
		To: &helper, Gas: 1_000_000, GasFeeCap: big.NewInt(1), Data: callData,
	})
	require.NoError(t, node.ethclient.SendTransaction(ctx, signed))
	node.waitForPendingEthTxs(ctx, t, signed)
	authorized := node.runConsensusLoop(ctx, t)
	receipt := authorized.Receipts()[0]
	require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status)
	require.Len(t, receipt.Logs, 1)
	event := receipt.Logs[0]
	require.Equal(t, helper, event.Address)
	require.Len(t, event.Topics, 2)
	require.Equal(t, parsed.Events["ImportAuthorized"].ID, event.Topics[0])
	fields, err := parsed.Unpack("ImportAuthorized", event.Data)
	require.NoError(t, err)
	unsigned := fields[0].([]byte)
	require.Equal(t, crypto.Keccak256Hash(unsigned), event.Topics[1])
	messages, err := warp.FromReceipts(authorized.Receipts())
	require.NoError(t, err)
	require.Empty(t, messages, "imports must not create Warp authorization history")

	// The event is a relay convenience. The verifier reads only contract state.
	atomicImport, err := tx.Parse(append(append([]byte{}, unsigned...), 0, 0, 0, 0))
	require.NoError(t, err)
	imp := atomicImport.Unsigned.(*tx.Import)
	require.Equal(t, &tx.Import{
		NetworkID: node.ctx.NetworkID, BlockchainID: node.ctx.ChainID, SourceChain: constants.PlatformChainID,
		ImportedInputs: []*avax.TransferableInput{{
			UTXOID: utxo.UTXOID, Asset: utxo.Asset,
			In: &secp256k1fx.TransferInput{Amt: amount, Input: secp256k1fx.Input{SigIndices: []uint32{0}}},
		}},
		Outs: []tx.Output{{Address: owner, Amount: amount - fee, AssetID: node.ctx.AVAXAssetID}},
	}, imp)
	atomicImport.Creds = []tx.Credential{&tx.ContractCredential{}}
	encoded, err := atomicImport.Bytes()
	require.NoError(t, err)

	// P data can be absent without stopping unrelated C-chain execution.
	require.Error(t, node.gossipSet.Add(toGossipTx(atomicImport)))
	require.False(t, node.pending.Has(atomicImport.ID()))
	other := common.Address{0x42}
	unrelated := wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
		To: &other, Gas: 21_000, GasFeeCap: big.NewInt(1), Value: big.NewInt(1),
	})
	require.NoError(t, node.ethclient.SendTransaction(ctx, unrelated))
	node.waitForPendingEthTxs(ctx, t, unrelated)
	progressed := node.runConsensusLoop(ctx, t)
	require.Equal(t, types.ReceiptStatusSuccessful, progressed.Receipts()[0].Status)

	// Restart without any pending-request database. An app retains the bytes
	// and can submit them again. The approval survives in ordinary EVM state.
	require.NoError(t, node.Shutdown(ctx))
	ctx, node = newSUT(t, opts...)
	s, err := node.LastExecutedState()
	require.NoError(t, err)
	require.Equal(t, common.Hash{31: 1}, s.GetState(helper, tx.ImportApprovalSlot(unsigned)))
	require.Error(t, node.gossipSet.Add(toGossipTx(atomicImport)))
	node.addUTXOs(t, node.ctx.ChainID, constants.PlatformChainID, utxo)
	changed, err := tx.Parse(encoded)
	require.NoError(t, err)
	changed.Unsigned.(*tx.Import).Outs[0].Amount--
	require.Error(t, node.gossipSet.Add(toGossipTx(changed)), "a relayer cannot raise the fee")
	require.NoError(t, node.gossipSet.Add(toGossipTx(atomicImport)))

	// Mempool admission is not settlement. A locally executed approval must
	// not authorize inclusion while the block still points to older state.
	node.unblockWaitForEvent()
	_, err = node.BuildBlock(ctx, nil)
	require.ErrorIs(t, err, errEmptyBlock)
	clock.Set(clock.Now().Add(time.Minute))
	before := node.balance(t, owner)
	imported := node.runConsensusLoop(ctx, t)
	require.GreaterOrEqual(t, imported.LastSettled().NumberU64(), authorized.NumberU64())
	require.Len(t, blockTxs(t, imported), 1)
	require.Equal(t, atomicImport.ID(), blockTxs(t, imported)[0].ID())
	after := node.balance(t, owner)
	delta := after.Sub(&after, &before)
	want := tx.ScaleAVAX(amount - fee)
	require.Equal(t, want, *delta)
	node.assertUTXOsMissing(t, node.ctx.ChainID, constants.PlatformChainID, utxo)
	require.Error(t, node.gossipSet.Add(toGossipTx(atomicImport)), "the input cannot be spent twice")
	s, err = node.LastExecutedState()
	require.NoError(t, err)
	require.Equal(t, common.Hash{31: 1}, s.GetState(helper, tx.ImportApprovalSlot(unsigned)), "no approval cleanup")

	for _, mode := range []snow.State{snow.NormalOp, snow.Bootstrapping} {
		t.Run(mode.String(), func(t *testing.T) {
			ctx, replay := newSUT(t, timeOpt, withState(mode), funded, trusted)
			for _, original := range []*blocks.Block{deployed, authorized, progressed, imported} {
				b, err := replay.ParseBlock(ctx, original.Bytes())
				require.NoError(t, err)
				if mode == snow.NormalOp && original.ID() == imported.ID() {
					require.Error(t, replay.VerifyBlock(ctx, nil, b))
					replay.addUTXOs(t, replay.ctx.ChainID, constants.PlatformChainID, utxo)
				}
				require.NoError(t, replay.VerifyBlock(ctx, nil, b))
				require.NoError(t, replay.AcceptBlock(ctx, b))
				require.NoError(t, b.WaitUntilExecuted(ctx))
				require.Equal(t, original.PostExecutionStateRoot(), b.PostExecutionStateRoot())
			}
			require.Equal(t, node.balance(t, owner), replay.balance(t, owner))
			replay.assertUTXOsMissing(t, replay.ctx.ChainID, constants.PlatformChainID, utxo)
			if mode == snow.Bootstrapping {
				// A delayed P export clears the removal marker without creating
				// another spendable input.
				replay.addUTXOs(t, replay.ctx.ChainID, constants.PlatformChainID, utxo)
				replay.assertUTXOsMissing(t, replay.ctx.ChainID, constants.PlatformChainID, utxo)
			}
		})
	}
}
