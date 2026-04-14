// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/params"
	"github.com/stretchr/testify/assert"
)

func TestImmediateReceipts(t *testing.T) {
	blocking := common.Address{'b', 'l', 'o', 'c', 'k'}
	opt, unblock := withBlockingPrecompile(blocking)
	ctx, sut := newSUT(t, 1, opt)
	t.Cleanup(unblock)

	var txs []*types.Transaction
	for _, to := range []*common.Address{&zeroAddr, &blocking} {
		txs = append(txs, sut.wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
			To:       to,
			Gas:      params.TxGas,
			GasPrice: big.NewInt(1),
		}))
	}
	notBlocked := txs[0]

	b := sut.runConsensusLoop(t, txs...)
	sut.testRPC(ctx, t, rpcTest{
		method: "eth_getTransactionReceipt",
		args:   []any{notBlocked.Hash()},
		want: &types.Receipt{
			Status:            types.ReceiptStatusSuccessful,
			EffectiveGasPrice: big.NewInt(1),
			GasUsed:           params.TxGas,
			CumulativeGasUsed: params.TxGas,
			Logs:              []*types.Log{},
			TxHash:            notBlocked.Hash(),
			BlockHash:         b.Hash(),
			BlockNumber:       b.Number(),
		},
	})
	assert.Falsef(t, b.Executed(), "%T.Executed()", b)
}
