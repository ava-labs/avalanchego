// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/rlp"
	"github.com/google/go-cmp/cmp"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	// Imported for [saexec.Execute] comment resolution.
	_ "github.com/ava-labs/avalanchego/vms/saevm/saexec"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/cchaintest"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
	"github.com/ava-labs/avalanchego/vms/saevm/cmputils"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
	"github.com/ava-labs/avalanchego/vms/saevm/txgossip/txgossiptest"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	cparams "github.com/ava-labs/avalanchego/graft/coreth/params"
	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
	ethparams "github.com/ava-labs/libevm/params"
	ethrpc "github.com/ava-labs/libevm/rpc"
)

func TestMain(m *testing.M) {
	evm.RegisterAllLibEVMExtras()
	goleak.VerifyTestMain(m, saetest.GoleakOptions()...)
}

var _ saetest.Peer = (*SUT)(nil)

// SUT is the system under test for the cchain [VM]. It bundles the [VM]
// itself and an HTTP [Client] connected to an in-process [httptest.Server].
type SUT struct {
	*VM
	*Client

	memory    *atomic.Memory
	sender    *saetest.Sender
	ethclient *ethclient.Client
}

func (s *SUT) NodeID() ids.NodeID      { return s.ctx.NodeID }
func (s *SUT) Sender() *saetest.Sender { return s.sender }

type (
	sutConfig struct {
		genesis    core.Genesis
		nodeID     ids.NodeID
		validators set.Set[ids.NodeID]
	}
	sutOption = options.Option[sutConfig]
)

// withMaxAllocFor configures the SUT's genesis to allocate the maximum possible
// balance to each address.
func withMaxAllocFor(addrs ...common.Address) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.genesis.Alloc = saetest.MaxAllocFor(addrs...)
	})
}

// withNodeID overrides the SUT's randomly generated NodeID.
func withNodeID(id ids.NodeID) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.nodeID = id
	})
}

// withValidators adds each NodeID to the validator set with weight 1.
func withValidators(vdrs set.Set[ids.NodeID]) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.validators = vdrs
	})
}

