// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
)

func TestStakingState(t *testing.T) {
	nativeState := newTestState(t, memdb.New())
	typedState := NewAdapter(nativeState)

	primaryValidator, err := typedState.GetCurrentPrimaryNetworkValidator(defaultValidatorNodeID)
	require.NoError(t, err)
	require.Equal(t, constants.PrimaryNetworkID, primaryValidator.SubnetID())

	now := time.Now().Truncate(time.Second)
	subnetValidator := &Staker{
		TxID:      ids.GenerateTestID(),
		NodeID:    ids.GenerateTestNodeID(),
		SubnetID:  ids.GenerateTestID(),
		Weight:    1,
		StartTime: now,
		EndTime:   now.Add(time.Hour),
		NextTime:  now.Add(time.Hour),
		Priority:  platform.SubnetPermissionedValidatorCurrentPriority,
	}
	require.NoError(t, typedState.PutCurrentSubnetValidator(newTestStakerTx(subnetValidator), currentSubnetValidator(subnetValidator)))

	gotSubnetValidator, err := typedState.GetCurrentSubnetValidator(subnetValidator.SubnetID, subnetValidator.NodeID)
	require.NoError(t, err)
	require.Equal(t, currentSubnetValidator(subnetValidator), gotSubnetValidator)

	gotNativeValidator, err := nativeState.GetCurrentValidator(subnetValidator.SubnetID, subnetValidator.NodeID)
	require.NoError(t, err)
	require.Equal(t, subnetValidator, gotNativeValidator)

	delegatorTx := &platform.Tx{Unsigned: &platform.AddDelegatorTx{Validator: platform.Validator{
		NodeID: defaultValidatorNodeID,
		Start:  uint64(now.Unix()),
		End:    uint64(now.Add(time.Minute).Unix()),
		Wght:   1,
	}}}
	nativeState.AddTx(delegatorTx, status.Committed)
	delegator := &Staker{
		TxID:            delegatorTx.ID(),
		NodeID:          defaultValidatorNodeID,
		SubnetID:        constants.PrimaryNetworkID,
		Weight:          1,
		StartTime:       now,
		EndTime:         now.Add(time.Minute),
		PotentialReward: 2,
		NextTime:        now.Add(time.Minute),
		Priority:        platform.PrimaryNetworkDelegatorCurrentPriority,
	}
	require.NoError(t, typedState.PutCurrentDelegator(delegatorTx, currentDelegator(delegator)))

	delegatorIt, err := typedState.GetCurrentDelegatorIterator(constants.PrimaryNetworkID, defaultValidatorNodeID)
	require.NoError(t, err)
	require.Equal(t, []CurrentDelegator{currentDelegator(delegator)}, iterator.ToSlice(delegatorIt))

	require.NoError(t, typedState.DeleteCurrentDelegator(delegator.TxID))
	require.NoError(t, typedState.DeleteCurrentSubnetValidator(subnetValidator.SubnetID, subnetValidator.NodeID))

	_, err = typedState.GetCurrentSubnetValidator(subnetValidator.SubnetID, subnetValidator.NodeID)
	require.ErrorIs(t, err, database.ErrNotFound)
}

func TestStakingStatePreservesPendingPriority(t *testing.T) {
	nativeState := newTestState(t, memdb.New())
	typedState := NewAdapter(nativeState)

	now := time.Now().Truncate(time.Second)
	delegatorTx := &platform.Tx{Unsigned: &platform.AddDelegatorTx{Validator: platform.Validator{
		NodeID: defaultValidatorNodeID,
		Start:  uint64(now.Add(time.Hour).Unix()),
		End:    uint64(now.Add(2 * time.Hour).Unix()),
		Wght:   1,
	}}}
	nativeState.AddTx(delegatorTx, status.Committed)
	delegator := &Staker{
		TxID:      delegatorTx.ID(),
		NodeID:    defaultValidatorNodeID,
		SubnetID:  constants.PrimaryNetworkID,
		Weight:    1,
		StartTime: now.Add(time.Hour),
		EndTime:   now.Add(2 * time.Hour),
		NextTime:  now.Add(time.Hour),
		Priority:  platform.PrimaryNetworkDelegatorApricotPendingPriority,
	}
	require.NoError(t, typedState.PutPendingDelegator(delegatorTx, pendingDelegator(delegator)))

	delegatorIt, err := typedState.GetPendingDelegatorIterator(constants.PrimaryNetworkID, defaultValidatorNodeID)
	require.NoError(t, err)
	require.Equal(t, []PendingDelegator{pendingDelegator(delegator)}, iterator.ToSlice(delegatorIt))

	gotNativeDelegatorIt, err := nativeState.GetPendingDelegatorIterator(constants.PrimaryNetworkID, defaultValidatorNodeID)
	require.NoError(t, err)
	require.Equal(t, []*Staker{delegator}, iterator.ToSlice(gotNativeDelegatorIt))

	require.NoError(t, typedState.DeletePendingDelegator(delegator.TxID))
}

