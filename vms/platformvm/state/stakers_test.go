// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

func TestBaseStakersPruning(t *testing.T) {
	require := require.New(t)
	staker := newTestStaker()
	delegator := newTestStaker()
	delegator.SubnetID = staker.SubnetID
	delegator.NodeID = staker.NodeID

	v := newBaseStakers()

	v.PutValidator(staker)

	_, err := v.GetValidator(staker.SubnetID, staker.NodeID)
	require.NoError(err)

	v.PutDelegator(delegator)

	_, err = v.GetValidator(staker.SubnetID, staker.NodeID)
	require.NoError(err)

	v.DeleteValidator(staker)

	_, err = v.GetValidator(staker.SubnetID, staker.NodeID)
	require.ErrorIs(err, database.ErrNotFound)

	v.DeleteDelegator(delegator)

	require.Empty(v.validators)

	v.PutValidator(staker)

	_, err = v.GetValidator(staker.SubnetID, staker.NodeID)
	require.NoError(err)

	v.PutDelegator(delegator)

	_, err = v.GetValidator(staker.SubnetID, staker.NodeID)
	require.NoError(err)

	v.DeleteDelegator(delegator)

	_, err = v.GetValidator(staker.SubnetID, staker.NodeID)
	require.NoError(err)

	v.DeleteValidator(staker)

	_, err = v.GetValidator(staker.SubnetID, staker.NodeID)
	require.ErrorIs(err, database.ErrNotFound)

	require.Empty(v.validators)
}

func TestBaseStakersValidator(t *testing.T) {
	require := require.New(t)
	staker := newTestStaker()
	delegator := newTestStaker()

	v := newBaseStakers()

	v.PutDelegator(delegator)

	_, err := v.GetValidator(ids.GenerateTestID(), delegator.NodeID)
	require.ErrorIs(err, database.ErrNotFound)

	_, err = v.GetValidator(delegator.SubnetID, ids.GenerateTestNodeID())
	require.ErrorIs(err, database.ErrNotFound)

	_, err = v.GetValidator(delegator.SubnetID, delegator.NodeID)
	require.ErrorIs(err, database.ErrNotFound)

	stakerIterator := v.GetStakerIterator()
	require.Equal(
		[]*Staker{delegator},
		iterator.ToSlice(stakerIterator),
	)

	v.PutValidator(staker)

	returnedStaker, err := v.GetValidator(staker.SubnetID, staker.NodeID)
	require.NoError(err)
	require.Equal(staker, returnedStaker)

	v.DeleteDelegator(delegator)

	stakerIterator = v.GetStakerIterator()
	require.Equal(
		[]*Staker{staker},
		iterator.ToSlice(stakerIterator),
	)

	v.DeleteValidator(staker)

	_, err = v.GetValidator(staker.SubnetID, staker.NodeID)
	require.ErrorIs(err, database.ErrNotFound)

	stakerIterator = v.GetStakerIterator()
	require.Empty(
		iterator.ToSlice(stakerIterator),
	)
}

func TestBaseStakersDelegator(t *testing.T) {
	require := require.New(t)
	staker := newTestStaker()
	delegator := newTestStaker()

	v := newBaseStakers()

	delegatorIterator := v.GetDelegatorIterator(delegator.SubnetID, delegator.NodeID)
	require.Empty(
		iterator.ToSlice(delegatorIterator),
	)

	v.PutDelegator(delegator)

	delegatorIterator = v.GetDelegatorIterator(delegator.SubnetID, ids.GenerateTestNodeID())
	require.Empty(
		iterator.ToSlice(delegatorIterator),
	)

	delegatorIterator = v.GetDelegatorIterator(delegator.SubnetID, delegator.NodeID)
	require.Equal(
		[]*Staker{delegator},
		iterator.ToSlice(delegatorIterator),
	)

	v.DeleteDelegator(delegator)

	delegatorIterator = v.GetDelegatorIterator(delegator.SubnetID, delegator.NodeID)
	require.Empty(
		iterator.ToSlice(delegatorIterator),
	)

	v.PutValidator(staker)

	v.PutDelegator(delegator)
	v.DeleteDelegator(delegator)

	delegatorIterator = v.GetDelegatorIterator(staker.SubnetID, staker.NodeID)
	require.Empty(
		iterator.ToSlice(delegatorIterator),
	)
}

