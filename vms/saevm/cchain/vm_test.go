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

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/google/go-cmp/cmp"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
)

func TestMain(m *testing.M) {
	evm.RegisterAllLibEVMExtras()
	goleak.VerifyTestMain(m, saetest.GoleakOptions()...)
}

// SUT is the system under test for the cchain [VM]. It bundles the [VM]
// itself and an HTTP [Client] connected to an in-process [httptest.Server].
type SUT struct {
	*VM
	*Client

	snowCtx *snow.Context
	memory  *atomic.Memory
}

type (
	sutConfig struct {
		genesis core.Genesis
	}
	sutOption = options.Option[sutConfig]
)

// newSUT initializes a cchain [VM], transitions it to [snow.NormalOp], and
// mounts its HTTP handlers behind a local [httptest.Server] at the paths
// [NewClient] expects.
func newSUT(tb testing.TB, opts ...sutOption) *SUT {
	tb.Helper()

	var (
		vm  = &VM{}
		ctx = tb.Context()
		db  = memdb.New()
		cfg = options.ApplyTo(&sutConfig{
			genesis: core.Genesis{
				Config:     saetest.ChainConfig(),
				Timestamp:  saeparams.TauSeconds,
				Difficulty: big.NewInt(0), // irrelevant but required to marshal
				Alloc:      types.GenesisAlloc{},
			},
		}, opts...)
	)

	// The VM and shared memory MUST share an underlying database so that
	// [atomic.SharedMemory.Apply] writes to the VM DB.
	memory := atomic.NewMemory(prefixdb.New([]byte("sharedmemory"), db))
	snowCtx := snowtest.Context(tb, snowtest.CChainID)
	snowCtx.SharedMemory = memory.NewSharedMemory(snowtest.CChainID)
	snowCtx.Log = saetest.NewTBLogger(tb, logging.Debug)

	chainDB := prefixdb.New([]byte("chain"), db)

	genesisBytes, err := json.Marshal(cfg.genesis)
	require.NoErrorf(tb, err, "json.Marshal(%T)", cfg.genesis)

	// The SAE mempool may push gossip transactions when they are issued.
	appSender := &enginetest.Sender{
		SendAppGossipF: func(context.Context, snowcommon.SendConfig, []byte) error {
			return nil
		},
	}

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

	handlers, err := vm.CreateHandlers(ctx)
	require.NoErrorf(tb, err, "%T.CreateHandlers()", vm)

	mux := http.NewServeMux()
	for path, h := range handlers {
		mux.Handle(cchainHTTPPrefix+path, h)
	}
	server := httptest.NewServer(mux)
	tb.Cleanup(server.Close)

	return &SUT{
		VM:      vm,
		Client:  NewClient(server.URL),
		snowCtx: snowCtx,
		memory:  memory,
	}
}

// assertUTXOsExist asserts that the shared memory between peerChainID and the
// C-Chain contains each of the expected UTXOs.
func (s *SUT) assertUTXOsExist(tb testing.TB, peerChainID ids.ID, want ...*avax.UTXO) {
	tb.Helper()

	keys := make([][]byte, len(want))
	for i, utxo := range want {
		inputID := utxo.InputID()
		keys[i] = inputID[:]
	}
	peerMemory := s.memory.NewSharedMemory(peerChainID)
	utxoBytes, err := peerMemory.Get(snowtest.CChainID, keys)
	require.NoErrorf(tb, err, "%T.Get()", peerMemory)

	got := make([]*avax.UTXO, len(utxoBytes))
	for i, b := range utxoBytes {
		got[i] = txtest.MustParseUTXO(tb, b)
	}
	if diff := cmp.Diff(want, got, txtest.UTXOCmpOpt()); diff != "" {
		tb.Errorf("UTXOs in shared memory with %s (-want +got):\n%s", peerChainID, diff)
	}
}

