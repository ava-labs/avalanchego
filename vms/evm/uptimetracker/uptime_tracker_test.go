// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptimetracker

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/utils/set"
)

func TestUptimeTracker_GetUptime(t *testing.T) {
	tests := []struct {
		name string
		// invariant: arrays must all be of the same length
		timestamps             []time.Time
		validators             [][]validators.GetCurrentValidatorOutput
		connectedValidators    [][]ids.NodeID
		disconnectedValidators [][]ids.NodeID

		validationID ids.ID

		wantUptime time.Duration
		wantLastUpdated time.Time
		wantOk bool
	}{
		{
			name:    "no validators",
			wantOk: false,
		},
		{
			name: "one validator",
			timestamps: []time.Time{
				time.Time{}.Add(10 * time.Second),
			},
			connectedValidators: [][]ids.NodeID{
				{ids.NodeID{1}},
			},
			disconnectedValidators: [][]ids.NodeID{
				{},
			},
			validators: [][]validators.GetCurrentValidatorOutput{
				{
					{
						ValidationID: ids.ID{1},
						NodeID:       ids.NodeID{2},
						StartTime:    uint64(time.Time{}.Unix()),
						IsActive:     true,
					},
				},
			},
			validationID: ids.ID{1},
			wantUptime:      10 * time.Second,
			wantLastUpdated: time.Time{}.Add(10 * time.Second),
			wantOk:          true,
		},
		{
			name: "one validator added and removed",
			timestamps: []time.Time{
				time.Time{}.Add(10 * time.Second),
				time.Time{}.Add(20 * time.Second),
			},
			connectedValidators: [][]ids.NodeID{
				{ids.NodeID{1}},
				{},
			},
			disconnectedValidators: [][]ids.NodeID{
				{},
				{},
			},
			validators: [][]validators.GetCurrentValidatorOutput{
				{
					{
						ValidationID: ids.ID{1},
						NodeID:       ids.NodeID{2},
						StartTime:    10,
						IsActive:     true,
					},
				},
				{},
			},
			validationID: ids.ID{1},
		},
		{
			name: "one validator deactivated",
			timestamps: []time.Time{
				{},
				time.Time{}.Add(10 * time.Second),
			},
			connectedValidators: [][]ids.NodeID{
				{ids.NodeID{1}},
				{},
			},
			disconnectedValidators: [][]ids.NodeID{
				{},
				{},
			},
			validators: [][]validators.GetCurrentValidatorOutput{
				{
					{
						ValidationID: ids.ID{1},
						NodeID:       ids.NodeID{1},
						StartTime:    uint64(time.Time{}.Unix()),
						IsActive:     true,
					},
				},
				{
					{
						ValidationID: ids.ID{1},
						NodeID:       ids.NodeID{1},
						StartTime:    uint64(time.Time{}.Unix()),
						IsActive:     false,
					},
				},
			},
			validationID: ids.ID{1},
			wantUptime:   10 * time.Second,
			wantLastUpdated: time.Time{}.Add(10 * time.Second),
			wantOk:          true,
		},
		{
			name: "one validator deactivated and reactivated",
			timestamps: []time.Time{
				{},
				time.Time{}.Add(10 * time.Second),
				time.Time{}.Add(20 * time.Second),
			},
			connectedValidators: [][]ids.NodeID{
				{ids.NodeID{1}},
				{},
				{},
			},
			disconnectedValidators: [][]ids.NodeID{
				{},
				{},
				{},
			},
			validators: [][]validators.GetCurrentValidatorOutput{
				{
					{
						ValidationID: ids.ID{1},
						NodeID:       ids.NodeID{1},
						StartTime:    uint64(time.Time{}.Unix()),
						IsActive:     true,
					},
				},
				{
					{
						ValidationID: ids.ID{1},
						NodeID:       ids.NodeID{1},
						StartTime:    uint64(time.Time{}.Unix()),
						IsActive:     false,
					},
				},
				{
					{
						ValidationID: ids.ID{1},
						NodeID:       ids.NodeID{1},
						StartTime:    uint64(time.Time{}.Unix()),
						IsActive:     true,
					},
				},
			},
			validationID: ids.ID{1},
			wantUptime:   10 * time.Second,
			wantLastUpdated: time.Time{}.Add(20 * time.Second),
			wantOk:          true,
		},
		{
			name: "one validator disconnected",
			timestamps: []time.Time{
				{},
				time.Time{}.Add(10 * time.Second),
			},
			connectedValidators: [][]ids.NodeID{
				{ids.NodeID{1}},
				{},
			},
			disconnectedValidators: [][]ids.NodeID{
				{},
				{ids.NodeID{1}},
			},
			validators: [][]validators.GetCurrentValidatorOutput{
				{
					{
						ValidationID: ids.ID{1},
						NodeID:       ids.NodeID{1},
						StartTime:    uint64(time.Time{}.Unix()),
						IsActive:     true,
					},
				},
				{
					{
						ValidationID: ids.ID{1},
						NodeID:       ids.NodeID{1},
						StartTime:    uint64(time.Time{}.Unix()),
						IsActive:     true,
					},
				},
			},
			validationID: ids.ID{1},
			wantUptime:   10 * time.Second,
			wantLastUpdated: time.Time{}.Add(10 * time.Second),
			wantOk:          true,
		},
		{
			name: "one validator connected and disconnected",
			timestamps: []time.Time{
				{},
				time.Time{}.Add(10 * time.Second),
				time.Time{}.Add(20 * time.Second),
			},
			connectedValidators: [][]ids.NodeID{
				{ids.NodeID{1}},
				{},
				{},
			},
			disconnectedValidators: [][]ids.NodeID{
				{},
				{ids.NodeID{1}},
				{},
			},
			validators: [][]validators.GetCurrentValidatorOutput{
				{
					{
						ValidationID: ids.ID{1},
						NodeID:       ids.NodeID{1},
						StartTime:    uint64(time.Time{}.Unix()),
						IsActive:     true,
					},
				},
				{
					{
						ValidationID: ids.ID{1},
						NodeID:       ids.NodeID{1},
						StartTime:    uint64(time.Time{}.Unix()),
						IsActive:     true,
					},
				},
				{
					{
						ValidationID: ids.ID{1},
						NodeID:       ids.NodeID{1},
						StartTime:    uint64(time.Time{}.Unix()),
						IsActive:     true,
					},
				},
			},
			validationID: ids.ID{1},
			wantUptime:   10 * time.Second,
			wantLastUpdated: time.Time{}.Add(20 * time.Second),
			wantOk:          true,
		},
		{
			name: "validator never connected",
			timestamps: []time.Time{
				{},
			},
			connectedValidators: [][]ids.NodeID{
				{},
			},
			disconnectedValidators: [][]ids.NodeID{
				{},
			},
			validators: [][]validators.GetCurrentValidatorOutput{
				{
					{
						ValidationID: ids.ID{1},
						NodeID:       ids.NodeID{1},
						StartTime:    uint64(time.Time{}.Unix()),
						IsActive:     true,
					},
				},
			},
			validationID: ids.ID{1},
			wantOk: true,
		},
		{
			name: "validator removed",
			timestamps: []time.Time{
				{},
				time.Time{}.Add(10 * time.Second),
			},
			connectedValidators: [][]ids.NodeID{
				{ids.NodeID{1}},
				{},
			},
			disconnectedValidators: [][]ids.NodeID{
				{},
				{},
			},
			validators: [][]validators.GetCurrentValidatorOutput{
				{
					{
						ValidationID: ids.ID{1},
						NodeID:       ids.NodeID{1},
						StartTime:    uint64(time.Time{}.Unix()),
						IsActive:     true,
					},
				},
				{
					{},
				},
			},
			validationID: ids.ID{1},
		},
		{
			name: "connected inactive validator becomes active",
			timestamps: []time.Time{
				{},
				time.Time{}.Add(10 * time.Second),
			},
			connectedValidators: [][]ids.NodeID{
				{ids.NodeID{1}},
				{},
			},
			disconnectedValidators: [][]ids.NodeID{
				{},
				{},
			},
			validators: [][]validators.GetCurrentValidatorOutput{
				{
					{
						ValidationID: ids.ID{1},
						NodeID:       ids.NodeID{1},
						StartTime:    uint64(time.Time{}.Unix()),
					},
				},
				{
					{
						ValidationID: ids.ID{1},
						NodeID:       ids.NodeID{1},
						StartTime:    uint64(time.Time{}.Unix()),
						IsActive:     true,
					},
				},
			},
			validationID: ids.ID{1},
			wantLastUpdated: time.Time{}.Add(10 * time.Second),
			wantOk: true,
		},
		{
			name: "disconnected inactive validator becomes connected + active",
			timestamps: []time.Time{
				{},
				time.Time{}.Add(10 * time.Second),
			},
			connectedValidators: [][]ids.NodeID{
				{},
				{ids.NodeID{1}},
			},
			disconnectedValidators: [][]ids.NodeID{
				{},
				{},
			},
			validators: [][]validators.GetCurrentValidatorOutput{
				{
					{
						ValidationID: ids.ID{1},
						NodeID:       ids.NodeID{1},
						StartTime:    uint64(time.Time{}.Unix()),
					},
				},
				{
					{
						ValidationID: ids.ID{1},
						NodeID:       ids.NodeID{1},
						StartTime:    uint64(time.Time{}.Unix()),
						IsActive:     true,
					},
				},
			},
			validationID: ids.ID{1},
			wantLastUpdated: time.Time{}.Add(10 * time.Second),
			wantOk: true,
		},
		{
			name: "deactivated validator leaves validator set",
			timestamps: []time.Time{
				{},
				time.Time{}.Add(10 * time.Second),
			},
			connectedValidators: [][]ids.NodeID{
				{ids.NodeID{1}},
				{},
			},
			disconnectedValidators: [][]ids.NodeID{
				{},
				{},
			},
			validators: [][]validators.GetCurrentValidatorOutput{
				{
					{
						ValidationID: ids.ID{1},
						NodeID:       ids.NodeID{1},
						StartTime:    uint64(time.Time{}.Unix()),
					},
				},
				{},
			},
			validationID: ids.ID{1},
		},
		{
			name: "validator has no updates",
			timestamps: []time.Time{
				{},
				time.Time{}.Add(10 * time.Second),
				time.Time{}.Add(20 * time.Second),
			},
			connectedValidators: [][]ids.NodeID{
				{ids.NodeID{1}},
				{},
				{},
			},
			disconnectedValidators: [][]ids.NodeID{
				{},
				{},
				{},
			},
			validators: [][]validators.GetCurrentValidatorOutput{
				{
					{
						ValidationID: ids.ID{1},
						NodeID:       ids.NodeID{1},
						StartTime:    uint64(time.Time{}.Unix()),
						IsActive:     true,
					},
				},
				{
					{
						ValidationID: ids.ID{1},
						NodeID:       ids.NodeID{1},
						StartTime:    uint64(time.Time{}.Unix()),
						IsActive:     true,
					},
				},
				{
					{
						ValidationID: ids.ID{1},
						NodeID:       ids.NodeID{1},
						StartTime:    uint64(time.Time{}.Unix()),
						IsActive:     true,
					},
				},
			},
			validationID:    ids.ID{1},
			wantUptime:      20 * time.Second,
			wantLastUpdated: time.Time{}.Add(20 * time.Second),
			wantOk:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			require.Len(
				set.Of(
					len(tt.timestamps),
					len(tt.validators),
					len(tt.connectedValidators),
					len(tt.disconnectedValidators),
				),
				1,
			)

			validatorState := &validatorstest.State{}
			subnetID := ids.GenerateTestID()
			db := memdb.New()
			clock := &mockable.Clock{}

			uptimeTracker, err := New(validatorState, subnetID, db, clock)
			require.NoError(err)

			pChainHeight := uint64(0)
			for i, timestamp := range tt.timestamps {
				clock.Set(timestamp)

				for _, v := range tt.connectedValidators[i] {
					require.NoError(uptimeTracker.Connect(v))
				}

				for _, v := range tt.disconnectedValidators[i] {
					require.NoError(uptimeTracker.Disconnect(v))
				}


				validatorState.GetCurrentValidatorSetF = func(
					context.Context,
					ids.ID,
				) (
					map[ids.ID]*validators.GetCurrentValidatorOutput,
					uint64,
					error,
				) {
					validatorsMap := make(map[ids.ID]*validators.GetCurrentValidatorOutput)

					for _, v := range tt.validators[i] {
						validatorsMap[v.ValidationID] = &v
					}

					pChainHeight += 1
					return validatorsMap, pChainHeight, nil
				}

				require.NoError(uptimeTracker.Sync(context.Background()))
			}

			gotUptime, gotLastUpdated, ok, err := uptimeTracker.GetUptime(
				tt.validationID,
			)
			require.NoError(err)
			require.Equal(tt.wantOk, ok)
			require.Equal(tt.wantLastUpdated, gotLastUpdated)
			require.Equal(tt.wantUptime, gotUptime)

			require.NoError(uptimeTracker.Shutdown())
		})
	}
}

