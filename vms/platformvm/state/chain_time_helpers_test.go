// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"

	feecomponent "github.com/ava-labs/avalanchego/vms/components/fee"
	txfee "github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
)

func TestPickFeeCalculator(t *testing.T) {
	var (
		createAssetTxFee uint64 = 0
		dynamicFeeConfig        = feecomponent.Config{}
		staticFeeConfig         = txfee.StaticConfig{}
	)

	apricotPhase2StaticFeeConfig := staticFeeConfig
	apricotPhase2StaticFeeConfig.CreateSubnetTxFee = createAssetTxFee
	apricotPhase2StaticFeeConfig.CreateBlockchainTxFee = createAssetTxFee

	tests := []struct {
		name     string
		fork     upgradetest.Fork
		expected txfee.Calculator
	}{
		{
			name:     "apricot phase 2",
			fork:     upgradetest.ApricotPhase2,
			expected: txfee.NewStaticCalculator(apricotPhase2StaticFeeConfig),
		},
		{
			name:     "apricot phase 3",
			fork:     upgradetest.ApricotPhase3,
			expected: txfee.NewStaticCalculator(staticFeeConfig),
		},
		// {
		// 	name: "etna",
		// 	fork: upgradetest.Etna,
		// },
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			var (
				config = &config.Config{
					CreateAssetTxFee: createAssetTxFee,
					StaticFeeConfig:  staticFeeConfig,
					DynamicFeeConfig: dynamicFeeConfig,
					UpgradeConfig:    upgradetest.GetConfig(test.fork),
				}
				s = newInitializedState(require)
			)
			actual := PickFeeCalculator(config, s)
			require.Equal(test.expected, actual)
		})
	}
}
