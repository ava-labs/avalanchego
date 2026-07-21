// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package upgradechaintest

import (
	"math/big"
	"slices"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/rlp"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/graft/coreth/nativeasset"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/vmtest"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	corethatomic "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	warpcontract "github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	commoneng "github.com/ava-labs/avalanchego/snow/engine/common"
	avalanchewarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	ethparams "github.com/ava-labs/libevm/params"
)

// buildAllBlocks accepts the fixture's blocks in height order, advancing the
// VM clock across each upgrade boundary so that each block is built under
// the intended upgrade's rules.
func (g *generator) buildAllBlocks(t *testing.T) {
	upgrades := g.fixture.Upgrades

	// At least one block per historical upgrade; each note names the behavior
	// exercised by consuming tests.
	g.setClock(upgrade.InitiallyActiveTime.Add(10 * time.Second))
	g.buildEthBlock(t, "apricotPhase1", "counter-contract deploy, AVAX transfer, and storage set-and-clear accruing an SSTORE refund that AP1 discards, all under AP1's fixed 225 gwei gas price (nil base fee)",
		g.counterDeployTx(t),
		g.transferTx(t, 1),
		g.storageClearTx(t),
	)

	g.setClock(upgrades.ApricotPhase2Time)
	g.buildEthBlock(t, "apricotPhase2", "legacy transfer in the Berlin activation block (the pool rejects typed transactions until the head is in Berlin)",
		g.transferTx(t, 14),
	)
	require.Equal(t, params.TestUpgradechainBerlinBlock, g.tip(), "Berlin (AP2) activation block height")

	g.advanceClock(10 * time.Second)
	g.buildEthBlock(t, "apricotPhase2", "access-list (EIP-2930) transfer, the transaction type Berlin introduces",
		g.accessListTransferTx(t, 15),
	)

	g.setClock(upgrades.ApricotPhase3Time)
	g.buildEthBlock(t, "apricotPhase3", "plain transfer in the London activation block (the atomic mempool prices imports by head rules, so imports wait until the head is in AP3)",
		g.transferTx(t, 16),
	)
	require.Equal(t, params.TestUpgradechainLondonBlock, g.tip(), "London (AP3) activation block height")

	g.advanceClock(10 * time.Second)
	g.buildBlock(t, "apricotPhase3", "single atomic import of AVAX (pre-AP5 extData encoding) under dynamic fees",
		[]*corethatomic.Tx{g.importAVAX(t, 10*units.Avax)},
		[]*types.Transaction{g.counterIncrementTx(t)},
		nil,
	)

	g.advanceClock(10 * time.Second)
	g.buildBlock(t, "apricotPhase3", "atomic import of a non-AVAX asset, funding a multicoin (ANT) balance",
		[]*corethatomic.Tx{g.importANT(t, 1_000)},
		[]*types.Transaction{g.transferTx(t, 8)},
		nil,
	)

	g.advanceClock(10 * time.Second)
	g.buildEthBlock(t, "apricotPhase3", "nativeAssetCall moving the imported ANT balance (functional precompile era)",
		g.nativeAssetCallTx(t, vmtest.TestEthAddrs[1], 100),
	)
	require.Equal(t, NativeAssetCallBlocks[0], g.tip(), "functional nativeAssetCall block height")

	g.setClock(upgrades.ApricotPhase4Time)
	g.buildBlock(t, "apricotPhase4", "atomic export of AVAX with AP4 header fields (extDataGasUsed, blockGasCost)",
		[]*corethatomic.Tx{g.exportAVAX(t, 1*units.Avax)},
		[]*types.Transaction{g.transferTx(t, 2)},
		nil,
	)

	g.setClock(upgrades.ApricotPhase5Time)
	g.buildBlock(t, "apricotPhase5", "two atomic imports batched into one block (post-AP5 extData encoding)",
		[]*corethatomic.Tx{g.importAVAX(t, 3*units.Avax), g.importAVAX(t, 4*units.Avax)},
		[]*types.Transaction{g.transferTx(t, 9)},
		nil,
	)

	g.setClock(upgrades.ApricotPhasePre6Time)
	g.buildEthBlock(t, "apricotPhasePre6", "nativeAssetCall against the deprecated precompile (failing receipt)",
		g.nativeAssetCallTx(t, vmtest.TestEthAddrs[1], 100),
	)
	require.Equal(t, DeprecatedNativeAssetCallBlock, g.tip(), "deprecated nativeAssetCall block height")

	g.setClock(upgrades.ApricotPhase6Time)
	g.buildEthBlock(t, "apricotPhase6", "nativeAssetCall functional again after AP6 re-enablement",
		g.nativeAssetCallTx(t, vmtest.TestEthAddrs[1], 100),
	)
	require.Equal(t, NativeAssetCallBlocks[1], g.tip(), "functional nativeAssetCall block height")

	g.setClock(upgrades.ApricotPhasePost6Time)
	g.buildEthBlock(t, "apricotPhasePost6", "plain transfer (no per-block format change)",
		g.transferTx(t, 3),
	)

	g.setClock(upgrades.BanffTime)
	g.buildBlock(t, "banff", "atomic AVAX export under Banff's AVAX-only restriction",
		[]*corethatomic.Tx{g.exportAVAX(t, 1*units.Avax)},
		[]*types.Transaction{g.transferTx(t, 10)},
		nil,
	)

	g.setClock(upgrades.CortinaTime)
	g.buildEthBlock(t, "cortina", "counter increment under Cortina's 15M gas limit",
		g.counterIncrementTx(t),
	)

	g.setClock(upgrades.DurangoTime)
	g.buildEthBlock(t, "durango", "sendWarpMessage precompile call emitting an unsigned warp message",
		g.sendWarpMessageTx(t),
	)
	require.Equal(t, SendWarpMessageBlock, g.tip(), "sendWarpMessage block height")

	g.advanceClock(10 * time.Second)
	g.buildBlock(t, "durango", "getVerifiedWarpMessage with an access-list predicate (results in header extra), plus a transfer whose tracing replays the predicate transaction",
		nil,
		[]*types.Transaction{g.verifiedWarpMessageTx(t), g.transferTx(t, 11)},
		&block.Context{PChainHeight: minValidPChainHeight},
	)

	g.setClock(upgrades.EtnaTime)
	g.buildEthBlock(t, "etna", "dynamic-fee transfer with Cancun header fields (blobGasUsed, excessBlobGas, parentBeaconRoot)",
		g.dynamicFeeTransferTx(t, 4),
	)

	g.setClock(upgrades.FortunaTime)
	g.buildEthBlock(t, "fortuna", "dynamic-fee transfer with the ACP-176 fee state as the extra-data prefix",
		g.dynamicFeeTransferTx(t, 5),
	)

	g.setClock(upgrades.GraniteTime)
	g.buildEthBlock(t, "granite", "first Granite block (timeMilliseconds, minDelayExcess header fields)",
		g.transferTx(t, 6),
	)

	g.advanceClock(2 * time.Second)
	g.buildEthBlock(t, "granite", "second Granite block, subject to the ACP-226 minimum block delay; three transactions so that tracing the last replays the first two",
		g.transferTx(t, 7), g.transferTx(t, 12), g.transferTx(t, 13),
	)
}