// newSUT initializes a cchain [VM], transitions it to [snow.NormalOp], and
// mounts its HTTP handlers behind a local [httptest.Server] at the paths
// [NewClient] expects.
func newSUT(tb testing.TB, opts ...sutOption) (context.Context, *SUT) {
	tb.Helper()

	// Run under the latest network upgrade rules by default.
	chainConfig := cparams.Copy(saetest.ChainConfig())
	cparams.WithExtra(&chainConfig, extras.TestChainConfig)

	var (
		vm = &VM{
			pullGossipPeriod: 100 * time.Millisecond,
			pushGossipPeriod: 100 * time.Millisecond,
		}
		db  = memdb.New()
		cfg = options.ApplyTo(&sutConfig{
			genesis: core.Genesis{
				Config:     &chainConfig,
				Timestamp:  saeparams.TauSeconds,
				Difficulty: big.NewInt(0), // irrelevant but required to marshal
				Alloc:      types.GenesisAlloc{},
			},
			nodeID: ids.GenerateTestNodeID(),
		}, opts...)
	)

	// The VM and shared memory MUST share an underlying database so that
	// [atomic.SharedMemory.Apply] writes to the VM DB.
	memory := atomic.NewMemory(prefixdb.New([]byte("sharedmemory"), db))
	snowCtx := snowtest.Context(tb, snowtest.CChainID)
	snowCtx.NodeID = cfg.nodeID
	snowCtx.SharedMemory = memory.NewSharedMemory(snowtest.CChainID)
	log := loggingtest.New(tb, logging.Debug)
	snowCtx.Log = log
	saetest.SetValidators(tb, snowCtx.ValidatorState, cfg.validators)

	chainDB := prefixdb.New([]byte("chain"), db)

	genesisBytes, err := json.Marshal(cfg.genesis)
	require.NoErrorf(tb, err, "json.Marshal(%T)", cfg.genesis)

	appSender := saetest.NewSender(tb, cfg.validators)

	ctx := log.CancelOnError(tb.Context())
	require.NoErrorf(tb, vm.Initialize(
		ctx,
		snowCtx,
		chainDB,
		genesisBytes,
		nil, // upgradeBytes
		nil, // configBytes
		nil, // fxs
		appSender,
	), "%T.Initialize()", vm)
	tb.Cleanup(func() {
		// The context is cancelled before cleanup is called, so we strip the
		// cancellation.
		ctx := context.WithoutCancel(tb.Context())
		require.NoErrorf(tb, vm.Shutdown(ctx), "%T.Shutdown()", vm)
	})
	require.NoErrorf(tb, vm.SetState(ctx, snow.NormalOp), "%T.SetState(%s)", vm, snow.NormalOp)

	// Avalanchego marks the local node as connected so that p2p protocols don't
	// need to treat our node as a special case.
	require.NoErrorf(tb, vm.Connected(ctx, snowCtx.NodeID, version.Current), "%T.Connected(%s)", vm, snowCtx.NodeID)

	handlers, err := vm.CreateHandlers(ctx)
	require.NoErrorf(tb, err, "%T.CreateHandlers()", vm)

	mux := http.NewServeMux()
	for path, h := range handlers {
		mux.Handle(cchainHTTPPrefix+path, h)
	}
	server := httptest.NewServer(mux)
	tb.Cleanup(server.Close)

	const wsHTTPPath = cchainHTTPPrefix + "/ws"
	wsURI := "ws://" + server.Listener.Addr().String() + wsHTTPPath
	ethRPCClient, err := ethrpc.Dial(wsURI)
	require.NoErrorf(tb, err, "rpc.Dial(%s)", wsURI)
	tb.Cleanup(ethRPCClient.Close)

	sut := &SUT{
		VM:        vm,
		Client:    NewClient(server.URL),
		memory:    memory,
		sender:    appSender,
		ethclient: ethclient.NewClient(ethRPCClient),
	}
	appSender.SetSelf(sut)
	tb.Cleanup(appSender.Close)
	return ctx, sut
}

// assertUTXOsExist asserts that reader chain can read the expected UTXOs from
// writer chain.
func (s *SUT) assertUTXOsExist(tb testing.TB, readerChainID, writerChainID ids.ID, want ...*avax.UTXO) {
	tb.Helper()

	keys := make([][]byte, len(want))
	for i, utxo := range want {
		inputID := utxo.InputID()
		keys[i] = inputID[:]
	}
	readerMemory := s.memory.NewSharedMemory(readerChainID)
	utxoBytes, err := readerMemory.Get(writerChainID, keys)
	require.NoErrorf(tb, err, "%T.Get()", readerMemory)

	got := make([]*avax.UTXO, len(utxoBytes))
	for i, b := range utxoBytes {
		got[i] = txtest.ParseUTXO(tb, b)
	}
	if diff := cmp.Diff(want, got, txtest.UTXOCmpOpt()); diff != "" {
		tb.Errorf("UTXOs in shared memory with %s (-want +got):\n%s", writerChainID, diff)
	}
}

// assertUTXOsExist asserts that reader chain can not read the unwanted UTXOs
// from writer chain.
func (s *SUT) assertUTXOsMissing(tb testing.TB, readerChainID, writerChainID ids.ID, unwanted ...*avax.UTXO) {
	tb.Helper()

	readerMemory := s.memory.NewSharedMemory(readerChainID)
	for i, utxo := range unwanted {
		inputID := utxo.InputID()
		key := inputID[:]

		keys := [][]byte{key}
		_, err := readerMemory.Get(writerChainID, keys)
		assert.ErrorIsf(tb, err, database.ErrNotFound, "%T.Get(utxo %d)", readerMemory, i)
	}
}

