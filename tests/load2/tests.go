// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load2

import (
	"context"
	"math/big"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/params"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests"
)

var _ Test = (*ZeroTransferTest)(nil)

type ZeroTransferTest struct {
	PollFrequency time.Duration
}

func (z ZeroTransferTest) Run(
	tc tests.TestContext,
	ctx context.Context,
	wallet *Wallet,
) {
	require := require.New(tc)

	maxValue := int64(100 * 1_000_000_000 / params.TxGas)
	maxFeeCap := big.NewInt(maxValue)
	bigGwei := big.NewInt(params.GWei)
	gasTipCap := new(big.Int).Mul(bigGwei, big.NewInt(1))
	gasFeeCap := new(big.Int).Mul(bigGwei, maxFeeCap)
	senderAddress := crypto.PubkeyToAddress(wallet.privKey.PublicKey)
	tx, err := types.SignNewTx(wallet.privKey, wallet.signer, &types.DynamicFeeTx{
		ChainID:   wallet.chainID,
		Nonce:     wallet.nonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       params.TxGas,
		To:        &senderAddress,
		Data:      nil,
		Value:     common.Big0,
	})
	require.NoError(err)

	require.NoError(wallet.SendTx(ctx, tx, z.PollFrequency))
}
