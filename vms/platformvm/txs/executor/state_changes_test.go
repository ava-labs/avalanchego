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

	const (
		secondsToAdvance  = 3
		durationToAdvance = secondsToAdvance * time.Second
	)

	var (
		currentTime = s.GetTimestamp()
		nextTime    = currentTime.Add(durationToAdvance)
		feeConfig   = fee.Config{
			MaxGasCapacity:     1000,
			MaxGasPerSecond:    100,
			TargetGasPerSecond: 50,
		}
	)

	// Ensure the invariant that [nextTime <= nextStakerChangeTime] on
	// AdvanceTimeTo is maintained.
	nextStakerChangeTime, err := state.GetNextStakerChangeTime(s)
	require.NoError(t, err)
	require.False(t, nextTime.After(nextStakerChangeTime))

	tests := []struct {
		name          string
		fork          upgradetest.Fork
		initialState  fee.State
		expectedState fee.State
	}{
		{
			name:          "Pre-Etna",
			fork:          upgradetest.Durango,
			initialState:  fee.State{},
			expectedState: fee.State{}, // Pre-Etna, fee state should not change
		},
		{
			name: "Etna with no usage",
			initialState: fee.State{
				Capacity: feeConfig.MaxGasCapacity,
				Excess:   0,
			},
			expectedState: fee.State{
				Capacity: feeConfig.MaxGasCapacity,
				Excess:   0,
			},
		},
		{
			name: "Etna with usage",
			fork: upgradetest.Etna,
			initialState: fee.State{
				Capacity: 1,
				Excess:   10_000,
			},
			expectedState: fee.State{
				Capacity: min(fee.Gas(1).AddPerSecond(feeConfig.MaxGasPerSecond, secondsToAdvance), feeConfig.MaxGasCapacity),
				Excess:   fee.Gas(10_000).SubPerSecond(feeConfig.TargetGasPerSecond, secondsToAdvance),
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
						DynamicFeeConfig: feeConfig,
						UpgradeConfig:    upgradetest.GetConfig(test.fork),
					},
				},
				modifiedState,
				nextTime,
			)
			require.NoError(err)
			require.False(validatorsModified)
			require.Equal(test.expectedState, modifiedState.GetFeeState())
			require.Equal(nextTime, modifiedState.GetTimestamp())
		})
	}
}