// addUTXOs acts as the writer chain and inserts the given UTXOs so that the
// reader chain can read them in the future.
func (s *SUT) addUTXOs(tb testing.TB, readerChainID, writerChainID ids.ID, utxos ...*avax.UTXO) {
	tb.Helper()

	elems := make([]*atomic.Element, len(utxos))
	for i, utxo := range utxos {
		inputID := utxo.InputID()
		e := &atomic.Element{
			Key:   inputID[:],
			Value: txtest.MarshalUTXO(tb, utxo),
		}
		if o, ok := utxo.Out.(avax.Addressable); ok {
			e.Traits = o.Addresses()
		}
		elems[i] = e
	}
	writerMemory := s.memory.NewSharedMemory(writerChainID)
	err := writerMemory.Apply(map[ids.ID]*atomic.Requests{
		readerChainID: {PutRequests: elems},
	})
	require.NoErrorf(tb, err, "%T.Apply()", writerMemory)
}

// balance returns the balance of addr at the last-executed state.
func (s *SUT) balance(tb testing.TB, addr common.Address) uint256.Int {
	tb.Helper()

	state, err := s.LastExecutedState()
	require.NoErrorf(tb, err, "%T.LastExecutedState()", s.VM)
	return *state.GetBalance(addr)
}

// assertAccount asserts addr's nonce and balance at the last-executed state.
func (s *SUT) assertAccount(tb testing.TB, addr common.Address, wantNonce uint64, wantBalance uint256.Int) {
	tb.Helper()

	state, err := s.LastExecutedState()
	require.NoErrorf(tb, err, "%T.LastExecutedState()", s.VM)

	gotNonce := state.GetNonce(addr)
	assert.Equalf(tb, wantNonce, gotNonce, "nonce of %s", addr)

	gotBalance := *state.GetBalance(addr)
	assert.Equalf(tb, wantBalance, gotBalance, "balance of %s", addr)
}

// issueAndExecute submits t through [Client.IssueTx] and drives the consensus
// loop to produce, accept, and execute the next block, which is returned.
func (s *SUT) issueAndExecute(ctx context.Context, tb testing.TB, t *tx.Tx) *blocks.Block {
	tb.Helper()

	require.NoErrorf(tb, s.IssueTx(ctx, t), "%T.IssueTx()", s.Client)
	return s.runConsensusLoop(ctx, tb)
}

// assertTxAccepted asserts that [Client.GetTx] returns the given tx at the
// given block height.
func (s *SUT) assertTxAccepted(ctx context.Context, tb testing.TB, want *tx.Tx, wantHeight uint64) {
	tb.Helper()

	got, gotHeight, err := s.GetTx(ctx, want.ID())
	require.NoErrorf(tb, err, "%T.GetTx()", s.Client)
	if diff := cmp.Diff(want, got, txtest.CmpOpt()); diff != "" {
		tb.Errorf("%T.GetTx() (-want +got):\n%s", s.Client, diff)
	}
	assert.Equalf(tb, wantHeight, gotHeight, "%T.GetTx() block height", s.Client)
}

