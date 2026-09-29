// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math/big"
	"reflect"
	"sync"
	"testing"

	"github.com/arr4n/shed/testerr"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/synchronoustest"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
	"github.com/ava-labs/avalanchego/vms/saevm/cmputils"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest/rpctest"

	avajson "github.com/ava-labs/avalanchego/utils/json"
)

// getTxStatus exposes the deprecated [service.GetAtomicTxStatus] endpoint.
func (c *Client) getTxStatus(ctx context.Context, txID ids.ID) (TxStatus, error) {
	var resp TxStatus
	err := c.r.SendRequest(
		ctx,
		"avax.getAtomicTxStatus",
		&api.JSONTxID{
			TxID: txID,
		},
		&resp,
	)
	return resp, err
}

// getAllUTXOs drains [Client.GetUTXOs] for addrs by walking pages of size limit
// until a short page signals the end of the result set.
func (c *Client) getAllUTXOs(
	ctx context.Context,
	tb testing.TB,
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
		page, endAddr, endUTXOID, err := c.GetUTXOs(
			ctx,
			addrs,
			sourceChain,
			limit,
			startAddr,
			startUTXOID,
		)
		require.NoErrorf(tb, err, "%T.GetUTXOs()", c)
		utxos = append(utxos, page...)
		// This termination condition matches the original synchronous C-Chain
		// API behavior. Changing the expected termination condition could
		// accidentally break legacy users.
		if uint64(len(page)) < uint64(limit) {
			return utxos
		}
		startAddr, startUTXOID = endAddr, endUTXOID
	}
}

// testRPC drives the [rpctest.Case] table against the SUT's RPC client.
func (s *SUT) testRPC(ctx context.Context, t *testing.T, cases ...rpctest.Case) {
	t.Helper()
	rpctest.Run(ctx, t, s.ethclient.Client(), cases...)
}

// TestIssueTxRejectsInvalidTransaction asserts that [Client.IssueTx] surfaces
// an error from the transaction pool's verification pipeline.
func TestIssueTxRejectsInvalidTransaction(t *testing.T) {
	ctx, sut := newSUT(t)

	sk := txtest.NewKey(t) // sk is NOT funded.
	w := newWallet(sk, sut.ctx, sut.Client)
	stx := w.newMinimalTx(t)

	err := sut.IssueTx(ctx, stx)
	require.ErrorContainsf(t, err, errIssuingTx.Error(), "%T.IssueTx()", sut.Client)
}

// TestIssueTxConcurrent issues multiple [tx.Export] transactions through
// [Client.IssueTx] simultaneously.
//
// This is a regression test ensuring that the txpool does not concurrently
// access a statedb instance.
//
// This test is best run with the race detector enabled.
func TestIssueTxConcurrent(t *testing.T) {
	const numConcurrentTxs = 2

	// Each tx uses a different key so that they don't conflict.
	keys := make([]*secp256k1.PrivateKey, numConcurrentTxs)
	addrs := make([]common.Address, numConcurrentTxs)
	for i := range keys {
		keys[i] = txtest.NewKey(t)
		addrs[i] = keys[i].EthAddress()
	}
	ctx, sut := newSUT(t, withMaxAllocFor(addrs...))

	txs := make([]*tx.Tx, numConcurrentTxs)
	for i, sk := range keys {
		const (
			txFee          = 1
			exportedAmount = 1
		)
		// Export transactions are validated against the statedb, so they must
		// be used rather than Import transactions here.
		txs[i], _ = newWallet(sk, sut.ctx, sut.Client).newExportTx(
			t,
			snowtest.XChainID,
			txFee,
			txtest.NewTransferOutput(exportedAmount, sk.Address()),
		)
	}

	var (
		done sync.WaitGroup
		errs = make([]error, numConcurrentTxs)
	)
	for i, stx := range txs {
		done.Go(func() {
			errs[i] = sut.IssueTx(ctx, stx)
		})
	}
	done.Wait()

	for i, stx := range txs {
		require.NoErrorf(t, errs[i], "%T.IssueTx(txs[%d])", sut.Client, i)
		require.Truef(t, sut.pending.Has(stx.ID()), "%T.Has(txs[%d])", sut.pending, i)
	}
}

// TestGetTxNotFound asserts that [Client.GetTx] surfaces an error when the
// requested tx has never been accepted.
func TestGetTxNotFound(t *testing.T) {
	ctx, sut := newSUT(t)

	_, _, err := sut.GetTx(ctx, ids.GenerateTestID())
	require.ErrorContainsf(t, err, errFetchingTx.Error(), "%T.GetTx()", sut.Client)
}

