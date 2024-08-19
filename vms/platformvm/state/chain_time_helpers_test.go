// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"

	feecomponent "github.com/ava-labs/avalanchego/vms/components/fee"
	txfee "github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
)

func TestPickFeeCalculator(t *testing.T) {
	var (
		createAssetTxFee uint64 = 9
		staticFeeConfig         = txfee.StaticConfig{
			TxFee:                         1,
			CreateSubnetTxFee:             2,
			TransformSubnetTxFee:          3,
			CreateBlockchainTxFee:         4,
			AddPrimaryNetworkValidatorFee: 5,
			AddPrimaryNetworkDelegatorFee: 6,
			AddSubnetValidatorFee:         7,
			AddSubnetDelegatorFee:         8,
		}
		dynamicFeeConfig = feecomponent.Config{
			Weights:                  feecomponent.Dimensions{1},
			MaxGasCapacity:           2,
			MaxGasPerSecond:          3,
			TargetGasPerSecond:       4,
			MinGasPrice:              5,
			ExcessConversionConstant: 6,
		}
	)

	apricotPhase2StaticFeeConfig := staticFeeConfig
	apricotPhase2StaticFeeConfig.CreateSubnetTxFee = createAssetTxFee
	apricotPhase2StaticFeeConfig.CreateBlockchainTxFee = createAssetTxFee

	tests := []struct {
		fork     upgradetest.Fork
		expected txfee.Calculator
	}{
		{
			fork:     upgradetest.ApricotPhase2,
			expected: txfee.NewStaticCalculator(apricotPhase2StaticFeeConfig),
		},
		{
			fork:     upgradetest.ApricotPhase3,
			expected: txfee.NewStaticCalculator(staticFeeConfig),
		},
		{
			fork: upgradetest.Etna,
			expected: txfee.NewDynamicCalculator(
				dynamicFeeConfig.Weights,
				dynamicFeeConfig.MinGasPrice,
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
