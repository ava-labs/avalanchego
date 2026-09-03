// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/statetest"
)

func TestGetMaxWeight(t *testing.T) {
	s := statetest.New(t, statetest.Config{})
	nodeID := genesistest.DefaultNodeIDs[0]

	tests := []struct {
		description string
		startTime   time.Time
		endTime     time.Time
	}{
		{
			description: "[validator.StartTime] == [startTime] < [endTime] == [validator.EndTime]",
			startTime:   genesistest.DefaultValidatorStartTime,
			endTime:     genesistest.DefaultValidatorEndTime,
		},
		{
			description: "[validator.StartTime] < [startTime] < [endTime] == [validator.EndTime]",
			startTime:   genesistest.DefaultValidatorStartTime.Add(time.Minute),
			endTime:     genesistest.DefaultValidatorEndTime,
		},
		{
			description: "[validator.StartTime] == [startTime] < [endTime] < [validator.EndTime]",
			startTime:   genesistest.DefaultValidatorStartTime,
			endTime:     genesistest.DefaultValidatorEndTime.Add(-time.Minute),
		},
		{
			description: "[validator.StartTime] < [startTime] < [endTime] < [validator.EndTime]",
			startTime:   genesistest.DefaultValidatorStartTime.Add(time.Minute),
			endTime:     genesistest.DefaultValidatorEndTime.Add(-time.Minute),
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			require := require.New(t)
			validator, _, err := getValidatorPeriod(s, constants.PrimaryNetworkID, nodeID)
			require.NoError(err)

			amount, err := getMaxWeight(
				s,
				validator.SubnetID(),
				validator.NodeID(),
				validator.Weight(),
				test.startTime,
				test.endTime,
			)
			require.NoError(err)
			require.Equal(genesistest.DefaultValidatorWeight, amount)
		})
	}
}
