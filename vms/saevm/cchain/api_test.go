// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
)

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
