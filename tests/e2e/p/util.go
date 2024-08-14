// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"github.com/mitchellh/mapstructure"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/admin"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
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

// TODO(marun) Enable GetConfig to return *node.Config directly. Currently, due
// to a circular dependency issue, a map-based equivalent is used for which
// manual unmarshaling is required.
func getRewardConfig(tc tests.TestContext, client admin.Client) reward.Config {
	require := require.New(tc)

	rawNodeConfigMap, err := client.GetConfig(tc.DefaultContext())
	require.NoError(err)
	nodeConfigMap, ok := rawNodeConfigMap.(map[string]interface{})
	require.True(ok)
	stakingConfigMap, ok := nodeConfigMap["stakingConfig"].(map[string]interface{})
	require.True(ok)

	var rewardConfig reward.Config
	require.NoError(mapstructure.Decode(
		stakingConfigMap["rewardConfig"],
		&rewardConfig,
	))
	return rewardConfig
}