func TestUptimeTracker_Restart(t *testing.T) {
	require := require.New(t)

	validationID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()
	start := time.Time{}
	validatorState := &validatorstest.State{
		GetCurrentValidatorSetF: func(
			ctx context.Context,
			subnetID ids.ID,
		) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
			return map[ids.ID]*validators.GetCurrentValidatorOutput{
				validationID: {
					ValidationID: validationID,
					StartTime:    uint64(start.Unix()),
					NodeID:       nodeID,
					IsActive:     true,
				},
			}, 0, nil
		},
	}
	subnetID := ids.GenerateTestID()
	db := memdb.New()
	clock := &mockable.Clock{}
	clock.Set(start)

	uptimeTracker, err := New(validatorState, subnetID, db, clock)
	require.NoError(err)

	require.NoError(uptimeTracker.Sync(context.Background()))
	require.NoError(uptimeTracker.Connect(nodeID))

	clock.Set(start.Add(10 * time.Second))
	uptime, lastUpdated, ok, err := uptimeTracker.GetUptime(validationID)
	require.NoError(err)
	require.True(ok)
	require.Equal(10*time.Second, uptime)
	require.Equal(time.Time{}.Add(10*time.Second), lastUpdated)

	require.NoError(uptimeTracker.Shutdown())

	clock.Set(start.Add(20 * time.Second))
	uptimeTracker, err = New(validatorState, subnetID, db, clock)
	require.NoError(err)
	require.NoError(uptimeTracker.Sync(context.Background()))

	uptime, lastUpdated, ok, err = uptimeTracker.GetUptime(validationID)
	require.NoError(err)
	require.True(ok)
	require.Equal(20*time.Second, uptime)
	require.Equal(time.Time{}.Add(20*time.Second), lastUpdated)
}
