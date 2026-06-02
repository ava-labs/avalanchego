// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

func TestBaseStakersPruning(t *testing.T) {
	require := require.New(t)
	staker := newTestStaker(constants.PrimaryNetworkID, ids.GenerateTestNodeID())
	delegator := newTestStaker(constants.PrimaryNetworkID, staker.NodeID)

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
	staker := newTestStaker(constants.PrimaryNetworkID, ids.GenerateTestNodeID())
	delegator := newTestStaker(constants.PrimaryNetworkID, ids.GenerateTestNodeID())

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
	staker := newTestStaker(constants.PrimaryNetworkID, ids.GenerateTestNodeID())
	delegator := newTestStaker(constants.PrimaryNetworkID, ids.GenerateTestNodeID())

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
	staker := newTestStaker(ids.GenerateTestID(), ids.GenerateTestNodeID())

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
	staker := newTestStaker(ids.GenerateTestID(), ids.GenerateTestNodeID())

	endTime := staker.EndTime.Add(genesistest.DefaultValidatorDuration)

	modifiedStaker := *staker
	modifiedStaker.Weight++
	modifiedStaker.EndTime = endTime

	diff := diffStakers{isAdditionAfterDeletionAllowed: StakerAdditionAfterDeletionAllowed}
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
	v1 := newTestStaker(ids.GenerateTestID(), ids.GenerateTestNodeID())

	v1Prime := *v1
	v1Prime.Weight++

	diff := diffStakers{isAdditionAfterDeletionAllowed: StakerAdditionAfterDeletionAllowed}

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

	// Verify that v1 is filtered out from a parent iterator that contains it.
	// This ensures the deletion is tracked in deletedStakers, not just in the
	// validator diff's removed field.
	parentIterator := iterator.FromSlice(v1)
	stakers := iterator.ToSlice(diff.GetStakerIterator(parentIterator))
	require.Empty(stakers, "v1 should be filtered from parent after delete-add-delete")
}

func TestDiffStakersDeleteThenReAddSameValidator(t *testing.T) {
	require := require.New(t)
	v1 := newTestStaker(ids.GenerateTestID(), ids.GenerateTestNodeID())

	diff := diffStakers{isAdditionAfterDeletionAllowed: StakerAdditionAfterDeletionAllowed}

	// Delete v1 (simulating removal of an existing validator from base state)
	diff.DeleteValidator(v1)
	_, status := diff.GetValidator(v1.SubnetID, v1.NodeID)
	require.Equal(deleted, status)

	// Re-add the exact same validator. The delete and add should cancel out.
	require.NoError(diff.PutValidator(v1))

	// The net effect should be: the validator is unmodified.
	_, status = diff.GetValidator(v1.SubnetID, v1.NodeID)
	require.Equal(unmodified, status, "delete then re-add of same validator should cancel out")

	// The validator should not appear in addedStakers.
	require.False(existsInDiff(&diff, v1), "validator should not be in addedStakers after cancellation")

	// The validator should not be filtered from the parent iterator either,
	// since the deletion was cancelled.
	parentIterator := iterator.FromSlice(v1)
	stakers := iterator.ToSlice(diff.GetStakerIterator(parentIterator))
	require.Equal([]*Staker{v1}, stakers, "validator should still come through from parent")
}