func (g *generator) advanceClock(d time.Duration) {
	g.setClock(g.vm.Clock().Time().Add(d))
}

// recordGenesis records the genesis block, which [vmtest.SetupTestVM] already
// committed, as fixture block 0.
func (g *generator) recordGenesis(t *testing.T) {
	t.Helper()

	genesis := g.vm.Ethereum().BlockChain().Genesis()
	genesisRLP, err := rlp.EncodeToBytes(genesis)
	require.NoError(t, err, "rlp.EncodeToBytes(genesis block)")

	statedb, err := g.vm.Ethereum().BlockChain().State()
	require.NoError(t, err, "BlockChain().State()")
	g.fixture.Blocks = append(g.fixture.Blocks, Block{
		Fork:  "apricotPhase1",
		Note:  "genesis block allocating the test accounts' funds",
		Hash:  genesis.Hash(),
		Time:  genesis.Time(),
		RLP:   genesisRLP,
		State: g.watchedState(statedb),
	})
}

// tip returns the height of the most recently recorded block.
func (g *generator) tip() uint64 {
	return g.fixture.Tip().Number
}

// buildEthBlock is buildBlock for the common case: only EVM transactions and
// no block context.
func (g *generator) buildEthBlock(t *testing.T, fork, note string, ethTxs ...*types.Transaction) {
	t.Helper()
	g.buildBlock(t, fork, note, nil, ethTxs, nil)
}