func TestDiffStakersAddDeleteAddDeleteValidator(t *testing.T) {
	require := require.New(t)
	staker := newTestStaker()

	diff := diffStakers{}
	require.False(existsInDiff(&diff, staker))

	// Add the validator
	require.NoError(diff.PutValidator(staker))

	// Ensure it exists in the diff
	require.True(existsInDiff(&diff, staker))
	returnedStaker, status := diff.GetValidator(staker.SubnetID, staker.NodeID)
	require.Equal(added, status)
	require.Equal(staker, returnedStaker)

	// Next, delete the validator
	diff.DeleteValidator(staker)

	// Validators created and deleted in the same diff are marked as unmodified.
	// This means they won't be pushed to baseState if diff.Apply(baseState) is
	// called.
	_, status = diff.GetValidator(staker.SubnetID, staker.NodeID)
	require.Equal(unmodified, status)
	require.False(existsInDiff(&diff, staker))

	// Add it back to the diff
	require.NoError(diff.PutValidator(staker))

	// Ensure it exists in the diff again
	require.True(existsInDiff(&diff, staker))
	returnedStaker, status = diff.GetValidator(staker.SubnetID, staker.NodeID)
	require.Equal(added, status)
	require.Equal(staker, returnedStaker)

	// Delete it again
	diff.DeleteValidator(staker)

	// Ensure it doesn't exist in the diff again
	_, status = diff.GetValidator(staker.SubnetID, staker.NodeID)
	require.Equal(unmodified, status)
	require.False(existsInDiff(&diff, staker))
}

func TestDiffStakersUpdateValidator(t *testing.T) {
	require := require.New(t)
	staker := newTestStaker()

	startTime := staker.StartTime
	endTime := staker.EndTime.Add(genesistest.DefaultValidatorDuration)

	modifiedStaker := *staker
	modifiedStaker.Weight++
	modifiedStaker.StartTime = startTime
	modifiedStaker.EndTime = endTime

	diff := diffStakers{}
	require.False(existsInDiff(&diff, staker))

	diff.DeleteValidator(staker)
	require.False(existsInDiff(&diff, staker))
	_, status := diff.GetValidator(staker.SubnetID, staker.NodeID)
	require.Equal(deleted, status)

	require.NoError(diff.PutValidator(&modifiedStaker))

	returnedStaker, status := diff.GetValidator(staker.SubnetID, staker.NodeID)
	require.Equal(added, status)
	require.Equal(&modifiedStaker, returnedStaker)
	require.True(existsInDiff(&diff, &modifiedStaker))
}

func TestDiffStakersDeleteAddDeleteValidator(t *testing.T) {
	require := require.New(t)
	v1 := newTestStaker()

	startTime := v1.StartTime
	endTime := v1.EndTime.Add(genesistest.DefaultValidatorDuration)

	v1Prime := *v1
	v1Prime.Weight++
	v1Prime.StartTime = startTime
	v1Prime.EndTime = endTime

	diff := diffStakers{}

	// Delete v1 (simulating removal of an existing validator from base state)
	diff.DeleteValidator(v1)
	_, status := diff.GetValidator(v1.SubnetID, v1.NodeID)
	require.Equal(deleted, status)
	require.False(existsInDiff(&diff, v1))

	// Add v1' (a modified replacement validator for the same node)
	require.NoError(diff.PutValidator(&v1Prime))
	_, status = diff.GetValidator(v1.SubnetID, v1.NodeID)
	require.Equal(added, status)

	// Delete v1' (undo the replacement)
	diff.DeleteValidator(&v1Prime)

	// The net effect should be: v1 is still deleted from the base state.
	// The add-then-delete of v1' should not erase v1's deletion.
	_, status = diff.GetValidator(v1.SubnetID, v1.NodeID)
	require.Equal(deleted, status, "original deletion of v1 was lost")
	require.False(existsInDiff(&diff, v1))
}

func TestDiffValidatorWeightDiffAfterDeleteAndAdd(t *testing.T) {
	require := require.New(t)
	staker := newTestStaker()
	staker.Weight = 5

	modifiedStaker := *staker
	modifiedStaker.Weight = 10

	diff := diffStakers{}

	// Delete the original validator (weight 5)
	diff.DeleteValidator(staker)

	// Add a replacement validator (weight 10) for the same node
	require.NoError(diff.PutValidator(&modifiedStaker))

	// Verify the validator was replaced
	returnedStaker, status := diff.GetValidator(staker.SubnetID, staker.NodeID)
	require.Equal(added, status)
	require.Equal(uint64(10), returnedStaker.Weight)

	// WeightDiff should reflect the net change: +10 - 5 = +5
	validatorDiff := diff.getOrCreateDiff(staker.SubnetID, staker.NodeID)
	weightDiff, err := validatorDiff.WeightDiff()
	require.NoError(err)
	require.False(weightDiff.Decrease)
	require.Equal(uint64(5), weightDiff.Amount, "expected net weight change of +5 (new 10 minus old 5)")
}

