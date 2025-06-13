// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load2

import (
	"context"
	"math/big"
	"time"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/load/c/contracts"
)

func TestRandomTx(
	tc tests.TestContext,
	wsURI string,
	wallet Wallet,
) {
	r := require.New(tc)
	ctx := context.Background()
	backend := wallet.Backend()

	client, err := ethclient.Dial(wsURI)
	r.NoError(err)

	var contractInstance *contracts.EVMLoadSimulator

	tc.By("deploying contract", func() {
		maxFeeCap := big.NewInt(300000000000)
		txOpts, err := NewTxOpts(
			backend.PrivKey(),
			backend.ChainID(),
			maxFeeCap,
			backend.Nonce(),
		)
		r.NoError(err)

		_, tx, _, err := contracts.DeployEVMLoadSimulator(txOpts, client)
		r.NoError(err)

		r.NoError(wallet.SendTx(
			ctx,
			tx,
			500*time.Millisecond,
			func(time.Duration) {},
			func(receipt *types.Receipt, _ time.Duration) {
				contractInstance, err = contracts.NewEVMLoadSimulator(
					receipt.ContractAddress,
					client,
				)
				r.NoError(err)
			},
		))
	})

	tc.By("sending random tx", func() {
		tx, err := BuildRandomTx(backend, contractInstance)
		r.NoError(err)

		r.NoError(wallet.SendTx(
			ctx,
			tx,
			500*time.Millisecond,
			func(time.Duration) {},
			func(*types.Receipt, time.Duration) {},
		))
	})
}
