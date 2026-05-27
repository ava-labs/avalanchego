// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae_test

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/params"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/saevm/saevmtest"
)

func TestTxTypeSupport(t *testing.T) {
	ctx, sut := saevmtest.NewSUT(t, 1)

	var to common.Address
	txs := []types.TxData{
		&types.LegacyTx{
			To:       &to,
			Gas:      params.TxGas,
			GasPrice: big.NewInt(1),
		},
		&types.AccessListTx{
			To:       &to,
			Gas:      params.TxGas,
			GasPrice: big.NewInt(1),
		},
		&types.DynamicFeeTx{
			To:        &to,
			Gas:       params.TxGas,
			GasFeeCap: big.NewInt(1),
		},
	}

	for _, tx := range txs {
		t.Run(fmt.Sprintf("%T", tx), func(t *testing.T) {
			sut.SendTxsAndWaitUntilPending(t, sut.Wallet.SetNonceAndSign(t, 0, tx))
		})
		if t.Failed() {
			t.FailNow()
		}
	}
	b := sut.RunConsensusLoop(t)
	require.NoErrorf(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)

	sdb := sut.StateAt(t, b.PostExecutionStateRoot())
	got := sdb.GetNonce(sut.Wallet.Addresses()[0])
	require.Equal(t, uint64(len(txs)), got, "Nonce of account sending all transaction types")
}
