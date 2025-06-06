// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"time"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/load"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
)

func TestRandomAction(
	tc tests.TestContext,
	wsURI string,
	privKey *secp256k1.PrivateKey,
) {
	require := require.New(tc)
	ctx := context.Background()

	client, err := ethclient.Dial(wsURI)
	require.NoError(err)

	issuer, err := NewIssuer(ctx, client, 0, privKey.ToECDSA())
	require.NoError(err)

	sender := load.NewSender(client)

	tc.By(
		"sending a random tx",
		func() {
			require.NoError(sender.SendTx(
				ctx,
				issuer,
				500*time.Millisecond,
				func(time.Duration) {},
				func(*types.Receipt, time.Duration) {}),
			)
		},
	)
}
