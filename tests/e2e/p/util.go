// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/wallet/chain/p/builder"
)

func feeCalculatorFromContext(context *builder.Context) fee.Calculator {
	if context.GasPrice != 0 {
		return fee.NewDynamicCalculator(context.ComplexityWeights, context.GasPrice)
	}
	return fee.NewStaticCalculator(context.StaticFeeConfig)
}

func newKey(tc tests.TestContext) *secp256k1.PrivateKey {
	key, err := secp256k1.NewPrivateKey()
	require.NoError(tc, err)
	return key
}