// buildBlock issues the given transactions, builds a block containing them,
// and accepts it, recording it in the fixture. blockCtx, if non-nil, selects
// building with a block context (required for predicate transactions).
func (g *generator) buildBlock(t *testing.T, fork, note string, atomicTxs []*corethatomic.Tx, ethTxs []*types.Transaction, blockCtx *block.Context) {
	t.Helper()

	for _, tx := range atomicTxs {
		require.NoError(t, g.vm.AtomicMempool.AddLocalTx(tx), "AtomicMempool.AddLocalTx()")
	}
	for i, err := range g.vm.Ethereum().TxPool().AddRemotesSync(ethTxs) {
		require.NoError(t, err, "TxPool().AddRemotesSync() tx %d", i)
	}

	msg, err := g.vm.WaitForEvent(t.Context())
	require.NoError(t, err, "vm.WaitForEvent()")
	require.Equal(t, commoneng.PendingTxs, msg, "vm.WaitForEvent()")

	var blk snowman.Block
	if blockCtx != nil {
		blk, err = g.vm.BuildBlockWithContext(t.Context(), blockCtx)
		require.NoError(t, err, "vm.BuildBlockWithContext()")

		verifier, ok := blk.(block.WithVerifyContext)
		require.True(t, ok, "%T does not implement block.WithVerifyContext", blk)
		require.NoError(t, verifier.VerifyWithContext(t.Context(), blockCtx), "block.VerifyWithContext()")
	} else {
		blk, err = g.vm.BuildBlock(t.Context())
		require.NoError(t, err, "vm.BuildBlock()")
		require.NoError(t, blk.Verify(t.Context()), "block.Verify()")
	}

	require.NoError(t, g.vm.SetPreference(t.Context(), blk.ID()), "vm.SetPreference()")
	require.NoError(t, blk.Accept(t.Context()), "block.Accept()")
	g.vm.Ethereum().BlockChain().DrainAcceptorQueue()

	ethBlock := new(types.Block)
	require.NoError(t, rlp.DecodeBytes(blk.Bytes(), ethBlock), "rlp.DecodeBytes(block)")
	require.Equal(t, common.Hash(blk.ID()), ethBlock.Hash(), "re-decoded block hash")

	statedb, err := g.vm.Ethereum().BlockChain().State()
	require.NoError(t, err, "BlockChain().State()")
	g.fixture.Blocks = append(g.fixture.Blocks, Block{
		Fork:    fork,
		Note:    note,
		Number:  ethBlock.NumberU64(),
		Hash:    ethBlock.Hash(),
		Time:    ethBlock.Time(),
		RLP:     blk.Bytes(),
		State:   g.watchedState(statedb),
		Counter: statedb.GetState(g.fixture.Counter, common.Hash{}).Big().Uint64(),
	})
}

// nonce returns the next nonce for the fixture's single EVM sender. A counter
// is required because a block's transactions are constructed before any of
// them reaches the transaction pool.
func (g *generator) nonce() uint64 {
	n := g.ethNonce
	g.ethNonce++
	return n
}

func (g *generator) chainID() *big.Int {
	return g.vm.Ethereum().BlockChain().Config().ChainID
}