// TestGetAtomicTxStatus exercises the deprecated avax.getAtomicTxStatus
// endpoint on both the unknown and accepted branches.
func TestGetAtomicTxStatus(t *testing.T) {
	sk := txtest.NewKey(t)
	ctx, sut := newSUT(t, withMaxAllocFor(sk.EthAddress()))

	stx := newWallet(sk, sut.ctx, sut.Client).newMinimalTx(t)
	t.Run("before_execution", func(t *testing.T) {
		got, err := sut.getTxStatus(ctx, stx.ID())
		require.NoErrorf(t, err, "%T.getTxStatus()", sut.Client)
		want := TxStatus{
			Status: choices.Unknown,
		}
		require.Equalf(t, want, got, "%T.getTxStatus()", sut.Client)
	})

	blk := sut.issueAndExecute(ctx, t, stx)
	t.Run("after_execution", func(t *testing.T) {
		got, err := sut.getTxStatus(ctx, stx.ID())
		require.NoErrorf(t, err, "%T.getTxStatus()", sut.Client)
		want := TxStatus{
			Status: choices.Accepted,
			Height: new(avajson.Uint64(blk.NumberU64())),
		}
		require.Equalf(t, want, got, "%T.getTxStatus()", sut.Client)
	})
}

// TestGetUTXOsPagination asserts that walking [Client.GetUTXOs] yields each
// seeded UTXO exactly once.
func TestGetUTXOsPagination(t *testing.T) {
	ctx, sut := newSUT(t)

	sourceChain := sut.ctx.XChainID
	const numUTXOs uint64 = 5
	want := make([]*avax.UTXO, numUTXOs)
	addr := txtest.NewKey(t).Address()
	for i := range numUTXOs {
		want[i] = txtest.NewUTXO(i+1, sut.ctx.AVAXAssetID, addr)
	}
	sut.addUTXOs(t, sut.ctx.ChainID, sourceChain, want...)

	// pageSize=1 stresses the boundary behavior so any off-by-one in the cursor
	// logic will surface here.
	const pageSize = 1
	got := sut.Client.getAllUTXOs(ctx, t, sourceChain, pageSize, addr)
	if diff := cmp.Diff(want, got, txtest.UTXOCmpOpt()); diff != "" {
		t.Errorf("paginated UTXOs (-want +got):\n%s", diff)
	}
}

// TestRPCExtras verifies that the libevm hooks correctly populate block and
// header extras.
func TestRPCExtras(t *testing.T) {
	key := txtest.NewKey(t)
	ctx, sut := newSUT(t, withMaxAllocFor(key.EthAddress()))
	w := newWallet(key, sut.ctx, sut.Client)

	// A cross-chain export gives the built block non-empty extData, so
	// extDataHash and blockExtraData carry meaningful (non-default) values.
	blk := sut.issueAndExecute(ctx, t, w.newMinimalTx(t))

	var (
		blockNumber = hexutil.EncodeUint64(blk.NumberU64())
		eth         = blk.EthBlock()
		blockHash   = eth.Hash()
		extra       = customtypes.GetHeaderExtra(blk.Header())
	)
	require.NotNilf(t, extra.ExtDataGasUsed, "%T.ExtDataGasUsed", extra)
	require.NotNilf(t, extra.BlockGasCost, "%T.BlockGasCost", extra)
	require.NotNilf(t, extra.TimeMilliseconds, "%T.TimeMilliseconds", extra)
	require.NotNilf(t, extra.MinDelayExcess, "%T.MinDelayExcess", extra)
	require.NotNilf(t, extra.TargetExponent, "%T.TargetExponent", extra)
	require.NotNilf(t, extra.MinPriceExponent, "%T.MinPriceExponent", extra)
	require.NotNilf(t, extra.SettledHeight, "%T.SettledHeight", extra)
	require.NotNilf(t, extra.SettledGasUnix, "%T.SettledGasUnix", extra)
	require.NotNilf(t, extra.SettledGasNumerator, "%T.SettledGasNumerator", extra)
	require.NotNilf(t, extra.SettledExcess, "%T.SettledExcess", extra)
	wantHeaderExtras := map[string]any{
		"extDataHash":           extra.ExtDataHash.Hex(),
		"extDataGasUsed":        hexutil.EncodeBig(extra.ExtDataGasUsed),
		"blockGasCost":          hexutil.EncodeBig(extra.BlockGasCost),
		"timestampMilliseconds": hexutil.EncodeUint64(*extra.TimeMilliseconds),
		"minDelayExcess":        hexutil.EncodeUint64(uint64(*extra.MinDelayExcess)),
		"targetExponent":        hexutil.EncodeUint64(uint64(*extra.TargetExponent)),
		"minPriceExponent":      hexutil.EncodeUint64(uint64(*extra.MinPriceExponent)),
		"settledHeight":         hexutil.EncodeUint64(*extra.SettledHeight),
		"settledGasUnix":        hexutil.EncodeUint64(*extra.SettledGasUnix),
		"settledGasNumerator":   hexutil.EncodeUint64(*extra.SettledGasNumerator),
		"settledExcess":         hexutil.EncodeUint64(*extra.SettledExcess),
	}
	numHeaderExtras := reflect.TypeFor[customtypes.HeaderExtra]().NumField()
	require.Lenf(t, wantHeaderExtras, numHeaderExtras, "%T field count", customtypes.HeaderExtra{})

	var (
		wantBlockExtras = maps.Clone(wantHeaderExtras)
		extData         = customtypes.BlockExtData(eth)
	)
	wantBlockExtras["blockExtraData"] = hexutil.Encode(extData)

	sut.testRPC(ctx, t, rpctest.WithCmpOpts(
		[]rpctest.Case{
			{
				Method: "eth_getHeaderByNumber",
				Args:   []any{blockNumber},
				Want:   wantHeaderExtras,
			},
			{
				Method: "eth_getHeaderByHash",
				Args:   []any{blockHash},
				Want:   wantHeaderExtras,
			},
		},
		onlyKeysOf(wantHeaderExtras),
	)...)
	sut.testRPC(ctx, t, rpctest.WithCmpOpts(
		[]rpctest.Case{
			{
				Method: "eth_getBlockByNumber",
				Args:   []any{blockNumber, true},
				Want:   wantBlockExtras,
			},
			{
				Method: "eth_getBlockByHash",
				Args:   []any{blockHash, true},
				Want:   wantBlockExtras,
			},
		},
		onlyKeysOf(wantBlockExtras),
	)...)
}