func TestDiffValidatorWeightDiffAfterDeleteAndAdd(t *testing.T) {
	require := require.New(t)
	staker := newTestStaker(ids.GenerateTestID(), ids.GenerateTestNodeID())
	staker.Weight = 5

	modifiedStaker := *staker
	modifiedStaker.Weight = 10

	diff := diffStakers{isAdditionAfterDeletionAllowed: StakerAdditionAfterDeletionAllowed}

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
	staker := newTestStaker(constants.PrimaryNetworkID, ids.GenerateTestNodeID())
	delegator := newTestStaker(constants.PrimaryNetworkID, ids.GenerateTestNodeID())

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
	staker := newTestStaker(constants.PrimaryNetworkID, ids.GenerateTestNodeID())
	delegator := newTestStaker(constants.PrimaryNetworkID, ids.GenerateTestNodeID())

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
	staker := newTestStaker(constants.PrimaryNetworkID, ids.GenerateTestNodeID())
	delegator := newTestStaker(constants.PrimaryNetworkID, ids.GenerateTestNodeID())

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

func TestDiffDelegatorCancelOut(t *testing.T) {
	setupState := func(t *testing.T) (*State, *Staker) {
		t.Helper()
		state := newTestState(t, memdb.New())
		subnetID := ids.GenerateTestID()
		nodeID := ids.GenerateTestNodeID()
		require.NoError(t, state.PutCurrentValidator(newTestStaker(subnetID, nodeID)))
		return state, newTestStaker(subnetID, nodeID)
	}

	t.Run("delete_then_put_identical_delegator", func(t *testing.T) {
		state, delegator := setupState(t)
		require.NoError(t, state.PutCurrentDelegator(delegator))

		diff, err := NewDiffOn(state, StakerAdditionAfterDeletionAllowed)
		require.NoError(t, err)
		require.NoError(t, diff.DeleteCurrentDelegator(delegator))
		require.NoError(t, diff.PutCurrentDelegator(delegator))
		require.NoError(t, diff.Apply(state))

		require.Equal(t, []*Staker{delegator}, currentDelegators(t, state, delegator))
	})

	t.Run("delete_then_put_updated_weight", func(t *testing.T) {
		state, delegator := setupState(t)
		delegator.Weight = 2
		require.NoError(t, state.PutCurrentDelegator(delegator))

		updated := *delegator
		updated.Weight = delegator.Weight - 1

		diff, err := NewDiffOn(state, StakerAdditionAfterDeletionAllowed)
		require.NoError(t, err)
		require.NoError(t, diff.DeleteCurrentDelegator(delegator))
		require.NoError(t, diff.PutCurrentDelegator(&updated))
		require.NoError(t, diff.Apply(state))

		require.Equal(t, []*Staker{&updated}, currentDelegators(t, state, delegator))
	})

	t.Run("put_then_delete_identical", func(t *testing.T) {
		state, delegator := setupState(t)

		diff, err := NewDiffOn(state, StakerAdditionAfterDeletionAllowed)
		require.NoError(t, err)
		require.NoError(t, diff.PutCurrentDelegator(delegator))
		require.NoError(t, diff.DeleteCurrentDelegator(delegator))
		require.NoError(t, diff.Apply(state))

		require.Empty(t, currentDelegators(t, state, delegator))
	})

	t.Run("put_then_delete_mismatched_weight", func(t *testing.T) {
		state, delegator := setupState(t)
		delegator.Weight = 2

		mismatched := *delegator
		mismatched.Weight = delegator.Weight - 1

		diff, err := NewDiffOn(state, StakerAdditionAfterDeletionAllowed)
		require.NoError(t, err)
		require.NoError(t, diff.PutCurrentDelegator(delegator))
		require.NoError(t, diff.DeleteCurrentDelegator(&mismatched))
		require.NoError(t, diff.Apply(state))

		require.Empty(t, currentDelegators(t, state, delegator))
	})

	t.Run("pending_delete_then_put_identical", func(t *testing.T) {
		state, delegator := setupState(t)
		state.PutPendingDelegator(delegator)

		diff, err := NewDiffOn(state, StakerAdditionAfterDeletionAllowed)
		require.NoError(t, err)
		diff.DeletePendingDelegator(delegator)
		diff.PutPendingDelegator(delegator)
		require.NoError(t, diff.Apply(state))

		require.Equal(t, []*Staker{delegator}, pendingDelegators(t, state, delegator))
	})
}

// TestStateDelegatorCancelOutCommitAndLoad verifies that deleting and re-adding
// a delegator within a single commit cycle persists correctly across a reload from disk.
func TestStateDelegatorCancelOutCommitAndLoad(t *testing.T) {
	newStateAndDelegator := func(t *testing.T, pending bool) (state *State, db database.Database, delegator *Staker) {
		t.Helper()
		db = memdb.New()
		state = newTestState(t, db)

		startTime := time.Now().Truncate(time.Second)
		endTime := startTime.Add(genesistest.DefaultValidatorDuration)

		unsignedDelegator := createPermissionlessDelegatorTx(constants.PrimaryNetworkID, txs.Validator{
			NodeID: defaultValidatorNodeID,
			End:    uint64(endTime.Unix()),
			Wght:   6789,
		})
		delegatorTx := &txs.Tx{Unsigned: unsignedDelegator}
		require.NoError(t, delegatorTx.Initialize(txs.Codec))
		state.AddTx(delegatorTx, status.Committed)

		var err error
		if pending {
			delegator, err = NewPendingStaker(delegatorTx.ID(), unsignedDelegator)
		} else {
			// non-zero reward so the reload assertions validate reward persistence
			delegator, err = NewCurrentStaker(delegatorTx.ID(), unsignedDelegator, startTime, 100)
		}
		require.NoError(t, err)
		return state, db, delegator
	}

	t.Run("delete_then_put_identical", func(t *testing.T) {
		state, db, delegator := newStateAndDelegator(t, false)
		require.NoError(t, state.PutCurrentDelegator(delegator))
		state.SetHeight(1)
		require.NoError(t, state.Commit())

		// Delete then re-add the identical delegator in one commit cycle.
		require.NoError(t, state.DeleteCurrentDelegator(delegator))
		require.NoError(t, state.PutCurrentDelegator(delegator))
		state.SetHeight(2)
		require.NoError(t, state.Commit())

		state = newTestState(t, db)
		require.Equal(t, []*Staker{delegator}, currentDelegators(t, state, delegator))
	})

	t.Run("put_then_delete_identical", func(t *testing.T) {
		state, db, delegator := newStateAndDelegator(t, false)

		// Put then delete the identical delegator in one commit cycle.
		require.NoError(t, state.PutCurrentDelegator(delegator))
		require.NoError(t, state.DeleteCurrentDelegator(delegator))
		state.SetHeight(1)
		require.NoError(t, state.Commit())

		state = newTestState(t, db)
		require.Empty(t, currentDelegators(t, state, delegator))
	})

	t.Run("delete_then_put_updated_reward", func(t *testing.T) {
		state, db, delegator := newStateAndDelegator(t, false)
		require.NoError(t, state.PutCurrentDelegator(delegator))
		state.SetHeight(1)
		require.NoError(t, state.Commit())

		updated := *delegator
		updated.PotentialReward = delegator.PotentialReward + 1

		require.NoError(t, state.DeleteCurrentDelegator(delegator))
		require.NoError(t, state.PutCurrentDelegator(&updated))
		state.SetHeight(2)
		require.NoError(t, state.Commit())

		state = newTestState(t, db)
		require.Equal(t, []*Staker{&updated}, currentDelegators(t, state, delegator))
	})

	t.Run("pending_delete_then_put_identical", func(t *testing.T) {
		state, db, delegator := newStateAndDelegator(t, true)
		state.PutPendingDelegator(delegator)
		state.SetHeight(1)
		require.NoError(t, state.Commit())

		// Delete then re-add the identical delegator in one commit cycle.
		state.DeletePendingDelegator(delegator)
		state.PutPendingDelegator(delegator)
		state.SetHeight(2)
		require.NoError(t, state.Commit())

		state = newTestState(t, db)
		require.Equal(t, []*Staker{delegator}, pendingDelegators(t, state, delegator))
	})

	t.Run("pending_delete_then_put_mismatched", func(t *testing.T) {
		state, db, delegator := newStateAndDelegator(t, true)
		state.PutPendingDelegator(delegator)
		state.SetHeight(1)
		require.NoError(t, state.Commit())

		// Differing weight prevents the cancel-out, so the diff holds both a
		// delete and an add for the same txID.
		mismatched := *delegator
		mismatched.Weight++
		state.DeletePendingDelegator(delegator)
		state.PutPendingDelegator(&mismatched)
		state.SetHeight(2)
		require.NoError(t, state.Commit())

		// We expect the original delegator, not mismatched: pending delegators
		// persist only the txID, so on reload the staker
		// is rebuilt entirely from the unchanged tx.
		state = newTestState(t, db)
		require.Equal(t, []*Staker{delegator}, pendingDelegators(t, state, delegator))
	})
}

func currentDelegators(t *testing.T, state *State, staker *Staker) []*Staker {
	t.Helper()
	it, err := state.GetCurrentDelegatorIterator(staker.SubnetID, staker.NodeID)
	require.NoError(t, err)
	defer it.Release()
	return iterator.ToSlice(it)
}

func pendingDelegators(t *testing.T, state *State, staker *Staker) []*Staker {
	t.Helper()
	it, err := state.GetPendingDelegatorIterator(staker.SubnetID, staker.NodeID)
	require.NoError(t, err)
	defer it.Release()
	return iterator.ToSlice(it)
}

func newTestStaker(subnetID ids.ID, nodeID ids.NodeID) *Staker {
	startTime := time.Time{}
	endTime := startTime.Add(genesistest.DefaultValidatorDuration)
	return &Staker{
		TxID:            ids.GenerateTestID(),
		NodeID:          nodeID,
		SubnetID:        subnetID,
		Weight:          1,
		StartTime:       startTime,
		EndTime:         endTime,
		PotentialReward: 1,

		NextTime: endTime,
		Priority: txs.PrimaryNetworkDelegatorCurrentPriority,
	}
}

func TestStakerEquals(t *testing.T) {
	// If this constant is wrong, a field was likely added to Staker without
	// updating the Equals method. Update Equals to compare the new field,
	// then fix this constant.
	const expectedStakerFieldCount = 10

	require.Equal(t,
		expectedStakerFieldCount,
		reflect.TypeOf(Staker{}).NumField(),
		"Staker struct field count changed; update Staker.Equals and this test",
	)

	signer1, err := localsigner.New()
	require.NoError(t, err)
	signer2, err := localsigner.New()
	require.NoError(t, err)

	pk1 := signer1.PublicKey()
	pk2 := signer2.PublicKey()

	now := time.Now().Round(time.Second)
	staker := &Staker{
		TxID:            ids.GenerateTestID(),
		NodeID:          ids.GenerateTestNodeID(),
		PublicKey:       pk1,
		SubnetID:        ids.GenerateTestID(),
		Weight:          100,
		StartTime:       now,
		EndTime:         now.Add(time.Hour),
		PotentialReward: 50,
		NextTime:        now.Add(time.Hour),
		Priority:        txs.PrimaryNetworkValidatorCurrentPriority,
	}

	// Test nil handling
	var nilStaker *Staker
	require.True(t, nilStaker.Equals(nil))
	require.False(t, nilStaker.Equals(staker))
	require.False(t, staker.Equals(nil))

	// Test identical stakers
	identical := *staker
	require.True(t, staker.Equals(&identical))

	// Test both public keys nil
	noPK1 := *staker
	noPK2 := *staker
	noPK1.PublicKey = nil
	noPK2.PublicKey = nil
	require.True(t, noPK1.Equals(&noPK2))

	// Test one public key nil
	onePKNil := *staker
	onePKNil.PublicKey = nil
	require.False(t, staker.Equals(&onePKNil))
	require.False(t, onePKNil.Equals(staker))

	// Test that each field is actually compared by Equals.
	type fieldMutation struct {
		name   string
		mutate func(s *Staker)
	}
	mutations := []fieldMutation{
		{"TxID", func(s *Staker) { s.TxID = ids.GenerateTestID() }},
		{"NodeID", func(s *Staker) { s.NodeID = ids.GenerateTestNodeID() }},
		{"PublicKey", func(s *Staker) { s.PublicKey = pk2 }},
		{"SubnetID", func(s *Staker) { s.SubnetID = ids.GenerateTestID() }},
		{"Weight", func(s *Staker) { s.Weight++ }},
		{"StartTime", func(s *Staker) { s.StartTime = s.StartTime.Add(time.Second) }},
		{"EndTime", func(s *Staker) { s.EndTime = s.EndTime.Add(time.Second) }},
		{"PotentialReward", func(s *Staker) { s.PotentialReward++ }},
		{"NextTime", func(s *Staker) { s.NextTime = s.NextTime.Add(time.Second) }},
		{"Priority", func(s *Staker) { s.Priority++ }},
	}

	require.Len(t, mutations, expectedStakerFieldCount,
		"each Staker field must have a corresponding mutation entry in this test",
	)

	for _, m := range mutations {
		t.Run("different "+m.name, func(t *testing.T) {
			other := *staker
			m.mutate(&other)
			require.False(t, staker.Equals(&other))
		})
	}
}

func TestGetStakerIteratorDeleteAndPut(t *testing.T) {
	// When a validator from the parent state is deleted and then replaced,
	// GetStakerIterator returns only the replacement — not both.
	tests := []struct {
		name            string
		makeReplacement func(*Staker) *Staker
	}{
		{
			name: "same TxID",
			makeReplacement: func(original *Staker) *Staker {
				replacement := *original
				replacement.Weight = 2
				return &replacement
			},
		},
		{
			name: "different TxID",
			makeReplacement: func(original *Staker) *Staker {
				replacement := *original
				replacement.TxID = ids.GenerateTestID()
				replacement.Weight = original.Weight + 10
				replacement.EndTime = original.EndTime.Add(time.Hour)
				replacement.NextTime = replacement.EndTime
				return &replacement
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			staker := newTestStaker(ids.GenerateTestID(), ids.GenerateTestNodeID())

			base := newBaseStakers()
			base.PutValidator(staker)

			diff := diffStakers{isAdditionAfterDeletionAllowed: StakerAdditionAfterDeletionAllowed}
			replacement := tt.makeReplacement(staker)

			diff.DeleteValidator(staker)
			require.NoError(diff.PutValidator(replacement))

			stakers := iterator.ToSlice(diff.GetStakerIterator(base.GetStakerIterator()))
			require.Equal([]*Staker{replacement}, stakers)
		})
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