// runConsensusLoop builds a block on top of the last-accepted block, drives it
// through verify+accept, and waits until it has been executed.
func (s *SUT) runConsensusLoop(ctx context.Context, tb testing.TB) *blocks.Block {
	tb.Helper()

	blk := s.buildVerifyAccept(ctx, tb)
	require.NoErrorf(tb, blk.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", blk)
	return blk
}

// buildVerifyAccept builds, verifies, and accepts a block on top of the
// last-accepted block.
func (s *SUT) buildVerifyAccept(ctx context.Context, tb testing.TB) *blocks.Block {
	tb.Helper()

	lastAccepted := s.lastAccepted(ctx, tb)
	blk := s.buildVerify(ctx, tb, lastAccepted)
	require.NoErrorf(tb, s.AcceptBlock(ctx, blk), "%T.AcceptBlock()", s.VM)
	return blk
}

// lastAccepted returns the ID of the last-accepted block.
func (s *SUT) lastAccepted(ctx context.Context, tb testing.TB) ids.ID {
	tb.Helper()

	id, err := s.LastAccepted(ctx)
	require.NoErrorf(tb, err, "%T.LastAccepted()", s.VM)
	return id
}

func (s *SUT) waitForPendingTxs(ctx context.Context, tb testing.TB) {
	tb.Helper()

	e, err := s.WaitForEvent(ctx)
	require.NoErrorf(tb, err, "%T.WaitForEvent()", s.VM)
	assert.Equalf(tb, snowcommon.PendingTxs, e, "%T.WaitForEvent() event", s.VM)
}

// waitForPendingEthTxs blocks until every tx is pending in the source the block
// builder draws from, so the built block includes them all rather than racing
// promotion. The geth RPC backend's [GetPoolTransactions] resolves the same
// [txpool.Pool.Pending] set used by [txgossip.Set.TransactionsByPriority]
// during block building.
func (s *SUT) waitForPendingEthTxs(ctx context.Context, tb testing.TB, txs ...*types.Transaction) {
	tb.Helper()
	txgossiptest.WaitUntilPending(tb, ctx, s.GethRPCBackends(), txs...)
}

// buildVerify builds and verifies a block on top of preferenceID.
func (s *SUT) buildVerify(ctx context.Context, tb testing.TB, preferenceID ids.ID) *blocks.Block {
	tb.Helper()

	// TODO(StephenButtolph): When implementing Warp, we will need to provide
	// meaningful block contexts.
	var blockCtx *block.Context
	require.NoErrorf(tb, s.SetPreference(ctx, preferenceID, blockCtx), "%T.SetPreference()", s.VM)

	s.waitForPendingTxs(ctx, tb)
	blk, err := s.BuildBlock(ctx, blockCtx)
	require.NoErrorf(tb, err, "%T.BuildBlock()", s.VM)
	require.NoErrorf(tb, s.VerifyBlock(ctx, blockCtx, blk), "%T.VerifyBlock()", s.VM)
	return blk
}

// wallet builds and signs cross-chain transactions on behalf of a single key.
type wallet struct {
	sk      *secp256k1.PrivateKey
	snowCtx *snow.Context
	client  *Client
	nonce   uint64
}

// newWallet returns a [*wallet] backed by sk for the chain described by
// snowCtx. client is queried when building imports to discover spendable
// UTXOs.
func newWallet(sk *secp256k1.PrivateKey, snowCtx *snow.Context, client *Client) *wallet {
	return &wallet{
		sk:      sk,
		snowCtx: snowCtx,
		client:  client,
	}
}

// newMinimalExportTx builds and signs an [tx.Export] sending a single output to
// [snowtest.XChainID].
func (w *wallet) newMinimalTx(tb testing.TB) *tx.Tx {
	tb.Helper()

	const (
		txFee          = 1
		exportedAmount = 1
	)
	t, _ := w.newExportTx(
		tb,
		snowtest.XChainID,
		txFee,
		txtest.NewTransferOutput(exportedAmount, w.sk.Address()),
	)
	return t
}

// newExportTx builds and signs an [tx.Export] sending outputs to
// destinationChain. The wallet contributes a single AVAX input from its eth
// address with Amount = sum(outputs.Amt) + fee, using its next nonce.
func (w *wallet) newExportTx(
	tb testing.TB,
	destinationChain ids.ID,
	fee uint64,
	outputs ...*secp256k1fx.TransferOutput,
) (*tx.Tx, *tx.Export) {
	tb.Helper()

	avaxAssetID := w.snowCtx.AVAXAssetID
	var exportedAmount uint64
	transferable := make([]*avax.TransferableOutput, len(outputs))
	for i, out := range outputs {
		transferable[i] = &avax.TransferableOutput{
			Asset: avax.Asset{ID: avaxAssetID},
			Out:   out,
		}
		exportedAmount += out.Amt
	}

	export := &tx.Export{
		NetworkID:        w.snowCtx.NetworkID,
		BlockchainID:     w.snowCtx.ChainID,
		DestinationChain: destinationChain,
		Ins: []tx.Input{{
			Address: w.sk.EthAddress(),
			Amount:  exportedAmount + fee,
			AssetID: avaxAssetID,
			Nonce:   w.nonce,
		}},
		ExportedOutputs: transferable,
	}
	w.nonce++

	return w.sign(tb, export, 1), export
}

// newImportTx builds and signs an [tx.Import] consuming all spendable AVAX
// UTXOs that have been exported to this chain from sourceChain and are owned
// by the wallet, crediting the total imported (minus fee) to `to` on the
// C-Chain.
func (w *wallet) newImportTx(
	ctx context.Context,
	tb testing.TB,
	sourceChain ids.ID,
	to common.Address,
	fee uint64,
) (*tx.Tx, *tx.Import) {
	tb.Helper()

	var (
		avaxAssetID  = w.snowCtx.AVAXAssetID
		importedAVAX uint64
		utxos        = w.client.getAllUTXOs(ctx, tb, sourceChain, maxGetUTXOsLimit, w.sk.Address())
		inputs       = make([]*avax.TransferableInput, 0, len(utxos))
	)
	for _, utxo := range utxos {
		if utxo.Asset.ID != avaxAssetID {
			continue
		}

		out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		require.Truef(tb, ok, "unexpected UTXO output type %T", utxo.Out)

		importedAVAX += out.Amt
		inputs = append(inputs, &avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  utxo.Asset,
			In: &secp256k1fx.TransferInput{
				Amt: out.Amt,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{0},
				},
			},
		})
	}
	require.Greaterf(tb, importedAVAX, fee, "imported AVAX insufficient to cover fee")

	imp := &tx.Import{
		NetworkID:      w.snowCtx.NetworkID,
		BlockchainID:   w.snowCtx.ChainID,
		SourceChain:    sourceChain,
		ImportedInputs: inputs,
		Outs: []tx.Output{{
			Address: to,
			Amount:  importedAVAX - fee,
			AssetID: avaxAssetID,
		}},
	}
	return w.sign(tb, imp, len(inputs)), imp
}

