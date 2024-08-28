// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
)

func TestPickFeeCalculator(t *testing.T) {
	var (
		createAssetTxFee = genesis.LocalParams.CreateAssetTxFee
		staticFeeConfig  = genesis.LocalParams.StaticFeeConfig
		dynamicFeeConfig = genesis.LocalParams.DynamicFeeConfig
	)

	apricotPhase2StaticFeeConfig := staticFeeConfig
	apricotPhase2StaticFeeConfig.CreateSubnetTxFee = createAssetTxFee
	apricotPhase2StaticFeeConfig.CreateBlockchainTxFee = createAssetTxFee

	tests := []struct {
		fork     upgradetest.Fork
		expected fee.Calculator
	}{
		{
			fork:     upgradetest.ApricotPhase2,
			expected: fee.NewStaticCalculator(apricotPhase2StaticFeeConfig),
		},
		{
			fork:     upgradetest.ApricotPhase3,
			expected: fee.NewStaticCalculator(staticFeeConfig),
		},
		{
			fork: upgradetest.Etna,
			expected: fee.NewDynamicCalculator(
				dynamicFeeConfig.Weights,
				dynamicFeeConfig.MinPrice,
			),
		},
	}
	for _, test := range tests {
		t.Run(test.fork.String(), func(t *testing.T) {
			var (
				config = &config.Config{
					CreateAssetTxFee: createAssetTxFee,
					StaticFeeConfig:  staticFeeConfig,
					DynamicFeeConfig: dynamicFeeConfig,
					UpgradeConfig:    upgradetest.GetConfig(test.fork),
				}
				s = newTestState(t, memdb.New())
			)
			actual := PickFeeCalculator(config, s)
			require.Equal(t, test.expected, actual)
		})
	}
}
