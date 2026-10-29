// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

func TestGetMaxWeight(t *testing.T) {
	tests := []struct {
		name      string
		startTime time.Time
		endTime   time.Time
	}{
		{
			name:      "window_matches_the_validation_period",
			startTime: genesistest.DefaultValidatorStartTime,
			endTime:   genesistest.DefaultValidatorEndTime,
		},
		{
			name:      "window_starts_after_the_validation_period",
			startTime: genesistest.DefaultValidatorStartTime.Add(time.Minute),
			endTime:   genesistest.DefaultValidatorEndTime,
		},
		{
			name:      "window_ends_before_the_validation_period",
			startTime: genesistest.DefaultValidatorStartTime,
			endTime:   genesistest.DefaultValidatorEndTime.Add(-time.Minute),
		},
		{
			name:      "window_inside_the_validation_period",
			startTime: genesistest.DefaultValidatorStartTime.Add(time.Minute),
			endTime:   genesistest.DefaultValidatorEndTime.Add(-time.Minute),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newEnvironment(t, upgradetest.Latest)
			env.ctx.Lock.Lock()
			defer env.ctx.Lock.Unlock()

			staker, err := GetValidator(env.state, constants.PrimaryNetworkID, genesistest.DefaultNodeIDs[0])
			require.NoError(t, err)

			// The genesis validator has no delegators, so its maximum weight
			// over any window of its validation period is its own weight.
			got, err := GetMaxWeight(env.state, staker, tt.startTime, tt.endTime)
			require.NoError(t, err)
			require.Equal(t, genesistest.DefaultValidatorWeight, got)
		})
	}
}

func TestGetMaxWeightWithDelegators(t *testing.T) {
	var (
		startTime           = genesistest.DefaultValidatorStartTime
		firstDelegatorEnd   = startTime.Add(time.Hour)
		secondDelegatorEnd  = startTime.Add(2 * time.Hour)
		firstDelegatorWght  = uint64(10)
		secondDelegatorWght = uint64(20)
	)

	tests := []struct {
		name      string
		startTime time.Time
		endTime   time.Time
		want      uint64
	}{
		{
			name:      "window_ends_before_any_delegator_ends",
			startTime: startTime,
			endTime:   firstDelegatorEnd.Add(-time.Second),
			want:      genesistest.DefaultValidatorWeight + firstDelegatorWght + secondDelegatorWght,
		},
		{
			name:      "window_spans_a_delegator_end",
			startTime: startTime,
			endTime:   firstDelegatorEnd.Add(time.Second),
			want:      genesistest.DefaultValidatorWeight + firstDelegatorWght + secondDelegatorWght,
		},
		{
			name:      "window_starts_after_a_delegator_ends",
			startTime: firstDelegatorEnd.Add(time.Second),
			endTime:   secondDelegatorEnd.Add(-time.Second),
			want:      genesistest.DefaultValidatorWeight + secondDelegatorWght,
		},
		{
			name:      "window_starts_after_all_delegators_end",
			startTime: secondDelegatorEnd.Add(time.Second),
			endTime:   genesistest.DefaultValidatorEndTime,
			want:      genesistest.DefaultValidatorWeight,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			env := newEnvironment(t, upgradetest.Latest)

			nodeID := genesistest.DefaultNodeIDs[0]
			diff, err := state.NewDiffOn(env.state, state.StakerAdditionAfterDeletionForbidden)
			require.NoError(err)

			for _, delegator := range []struct {
				weight  uint64
				endTime time.Time
			}{
				{weight: firstDelegatorWght, endTime: firstDelegatorEnd},
				{weight: secondDelegatorWght, endTime: secondDelegatorEnd},
			} {
				require.NoError(diff.PutCurrentDelegator(&state.Staker{
					TxID:      ids.GenerateTestID(),
					NodeID:    nodeID,
					SubnetID:  constants.PrimaryNetworkID,
					Weight:    delegator.weight,
					StartTime: startTime,
					EndTime:   delegator.endTime,
					NextTime:  delegator.endTime,
					Priority:  platform.PrimaryNetworkDelegatorCurrentPriority,
				}))
			}

			staker, err := GetValidator(diff, constants.PrimaryNetworkID, nodeID)
			require.NoError(err)

			got, err := GetMaxWeight(diff, staker, tt.startTime, tt.endTime)
			require.NoError(err)
			require.Equal(tt.want, got)
		})
	}
}