// sign wraps u in a [tx.Tx] with numCreds copies of a single-sig credential
// over u.
func (w *wallet) sign(tb testing.TB, u tx.Unsigned, numCreds int) *tx.Tx {
	tb.Helper()

	sig := txtest.Sign(tb, u, w.sk)
	creds := make([]tx.Credential, numCreds)
	for i := range creds {
		creds[i] = &secp256k1fx.Credential{Sigs: []txtest.Signature{sig}}
	}
	return &tx.Tx{
		Unsigned: u,
		Creds:    creds,
	}
}

// addNAVAX returns balance + nAVAXDelta. nAVAXDelta MAY be negative.
func addNAVAX(tb testing.TB, balance uint256.Int, nAVAXDelta int64) uint256.Int {
	tb.Helper()

	var (
		op       = balance.AddOverflow
		absDelta = uint64(nAVAXDelta)
	)
	if nAVAXDelta < 0 {
		op = balance.SubOverflow
		absDelta = -absDelta
	}

	delta := tx.ScaleAVAX(absDelta)
	_, overflow := op(&balance, &delta)
	require.Falsef(tb, overflow, "addNAVAX(%s, %d) overflows uint256", balance, nAVAXDelta)
	return balance
}

// TestExport exercises the cchain VM end-to-end with an Export tx.
func TestExport(t *testing.T) {
	sk := txtest.NewKey(t)
	sender := sk.EthAddress()
	ctx, sut := newSUT(t, options.Func[sutConfig](func(c *sutConfig) {
		c.genesis.Alloc = saetest.MaxAllocFor(sender)
	}))

	var (
		w                = newWallet(sk, sut.ctx, sut.Client)
		destinationChain = sut.ctx.XChainID
	)
	const (
		txFee          = 50
		exportedAmount = 50
	)
	signedExport, export := w.newExportTx(
		t,
		destinationChain,
		txFee,
		txtest.NewTransferOutput(exportedAmount, sk.Address()),
	)

	initialBalance := sut.balance(t, sender)
	blk := sut.issueAndExecute(ctx, t, signedExport)
	sut.assertTxAccepted(ctx, t, signedExport, blk.NumberU64())
	const (
		nonce        = 1
		amountBurned = exportedAmount + txFee
	)
	sut.assertAccount(t, sender, nonce, addNAVAX(t, initialBalance, -amountBurned))
	sut.assertUTXOsExist(t, destinationChain, sut.ctx.ChainID, txtest.ExportedUTXOs(signedExport.ID(), export)...)
}

