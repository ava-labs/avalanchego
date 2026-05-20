// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/rpc"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
)

// getAtomicTxStatus exposes the deprecated [service.GetAtomicTxStatus]
// endpoint to tests in this package only. It is intentionally not part of
// [Client]'s production surface: new code should call [Client.GetTx], which
// returns the tx and its block height in a single call. Defined here so the
// deprecated endpoint stays exercisable without inviting external use.
func (c *Client) getAtomicTxStatus(ctx context.Context, txID ids.ID, options ...rpc.Option) (TxStatus, error) {
	res := TxStatus{}
	err := c.r.SendRequest(ctx, "avax.getAtomicTxStatus", &api.JSONTxID{
		TxID: txID,
	}, &res, options...)
	return res, err
}

// TestIssueTxRejectsInvalidTransaction asserts that [Client.IssueTx] surfaces
// an error from the transaction pool's verification pipeline.
func TestIssueTxRejectsInvalidTransaction(t *testing.T) {
	sut := newSUT(t)

	sk := txtest.NewKey(t) // sk is NOT funded.
	w := newWallet(sk, sut.snowCtx, sut.Client)
	const (
		txFee          = 50
		exportedAmount = 50
	)
	tx, _ := w.newExportTx(
		t,
		sut.snowCtx.XChainID,
		txFee,
		txtest.NewTransferOutput(exportedAmount, sk.Address()),
	)

	err := sut.IssueTx(t.Context(), tx)
	require.ErrorContainsf(t, err, errIssuingTx.Error(), "%T.IssueTx()", sut.Client)
}

// TestGetTxNotFound asserts that [Client.GetTx] surfaces an error when the
// requested tx has never been accepted.
func TestGetTxNotFound(t *testing.T) {
	sut := newSUT(t)

	_, _, err := sut.GetTx(t.Context(), ids.GenerateTestID())
	require.ErrorContainsf(t, err, errFetchingTx.Error(), "%T.GetTx()", sut.Client)
}

// TestGetAtomicTxStatus exercises the deprecated avax.getAtomicTxStatus
// endpoint on both the unknown and accepted branches.
func TestGetAtomicTxStatus(t *testing.T) {
	sut := newSUT(t)

	// Unknown: a freshly-generated txID has never been seen.
	status, err := sut.getAtomicTxStatus(t.Context(), ids.GenerateTestID())
	require.NoError(t, err)
	require.Equal(t, Unknown, status.Status)
	require.Nil(t, status.Height)

	// Accepted: import a UTXO and have the resulting tx accepted in a block.
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

	status, err = sut.getAtomicTxStatus(t.Context(), signedImport.ID())
	require.NoError(t, err)
	require.Equal(t, Accepted, status.Status)
	require.NotNil(t, status.Height)
	require.Equal(t, blk.NumberU64(), uint64(*status.Height))
}

// TestGetUTXOsPagination asserts that walking [Client.GetUTXOs] yields each
// seeded UTXO exactly once.
func TestGetUTXOsPagination(t *testing.T) {
	sut := newSUT(t)

	const numUTXOs uint64 = 5
	want := make([]*avax.UTXO, numUTXOs)
	addr := txtest.NewKey(t).Address()
	for i := range numUTXOs {
		want[i] = txtest.NewUTXO(i+1, sut.snowCtx.AVAXAssetID, addr)
	}
	sut.addUTXOs(t, snowtest.XChainID, want...)

	// pageSize=1 stresses the boundary behavior so any off-by-one in the cursor
	// logic will surface here.
	const pageSize = 1
	got := getUTXOs(t, sut.Client, snowtest.XChainID, pageSize, addr)
	if diff := cmp.Diff(want, got, txtest.UTXOCmpOpt()); diff != "" {
		t.Errorf("paginated UTXOs (-want +got):\n%s", diff)
	}
}
