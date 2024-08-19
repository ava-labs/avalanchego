// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/vms/components/fee"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/statetest"
)

func TestAdvanceTimeTo_UpdatesFeeState(t *testing.T) {
	s := statetest.New(t, memdb.New())
	nextStakerChangeTime, err := state.GetNextStakerChangeTime(s)
	require.NoError(t, err)

	currentTime := s.GetTimestamp()
	durationToAdvance := nextStakerChangeTime.Sub(currentTime)
	secondsToAdvance := uint64(durationToAdvance / time.Second)

	tests := []struct {
		name          string
		fork          upgradetest.Fork
		config        fee.Config
		initialState  fee.State
		expectedState fee.State
	}{
		{
			name: "Pre-Etna",
			fork: upgradetest.Durango,
			config: fee.Config{
				MaxGasCapacity:     1000,
				MaxGasPerSecond:    100,
				TargetGasPerSecond: 50,
			},
			initialState:  fee.State{},
			expectedState: fee.State{},
		},
		{
			name: "Etna with no usage",
			fork: upgradetest.Etna,
			config: fee.Config{
				MaxGasCapacity:     1000,
				MaxGasPerSecond:    100,
				TargetGasPerSecond: 50,
			},
			initialState: fee.State{
				Capacity: 1000,
				Excess:   0,
			},
			expectedState: fee.State{
				Capacity: 1000,
				Excess:   0,
			},
		},
		{
			name: "Etna with usage",
			fork: upgradetest.Etna,
			config: fee.Config{
				MaxGasCapacity:     1000,
				MaxGasPerSecond:    100,
				TargetGasPerSecond: 50,
			},
			initialState: fee.State{
				Capacity: 600,
				Excess:   400,
			},
			expectedState: fee.State{
				Capacity: min(fee.Gas(600).AddPerSecond(100, secondsToAdvance), 1000),
				Excess:   fee.Gas(400).SubPerSecond(50, secondsToAdvance),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			modifiedState, err := state.NewDiffOn(s)
			require.NoError(err)

			modifiedState.SetFeeState(test.initialState)

			validatorsModified, err := AdvanceTimeTo(
				&Backend{
					Config: &config.Config{
						DynamicFeeConfig: test.config,
						UpgradeConfig:    upgradetest.GetConfig(test.fork),
					},
				},
				modifiedState,
				nextStakerChangeTime,
			)
			require.NoError(err)
			require.False(validatorsModified)

			feeState := modifiedState.GetFeeState()
			require.Equal(test.expectedState, feeState)
		})
	}
}