// TestImport exercises the cchain VM end-to-end with an Import tx.
func TestImport(t *testing.T) {
	ctx, sut := newSUT(t)

	const utxoAmount = 100
	var (
		sk          = txtest.NewKey(t)
		utxo        = txtest.NewUTXO(utxoAmount, sut.ctx.AVAXAssetID, sk.Address())
		sourceChain = sut.ctx.XChainID
	)
	sut.addUTXOs(t, sut.ctx.ChainID, sourceChain, utxo)

	var (
		w        = newWallet(sk, sut.ctx, sut.Client)
		receiver = txtest.NewKey(t).EthAddress()
	)
	const txFee = 50
	signedImport, _ := w.newImportTx(ctx, t, sourceChain, receiver, txFee)

	blk := sut.issueAndExecute(ctx, t, signedImport)
	sut.assertTxAccepted(ctx, t, signedImport, blk.NumberU64())
	const (
		nonce        = 0
		amountMinted = utxoAmount - txFee
	)
	sut.assertAccount(t, receiver, nonce, tx.ScaleAVAX(amountMinted))
	sut.assertUTXOsMissing(t, sut.ctx.ChainID, sourceChain, utxo)
}

// TestBuildBlockOnProcessing verifies that the block builder excludes a mempool
// candidate whose inputs were already consumed by an unsettled ancestor block.
func TestBuildBlockOnProcessing(t *testing.T) {
	keys := make([]*secp256k1.PrivateKey, 2)
	for i := range keys {
		keys[i] = txtest.NewKey(t)
	}
	ctx, sut := newSUT(t, options.Func[sutConfig](func(c *sutConfig) {
		addrs := make([]common.Address, len(keys))
		for i, sk := range keys {
			addrs[i] = sk.EthAddress()
		}
		c.genesis.Alloc = saetest.MaxAllocFor(addrs...)
	}))

	var (
		preference = sut.lastAccepted(ctx, t)
		blocks     = make([]*blocks.Block, len(keys))
	)
	for i, sk := range keys {
		stx := newWallet(sk, sut.ctx, sut.Client).newMinimalTx(t)
		require.NoErrorf(t, sut.IssueTx(ctx, stx), "%T.IssueTx(tx)", sut.Client)

		// Delaying acceptance ensures that already-issued txs are still in the
		// mempool and are therefore (ineligible) candidates for inclusion here.
		block := sut.buildVerify(ctx, t, preference)
		if diff := cmp.Diff([]*tx.Tx{stx}, blockTxs(t, block), txtest.CmpOpt()); diff != "" {
			t.Errorf("%T txs (-want +got):\n%s", block, diff)
		}
		blocks[i] = block
		preference = block.ID()
	}
	for i, block := range blocks {
		require.NoErrorf(t, sut.AcceptBlock(ctx, block), "%T.AcceptBlock(%d)", sut.VM, i)
		require.NoErrorf(t, block.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted(%d)", block, i)
		for _, tx := range blockTxs(t, block) {
			sut.assertTxAccepted(ctx, t, tx, block.NumberU64())
		}
	}
}

// blockTxs returns every cross-chain tx encoded in the block.
func blockTxs(tb testing.TB, blk *blocks.Block) []*tx.Tx {
	tb.Helper()

	txs, err := tx.ParseSlice(customtypes.BlockExtData(blk.EthBlock()))
	require.NoErrorf(tb, err, "tx.ParseSlice()")
	return txs
}

// TestDebugTraceDoesNotApplyAtomicState asserts that executing a debug trace
// does not apply atomic state changes before the block is accepted.
func TestDebugTraceDoesNotApplyAtomicState(t *testing.T) {
	ethWallet := saetest.NewUNSAFEWallet(t, 1, types.LatestSigner(saetest.ChainConfig()))
	ethSender := ethWallet.Addresses()[0]
	exportKey := txtest.NewKey(t)
	ctx, sut := newSUT(t, options.Func[sutConfig](func(c *sutConfig) {
		c.genesis.Alloc = saetest.MaxAllocFor(
			ethSender,
			exportKey.EthAddress(),
		)
	}))

	// Tracing will error if there isn't at least one ethereum transaction in
	// the block.
	tracedTx := ethWallet.SetNonceAndSign(t, 0, &types.LegacyTx{
		To:       &ethSender,
		Gas:      ethparams.TxGas,
		GasPrice: big.NewInt(1),
	})
	require.NoErrorf(t, sut.ethclient.SendTransaction(ctx, tracedTx), "%T.SendTransaction(%#x)", sut.ethclient, tracedTx.Hash())

	// Export gives us observable external state.
	var (
		w                = newWallet(exportKey, sut.ctx, sut.Client)
		destinationChain = sut.ctx.XChainID
	)
	const (
		txFee          = 50
		exportedAmount = 50
	)
	signedExport, export := w.newExportTx(
		t,
		destinationChain,
		txFee,
		txtest.NewTransferOutput(exportedAmount, exportKey.Address()),
	)
	require.NoErrorf(t, sut.IssueTx(ctx, signedExport), "%T.IssueTx()", sut.Client)

	blk := sut.buildVerify(ctx, t, sut.lastAccepted(ctx, t))
	if diff := cmp.Diff(types.Transactions{tracedTx}, blk.Transactions(), cmputils.TransactionsByHash()); diff != "" {
		t.Errorf("%T eth txs (-want +got):\n%s", blk, diff)
	}
	if diff := cmp.Diff([]*tx.Tx{signedExport}, blockTxs(t, blk), txtest.CmpOpt()); diff != "" {
		t.Errorf("%T cross-chain txs (-want +got):\n%s", blk, diff)
	}

	rpc := sut.GethRPCBackends()
	// To rebuild the state at a particular tx, the [saexec.Execute] method is
	// called with all preceding transactions. In turn, this calls post-block
	// hooks of which atomic-state application would be one, but must be
	// excluded when tracing.
	_, _, _, release, err := rpc.StateAtTransaction(ctx, blk.EthBlock(), 0, 0)
	require.NoErrorf(t, err, "%T.StateAtTransaction(...)", rpc)
	defer release()

	// We haven't accepted the block yet, so it should be impossible for the
	// execution results to have been applied.
	exportedUTXOs := txtest.ExportedUTXOs(signedExport.ID(), export)
	sut.assertUTXOsMissing(t, destinationChain, sut.ctx.ChainID, exportedUTXOs...)
}

// TestMinGasConsumptionFloor asserts that the cchain VM charges the ACP-194
// gas floor of max(actualGasUsed, ceil(gasLimit/2)).
func TestMinGasConsumptionFloor(t *testing.T) {
	w := saetest.NewUNSAFEWallet(t, 1, types.LatestSigner(saetest.ChainConfig()))
	sender := w.Addresses()[0]

	ctx, sut := newSUT(t, options.Func[sutConfig](func(c *sutConfig) {
		c.genesis.Alloc = saetest.MaxAllocFor(sender)
	}))

	const highLimit = 1_000_000
	tests := []struct {
		name        string
		gasLimit    uint64
		wantGasUsed uint64
	}{
		{
			name:        "low_usage_charged_floor",
			gasLimit:    highLimit,
			wantGasUsed: highLimit / 2,
		},
		{
			name:        "usage_above_floor_charged_actual",
			gasLimit:    ethparams.TxGas,
			wantGasUsed: ethparams.TxGas,
		},
	}

	// A GasFeeCap of 1 pins the effective gas price to 1, so the AVAX burned
	// equals the gas charged.
	txs := make([]*types.Transaction, len(tests))
	for i, tt := range tests {
		txs[i] = w.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
			To:        &common.Address{},
			Gas:       tt.gasLimit,
			GasFeeCap: big.NewInt(1),
		})
		require.NoErrorf(t, sut.ethclient.SendTransaction(ctx, txs[i]), "%T.SendTransaction(%s)", sut.ethclient, tt.name)
	}

	// Ensure every tx is pending so the builder includes them all in one block.
	sut.waitForPendingEthTxs(ctx, t, txs...)

	preBalance := sut.balance(t, sender)
	blk := sut.runConsensusLoop(ctx, t)
	require.Lenf(t, blk.Receipts(), len(tests), "%T.Receipts()", blk)

	receiptByTx := make(map[common.Hash]*types.Receipt, len(blk.Receipts()))
	for _, r := range blk.Receipts() {
		receiptByTx[r.TxHash] = r
	}

	var totalCharged uint64
	for i, tt := range tests {
		receipt, ok := receiptByTx[txs[i].Hash()]
		require.Truef(t, ok, "receipt for %s", tt.name)
		totalCharged += tt.wantGasUsed
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status, "tx status")
			assert.Equal(t, tt.wantGasUsed, receipt.GasUsed, "gas charged")
			assert.Equal(t, big.NewInt(1), receipt.EffectiveGasPrice, "effective gas price")
		})
	}

	wantBalance := new(uint256.Int).Sub(&preBalance, uint256.NewInt(totalCharged))
	assert.Equalf(t, *wantBalance, sut.balance(t, sender), "sender balance reflects gas charged")
}