func TestPutCurrentDelegatorUsesTransactionID(t *testing.T) {
	nativeState := newTestState(t, memdb.New())
	typedState := NewAdapter(nativeState)

	now := time.Now().Truncate(time.Second)
	delegator := &Staker{
		TxID:      ids.GenerateTestID(),
		NodeID:    defaultValidatorNodeID,
		SubnetID:  constants.PrimaryNetworkID,
		Weight:    1,
		StartTime: now,
		EndTime:   now.Add(time.Hour),
		NextTime:  now.Add(time.Hour),
		Priority:  platform.PrimaryNetworkDelegatorCurrentPriority,
	}
	tx := newTestStakerTx(delegator)
	tx.TxID = ids.GenerateTestID()
	require.NotEqual(t, delegator.TxID, tx.ID())

	require.NoError(t, typedState.PutCurrentDelegator(tx, currentDelegator(delegator)))

	it, err := nativeState.GetCurrentDelegatorIterator(delegator.SubnetID, delegator.NodeID)
	require.NoError(t, err)
	storedDelegators := iterator.ToSlice(it)
	require.Len(t, storedDelegators, 1)
	require.Equal(t, tx.ID(), storedDelegators[0].TxID)
}

func newTestStakerTx(staker *Staker) *platform.Tx {
	return &platform.Tx{
		Unsigned: &platform.AddDelegatorTx{Validator: platform.Validator{
			NodeID: staker.NodeID,
			Start:  uint64(staker.StartTime.Unix()),
			End:    uint64(staker.EndTime.Unix()),
			Wght:   staker.Weight,
		}},
		TxID: staker.TxID,
	}
}

// Embedding promotes the sum markers, so AutoRenewedValidator is also a
// CurrentValidator. Type switches over the sum must not assume newCurrentStaker
// produces its only inhabitants.
var (
	_ CurrentValidator = AutoRenewedValidator{}
	_ CurrentStaker    = AutoRenewedValidator{}
)

func TestNewCurrentStakerClassifiesEveryPriority(t *testing.T) {
	tests := []struct {
		priority platform.Priority
		want     CurrentStaker
	}{
		{platform.PrimaryNetworkDelegatorCurrentPriority, CurrentDelegator{}},
		{platform.SubnetPermissionlessDelegatorCurrentPriority, CurrentDelegator{}},
		{platform.PrimaryNetworkValidatorCurrentPriority, CurrentPrimaryNetworkValidator{}},
		{platform.SubnetPermissionedValidatorCurrentPriority, CurrentSubnetValidator{}},
		{platform.SubnetPermissionlessValidatorCurrentPriority, CurrentSubnetValidator{}},
	}
	for _, test := range tests {
		t.Run(fmt.Sprint(test.priority), func(t *testing.T) {
			require.IsType(t, test.want, newCurrentStaker(&Staker{Priority: test.priority}))
		})
	}
}

func TestNewPendingStakerClassifiesEveryPriority(t *testing.T) {
	tests := []struct {
		priority platform.Priority
		want     PendingStaker
	}{
		{platform.PrimaryNetworkDelegatorApricotPendingPriority, PendingDelegator{}},
		{platform.PrimaryNetworkDelegatorBanffPendingPriority, PendingDelegator{}},
		{platform.SubnetPermissionlessDelegatorPendingPriority, PendingDelegator{}},
		{platform.PrimaryNetworkValidatorPendingPriority, PendingPrimaryNetworkValidator{}},
		{platform.SubnetPermissionedValidatorPendingPriority, PendingSubnetValidator{}},
		{platform.SubnetPermissionlessValidatorPendingPriority, PendingSubnetValidator{}},
	}
	for _, test := range tests {
		t.Run(fmt.Sprint(test.priority), func(t *testing.T) {
			require.IsType(t, test.want, newPendingStaker(&Staker{Priority: test.priority}))
		})
	}
}

// An unrecognized priority must not be silently classified.
func TestNewStakerRejectsUnknownPriority(t *testing.T) {
	for _, priority := range []platform.Priority{0, 200} {
		t.Run(fmt.Sprint(priority), func(t *testing.T) {
			require.Panics(t, func() { newCurrentStaker(&Staker{Priority: priority}) })
			require.Panics(t, func() { newPendingStaker(&Staker{Priority: priority}) })
		})
	}
}

// Promotion must preserve permissioned status; native ordering depends on it.
func TestPendingToCurrentPriorityMatchesTx(t *testing.T) {
	for _, priority := range []platform.Priority{
		platform.PrimaryNetworkDelegatorApricotPendingPriority,
		platform.PrimaryNetworkDelegatorBanffPendingPriority,
		platform.PrimaryNetworkValidatorPendingPriority,
		platform.SubnetPermissionedValidatorPendingPriority,
		platform.SubnetPermissionlessValidatorPendingPriority,
		platform.SubnetPermissionlessDelegatorPendingPriority,
	} {
		t.Run(fmt.Sprint(priority), func(t *testing.T) {
			current := platform.PendingToCurrentPriorities[priority]
			require.True(t, current.IsCurrent(), "pending priority must map to a current priority")
			require.Equal(
				t,
				priority.IsPermissionedValidator(),
				current.IsPermissionedValidator(),
				"promotion must preserve permissioned status",
			)
		})
	}
}