// rules returns the chain rules the NEXT block will be built under.
// [vm.VM.CurrentRules] is unsuitable: it derives rules from the head block,
// which just after an upgrade boundary still has the previous upgrade's rules.
func (g *generator) rules() extras.Rules {
	config := g.vm.Ethereum().BlockChain().Config()
	head := g.vm.Ethereum().BlockChain().CurrentHeader()
	nextNumber := new(big.Int).Add(head.Number, big.NewInt(1))
	ethRules := config.Rules(nextNumber, params.IsMergeTODO, g.vm.Clock().Unix())
	return *params.GetRulesExtra(ethRules)
}

// signedTx returns tx signed by the fixture's single EVM sender,
// [vmtest.TestKeys[0]].
func (g *generator) signedTx(t *testing.T, tx types.TxData) *types.Transaction {
	t.Helper()
	signed, err := types.SignTx(types.NewTx(tx), types.LatestSignerForChainID(g.chainID()), vmtest.TestKeys[0].ToECDSA())
	require.NoError(t, err, "types.SignTx()")
	return signed
}

// transferTx returns a legacy transfer of n wei to a fixed recipient. Each
// transfer moves a distinct amount so per-block states differ.
func (g *generator) transferTx(t *testing.T, n int64) *types.Transaction {
	t.Helper()
	return g.signedTx(t, &types.LegacyTx{
		Nonce:    g.nonce(),
		To:       &transferRecipient,
		Value:    big.NewInt(n),
		Gas:      ethparams.TxGas,
		GasPrice: vmtest.InitialBaseFee,
	})
}

// accessListTransferTx returns an EIP-2930 access-list transfer of n wei to
// the fixed recipient — the transaction type introduced by Berlin (AP2).
func (g *generator) accessListTransferTx(t *testing.T, n int64) *types.Transaction {
	t.Helper()
	return g.signedTx(t, &types.AccessListTx{
		ChainID:    g.chainID(),
		Nonce:      g.nonce(),
		To:         &transferRecipient,
		Value:      big.NewInt(n),
		Gas:        ethparams.TxGas + ethparams.TxAccessListAddressGas,
		GasPrice:   vmtest.InitialBaseFee,
		AccessList: types.AccessList{{Address: transferRecipient}},
	})
}

func (g *generator) dynamicFeeTransferTx(t *testing.T, n int64) *types.Transaction {
	t.Helper()
	return g.signedTx(t, &types.DynamicFeeTx{
		ChainID:   g.chainID(),
		Nonce:     g.nonce(),
		To:        &transferRecipient,
		Value:     big.NewInt(n),
		Gas:       ethparams.TxGas,
		GasFeeCap: vmtest.InitialBaseFee,
		GasTipCap: big.NewInt(params.GWei),
	})
}

// counterDeployTx returns a transaction deploying the counter contract: with
// empty call data it increments its storage slot 0; with any call data it
// returns the slot's value as a 32-byte word.
func (g *generator) counterDeployTx(t *testing.T) *types.Transaction {
	t.Helper()
	runtime := slices.Concat(
		saetest.Ops(vm.CALLDATASIZE, vm.PUSH1, 14, vm.JUMPI), // jump to the JUMPDEST at offset 14
		saetest.Ops(vm.PUSH1, 0, vm.SLOAD, vm.PUSH1, 1, vm.ADD, vm.PUSH1, 0, vm.SSTORE, vm.STOP),
		saetest.Ops(vm.JUMPDEST, vm.PUSH1, 0, vm.SLOAD, vm.PUSH1, 0, vm.MSTORE, vm.PUSH1, 32, vm.PUSH1, 0, vm.RETURN),
	)
	// Creation code: the runtime code PUSHed as one word, MSTOREd
	// (right-aligned) at offset 0, and returned from there.
	creation := slices.Concat(
		saetest.Push(t, runtime),
		saetest.Ops(vm.PUSH1, 0, vm.MSTORE),
		saetest.Ops(vm.PUSH1, vm.OpCode(len(runtime)), vm.PUSH1, vm.OpCode(32-len(runtime)), vm.RETURN),
	)
	return g.signedTx(t, &types.LegacyTx{
		Nonce:    g.nonce(),
		Gas:      100_000,
		GasPrice: vmtest.InitialBaseFee,
		Data:     creation,
	})
}