// addUTXOs puts the given UTXOs into shared memory between peerChainID and the
// C-Chain.
func (s *SUT) addUTXOs(tb testing.TB, peerChainID ids.ID, utxos ...*avax.UTXO) {
	tb.Helper()

	elems := make([]*atomic.Element, len(utxos))
	for i, utxo := range utxos {
		inputID := utxo.InputID()
		e := &atomic.Element{
			Key:   inputID[:],
			Value: txtest.MustMarshalUTXO(tb, utxo),
		}
		if o, ok := utxo.Out.(avax.Addressable); ok {
			e.Traits = o.Addresses()
		}
		elems[i] = e
	}
	peerMemory := s.memory.NewSharedMemory(peerChainID)
	err := peerMemory.Apply(map[ids.ID]*atomic.Requests{
		snowtest.CChainID: {PutRequests: elems},
	})
	require.NoErrorf(tb, err, "%T.Apply()", peerMemory)
}

// balance returns the balance of addr at the last-executed state.
func (s *SUT) balance(tb testing.TB, addr common.Address) uint256.Int {
	tb.Helper()

	state, err := s.LastExecutedState()
	require.NoErrorf(tb, err, "%T.LastExecutedState()", s.VM)
	return *state.GetBalance(addr)
}

// assertBalance asserts addr's balance at the last-executed state.
func (s *SUT) assertBalance(tb testing.TB, addr common.Address, want uint256.Int) {
	tb.Helper()

	got := s.balance(tb, addr)
	assert.Equalf(tb, want, got, "balance of %s", addr)
}

// issueAndExecute submits t through [Client.IssueTx] and drives the consensus
// loop to produce, accept, and execute the next block, which is returned.
func (s *SUT) issueAndExecute(tb testing.TB, t *tx.Tx) *blocks.Block {
	tb.Helper()

	require.NoErrorf(tb, s.IssueTx(tb.Context(), t), "%T.IssueTx()", s.Client)
	return s.runConsensusLoop(tb)
}

// assertTxAccepted asserts that [Client.GetTx] returns the given tx at the
// given block height.
func (s *SUT) assertTxAccepted(tb testing.TB, want *tx.Tx, wantHeight uint64) {
	tb.Helper()

	got, gotHeight, err := s.GetTx(tb.Context(), want.ID())
	require.NoErrorf(tb, err, "%T.GetTx()", s.Client)
	if diff := cmp.Diff(want, got, txtest.CmpOpt()); diff != "" {
		tb.Errorf("%T.GetTx() (-want +got):\n%s", s.Client, diff)
	}
	assert.Equalf(tb, wantHeight, gotHeight, "%T.GetTx() block height", s.Client)
}

// runConsensusLoop builds a block on top of the last-accepted block, drives it
// through verify+accept, and waits until it has been executed.
func (s *SUT) runConsensusLoop(tb testing.TB) *blocks.Block {
	tb.Helper()

	blk := s.buildVerifyAccept(tb)
	require.NoErrorf(tb, blk.WaitUntilExecuted(tb.Context()), "%T.WaitUntilExecuted()", blk)
	return blk
}

// buildVerifyAccept builds, verifies, and accepts a block on top of the
// last-accepted block.
func (s *SUT) buildVerifyAccept(tb testing.TB) *blocks.Block {
	tb.Helper()

	blk := s.buildVerify(tb, s.lastAccepted(tb))
	require.NoErrorf(tb, s.AcceptBlock(tb.Context(), blk), "%T.AcceptBlock()", s.VM)
	return blk
}

// lastAccepted returns the ID of the last-accepted block.
func (s *SUT) lastAccepted(tb testing.TB) ids.ID {
	tb.Helper()

	id, err := s.LastAccepted(tb.Context())
	require.NoErrorf(tb, err, "%T.LastAccepted()", s.VM)
	return id
}

