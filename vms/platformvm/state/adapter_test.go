// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
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
	require.NoError(t, typedState.PutCurrentSubnetValidator(testStakerTx{subnetValidator}, currentSubnetValidator(subnetValidator)))

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
	require.NoError(t, typedState.PutCurrentDelegator(testStakerTx{delegator}, currentDelegator(delegator)))

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
	require.NoError(t, typedState.PutPendingDelegator(testStakerTx{delegator}, pendingDelegator(delegator)))

	delegatorIt, err := typedState.GetPendingDelegatorIterator(constants.PrimaryNetworkID, defaultValidatorNodeID)
	require.NoError(t, err)
	require.Equal(t, []PendingDelegator{pendingDelegator(delegator)}, iterator.ToSlice(delegatorIt))

	gotNativeDelegatorIt, err := nativeState.GetPendingDelegatorIterator(constants.PrimaryNetworkID, defaultValidatorNodeID)
	require.NoError(t, err)
	require.Equal(t, []*Staker{delegator}, iterator.ToSlice(gotNativeDelegatorIt))

	require.NoError(t, typedState.DeletePendingDelegator(delegator.TxID))
}

type testStakerTx struct {
	*Staker
}

func (tx testStakerTx) SubnetID() ids.ID {
	return tx.Staker.SubnetID
}

func (tx testStakerTx) NodeID() ids.NodeID {
	return tx.Staker.NodeID
}

func (testStakerTx) PublicKey() (*bls.PublicKey, bool, error) {
	return nil, false, nil
}

func (tx testStakerTx) Weight() uint64 {
	return tx.Staker.Weight
}

func (tx testStakerTx) CurrentPriority() platform.Priority {
	return tx.Staker.Priority
}