func (g *generator) counterIncrementTx(t *testing.T) *types.Transaction {
	t.Helper()
	return g.signedTx(t, &types.LegacyTx{
		Nonce:    g.nonce(),
		To:       &g.fixture.Counter,
		Gas:      50_000,
		GasPrice: vmtest.InitialBaseFee,
	})
}

// storageClearTx returns a contract-creation transaction whose init code sets
// storage slot 0 and clears it again, accruing an SSTORE refund. coreth
// disables the refund counter from Apricot Phase 1, so the receipt's gasUsed
// retains the gas that EIP-3529 semantics would refund.
func (g *generator) storageClearTx(t *testing.T) *types.Transaction {
	t.Helper()
	return g.signedTx(t, &types.LegacyTx{
		Nonce:    g.nonce(),
		Gas:      100_000,
		GasPrice: vmtest.InitialBaseFee,
		Data: saetest.Ops(
			vm.PUSH1, 1, vm.PUSH1, 0, vm.SSTORE, // slot 0: 0 -> 1
			vm.PUSH1, 0, vm.PUSH1, 0, vm.SSTORE, // slot 0: 1 -> 0, accruing the refund
			vm.STOP,
		),
	})
}

func (g *generator) nativeAssetCallTx(t *testing.T, to common.Address, amount int64) *types.Transaction {
	t.Helper()
	return g.signedTx(t, &types.LegacyTx{
		Nonce:    g.nonce(),
		To:       &nativeasset.NativeAssetCallAddr,
		Gas:      200_000,
		GasPrice: vmtest.InitialBaseFee,
		Data: nativeasset.PackNativeAssetCallInput(
			to,
			common.Hash(antAssetID),
			big.NewInt(amount),
			nil,
		),
	})
}

// seedUTXO writes a UTXO owned by the test keychain into the shared memory
// of the X-chain, from where it can be atomically imported.
func (g *generator) seedUTXO(t *testing.T, assetID ids.ID, amount uint64) *avax.UTXO {
	t.Helper()
	g.utxoTxID++
	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{TxID: ids.Empty.Prefix(g.utxoTxID)},
		Asset:  avax.Asset{ID: assetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: amount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{vmtest.TestShortIDAddrs[0]},
			},
		},
	}
	utxoBytes, err := corethatomic.Codec.Marshal(corethatomic.CodecVersion, utxo)
	require.NoError(t, err, "atomic.Codec.Marshal(UTXO)")

	inputID := utxo.InputID()
	xChainSharedMemory := g.memory.NewSharedMemory(g.ctx.XChainID)
	require.NoError(t, xChainSharedMemory.Apply(map[ids.ID]*atomic.Requests{
		g.ctx.ChainID: {PutRequests: []*atomic.Element{{
			Key:    inputID[:],
			Value:  utxoBytes,
			Traits: [][]byte{vmtest.TestShortIDAddrs[0].Bytes()},
		}}},
	}), "sharedMemory.Apply(UTXO)")
	return utxo
}

// importTx returns an import transaction consuming the given seeded UTXOs
// from X-chain shared memory, crediting TestEthAddrs[0].
func (g *generator) importTx(t *testing.T, utxos ...*avax.UTXO) *corethatomic.Tx {
	t.Helper()
	tx, err := corethatomic.NewImportTx(
		g.ctx,
		g.rules(),
		g.vm.Clock().Unix(),
		g.ctx.XChainID,
		vmtest.TestEthAddrs[0],
		vmtest.InitialBaseFee,
		g.kc,
		utxos,
	)
	require.NoError(t, err, "atomic.NewImportTx()")
	return tx
}