// onlyKeysOf restricts a comparison of JSON objects to the keys of want.
func onlyKeysOf(want map[string]any) cmp.Option {
	return cmpopts.IgnoreMapEntries(func(k string, _ any) bool {
		_, ok := want[k]
		return !ok
	})
}

// TestSynchronousRPCs replays JSON-RPC calls recorded from the synchronous VM
// and requires an identical response, covering state, receipt, log, and tracing
// RPCs at every height for every pre-SAE network upgrade.
func TestSynchronousRPCs(t *testing.T) {
	// The fixture's keys are relative to the VM's own database rather than to
	// the base database that contains it.
	fixture := synchronoustest.Load(t)
	db := memdb.New()
	fixture.WriteDatabase(t, prefixdb.New(chainDBPrefix, db))

	ctx, sut := newSUT(t,
		withDB(db),
		withGenesis(fixture.CoreGenesis(t)),
		withUpgrades(fixture.Upgrades),
		// The fixture was generated without pruning, which marked the database
		// to refuse later pruning runs.
		withArchival(),
	)

	cases := make([]rpctest.Case, len(fixture.RPCCalls))
	for i, call := range fixture.RPCCalls {
		cases[i] = rpctest.Case{
			Name:     call.Name,
			Method:   call.Method,
			Args:     call.Args(),
			Want:     call.Result,
			WantErr:  errorMessage(call.Error),
			Parallel: true,
		}
	}
	sut.testRPC(ctx, t, rpctest.WithCmpOpts(cases, jsonContent())...)

	// We test block lookups separately because SAE decided not to support
	// totalDifficulty and always report 0.
	//
	// TODO: Once libevm is updated to remove totalDifficulty, we can remove
	// this special case and test block lookups like any other RPC.
	opts := cmp.Options{
		cmputils.Blocks(),
		cmputils.Headers(),
		cmpopts.EquateEmpty(),
	}
	for _, block := range fixture.Blocks {
		t.Run(fmt.Sprintf("block_%02d_%s", block.Number, block.Fork), func(t *testing.T) {
			t.Parallel()

			t.Logf("%s", block.Description)
			want := block.EthBlock(t)

			byNumber, err := sut.ethclient.BlockByNumber(ctx, new(big.Int).SetUint64(block.Number))
			require.NoErrorf(t, err, "BlockByNumber(%d)", block.Number)
			if diff := cmp.Diff(want, byNumber, opts); diff != "" {
				t.Errorf("BlockByNumber(%d) diff (-want +got):\n%s", block.Number, diff)
			}

			byHash, err := sut.ethclient.BlockByHash(ctx, block.Hash)
			require.NoErrorf(t, err, "BlockByHash(%s)", block.Hash)
			if diff := cmp.Diff(want, byHash, opts); diff != "" {
				t.Errorf("BlockByHash(%s) diff (-want +got):\n%s", block.Hash, diff)
			}
		})
	}
}

// jsonContent compares JSON-RPC results by content rather than by encoding.
// Numbers are preserved as [json.Number] to avoid precision loss.
func jsonContent() cmp.Option {
	return cmp.Transformer("decodeJSON", func(raw json.RawMessage) any {
		if len(raw) == 0 {
			return nil
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			return fmt.Sprintf("undecodable JSON %s: %v", raw, err)
		}
		return v
	})
}

// errorMessage wants an error whose message is exactly msg, or no error if msg
// is empty.
func errorMessage(msg string) testerr.Want {
	if msg == "" {
		return nil
	}
	return testerr.Func(func(got error) string {
		if got != nil && got.Error() == msg {
			return ""
		}
		return fmt.Sprintf("error with message %q", msg)
	})
}