func TestDiffStakersValidator(t *testing.T) {
	require := require.New(t)
	staker := newTestStaker()
	delegator := newTestStaker()

	v := diffStakers{}

	v.PutDelegator(delegator)

	// validators not available in the diff are marked as unmodified
	_, status := v.GetValidator(ids.GenerateTestID(), delegator.NodeID)
	require.Equal(unmodified, status)

	_, status = v.GetValidator(delegator.SubnetID, ids.GenerateTestNodeID())
	require.Equal(unmodified, status)

	// delegator addition shouldn't change validatorStatus
	_, status = v.GetValidator(delegator.SubnetID, delegator.NodeID)
	require.Equal(unmodified, status)

	stakerIterator := v.GetStakerIterator(iterator.Empty[*Staker]{})
	require.Equal(
		[]*Staker{delegator},
		iterator.ToSlice(stakerIterator),
	)

	require.NoError(v.PutValidator(staker))

	returnedStaker, status := v.GetValidator(staker.SubnetID, staker.NodeID)
	require.Equal(added, status)
	require.Equal(staker, returnedStaker)

	v.DeleteValidator(staker)

	// Validators created and deleted in the same diff are marked as unmodified.
	// This means they won't be pushed to baseState if diff.Apply(baseState) is
	// called.
	_, status = v.GetValidator(staker.SubnetID, staker.NodeID)
	require.Equal(unmodified, status)

	stakerIterator = v.GetStakerIterator(iterator.Empty[*Staker]{})
	require.Equal(
		[]*Staker{delegator},
		iterator.ToSlice(stakerIterator),
	)
}

func TestDiffStakersDeleteValidator(t *testing.T) {
	require := require.New(t)
	staker := newTestStaker()
	delegator := newTestStaker()

	v := diffStakers{}

	_, status := v.GetValidator(ids.GenerateTestID(), delegator.NodeID)
	require.Equal(unmodified, status)

	v.DeleteValidator(staker)

	returnedStaker, status := v.GetValidator(staker.SubnetID, staker.NodeID)
	require.Equal(deleted, status)
	require.Nil(returnedStaker)
}

func TestDiffStakersDelegator(t *testing.T) {
	require := require.New(t)
	staker := newTestStaker()
	delegator := newTestStaker()

	v := diffStakers{}

	require.NoError(v.PutValidator(staker))

	delegatorIterator := v.GetDelegatorIterator(iterator.Empty[*Staker]{}, ids.GenerateTestID(), delegator.NodeID)
	require.Empty(
		iterator.ToSlice(delegatorIterator),
	)

	v.PutDelegator(delegator)

	delegatorIterator = v.GetDelegatorIterator(iterator.Empty[*Staker]{}, delegator.SubnetID, delegator.NodeID)
	require.Equal(
		[]*Staker{delegator},
		iterator.ToSlice(delegatorIterator),
	)

	v.DeleteDelegator(delegator)

	delegatorIterator = v.GetDelegatorIterator(iterator.Empty[*Staker]{}, ids.GenerateTestID(), delegator.NodeID)
	require.Empty(
		iterator.ToSlice(delegatorIterator),
	)
}

func newTestStaker() *Staker {
	startTime := time.Now().Round(time.Second)
	endTime := startTime.Add(genesistest.DefaultValidatorDuration)
	return &Staker{
		TxID:            ids.GenerateTestID(),
		NodeID:          ids.GenerateTestNodeID(),
		SubnetID:        ids.GenerateTestID(),
		Weight:          1,
		StartTime:       startTime,
		EndTime:         endTime,
		PotentialReward: 1,

		NextTime: endTime,
		Priority: txs.PrimaryNetworkDelegatorCurrentPriority,
	}
}

func existsInDiff(bs *diffStakers, staker *Staker) bool {
	it := bs.GetStakerIterator(iterator.Empty[*Staker]{})
	defer it.Release()

	for it.Next() {
		if it.Value().Equals(staker) {
			return true
		}
	}
	return false
}