// importAVAX seeds an AVAX UTXO of the given amount (in nAVAX) and returns an
// import transaction consuming it, crediting TestEthAddrs[0].
func (g *generator) importAVAX(t *testing.T, amount uint64) *corethatomic.Tx {
	t.Helper()
	return g.importTx(t, g.seedUTXO(t, g.ctx.AVAXAssetID, amount))
}

// importANT seeds one ANT UTXO plus one AVAX UTXO (to pay the fee) and
// returns an import transaction consuming both, funding a multicoin balance
// at TestEthAddrs[0].
func (g *generator) importANT(t *testing.T, amount uint64) *corethatomic.Tx {
	t.Helper()
	return g.importTx(t,
		g.seedUTXO(t, antAssetID, amount),
		g.seedUTXO(t, g.ctx.AVAXAssetID, 1*units.Avax),
	)
}

// exportAVAX returns an export transaction moving the given amount (in nAVAX)
// from TestEthAddrs[1] to the X-chain. The exporter is deliberately NOT the
// EVM sender: an export spends the exporting account's nonce, which would
// conflict with the generator's nonce sequence.
func (g *generator) exportAVAX(t *testing.T, amount uint64) *corethatomic.Tx {
	t.Helper()
	statedb, err := g.vm.Ethereum().BlockChain().State()
	require.NoError(t, err, "BlockChain().State()")
	tx, err := corethatomic.NewExportTx(
		g.ctx,
		g.rules(),
		extstate.New(statedb),
		g.ctx.AVAXAssetID,
		amount,
		g.ctx.XChainID,
		vmtest.TestShortIDAddrs[1],
		vmtest.InitialBaseFee,
		vmtest.TestKeys[1:2],
	)
	require.NoError(t, err, "atomic.NewExportTx(%d nAVAX)", amount)
	return tx
}

func (g *generator) sendWarpMessageTx(t *testing.T) *types.Transaction {
	t.Helper()
	input, err := warpcontract.PackSendWarpMessage([]byte("upgradechain fixture warp payload"))
	require.NoError(t, err, "warp.PackSendWarpMessage()")
	return g.signedTx(t, &types.LegacyTx{
		Nonce:    g.nonce(),
		To:       &warpcontract.ContractAddress,
		Gas:      200_000,
		GasPrice: vmtest.InitialBaseFee,
		Data:     input,
	})
}

// verifiedWarpMessageTx returns a getVerifiedWarpMessage call carrying, as an
// access-list predicate, an incoming warp message signed by the fixture's
// fixed validator set.
func (g *generator) verifiedWarpMessageTx(t *testing.T) *types.Transaction {
	t.Helper()

	addressedPayload, err := payload.NewAddressedCall(
		vmtest.TestEthAddrs[0].Bytes(),
		[]byte("upgradechain fixture verified payload"),
	)
	require.NoError(t, err, "payload.NewAddressedCall()")
	unsignedMessage, err := avalanchewarp.NewUnsignedMessage(
		constants.UnitTestID,
		warpSourceChainID,
		addressedPayload.Bytes(),
	)
	require.NoError(t, err, "warp.NewUnsignedMessage()")
	signedMessage := g.warpValidators.Sign(t, unsignedMessage)

	input, err := warpcontract.PackGetVerifiedWarpMessage(0)
	require.NoError(t, err, "warp.PackGetVerifiedWarpMessage()")
	return g.signedTx(t, &types.DynamicFeeTx{
		ChainID:   g.chainID(),
		Nonce:     g.nonce(),
		To:        &warpcontract.ContractAddress,
		Gas:       1_000_000,
		GasFeeCap: vmtest.InitialBaseFee,
		GasTipCap: big.NewInt(params.GWei),
		Data:      input,
		AccessList: types.AccessList{{
			Address:     warpcontract.ContractAddress,
			StorageKeys: predicate.New(signedMessage.Bytes()),
		}},
	})
}