// TestParseBlock verifies that the cchain ParseBlock override accepts
// well-formed blocks and rejects blocks whose extData does not match the
// ExtDataHash committed in the header.
func TestParseBlock(t *testing.T) {
	ctx, sut := newSUT(t)

	w := newWallet(txtest.NewKey(t), snowtest.Context(t, snowtest.CChainID), nil)
	tx1 := w.newMinimalTx(t)

	tests := []struct {
		name    string
		block   *types.Block
		wantErr error
	}{
		{
			name:  "valid",
			block: cchaintest.NewBlock(t, 1, common.Hash{}, tx1),
		},
		{
			name:  "valid_empty",
			block: cchaintest.NewBlock(t, 1, common.Hash{}),
		},
		{
			name:    "extdata_hash_mismatch",
			block:   cchaintest.NewTamperedBlock(t, 1, common.Hash{}, tx1),
			wantErr: errExtDataHashMismatch,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf, err := rlp.EncodeToBytes(tt.block)
			require.NoError(t, err, "rlp.EncodeToBytes(block)")

			got, err := sut.ParseBlock(ctx, buf)
			require.ErrorIs(t, err, tt.wantErr, "vm.ParseBlock(buf)")
			if tt.wantErr != nil {
				return
			}

			require.Equal(t, tt.block.Hash(), got.EthBlock().Hash(), "vm.ParseBlock() block hash")
		})
	}
}
