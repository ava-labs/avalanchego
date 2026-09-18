// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
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
