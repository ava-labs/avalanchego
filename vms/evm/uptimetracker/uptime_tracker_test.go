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
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

type testStep struct {
	timestamp              time.Time
	validators             []validators.GetCurrentValidatorOutput
	connectedValidators    []ids.NodeID
	disconnectedValidators []ids.NodeID
}

func TestUptimeTracker_GetUptime(t *testing.T) {
	tests := []struct {
		name  string
		steps []testStep

		validationID ids.ID

		wantUptime      time.Duration
		wantLastUpdated time.Time
		wantOk          bool
	}{
		{
			name:   "no validators",
			wantOk: false,
		},
		{
			name: "one validator",
			steps: []testStep{
				{
					timestamp: time.Time{}.Add(10 * time.Second),
					connectedValidators: []ids.NodeID{
						ids.NodeID{1},
					},
					validators: []validators.GetCurrentValidatorOutput{
						{
							ValidationID: ids.ID{1},
							NodeID:       ids.NodeID{2},
							StartTime:    uint64(time.Time{}.Unix()),
							IsActive:     true,
						},
					},
				},
			},
			validationID:    ids.ID{1},
			wantUptime:      10 * time.Second,
			wantLastUpdated: time.Time{}.Add(10 * time.Second),
			wantOk:          true,
		},
		{
			name: "one validator added and removed",
			steps: []testStep{
				{
					timestamp: time.Time{}.Add(10 * time.Second),
					connectedValidators: []ids.NodeID{
						ids.NodeID{1},
					},
					validators: []validators.GetCurrentValidatorOutput{
						{
							ValidationID: ids.ID{1},
							NodeID:       ids.NodeID{2},
							StartTime:    10,
							IsActive:     true,
						},
					},
				},
				{
					timestamp: time.Time{}.Add(20 * time.Second),
				},
			},
			validationID: ids.ID{1},
		},
		{
			name: "one validator deactivated",
			steps: []testStep{
				{
					connectedValidators: []ids.NodeID{
						ids.NodeID{1},
					},
					validators: []validators.GetCurrentValidatorOutput{
						{
							ValidationID: ids.ID{1},
							NodeID:       ids.NodeID{1},
							StartTime:    uint64(time.Time{}.Unix()),
							IsActive:     true,
						},
					},
				},
				{
					timestamp: time.Time{}.Add(10 * time.Second),
					validators: []validators.GetCurrentValidatorOutput{
						{
							ValidationID: ids.ID{1},
							NodeID:       ids.NodeID{1},
							StartTime:    uint64(time.Time{}.Unix()),
							IsActive:     false,
						},
					},
				},
			},
			validationID:    ids.ID{1},
			wantUptime:      10 * time.Second,
			wantLastUpdated: time.Time{}.Add(10 * time.Second),
			wantOk:          true,
		},
		{
			name: "one validator deactivated and reactivated",
			steps: []testStep{
				{
					connectedValidators: []ids.NodeID{
						ids.NodeID{1},
					},
					validators: []validators.GetCurrentValidatorOutput{
						{
							ValidationID: ids.ID{1},
							NodeID:       ids.NodeID{1},
							StartTime:    uint64(time.Time{}.Unix()),
							IsActive:     true,
						},
					},
				},
				{
					timestamp: time.Time{}.Add(10 * time.Second),
					validators: []validators.GetCurrentValidatorOutput{
						{
							ValidationID: ids.ID{1},
							NodeID:       ids.NodeID{1},
							StartTime:    uint64(time.Time{}.Unix()),
							IsActive:     false,
						},
					},
				},
				{
					timestamp: time.Time{}.Add(20 * time.Second),
					validators: []validators.GetCurrentValidatorOutput{
						{
							ValidationID: ids.ID{1},
							NodeID:       ids.NodeID{1},
							StartTime:    uint64(time.Time{}.Unix()),
							IsActive:     true,
						},
					},
				},
			},
			validationID:    ids.ID{1},
			wantUptime:      10 * time.Second,
			wantLastUpdated: time.Time{}.Add(20 * time.Second),
			wantOk:          true,
		},
		{
			name: "one validator disconnected",
			steps: []testStep{
				{
					connectedValidators: []ids.NodeID{
						ids.NodeID{1},
					},
					validators: []validators.GetCurrentValidatorOutput{
						{
							ValidationID: ids.ID{1},
							NodeID:       ids.NodeID{1},
							StartTime:    uint64(time.Time{}.Unix()),
							IsActive:     true,
						},
					},
				},
				{
					timestamp: time.Time{}.Add(10 * time.Second),
					disconnectedValidators: []ids.NodeID{
						ids.NodeID{1},
					},
					validators: []validators.GetCurrentValidatorOutput{
						{
							ValidationID: ids.ID{1},
							NodeID:       ids.NodeID{1},
							StartTime:    uint64(time.Time{}.Unix()),
							IsActive:     true,
						},
					},
				},
			},
			validationID:    ids.ID{1},
			wantUptime:      10 * time.Second,
			wantLastUpdated: time.Time{}.Add(10 * time.Second),
			wantOk:          true,
		},
		{
			name: "one validator connected and disconnected",
			steps: []testStep{
				{
					connectedValidators: []ids.NodeID{
						ids.NodeID{1},
					},
					validators: []validators.GetCurrentValidatorOutput{
						{
							ValidationID: ids.ID{1},
							NodeID:       ids.NodeID{1},
							StartTime:    uint64(time.Time{}.Unix()),
							IsActive:     true,
						},
					},
				},
				{
					timestamp: time.Time{}.Add(10 * time.Second),
					disconnectedValidators: []ids.NodeID{
						ids.NodeID{1},
					},
					validators: []validators.GetCurrentValidatorOutput{
						{
							ValidationID: ids.ID{1},
							NodeID:       ids.NodeID{1},
							StartTime:    uint64(time.Time{}.Unix()),
							IsActive:     true,
						},
					},
				},
				{
					timestamp: time.Time{}.Add(20 * time.Second),
					validators: []validators.GetCurrentValidatorOutput{
						{
							ValidationID: ids.ID{1},
							NodeID:       ids.NodeID{1},
							StartTime:    uint64(time.Time{}.Unix()),
							IsActive:     true,
						},
					},
				},
			},
			validationID:    ids.ID{1},
			wantUptime:      10 * time.Second,
			wantLastUpdated: time.Time{}.Add(20 * time.Second),
			wantOk:          true,
		},
		{
			name: "validator never connected",
			steps: []testStep{
				{
					validators: []validators.GetCurrentValidatorOutput{
						{
							ValidationID: ids.ID{1},
							NodeID:       ids.NodeID{1},
							StartTime:    uint64(time.Time{}.Unix()),
							IsActive:     true,
						},
					},
				},
			},
			validationID: ids.ID{1},
			wantOk:       true,
		},
		{
			name: "validator removed",
			steps: []testStep{
				{
					connectedValidators: []ids.NodeID{
						ids.NodeID{1},
					},
					validators: []validators.GetCurrentValidatorOutput{
						{
							ValidationID: ids.ID{1},
							NodeID:       ids.NodeID{1},
							StartTime:    uint64(time.Time{}.Unix()),
							IsActive:     true,
						},
					},
				},
				{
					timestamp: time.Time{}.Add(10 * time.Second),
					validators: []validators.GetCurrentValidatorOutput{
						{},
					},
				},
			},
			validationID: ids.ID{1},
		},
		{
			name: "connected inactive validator becomes active",
			steps: []testStep{
				{
					connectedValidators: []ids.NodeID{
						ids.NodeID{1},
					},
					validators: []validators.GetCurrentValidatorOutput{
						{
							ValidationID: ids.ID{1},
							NodeID:       ids.NodeID{1},
							StartTime:    uint64(time.Time{}.Unix()),
						},
					},
				},
				{
					timestamp: time.Time{}.Add(10 * time.Second),
					validators: []validators.GetCurrentValidatorOutput{
						{
							ValidationID: ids.ID{1},
							NodeID:       ids.NodeID{1},
							StartTime:    uint64(time.Time{}.Unix()),
							IsActive:     true,
						},
					},
				},
			},
			validationID:    ids.ID{1},
			wantLastUpdated: time.Time{}.Add(10 * time.Second),
			wantOk:          true,
		},
		{
			name: "disconnected inactive validator becomes connected + active",
			steps: []testStep{
				{
					validators: []validators.GetCurrentValidatorOutput{
						{
							ValidationID: ids.ID{1},
							NodeID:       ids.NodeID{1},
							StartTime:    uint64(time.Time{}.Unix()),
						},
					},
				},
				{
					timestamp: time.Time{}.Add(10 * time.Second),
					connectedValidators: []ids.NodeID{
						ids.NodeID{1},
					},
					validators: []validators.GetCurrentValidatorOutput{
						{
							ValidationID: ids.ID{1},
							NodeID:       ids.NodeID{1},
							StartTime:    uint64(time.Time{}.Unix()),
							IsActive:     true,
						},
					},
				},
			},
			validationID:    ids.ID{1},
			wantLastUpdated: time.Time{}.Add(10 * time.Second),
			wantOk:          true,
		},
		{
			name: "deactivated validator leaves validator set",
			steps: []testStep{
				{
					connectedValidators: []ids.NodeID{
						ids.NodeID{1},
					},
					validators: []validators.GetCurrentValidatorOutput{
						{
							ValidationID: ids.ID{1},
							NodeID:       ids.NodeID{1},
							StartTime:    uint64(time.Time{}.Unix()),
						},
					},
				},
				{
					timestamp: time.Time{}.Add(10 * time.Second),
				},
			},
			validationID: ids.ID{1},
		},
		{
			name: "validator has no updates",
			steps: []testStep{
				{
					connectedValidators: []ids.NodeID{
						ids.NodeID{1},
					},
					validators: []validators.GetCurrentValidatorOutput{
						{
							ValidationID: ids.ID{1},
							NodeID:       ids.NodeID{1},
							StartTime:    uint64(time.Time{}.Unix()),
							IsActive:     true,
						},
					},
				},
				{
					timestamp: time.Time{}.Add(10 * time.Second),
					validators: []validators.GetCurrentValidatorOutput{
						{
							ValidationID: ids.ID{1},
							NodeID:       ids.NodeID{1},
							StartTime:    uint64(time.Time{}.Unix()),
							IsActive:     true,
						},
					},
				},
				{
					timestamp: time.Time{}.Add(20 * time.Second),
					validators: []validators.GetCurrentValidatorOutput{
						{
							ValidationID: ids.ID{1},
							NodeID:       ids.NodeID{1},
							StartTime:    uint64(time.Time{}.Unix()),
							IsActive:     true,
						},
					},
				},
			},
			validationID:    ids.ID{1},
			wantUptime:      20 * time.Second,
			wantLastUpdated: time.Time{}.Add(20 * time.Second),
			wantOk:          true,
		},
		{
			name: "connected validator rejoins validator set",
			steps: []testStep{
				{
					connectedValidators: []ids.NodeID{
						ids.NodeID{1},
					},
					validators: []validators.GetCurrentValidatorOutput{
						{
							ValidationID: ids.ID{1},
							NodeID:       ids.NodeID{1},
							StartTime:    uint64(time.Time{}.Unix()),
							IsActive:     true,
						},
					},
				},
				{
					timestamp: time.Time{}.Add(10 * time.Second),
				},
				{
					timestamp: time.Time{}.Add(20 * time.Second),
					validators: []validators.GetCurrentValidatorOutput{
						{
							ValidationID: ids.ID{1},
							NodeID:       ids.NodeID{1},
							StartTime:    uint64(time.Time{}.Add(10 * time.Second).Unix()),
							IsActive:     true,
						},
					},
				},
			},
			validationID:    ids.ID{1},
			wantUptime:      10 * time.Second,
			wantLastUpdated: time.Time{}.Add(20 * time.Second),
			wantOk:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			validatorState := &validatorstest.State{}
			subnetID := ids.GenerateTestID()
			db := memdb.New()
			clock := &mockable.Clock{}

			uptimeTracker, err := New(validatorState, subnetID, db, clock)
			require.NoError(err)

			pChainHeight := uint64(0)
			for _, step := range tt.steps {
				clock.Set(step.timestamp)

				for _, v := range step.connectedValidators {
					require.NoError(uptimeTracker.Connect(v))
				}

				for _, v := range step.disconnectedValidators {
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

					for _, v := range step.validators {
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
		GetCurrentValidatorSetF: func(context.Context, ids.ID) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
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
