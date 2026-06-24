// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"
	"maps"
	"reflect"
	"testing"

	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
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
		// This termination condition matches the initial API behavior from
		// coreth. Changing the expected termination condition could
		// accidentally break legacy users.
		if uint64(len(page)) < uint64(limit) {
			return utxos
		}
		startAddr, startUTXOID = endAddr, endUTXOID
	}
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
	ctx, sut := newSUT(t, options.Func[sutConfig](func(c *sutConfig) {
		c.genesis.Alloc = saetest.MaxAllocFor(sk.EthAddress())
	}))

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
			Height: utils.PointerTo(json.Uint64(blk.NumberU64())),
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
	require.NotNilf(t, extra.MinPriceExponent, "%T.MinPriceExponent", extra)
	wantHeaderExtras := map[string]string{
		"extDataHash":           extra.ExtDataHash.Hex(),
		"extDataGasUsed":        hexutil.EncodeBig(extra.ExtDataGasUsed),
		"blockGasCost":          hexutil.EncodeBig(extra.BlockGasCost),
		"timestampMilliseconds": hexutil.EncodeUint64(*extra.TimeMilliseconds),
		"minDelayExcess":        hexutil.EncodeUint64(uint64(*extra.MinDelayExcess)),
		"minPriceExponent":      hexutil.EncodeUint64(uint64(*extra.MinPriceExponent)),
	}
	numHeaderExtras := reflect.TypeFor[customtypes.HeaderExtra]().NumField()
	require.Lenf(t, wantHeaderExtras, numHeaderExtras, "%T field count", customtypes.HeaderExtra{})

	var (
		wantBlockExtras = maps.Clone(wantHeaderExtras)
		extData         = customtypes.BlockExtData(eth)
	)
	wantBlockExtras["blockExtraData"] = hexutil.Encode(extData)

	tests := []struct {
		method string
		args   []any
		want   map[string]string
	}{
		{
			method: "eth_getHeaderByNumber",
			args:   []any{blockNumber},
			want:   wantHeaderExtras,
		},
		{
			method: "eth_getHeaderByHash",
			args:   []any{blockHash},
			want:   wantHeaderExtras,
		},
		{
			method: "eth_getBlockByNumber",
			args:   []any{blockNumber, true},
			want:   wantBlockExtras,
		},
		{
			method: "eth_getBlockByHash",
			args:   []any{blockHash, true},
			want:   wantBlockExtras,
		},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			client := sut.ethclient.Client()
			var got map[string]any
			err := client.CallContext(ctx, &got, tt.method, tt.args...)
			require.NoErrorf(t, err, "%s(%v)", tt.method, tt.args)
			for k, want := range tt.want {
				assert.Equalf(t, want, got[k], "field %q", k)
			}
		})
	}
}
