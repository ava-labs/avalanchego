// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

func TestBaseStakersPruning(t *testing.T) {
	require := require.New(t)
	staker := newTestStaker(t)
	delegator := newTestStaker(t)
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
	staker := newTestStaker(t)
	delegator := newTestStaker(t)

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

func TestBaseStakersUpdateValidator(t *testing.T) {
	require := require.New(t)

	v := newBaseStakers()
	staker := newTestStaker(t)

	v.PutValidator(staker)

	mutatedStaker := *staker
	mutatedStaker.Weight = 10
	v.UpdateValidator(&mutatedStaker)

	returnedStaker, err := v.GetValidator(staker.SubnetID, staker.NodeID)
	require.Nil(err)
	require.Equal(&mutatedStaker, returnedStaker)
}

func TestBaseStakersDelegator(t *testing.T) {
	require := require.New(t)
	staker := newTestStaker(t)
	delegator := newTestStaker(t)

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

func TestDiffStakersValidator(t *testing.T) {
	require := require.New(t)
	staker := newTestStaker(t)
	delegator := newTestStaker(t)

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
	t.Run("delete validator", func(t *testing.T) {
		require := require.New(t)

		v := diffStakers{}
		staker := newTestStaker(t)

		v.DeleteValidator(staker)

		returnedStaker, status := v.GetValidator(staker.SubnetID, staker.NodeID)
		require.Equal(deleted, status)
		require.Nil(returnedStaker)
	})

	t.Run("delete after update existing validator", func(t *testing.T) {
		require := require.New(t)

		v := diffStakers{}
		staker := newTestStaker(t)

		v.UpdateValidator(staker, staker)

		returnedStaker, status := v.GetValidator(staker.SubnetID, staker.NodeID)
		require.Equal(modified, status)
		require.Equal(staker, returnedStaker)

		v.DeleteValidator(staker)

		returnedStaker, status = v.GetValidator(staker.SubnetID, staker.NodeID)
		require.Equal(deleted, status)
		require.Nil(returnedStaker)
	})

	t.Run("delete after update new validator", func(t *testing.T) {
		require := require.New(t)

		v := diffStakers{}
		staker := newTestStaker(t)

		require.NoError(v.PutValidator(staker))
		returnedStaker, status := v.GetValidator(staker.SubnetID, staker.NodeID)
		require.Equal(added, status)
		require.Equal(staker, returnedStaker)

		v.UpdateValidator(staker, staker)
		returnedStaker, status = v.GetValidator(staker.SubnetID, staker.NodeID)
		require.Equal(added, status)
		require.Equal(staker, returnedStaker)

		v.DeleteValidator(staker)
		returnedStaker, status = v.GetValidator(staker.SubnetID, staker.NodeID)
		require.Equal(unmodified, status)
		require.Nil(returnedStaker)
	})
}

func TestDiffStakersGetStakerIterator(t *testing.T) {
	require := require.New(t)

	v := diffStakers{}

	state := newTestState(t, memdb.New())

	// 1. Existing validator
	existingStaker := newTestStaker(t)
	require.NoError(state.PutCurrentValidator(existingStaker))

	// 2. Updated validator
	updatedStaker := newTestStaker(t)

	require.NoError(state.PutCurrentValidator(updatedStaker))
	v.UpdateValidator(updatedStaker, updatedStaker)

	// 3. Added validator
	addedStaker := newTestStaker(t)
	require.NoError(v.PutValidator(addedStaker))

	stateStakersIterator, err := state.GetCurrentStakerIterator()
	require.NoError(err)
	require.Len(iterator.ToSlice(v.GetStakerIterator(stateStakersIterator)), 4)

	v.DeleteValidator(existingStaker)
	v.DeleteValidator(updatedStaker)
	v.DeleteValidator(addedStaker)

	stateStakersIterator, err = state.GetCurrentStakerIterator()
	require.NoError(err)
	require.Len(iterator.ToSlice(v.GetStakerIterator(stateStakersIterator)), 1)
}

func TestDiffStakersDelegator(t *testing.T) {
	require := require.New(t)
	staker := newTestStaker(t)
	delegator := newTestStaker(t)

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

func TestBaseStakersWeightDiff(t *testing.T) {
	require := require.New(t)

	staker := newTestStaker(t)
	delegator := newTestStaker(t)
	delegator.SubnetID = staker.SubnetID
	delegator.NodeID = staker.NodeID

	mutatedStaker := *staker
	mutatedStaker.Weight += 10

	v := newBaseStakers()

	{
		v.PutValidator(staker)
		diffValidator := v.getOrCreateValidatorDiff(staker.SubnetID, staker.NodeID)
		weightDiff, err := diffValidator.WeightDiff()

		require.NoError(err)
		require.Equal(false, weightDiff.Decrease)
		require.Equal(staker.Weight, weightDiff.Amount)
	}

	{
		v.PutDelegator(delegator)
		diffValidator := v.getOrCreateValidatorDiff(staker.SubnetID, staker.NodeID)
		weightDiff, err := diffValidator.WeightDiff()

		require.NoError(err)
		require.Equal(false, weightDiff.Decrease)
		require.Equal(staker.Weight+delegator.Weight, weightDiff.Amount)
	}

	{
		v.UpdateValidator(&mutatedStaker)
		diffValidator := v.getOrCreateValidatorDiff(mutatedStaker.SubnetID, mutatedStaker.NodeID)
		weightDiff, err := diffValidator.WeightDiff()

		require.NoError(err)
		require.Equal(false, weightDiff.Decrease)
		require.Equal(mutatedStaker.Weight+delegator.Weight, weightDiff.Amount)
	}

	{
		v.DeleteDelegator(delegator)
		diffValidator := v.getOrCreateValidatorDiff(staker.SubnetID, staker.NodeID)
		weightDiff, err := diffValidator.WeightDiff()

		require.NoError(err)
		require.Equal(false, weightDiff.Decrease)
		require.Equal(mutatedStaker.Weight, weightDiff.Amount)
	}

	maps.Clear(v.validatorDiffs)

	{
		v.DeleteDelegator(delegator)

		diffValidator := v.getOrCreateValidatorDiff(staker.SubnetID, staker.NodeID)
		weightDiff, err := diffValidator.WeightDiff()

		require.NoError(err)
		require.Equal(true, weightDiff.Decrease)
		require.Equal(delegator.Weight, weightDiff.Amount)
	}

	{
		v.DeleteValidator(staker)

		diffValidator := v.getOrCreateValidatorDiff(staker.SubnetID, staker.NodeID)
		weightDiff, err := diffValidator.WeightDiff()

		require.NoError(err)
		require.Equal(true, weightDiff.Decrease)
		require.Equal(staker.Weight+delegator.Weight, weightDiff.Amount)
	}

	{
		staker = newTestStaker(t)
		v.PutValidator(staker)
		v.DeleteValidator(staker)

		diffValidator := v.getOrCreateValidatorDiff(staker.SubnetID, staker.NodeID)
		weightDiff, err := diffValidator.WeightDiff()

		require.NoError(err)
		require.Equal(true, weightDiff.Decrease)
		require.Equal(staker.Weight, weightDiff.Amount)
	}

	{
		staker = newTestStaker(t)
		v.PutValidator(staker)

		mutatedStaker := *staker
		mutatedStaker.Weight += 10
		v.UpdateValidator(staker)
		v.DeleteValidator(staker)

		diffValidator := v.getOrCreateValidatorDiff(staker.SubnetID, staker.NodeID)
		weightDiff, err := diffValidator.WeightDiff()

		require.NoError(err)
		require.Equal(true, weightDiff.Decrease)
		require.Equal(staker.Weight, weightDiff.Amount)
	}
}

func TestDiffStakersWeightDiff(t *testing.T) {
	require := require.New(t)

	staker := newTestStaker(t)
	delegator := newTestStaker(t)
	delegator.SubnetID = staker.SubnetID
	delegator.NodeID = staker.NodeID

	mutatedStaker := *staker
	mutatedStaker.Weight += 10

	v := diffStakers{}

	{
		require.NoError(v.PutValidator(staker))
		diffValidator := v.getOrCreateDiff(staker.SubnetID, staker.NodeID)
		weightDiff, err := diffValidator.WeightDiff()

		require.NoError(err)
		require.Equal(false, weightDiff.Decrease)
		require.Equal(staker.Weight, weightDiff.Amount)
	}

	{
		v.PutDelegator(delegator)
		diffValidator := v.getOrCreateDiff(staker.SubnetID, staker.NodeID)
		weightDiff, err := diffValidator.WeightDiff()

		require.NoError(err)
		require.Equal(false, weightDiff.Decrease)
		require.Equal(staker.Weight+delegator.Weight, weightDiff.Amount)
	}

	{
		v.UpdateValidator(staker, &mutatedStaker)
		diffValidator := v.getOrCreateDiff(mutatedStaker.SubnetID, mutatedStaker.NodeID)
		weightDiff, err := diffValidator.WeightDiff()

		require.NoError(err)
		require.Equal(false, weightDiff.Decrease)
		require.Equal(mutatedStaker.Weight+delegator.Weight, weightDiff.Amount)
	}

	{
		v.DeleteDelegator(delegator)
		diffValidator := v.getOrCreateDiff(staker.SubnetID, staker.NodeID)
		weightDiff, err := diffValidator.WeightDiff()

		require.NoError(err)
		require.Equal(false, weightDiff.Decrease)
		require.Equal(mutatedStaker.Weight, weightDiff.Amount)
	}
}

func newTestStaker(t testing.TB) *Staker {
	startTime := time.Now().Round(time.Second)
	endTime := startTime.Add(genesistest.DefaultValidatorDuration)

	blsKey, err := localsigner.New()
	require.NoError(t, err)

	return &Staker{
		TxID:            ids.GenerateTestID(),
		PublicKey:       blsKey.PublicKey(),
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
