// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
		wantErr         error
	}{
		{
			name:    "no validators",
			wantErr: ErrValidationIDNotFound,
		},
		{
			name: "one validator",
			steps: []testStep{
				{
					timestamp: time.Time{}.Add(10 * time.Second),
					connectedValidators: []ids.NodeID{
						{1},
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
		},
		{
			name: "one validator added and removed",
			steps: []testStep{
				{
					timestamp: time.Time{}.Add(10 * time.Second),
					connectedValidators: []ids.NodeID{
						{1},
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
			wantErr:      ErrValidationIDNotFound,
		},
		{
			name: "one validator deactivated",
			steps: []testStep{
				{
					connectedValidators: []ids.NodeID{
						{1},
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
		},
		{
			name: "one validator deactivated and reactivated",
			steps: []testStep{
				{
					connectedValidators: []ids.NodeID{
						{1},
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
		},
		{
			name: "one validator disconnected",
			steps: []testStep{
				{
					connectedValidators: []ids.NodeID{
						{1},
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
						{1},
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
		},
		{
			name: "one validator connected and disconnected",
			steps: []testStep{
				{
					connectedValidators: []ids.NodeID{
						{1},
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
						{1},
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
		},
		{
			name: "validator removed",
			steps: []testStep{
				{
					connectedValidators: []ids.NodeID{
						{1},
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
			wantErr:      ErrValidationIDNotFound,
		},
		{
			name: "connected inactive validator becomes active",
			steps: []testStep{
				{
					connectedValidators: []ids.NodeID{
						{1},
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
						{1},
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
		},
		{
			name: "deactivated validator leaves validator set",
			steps: []testStep{
				{
					connectedValidators: []ids.NodeID{
						{1},
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
			wantErr:      ErrValidationIDNotFound,
		},
		{
			name: "validator has no updates",
			steps: []testStep{
				{
					connectedValidators: []ids.NodeID{
						{1},
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
		},
		{
			name: "connected validator rejoins validator set",
			steps: []testStep{
				{
					connectedValidators: []ids.NodeID{
						{1},
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
		},
		{
			name: "disconnected validator reactivated",
			steps: []testStep{
				{
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
					timestamp: time.Time{}.Add(80 * time.Second),
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
			wantUptime:      0, // never connected
			wantLastUpdated: time.Time{}.Add(80 * time.Second),
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

				require.NoError(uptimeTracker.Sync(t.Context()))
			}

			gotUptime, gotLastUpdated, err := uptimeTracker.GetUptime(
				tt.validationID,
			)

			require.ErrorIs(err, tt.wantErr)
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

	require.NoError(uptimeTracker.Sync(t.Context()))
	require.NoError(uptimeTracker.Connect(nodeID))

	clock.Set(start.Add(10 * time.Second))
	uptime, lastUpdated, err := uptimeTracker.GetUptime(validationID)
	require.NoError(err)
	require.Equal(10*time.Second, uptime)
	require.Equal(time.Time{}.Add(10*time.Second), lastUpdated)

	require.NoError(uptimeTracker.Shutdown())

	clock.Set(start.Add(20 * time.Second))
	uptimeTracker, err = New(validatorState, subnetID, db, clock)
	require.NoError(err)
	require.NoError(uptimeTracker.Sync(t.Context()))

	uptime, lastUpdated, err = uptimeTracker.GetUptime(validationID)
	require.NoError(err)
	require.Equal(20*time.Second, uptime)
	require.Equal(time.Time{}.Add(20*time.Second), lastUpdated)
}

// TestUptimeTracker_RemoveDisconnectedValidator ensures that removing a
// validator with a pending uptime update is reflected on restart.
func TestUptimeTracker_RemoveDisconnectedValidator(t *testing.T) {
	var (
		height uint64
		start  time.Time
	)

	var (
		subnetID              = ids.GenerateTestID()
		removedValidationID   = ids.GenerateTestID()
		removedNodeID         = ids.GenerateTestNodeID()
		remainingValidationID = ids.GenerateTestID()
		remainingNodeID       = ids.GenerateTestNodeID()
	)

	removedValidator := &validators.GetCurrentValidatorOutput{
		ValidationID: removedValidationID,
		NodeID:       removedNodeID,
		StartTime:    uint64(start.Unix()),
		IsActive:     true,
	}
	remainingValidator := &validators.GetCurrentValidatorOutput{
		ValidationID: remainingValidationID,
		NodeID:       remainingNodeID,
		StartTime:    uint64(start.Unix()),
		IsActive:     true,
	}
	currentValidators := map[ids.ID]*validators.GetCurrentValidatorOutput{
		removedValidationID:   removedValidator,
		remainingValidationID: remainingValidator,
	}

	validatorState := &validatorstest.State{
		GetCurrentValidatorSetF: func(context.Context, ids.ID) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
			return currentValidators, height, nil
		},
	}

	db := memdb.New()
	clock := &mockable.Clock{}
	clock.Set(start)
	uptimeTracker, err := New(validatorState, subnetID, db, clock)
	require.NoError(t, err, "New()")

	require.NoErrorf(t, uptimeTracker.Sync(t.Context()), "%T.Sync()", uptimeTracker)
	require.NoErrorf(t, uptimeTracker.Connect(removedNodeID), "%T.Connect(%s)", uptimeTracker, removedNodeID)
	require.NoErrorf(t, uptimeTracker.Connect(remainingNodeID), "%T.Connect(%s)", uptimeTracker, remainingNodeID)

	// The node disconnects, leaving a pending uptime update, and the validator
	// is removed from the P-chain before the next Sync.
	clock.Set(start.Add(10 * time.Second))
	require.NoErrorf(t, uptimeTracker.Disconnect(removedNodeID), "%T.Disconnect(%s)", uptimeTracker, removedNodeID)
	delete(currentValidators, removedValidationID)
	height = 1
	require.NoErrorf(t, uptimeTracker.Sync(t.Context()), "%T.Sync() after removing a disconnected validator", uptimeTracker)

	clock.Set(start.Add(20 * time.Second))
	require.NoErrorf(t, uptimeTracker.Shutdown(), "%T.Shutdown()", uptimeTracker)

	t.Run("restart", func(t *testing.T) {
		uptimeTracker, err = New(validatorState, subnetID, db, clock)
		require.NoError(t, err, "New()")

		_, _, err = uptimeTracker.GetUptime(removedValidationID)
		require.ErrorIsf(t, err, ErrValidationIDNotFound, "%T.GetUptime() of removed validator", uptimeTracker)

		require.NoErrorf(t, uptimeTracker.Sync(t.Context()), "%T.Sync()", uptimeTracker)

		uptime, lastUpdated, err := uptimeTracker.GetUptime(remainingValidationID)
		require.NoErrorf(t, err, "%T.GetUptime() of remaining validator", uptimeTracker)
		require.Equalf(t, 20*time.Second, uptime, "%T.GetUptime() uptime of remaining validator", uptimeTracker)
		require.Equalf(t, start.Add(20*time.Second), lastUpdated, "%T.GetUptime() lastUpdated of remaining validator", uptimeTracker)
	})
}

// TestUptimeTracker_ReregisterConnectedDeactivatedValidator ensures that a
// node which stays connected while its inactive validator is removed.
func TestUptimeTracker_ReregisterConnectedDeactivatedValidator(t *testing.T) {
	var (
		oldValidationID   = ids.GenerateTestID()
		newValidationID   = ids.GenerateTestID()
		nodeID            = ids.GenerateTestNodeID()
		currentValidators = make(map[ids.ID]*validators.GetCurrentValidatorOutput)
	)

	var (
		start  time.Time
		height uint64
	)
	validatorState := &validatorstest.State{
		GetCurrentValidatorSetF: func(context.Context, ids.ID) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
			return currentValidators, height, nil
		},
	}
	clock := &mockable.Clock{}
	clock.Set(start)

	uptimeTracker, err := New(validatorState, ids.GenerateTestID(), memdb.New(), clock)
	require.NoError(t, err, "New()")

	// The node is connected while its validator is inactive.
	currentValidators[oldValidationID] = &validators.GetCurrentValidatorOutput{
		ValidationID: oldValidationID,
		NodeID:       nodeID,
		StartTime:    uint64(start.Unix()),
		IsActive:     false,
	}
	require.NoErrorf(t, uptimeTracker.Sync(t.Context()), "%T.Sync() with inactive validator", uptimeTracker)
	require.NoErrorf(t, uptimeTracker.Connect(nodeID), "%T.Connect(%s)", uptimeTracker, nodeID)

	// The inactive validator is removed while the node stays connected.
	delete(currentValidators, oldValidationID)
	height = 1
	require.NoErrorf(t, uptimeTracker.Sync(t.Context()), "%T.Sync() after removal", uptimeTracker)

	// The node is registered as a new, active validator.
	currentValidators[newValidationID] = &validators.GetCurrentValidatorOutput{
		ValidationID: newValidationID,
		NodeID:       nodeID,
		StartTime:    uint64(start.Unix()),
		IsActive:     true,
	}
	height = 2
	require.NoErrorf(t, uptimeTracker.Sync(t.Context()), "%T.Sync() after re-registration", uptimeTracker)

	clock.Set(start.Add(10 * time.Second))
	uptime, _, err := uptimeTracker.GetUptime(newValidationID)
	require.NoErrorf(t, err, "%T.GetUptime()", uptimeTracker)
	require.Equalf(t, 10*time.Second, uptime, "%T.GetUptime() of connected, re-registered validator", uptimeTracker)
}

// TestUptimeTracker_RestartWithInactiveValidator ensures that a validator that
// was inactive before a restart, and is still inactive after it, continues to
// be treated as offline when its node connects.
func TestUptimeTracker_RestartWithInactiveValidator(t *testing.T) {
	var (
		validationID = ids.GenerateTestID()
		nodeID       = ids.GenerateTestNodeID()
		subnetID     = ids.GenerateTestID()
	)

	db := memdb.New()
	start := time.Time{}
	clock := &mockable.Clock{}
	clock.Set(start)
	validatorState := &validatorstest.State{
		GetCurrentValidatorSetF: func(context.Context, ids.ID) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
			return map[ids.ID]*validators.GetCurrentValidatorOutput{
				validationID: {
					ValidationID: validationID,
					StartTime:    uint64(start.Unix()),
					NodeID:       nodeID,
					IsActive:     false,
				},
			}, 0, nil
		},
	}
	uptimeTracker, err := New(validatorState, subnetID, db, clock)
	require.NoError(t, err, "New()")
	require.NoErrorf(t, uptimeTracker.Sync(t.Context()), "%T.Sync()", uptimeTracker)
	require.NoErrorf(t, uptimeTracker.Shutdown(), "%T.Shutdown()", uptimeTracker)

	uptimeTracker, err = New(validatorState, subnetID, db, clock)
	require.NoError(t, err, "New() after restart")
	require.NoErrorf(t, uptimeTracker.Sync(t.Context()), "%T.Sync() after restart", uptimeTracker)
	require.NoErrorf(t, uptimeTracker.Connect(nodeID), "%T.Connect(%s)", uptimeTracker, nodeID)

	clock.Set(start.Add(10 * time.Second))
	uptime, _, err := uptimeTracker.GetUptime(validationID)
	require.NoErrorf(t, err, "%T.GetUptime()", uptimeTracker)
	require.Zerof(t, uptime, "%T.GetUptime() of inactive validator", uptimeTracker)
}