// buildVerify builds and verifies a block on top of preferenceID.
func (s *SUT) buildVerify(tb testing.TB, preferenceID ids.ID) *blocks.Block {
	tb.Helper()

	ctx := tb.Context()
	// TODO(StephenButtolph): When implementing Warp, we will need to provide
	// meaningful block contexts.
	var blockCtx *block.Context
	require.NoErrorf(tb, s.SetPreference(ctx, preferenceID, blockCtx), "%T.SetPreference()", s.VM)

	e, err := s.WaitForEvent(ctx)
	require.NoErrorf(tb, err, "%T.WaitForEvent()", s.VM)
	assert.Equalf(tb, snowcommon.PendingTxs, e, "%T.WaitForEvent() event", s.VM)

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
	tb testing.TB,
	sourceChain ids.ID,
	to common.Address,
	fee uint64,
) (*tx.Tx, *tx.Import) {
	tb.Helper()

	utxos := w.getUTXOs(tb, sourceChain)

	var (
		avaxAssetID  = w.snowCtx.AVAXAssetID
		importedAVAX uint64
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

// getUTXOs returns every UTXO controlled by the wallet that has been exported
// to this chain from sourceChain.
func (w *wallet) getUTXOs(tb testing.TB, sourceChain ids.ID) []*avax.UTXO {
	tb.Helper()
	return getUTXOs(tb, w.client, sourceChain, maxGetUTXOsLimit, w.sk.Address())
}

// getUTXOs drains [Client.GetUTXOs] for addrs by walking pages of size limit
// until a short page signals the end of the result set.
func getUTXOs(
	tb testing.TB,
	client *Client,
	sourceChain ids.ID,
	limit uint32,
	addrs ...ids.ShortID,
) []*avax.UTXO {
	tb.Helper()

	var (
		startAddr   ids.ShortID
		startUTXOID ids.ID
		utxos       []*avax.UTXO
	)
	for {
		page, endAddr, endUTXOID, err := client.GetUTXOs(
			tb.Context(),
			addrs,
			sourceChain,
			limit,
			startAddr,
			startUTXOID,
		)
		require.NoErrorf(tb, err, "%T.GetUTXOs()", client)
		utxos = append(utxos, page...)
		if uint64(len(page)) < uint64(limit) {
			return utxos
		}
		startAddr, startUTXOID = endAddr, endUTXOID
	}
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

var x2cRate = big.NewInt(tx.X2CRate)

// addNAVAX returns balance + nAVAXDelta. nAVAXDelta may be negative.
func addNAVAX(tb testing.TB, balance uint256.Int, nAVAXDelta int64) uint256.Int {
	tb.Helper()

	delta := big.NewInt(nAVAXDelta)
	delta.Mul(delta, x2cRate)
	bigBalance := balance.ToBig()
	bigBalance.Add(bigBalance, delta)

	result, overflow := uint256.FromBig(bigBalance)
	require.Falsef(tb, overflow, "addNAVAX(%s, %d) overflows uint256", balance, nAVAXDelta)
	return *result
}

// TestExport exercises the cchain VM end-to-end with an Export tx.
func TestExport(t *testing.T) {
	sk := txtest.NewKey(t)
	sender := sk.EthAddress()
	sut := newSUT(t, options.Func[sutConfig](func(c *sutConfig) {
		c.genesis.Alloc = saetest.MaxAllocFor(sender)
	}))

	w := newWallet(sk, sut.snowCtx, sut.Client)
	const (
		txFee          = 50
		exportedAmount = 50
	)
	signedExport, export := w.newExportTx(
		t,
		sut.snowCtx.XChainID,
		txFee,
		txtest.NewTransferOutput(exportedAmount, sk.Address()),
	)

	initialBalance := sut.balance(t, sender)
	blk := sut.issueAndExecute(t, signedExport)
	sut.assertTxAccepted(t, signedExport, blk.NumberU64())
	const amountBurned = exportedAmount + txFee
	sut.assertBalance(t, sender, addNAVAX(t, initialBalance, -amountBurned))
	sut.assertUTXOsExist(t, sut.snowCtx.XChainID, txtest.ExportedUTXOs(signedExport.ID(), export)...)
}

// TestImport exercises the cchain VM end-to-end with an Import tx.
func TestImport(t *testing.T) {
	sut := newSUT(t)

	const utxoAmount = 100
	sk := txtest.NewKey(t)
	sut.addUTXOs(
		t,
		snowtest.XChainID,
		txtest.NewUTXO(utxoAmount, sut.snowCtx.AVAXAssetID, sk.Address()),
	)

	w := newWallet(sk, sut.snowCtx, sut.Client)
	receiver := txtest.NewKey(t).EthAddress()
	const txFee = 50
	signedImport, _ := w.newImportTx(t, sut.snowCtx.XChainID, receiver, txFee)

	blk := sut.issueAndExecute(t, signedImport)
	sut.assertTxAccepted(t, signedImport, blk.NumberU64())
	const amountMinted = utxoAmount - txFee
	sut.assertBalance(t, receiver, tx.ScaleAVAX(amountMinted))
}

// TestBuildBlockOnProcessing verifies that the block builder excludes a mempool
// candidate whose inputs were already consumed by an unsettled ancestor block.
func TestBuildBlockOnProcessing(t *testing.T) {
	var (
		skA = txtest.NewKey(t)
		skB = txtest.NewKey(t)
	)
	sut := newSUT(t, options.Func[sutConfig](func(c *sutConfig) {
		c.genesis.Alloc = saetest.MaxAllocFor(
			skA.EthAddress(),
			skB.EthAddress(),
		)
	}))

	newExport := func(w *wallet) *tx.Tx {
		const (
			txFee          = 50
			exportedAmount = 50
		)
		signedExport, _ := w.newExportTx(
			t,
			sut.snowCtx.XChainID,
			txFee,
			txtest.NewTransferOutput(exportedAmount, w.sk.Address()),
		)
		return signedExport
	}

	ctx := t.Context()
	wA := newWallet(skA, sut.snowCtx, sut.Client)
	txA := newExport(wA)
	require.NoErrorf(t, sut.IssueTx(ctx, txA), "%T.IssueTx(txA)", sut.Client)
	blockA := sut.buildVerify(t, sut.lastAccepted(t))
	if diff := cmp.Diff([]*tx.Tx{txA}, blockTxs(t, blockA), txtest.CmpOpt()); diff != "" {
		t.Errorf("%T txs (-want +got):\n%s", blockA, diff)
	}

	// blockA is verified but not accepted, so txA stays in the mempool and
	// is presented to blockB's builder as a candidate.
	wB := newWallet(skB, sut.snowCtx, sut.Client)
	txB := newExport(wB)
	require.NoErrorf(t, sut.IssueTx(ctx, txB), "%T.IssueTx(txB)", sut.Client)
	blockB := sut.buildVerify(t, blockA.ID())
	if diff := cmp.Diff([]*tx.Tx{txB}, blockTxs(t, blockB), txtest.CmpOpt()); diff != "" {
		t.Errorf("%T txs (-want +got):\n%s", blockB, diff)
	}

	require.NoErrorf(t, sut.AcceptBlock(ctx, blockA), "%T.AcceptBlock(blockA)", sut.VM)
	require.NoErrorf(t, sut.AcceptBlock(ctx, blockB), "%T.AcceptBlock(blockB)", sut.VM)
	require.NoErrorf(t, blockA.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted(blockA)", blockA)
	require.NoErrorf(t, blockB.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted(blockB)", blockB)

	sut.assertTxAccepted(t, txA, blockA.NumberU64())
	sut.assertTxAccepted(t, txB, blockB.NumberU64())
}

// blockTxs returns every cross-chain tx encoded in the block.
func blockTxs(tb testing.TB, blk *blocks.Block) []*tx.Tx {
	tb.Helper()

	txs, err := tx.ParseSlice(customtypes.BlockExtData(blk.EthBlock()))
	require.NoErrorf(tb, err, "tx.ParseSlice()")
	return txs
}
