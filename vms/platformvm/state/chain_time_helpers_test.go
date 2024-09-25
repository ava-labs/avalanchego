// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
)

func TestNextBlockTime(t *testing.T) {
	tests := []struct {
		name           string
		chainTime      time.Time
		now            time.Time
		expectedTime   time.Time
		expectedCapped bool
	}{
		{
			name:           "parent time is after now",
			chainTime:      genesistest.DefaultValidatorStartTime,
			now:            genesistest.DefaultValidatorStartTime.Add(-time.Second),
			expectedTime:   genesistest.DefaultValidatorStartTime,
			expectedCapped: false,
		},
		{
			name:           "parent time is before now",
			chainTime:      genesistest.DefaultValidatorStartTime,
			now:            genesistest.DefaultValidatorStartTime.Add(time.Second),
			expectedTime:   genesistest.DefaultValidatorStartTime.Add(time.Second),
			expectedCapped: false,
		},
		{
			name:           "now is at next staker change time",
			chainTime:      genesistest.DefaultValidatorStartTime,
			now:            genesistest.DefaultValidatorEndTime,
			expectedTime:   genesistest.DefaultValidatorEndTime,
			expectedCapped: true,
		},
		{
			name:           "now is after next staker change time",
			chainTime:      genesistest.DefaultValidatorStartTime,
			now:            genesistest.DefaultValidatorEndTime.Add(time.Second),
			expectedTime:   genesistest.DefaultValidatorEndTime,
			expectedCapped: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var (
				require = require.New(t)
				s       = newTestState(t, memdb.New())
				clk     mockable.Clock
			)

			s.SetTimestamp(test.chainTime)
			clk.Set(test.now)

			actualTime, actualCapped, err := NextBlockTime(s, &clk)
			require.NoError(err)
			require.Equal(test.expectedTime.Local(), actualTime.Local())
			require.Equal(test.expectedCapped, actualCapped)
		})
	}
}

func TestGetNextStakerChangeTime(t *testing.T) {
	tests := []struct {
		name     string
		pending  []*Staker
		maxTime  time.Time
		expected time.Time
	}{
		{
			name:     "only current validators",
			maxTime:  mockable.MaxTime,
			expected: genesistest.DefaultValidatorEndTime,
		},
		{
			name: "current and pending validators",
			pending: []*Staker{
				{
					TxID:      ids.GenerateTestID(),
					NodeID:    ids.GenerateTestNodeID(),
					PublicKey: nil,
					SubnetID:  constants.PrimaryNetworkID,
					Weight:    1,
					StartTime: genesistest.DefaultValidatorStartTime.Add(time.Second),
					EndTime:   genesistest.DefaultValidatorEndTime,
					NextTime:  genesistest.DefaultValidatorStartTime.Add(time.Second),
					Priority:  txs.PrimaryNetworkValidatorPendingPriority,
				},
			},
			maxTime:  mockable.MaxTime,
			expected: genesistest.DefaultValidatorStartTime.Add(time.Second),
		},
		{
			name:     "restricted timestamp",
			maxTime:  genesistest.DefaultValidatorStartTime,
			expected: genesistest.DefaultValidatorStartTime,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var (
				require = require.New(t)
				s       = newTestState(t, memdb.New())
			)
			for _, staker := range test.pending {
				require.NoError(s.PutPendingValidator(staker))
			}

			actual, err := GetNextStakerChangeTime(s, test.maxTime)
			require.NoError(err)
			require.Equal(test.expected.Local(), actual.Local())
		})
	}
}

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
