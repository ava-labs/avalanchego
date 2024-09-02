// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/statetest"
)

func TestAdvanceTimeTo_UpdatesFeeState(t *testing.T) {
	const (
		secondsToAdvance  = 3
		durationToAdvance = secondsToAdvance * time.Second
	)

	feeConfig := gas.Config{
		MaxCapacity:     1000,
		MaxPerSecond:    100,
		TargetPerSecond: 50,
	}

	tests := []struct {
		name          string
		fork          upgradetest.Fork
		initialState  gas.State
		expectedState gas.State
	}{
		{
			name:          "Pre-Etna",
			fork:          upgradetest.Durango,
			initialState:  gas.State{},
			expectedState: gas.State{}, // Pre-Etna, fee state should not change
		},
		{
			name: "Etna with no usage",
			initialState: gas.State{
				Capacity: feeConfig.MaxCapacity,
				Excess:   0,
			},
			expectedState: gas.State{
				Capacity: feeConfig.MaxCapacity,
				Excess:   0,
			},
		},
		{
			name: "Etna with usage",
			fork: upgradetest.Etna,
			initialState: gas.State{
				Capacity: 1,
				Excess:   10_000,
			},
			expectedState: gas.State{
				Capacity: min(gas.Gas(1).AddPerSecond(feeConfig.MaxPerSecond, secondsToAdvance), feeConfig.MaxCapacity),
				Excess:   gas.Gas(10_000).SubPerSecond(feeConfig.TargetPerSecond, secondsToAdvance),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var (
				require = require.New(t)

				s        = statetest.New(t, statetest.Config{})
				nextTime = s.GetTimestamp().Add(durationToAdvance)
			)

			// Ensure the invariant that [nextTime <= nextStakerChangeTime] on
			// AdvanceTimeTo is maintained.
			nextStakerChangeTime, err := state.GetNextStakerChangeTime(s)
			require.NoError(err)
			require.False(nextTime.After(nextStakerChangeTime))

			s.SetFeeState(test.initialState)

			validatorsModified, err := AdvanceTimeTo(
				&Backend{
					Config: &config.Config{
						DynamicFeeConfig: feeConfig,
						UpgradeConfig:    upgradetest.GetConfig(test.fork),
					},
				},
				s,
				nextTime,
			)
			require.NoError(err)
			require.False(validatorsModified)
			require.Equal(test.expectedState, s.GetFeeState())
			require.Equal(nextTime, s.GetTimestamp())
		})
	}
}
