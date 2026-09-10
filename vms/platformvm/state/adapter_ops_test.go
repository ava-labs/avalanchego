// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
)

// TestAdapterStakerOps exercises the [Adapter] CRUD surface over a *Diff and
// then over the *State it was applied to. Ops mutate through an adapter and
// assertions read through one, so follow-up branches can extend the harness
// with new ops and assertions without changing its shape.
func TestAdapterStakerOps(t *testing.T) {
	type op func(t *testing.T, a Adapter)
	type assertion func(t *testing.T, a Adapter)

	putCurrentValidator := func(v CurrentValidator) op {
		return func(t *testing.T, a Adapter) {
			require.NoError(t, a.PutCurrentValidator(v))
		}
	}

	deleteCurrentValidator := func(subnetID ids.ID, nodeID ids.NodeID) op {
		return func(t *testing.T, a Adapter) {
			require.NoError(t, a.DeleteCurrentValidator(subnetID, nodeID))
		}
	}

	deleteMissingCurrentValidator := func(subnetID ids.ID, nodeID ids.NodeID) op {
		return func(t *testing.T, a Adapter) {
			require.ErrorIs(t, a.DeleteCurrentValidator(subnetID, nodeID), database.ErrNotFound)
		}
	}

	putCurrentDelegator := func(d CurrentDelegator) op {
		return func(t *testing.T, a Adapter) {
			require.NoError(t, a.PutCurrentDelegator(d))
		}
	}

	deleteCurrentDelegator := func(d CurrentDelegator) op {
		return func(t *testing.T, a Adapter) {
			require.NoError(t, a.DeleteCurrentDelegator(d))
		}
	}

	putPendingValidator := func(tx Tx[platform.ValidatorTx]) op {
		return func(t *testing.T, a Adapter) {
			require.NoError(t, a.PutPendingValidator(tx))
		}
	}

	deletePendingValidator := func(subnetID ids.ID, nodeID ids.NodeID) op {
		return func(t *testing.T, a Adapter) {
			require.NoError(t, a.DeletePendingValidator(subnetID, nodeID))
		}
	}

	deleteMissingPendingValidator := func(subnetID ids.ID, nodeID ids.NodeID) op {
		return func(t *testing.T, a Adapter) {
			require.ErrorIs(t, a.DeletePendingValidator(subnetID, nodeID), database.ErrNotFound)
		}
	}

	putPendingDelegator := func(tx Tx[platform.Delegator]) op {
		return func(t *testing.T, a Adapter) {
			require.NoError(t, a.PutPendingDelegator(tx))
		}
	}

	deletePendingDelegator := func(d PendingDelegator) op {
		return func(_ *testing.T, a Adapter) {
			a.DeletePendingDelegator(d)
		}
	}

	promoteValidator := func(subnetID ids.ID, nodeID ids.NodeID, potentialReward uint64) op {
		return func(t *testing.T, a Adapter) {
			v, err := a.GetPendingValidator(subnetID, nodeID)
			require.NoError(t, err)
			require.NoError(t, a.DeletePendingValidator(subnetID, nodeID))
			require.NoError(t, a.PutCurrentValidator(v.Promote(potentialReward)))
		}
	}

	promoteDelegator := func(d PendingDelegator, potentialReward uint64) op {
		return func(t *testing.T, a Adapter) {
			a.DeletePendingDelegator(d)
			require.NoError(t, a.PutCurrentDelegator(d.Promote(potentialReward)))
		}
	}

	hasCurrentValidator := func(want CurrentValidator) assertion {
		return func(t *testing.T, a Adapter) {
			got, err := a.GetCurrentValidator(want.StakingPeriod().SubnetID(), want.StakingPeriod().NodeID())
			require.NoError(t, err)
			require.Equal(t, want, got)
		}
	}

	noCurrentValidator := func(subnetID ids.ID, nodeID ids.NodeID) assertion {
		return func(t *testing.T, a Adapter) {
			_, err := a.GetCurrentValidator(subnetID, nodeID)
			require.ErrorIs(t, err, database.ErrNotFound)
		}
	}

	hasCurrentDelegators := func(subnetID ids.ID, nodeID ids.NodeID, want ...CurrentDelegator) assertion {
		return func(t *testing.T, a Adapter) {
			got, err := a.GetCurrentDelegators(subnetID, nodeID)
			require.NoError(t, err)
			require.Equal(t, want, slices.Collect(got))
		}
	}

	hasPendingValidator := func(want PendingValidator) assertion {
		return func(t *testing.T, a Adapter) {
			got, err := a.GetPendingValidator(want.StakingPeriod().SubnetID(), want.StakingPeriod().NodeID())
			require.NoError(t, err)
			require.Equal(t, want, got)
		}
	}

	noPendingValidator := func(subnetID ids.ID, nodeID ids.NodeID) assertion {
		return func(t *testing.T, a Adapter) {
			_, err := a.GetPendingValidator(subnetID, nodeID)
			require.ErrorIs(t, err, database.ErrNotFound)
		}
	}

	hasPendingDelegators := func(subnetID ids.ID, nodeID ids.NodeID, want ...PendingDelegator) assertion {
		return func(t *testing.T, a Adapter) {
			got, err := a.GetPendingDelegators(subnetID, nodeID)
			require.NoError(t, err)
			require.Equal(t, want, slices.Collect(got))
		}
	}

	// hasCurrentStakers asserts the current staker set is exactly want plus
	// the genesis validator seeded by newTestState.
	hasCurrentStakers := func(want ...CurrentStaker) assertion {
		return func(t *testing.T, a Adapter) {
			genesisValidator, err := a.GetCurrentValidator(constants.PrimaryNetworkID, defaultValidatorNodeID)
			require.NoError(t, err)

			got, err := a.GetCurrentStakers()
			require.NoError(t, err)
			require.ElementsMatch(t, append([]CurrentStaker{genesisValidator}, want...), slices.Collect(got))
		}
	}

	hasPendingStakers := func(want ...PendingStaker) assertion {
		return func(t *testing.T, a Adapter) {
			got, err := a.GetPendingStakers()
			require.NoError(t, err)
			require.ElementsMatch(t, want, slices.Collect(got))
		}
	}

	signTx := func(unsigned platform.UnsignedTx) *platform.Tx {
		tx := &platform.Tx{Unsigned: unsigned}
		require.NoError(t, tx.Initialize(platform.Codec))
		return tx
	}

	// Round-trip the genesis times through time.Unix so fixtures match records
	// whose times a put derived from the transaction.
	start := time.Unix(genesistest.DefaultValidatorStartTime.Unix(), 0)
	end := time.Unix(genesistest.DefaultValidatorEndTime.Unix(), 0)

	validatorNodeID := ids.GenerateTestNodeID()
	validatorUnsigned := createPermissionlessValidatorTx(t, constants.PrimaryNetworkID, platform.Validator{
		NodeID: validatorNodeID,
		Start:  uint64(start.Unix()),
		End:    uint64(end.Unix()),
		Wght:   5,
	})
	validatorTx := signTx(validatorUnsigned)
	validatorStakerTx, err := NewTx[platform.ValidatorTx](validatorTx)
	require.NoError(t, err)
	validator, err := NewCurrentValidator(validatorStakerTx, start, end, 5, 10)
	require.NoError(t, err)

	delegatorUnsigned := createPermissionlessDelegatorTx(constants.PrimaryNetworkID, platform.Validator{
		NodeID: validatorNodeID,
		Start:  uint64(start.Unix()),
		End:    uint64(end.Unix()),
		Wght:   3,
	})
	delegatorTx := signTx(delegatorUnsigned)
	delegatorStakerTx, err := NewTx[platform.Delegator](delegatorTx)
	require.NoError(t, err)
	delegator := NewCurrentDelegator(delegatorStakerTx, start, end, 3, 7)

	// Subnet validators must also validate the primary network:
	// [State.PutCurrentValidator] reads the node's primary network entry, so
	// the subnet validator reuses the genesis validator's node.
	subnetID := ids.GenerateTestID()
	subnetValidatorUnsigned := createPermissionlessValidatorTx(t, subnetID, platform.Validator{
		NodeID: defaultValidatorNodeID,
		Start:  uint64(start.Unix()),
		End:    uint64(end.Unix()),
		Wght:   2,
	})
	subnetValidatorTx := signTx(subnetValidatorUnsigned)
	subnetValidatorStakerTx, err := NewTx[platform.ValidatorTx](subnetValidatorTx)
	require.NoError(t, err)
	subnetValidator, err := NewCurrentValidator(subnetValidatorStakerTx, start, end, 2, 0)
	require.NoError(t, err)

	pendingValidatorNodeID := ids.GenerateTestNodeID()
	pendingValidatorUnsigned := createPermissionlessValidatorTx(t, constants.PrimaryNetworkID, platform.Validator{
		NodeID: pendingValidatorNodeID,
		Start:  uint64(start.Unix()),
		End:    uint64(end.Unix()),
		Wght:   4,
	})
	pendingValidatorTx := signTx(pendingValidatorUnsigned)
	pendingValidatorStakerTx, err := NewTx[platform.ValidatorTx](pendingValidatorTx)
	require.NoError(t, err)
	pendingValidatorKey, _, err := pendingValidatorUnsigned.PublicKey()
	require.NoError(t, err)
	pendingValidator := pendingValidatorFromStaker(&Staker{
		TxID:      pendingValidatorTx.ID(),
		NodeID:    pendingValidatorNodeID,
		SubnetID:  constants.PrimaryNetworkID,
		PublicKey: pendingValidatorKey,
		Weight:    4,
		StartTime: start,
		EndTime:   end,
		NextTime:  start,
		Priority:  platform.PrimaryNetworkValidatorPendingPriority,
	})
	promotedValidator := currentValidatorFromStaker(&Staker{
		TxID:            pendingValidatorTx.ID(),
		NodeID:          pendingValidatorNodeID,
		SubnetID:        constants.PrimaryNetworkID,
		PublicKey:       pendingValidatorKey,
		Weight:          4,
		StartTime:       start,
		EndTime:         end,
		PotentialReward: 11,
		NextTime:        end,
		Priority:        platform.PrimaryNetworkValidatorCurrentPriority,
	})

	pendingDelegatorUnsigned := createPermissionlessDelegatorTx(constants.PrimaryNetworkID, platform.Validator{
		NodeID: defaultValidatorNodeID,
		Start:  uint64(start.Unix()),
		End:    uint64(end.Unix()),
		Wght:   2,
	})
	pendingDelegatorTx := signTx(pendingDelegatorUnsigned)
	pendingDelegatorStakerTx, err := NewTx[platform.Delegator](pendingDelegatorTx)
	require.NoError(t, err)
	pendingDelegator := pendingDelegatorFromStaker(&Staker{
		TxID:      pendingDelegatorTx.ID(),
		NodeID:    defaultValidatorNodeID,
		SubnetID:  constants.PrimaryNetworkID,
		Weight:    2,
		StartTime: start,
		EndTime:   end,
		NextTime:  start,
		Priority:  platform.PrimaryNetworkDelegatorBanffPendingPriority,
	})
	promotedDelegator := currentDelegatorFromStaker(&Staker{
		TxID:            pendingDelegatorTx.ID(),
		NodeID:          defaultValidatorNodeID,
		SubnetID:        constants.PrimaryNetworkID,
		Weight:          2,
		StartTime:       start,
		EndTime:         end,
		PotentialReward: 13,
		NextTime:        end,
		Priority:        platform.PrimaryNetworkDelegatorCurrentPriority,
	})

	missingNodeID := ids.GenerateTestNodeID()

	type diff struct {
		ops        []op
		assertions []assertion
	}
	tests := []struct {
		name  string
		txs   []*platform.Tx
		diffs []diff
	}{
		{
			name: "current_validator_lifecycle",
			txs:  []*platform.Tx{validatorTx},
			diffs: []diff{
				{
					ops: []op{putCurrentValidator(validator)},
					assertions: []assertion{
						hasCurrentValidator(validator),
						hasCurrentDelegators(constants.PrimaryNetworkID, validatorNodeID),
						hasCurrentStakers(validator),
					},
				},
				{
					ops: []op{deleteCurrentValidator(constants.PrimaryNetworkID, validatorNodeID)},
					assertions: []assertion{
						noCurrentValidator(constants.PrimaryNetworkID, validatorNodeID),
						hasCurrentStakers(),
					},
				},
			},
		},
		{
			name: "current_delegator_lifecycle",
			txs:  []*platform.Tx{validatorTx, delegatorTx},
			diffs: []diff{
				{
					ops: []op{
						putCurrentValidator(validator),
						putCurrentDelegator(delegator),
					},
					assertions: []assertion{
						hasCurrentDelegators(constants.PrimaryNetworkID, validatorNodeID, delegator),
						hasCurrentStakers(validator, delegator),
					},
				},
				{
					ops: []op{deleteCurrentDelegator(delegator)},
					assertions: []assertion{
						hasCurrentValidator(validator),
						hasCurrentDelegators(constants.PrimaryNetworkID, validatorNodeID),
						hasCurrentStakers(validator),
					},
				},
			},
		},
		{
			name: "subnet_validator_lifecycle",
			txs:  []*platform.Tx{subnetValidatorTx},
			diffs: []diff{
				{
					ops: []op{putCurrentValidator(subnetValidator)},
					assertions: []assertion{
						hasCurrentValidator(subnetValidator),
						hasCurrentStakers(subnetValidator),
					},
				},
				{
					ops: []op{deleteCurrentValidator(subnetID, defaultValidatorNodeID)},
					assertions: []assertion{
						noCurrentValidator(subnetID, defaultValidatorNodeID),
						hasCurrentStakers(),
					},
				},
			},
		},
		{
			name: "pending_validator_lifecycle",
			txs:  []*platform.Tx{pendingValidatorTx},
			diffs: []diff{
				{
					ops: []op{putPendingValidator(pendingValidatorStakerTx)},
					assertions: []assertion{
						hasPendingValidator(pendingValidator),
						hasPendingDelegators(constants.PrimaryNetworkID, pendingValidatorNodeID),
						hasPendingStakers(pendingValidator),
					},
				},
				{
					ops: []op{deletePendingValidator(constants.PrimaryNetworkID, pendingValidatorNodeID)},
					assertions: []assertion{
						noPendingValidator(constants.PrimaryNetworkID, pendingValidatorNodeID),
						hasPendingStakers(),
					},
				},
			},
		},
		{
			name: "pending_delegator_lifecycle",
			txs:  []*platform.Tx{pendingDelegatorTx},
			diffs: []diff{
				{
					ops: []op{putPendingDelegator(pendingDelegatorStakerTx)},
					assertions: []assertion{
						hasPendingDelegators(constants.PrimaryNetworkID, defaultValidatorNodeID, pendingDelegator),
						hasPendingStakers(pendingDelegator),
					},
				},
				{
					ops: []op{deletePendingDelegator(pendingDelegator)},
					assertions: []assertion{
						hasPendingDelegators(constants.PrimaryNetworkID, defaultValidatorNodeID),
						hasPendingStakers(),
					},
				},
			},
		},
		{
			name: "promote_pending_validator",
			txs:  []*platform.Tx{pendingValidatorTx},
			diffs: []diff{
				{
					ops:        []op{putPendingValidator(pendingValidatorStakerTx)},
					assertions: []assertion{hasPendingValidator(pendingValidator)},
				},
				{
					ops: []op{promoteValidator(constants.PrimaryNetworkID, pendingValidatorNodeID, 11)},
					assertions: []assertion{
						noPendingValidator(constants.PrimaryNetworkID, pendingValidatorNodeID),
						hasCurrentValidator(promotedValidator),
						hasCurrentStakers(promotedValidator),
						hasPendingStakers(),
					},
				},
			},
		},
		{
			name: "promote_pending_delegator",
			txs:  []*platform.Tx{pendingDelegatorTx},
			diffs: []diff{
				{
					ops: []op{putPendingDelegator(pendingDelegatorStakerTx)},
					assertions: []assertion{
						hasPendingDelegators(constants.PrimaryNetworkID, defaultValidatorNodeID, pendingDelegator),
					},
				},
				{
					ops: []op{promoteDelegator(pendingDelegator, 13)},
					assertions: []assertion{
						hasPendingDelegators(constants.PrimaryNetworkID, defaultValidatorNodeID),
						hasCurrentDelegators(constants.PrimaryNetworkID, defaultValidatorNodeID, promotedDelegator),
						hasPendingStakers(),
					},
				},
			},
		},
		{
			name: "not_found",
			diffs: []diff{
				{
					ops: []op{
						deleteMissingCurrentValidator(constants.PrimaryNetworkID, missingNodeID),
						deleteMissingPendingValidator(constants.PrimaryNetworkID, missingNodeID),
					},
					assertions: []assertion{
						noCurrentValidator(constants.PrimaryNetworkID, missingNodeID),
						noPendingValidator(constants.PrimaryNetworkID, missingNodeID),
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := newTestState(t, memdb.New())
			for _, tx := range tt.txs {
				state.AddTx(tx, status.Committed)
			}

			for _, d := range tt.diffs {
				diff, err := NewDiffOn(state, StakerAdditionAfterDeletionAllowed)
				require.NoError(t, err)

				adapter := NewAdapter(diff)
				for _, o := range d.ops {
					o(t, adapter)
				}

				// Verify the Diff's effective reads before Apply mutates state.
				for _, a := range d.assertions {
					a(t, adapter)
				}

				applyDiffAndCommit(t, state, diff)
				for _, a := range d.assertions {
					a(t, NewAdapter(state))
				}
			}
		})
	}
}
