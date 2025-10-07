// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx/fxmock"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

type nilStateGetter struct{}

func (nilStateGetter) GetState(ids.ID) (Chain, bool) {
	return nil, false
}

func TestDiffMissingState(t *testing.T) {
	parentID := ids.GenerateTestID()
	_, err := NewDiff(parentID, nilStateGetter{})
	require.ErrorIs(t, err, ErrMissingParentState)
}

func TestMutatedValidatorDiffState(t *testing.T) {
	require := require.New(t)

	blsKey, err := localsigner.New()
	require.NoError(err)

	state := newTestState(t, memdb.New())

	// Put a current validator
	currentValidator := &Staker{
		TxID:               ids.GenerateTestID(),
		PublicKey:          blsKey.PublicKey(),
		SubnetID:           ids.GenerateTestID(),
		NodeID:             ids.GenerateTestNodeID(),
		Weight:             100,
		ContinuationPeriod: 100 * time.Second,
	}
	require.NoError(state.PutCurrentValidator(currentValidator))

	d, err := NewDiffOn(state)
	require.NoError(err)

	staker, err := d.GetCurrentValidator(currentValidator.SubnetID, currentValidator.NodeID)
	require.NoError(err)
	require.Equal(100*time.Second, staker.ContinuationPeriod)

	err = d.StopContinuousValidator(staker.SubnetID, staker.NodeID)
	require.NoError(err)

	stakerAgain, err := d.GetCurrentValidator(currentValidator.SubnetID, currentValidator.NodeID)
	require.NoError(err)
	require.Equal(time.Duration(0), stakerAgain.ContinuationPeriod)

	stateStaker, err := state.GetCurrentValidator(currentValidator.SubnetID, currentValidator.NodeID)
	require.NoError(err)
	require.Equal(100*time.Second, stateStaker.ContinuationPeriod)
}

func TestNewDiffOn(t *testing.T) {
	require := require.New(t)

	state := newTestState(t, memdb.New())

	d, err := NewDiffOn(state)
	require.NoError(err)

	assertChainsEqual(t, state, d)
}

func TestDiffFeeState(t *testing.T) {
	require := require.New(t)

	state := newTestState(t, memdb.New())

	d, err := NewDiffOn(state)
	require.NoError(err)

	initialFeeState := state.GetFeeState()
	newFeeState := gas.State{
		Capacity: initialFeeState.Capacity + 1,
		Excess:   initialFeeState.Excess + 1,
	}
	d.SetFeeState(newFeeState)
	require.Equal(newFeeState, d.GetFeeState())
	require.Equal(initialFeeState, state.GetFeeState())

	require.NoError(d.Apply(state))
	assertChainsEqual(t, state, d)
}

func TestDiffL1ValidatorExcess(t *testing.T) {
	require := require.New(t)

	state := newTestState(t, memdb.New())

	d, err := NewDiffOn(state)
	require.NoError(err)

	initialExcess := state.GetL1ValidatorExcess()
	newExcess := initialExcess + 1
	d.SetL1ValidatorExcess(newExcess)
	require.Equal(newExcess, d.GetL1ValidatorExcess())
	require.Equal(initialExcess, state.GetL1ValidatorExcess())

	require.NoError(d.Apply(state))
	assertChainsEqual(t, state, d)
}

func TestDiffAccruedFees(t *testing.T) {
	require := require.New(t)

	state := newTestState(t, memdb.New())

	d, err := NewDiffOn(state)
	require.NoError(err)

	initialAccruedFees := state.GetAccruedFees()
	newAccruedFees := initialAccruedFees + 1
	d.SetAccruedFees(newAccruedFees)
	require.Equal(newAccruedFees, d.GetAccruedFees())
	require.Equal(initialAccruedFees, state.GetAccruedFees())

	require.NoError(d.Apply(state))
	assertChainsEqual(t, state, d)
}

func TestDiffCurrentSupply(t *testing.T) {
	require := require.New(t)

	state := newTestState(t, memdb.New())

	d, err := NewDiffOn(state)
	require.NoError(err)

	initialCurrentSupply, err := d.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(err)

	newCurrentSupply := initialCurrentSupply + 1
	d.SetCurrentSupply(constants.PrimaryNetworkID, newCurrentSupply)

	returnedNewCurrentSupply, err := d.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(err)
	require.Equal(newCurrentSupply, returnedNewCurrentSupply)

	returnedBaseCurrentSupply, err := state.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(err)
	require.Equal(initialCurrentSupply, returnedBaseCurrentSupply)

	require.NoError(d.Apply(state))
	assertChainsEqual(t, state, d)
}

func TestDiffExpiry(t *testing.T) {
	type op struct {
		put   bool
		entry ExpiryEntry
	}
	tests := []struct {
		name            string
		initialExpiries []ExpiryEntry
		ops             []op
	}{
		{
			name: "empty noop",
		},
		{
			name: "insert",
			ops: []op{
				{
					put:   true,
					entry: ExpiryEntry{Timestamp: 1},
				},
			},
		},
		{
			name: "remove",
			initialExpiries: []ExpiryEntry{
				{Timestamp: 1},
			},
			ops: []op{
				{
					put:   false,
					entry: ExpiryEntry{Timestamp: 1},
				},
			},
		},
		{
			name: "add and immediately remove",
			ops: []op{
				{
					put:   true,
					entry: ExpiryEntry{Timestamp: 1},
				},
				{
					put:   false,
					entry: ExpiryEntry{Timestamp: 1},
				},
			},
		},
		{
			name: "add + remove + add",
			ops: []op{
				{
					put:   true,
					entry: ExpiryEntry{Timestamp: 1},
				},
				{
					put:   false,
					entry: ExpiryEntry{Timestamp: 1},
				},
				{
					put:   true,
					entry: ExpiryEntry{Timestamp: 1},
				},
			},
		},
		{
			name: "everything",
			initialExpiries: []ExpiryEntry{
				{Timestamp: 1},
				{Timestamp: 2},
				{Timestamp: 3},
			},
			ops: []op{
				{
					put:   false,
					entry: ExpiryEntry{Timestamp: 1},
				},
				{
					put:   false,
					entry: ExpiryEntry{Timestamp: 2},
				},
				{
					put:   true,
					entry: ExpiryEntry{Timestamp: 1},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			state := newTestState(t, memdb.New())
			for _, expiry := range test.initialExpiries {
				state.PutExpiry(expiry)
			}

			d, err := NewDiffOn(state)
			require.NoError(err)

			var (
				expectedExpiries   = set.Of(test.initialExpiries...)
				unexpectedExpiries set.Set[ExpiryEntry]
			)
			for _, op := range test.ops {
				if op.put {
					d.PutExpiry(op.entry)
					expectedExpiries.Add(op.entry)
					unexpectedExpiries.Remove(op.entry)
				} else {
					d.DeleteExpiry(op.entry)
					expectedExpiries.Remove(op.entry)
					unexpectedExpiries.Add(op.entry)
				}
			}

			// If expectedExpiries is empty, we want expectedExpiriesSlice to be
			// nil.
			var expectedExpiriesSlice []ExpiryEntry
			if expectedExpiries.Len() > 0 {
				expectedExpiriesSlice = expectedExpiries.List()
				utils.Sort(expectedExpiriesSlice)
			}

			verifyChain := func(chain Chain) {
				expiryIterator, err := chain.GetExpiryIterator()
				require.NoError(err)
				require.Equal(
					expectedExpiriesSlice,
					iterator.ToSlice(expiryIterator),
				)

				for expiry := range expectedExpiries {
					has, err := chain.HasExpiry(expiry)
					require.NoError(err)
					require.True(has)
				}
				for expiry := range unexpectedExpiries {
					has, err := chain.HasExpiry(expiry)
					require.NoError(err)
					require.False(has)
				}
			}

			verifyChain(d)
			require.NoError(d.Apply(state))
			verifyChain(state)
			assertChainsEqual(t, d, state)
		})
	}
}

func TestDiffL1ValidatorsErrors(t *testing.T) {
	l1Validator := L1Validator{
		ValidationID: ids.GenerateTestID(),
		SubnetID:     ids.GenerateTestID(),
		NodeID:       ids.GenerateTestNodeID(),
		Weight:       1, // Not removed
	}

	tests := []struct {
		name                     string
		initialEndAccumulatedFee uint64
		l1Validator              L1Validator
		expectedErr              error
	}{
		{
			name:                     "mutate active constants",
			initialEndAccumulatedFee: 1,
			l1Validator: L1Validator{
				ValidationID: l1Validator.ValidationID,
				NodeID:       ids.GenerateTestNodeID(),
			},
			expectedErr: ErrMutatedL1Validator,
		},
		{
			name:                     "mutate inactive constants",
			initialEndAccumulatedFee: 0,
			l1Validator: L1Validator{
				ValidationID: l1Validator.ValidationID,
				NodeID:       ids.GenerateTestNodeID(),
			},
			expectedErr: ErrMutatedL1Validator,
		},
		{
			name:                     "conflicting legacy subnetID and nodeID pair",
			initialEndAccumulatedFee: 1,
			l1Validator: L1Validator{
				ValidationID: ids.GenerateTestID(),
				NodeID:       defaultValidatorNodeID,
			},
			expectedErr: ErrConflictingL1Validator,
		},
		{
			name:                     "duplicate active subnetID and nodeID pair",
			initialEndAccumulatedFee: 1,
			l1Validator: L1Validator{
				ValidationID: ids.GenerateTestID(),
				NodeID:       l1Validator.NodeID,
			},
			expectedErr: ErrDuplicateL1Validator,
		},
		{
			name:                     "duplicate inactive subnetID and nodeID pair",
			initialEndAccumulatedFee: 0,
			l1Validator: L1Validator{
				ValidationID: ids.GenerateTestID(),
				NodeID:       l1Validator.NodeID,
			},
			expectedErr: ErrDuplicateL1Validator,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			state := newTestState(t, memdb.New())

			require.NoError(state.PutCurrentValidator(&Staker{
				TxID:     ids.GenerateTestID(),
				SubnetID: l1Validator.SubnetID,
				NodeID:   defaultValidatorNodeID,
			}))

			l1Validator.EndAccumulatedFee = test.initialEndAccumulatedFee
			require.NoError(state.PutL1Validator(l1Validator))

			d, err := NewDiffOn(state)
			require.NoError(err)

			// Initialize subnetID, weight, and endAccumulatedFee as they are
			// constant among all tests.
			test.l1Validator.SubnetID = l1Validator.SubnetID
			test.l1Validator.Weight = 1                        // Not removed
			test.l1Validator.EndAccumulatedFee = rand.Uint64() //#nosec G404
			err = d.PutL1Validator(test.l1Validator)
			require.ErrorIs(err, test.expectedErr)

			// The invalid addition should not have modified the diff.
			assertChainsEqual(t, state, d)
		})
	}
}

func TestDiffCurrentValidator(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	state := NewMockState(ctrl)
	// Called in NewDiffOn
	state.EXPECT().GetTimestamp().Return(time.Now()).Times(1)
	state.EXPECT().GetFeeState().Return(gas.State{}).Times(1)
	state.EXPECT().GetL1ValidatorExcess().Return(gas.Gas(0)).Times(1)
	state.EXPECT().GetAccruedFees().Return(uint64(0)).Times(1)
	state.EXPECT().NumActiveL1Validators().Return(0).Times(1)

	d, err := NewDiffOn(state)
	require.NoError(err)

	// Put a current validator
	currentValidator := &Staker{
		TxID:     ids.GenerateTestID(),
		SubnetID: ids.GenerateTestID(),
		NodeID:   ids.GenerateTestNodeID(),
	}
	require.NoError(d.PutCurrentValidator(currentValidator))

	// Assert that we get the current validator back
	gotCurrentValidator, err := d.GetCurrentValidator(currentValidator.SubnetID, currentValidator.NodeID)
	require.NoError(err)
	require.Equal(currentValidator, gotCurrentValidator)

	// Delete the current validator
	d.DeleteCurrentValidator(currentValidator)

	// Make sure the deletion worked
	state.EXPECT().GetCurrentValidator(currentValidator.SubnetID, currentValidator.NodeID).Return(nil, database.ErrNotFound).Times(1)
	_, err = d.GetCurrentValidator(currentValidator.SubnetID, currentValidator.NodeID)
	require.ErrorIs(err, database.ErrNotFound)
}

func TestDiffPendingValidator(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	state := NewMockState(ctrl)
	// Called in NewDiffOn
	state.EXPECT().GetTimestamp().Return(time.Now()).Times(1)
	state.EXPECT().GetFeeState().Return(gas.State{}).Times(1)
	state.EXPECT().GetL1ValidatorExcess().Return(gas.Gas(0)).Times(1)
	state.EXPECT().GetAccruedFees().Return(uint64(0)).Times(1)
	state.EXPECT().NumActiveL1Validators().Return(0).Times(1)

	d, err := NewDiffOn(state)
	require.NoError(err)

	// Put a pending validator
	pendingValidator := &Staker{
		TxID:     ids.GenerateTestID(),
		SubnetID: ids.GenerateTestID(),
		NodeID:   ids.GenerateTestNodeID(),
	}
	require.NoError(d.PutPendingValidator(pendingValidator))

	// Assert that we get the pending validator back
	gotPendingValidator, err := d.GetPendingValidator(pendingValidator.SubnetID, pendingValidator.NodeID)
	require.NoError(err)
	require.Equal(pendingValidator, gotPendingValidator)

	// Delete the pending validator
	d.DeletePendingValidator(pendingValidator)

	// Make sure the deletion worked
	state.EXPECT().GetPendingValidator(pendingValidator.SubnetID, pendingValidator.NodeID).Return(nil, database.ErrNotFound).Times(1)
	_, err = d.GetPendingValidator(pendingValidator.SubnetID, pendingValidator.NodeID)
	require.ErrorIs(err, database.ErrNotFound)
}

func TestDiffCurrentDelegator(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	currentDelegator := &Staker{
		TxID:     ids.GenerateTestID(),
		SubnetID: ids.GenerateTestID(),
		NodeID:   ids.GenerateTestNodeID(),
	}

	state := NewMockState(ctrl)
	// Called in NewDiffOn
	state.EXPECT().GetTimestamp().Return(time.Now()).Times(1)
	state.EXPECT().GetFeeState().Return(gas.State{}).Times(1)
	state.EXPECT().GetL1ValidatorExcess().Return(gas.Gas(0)).Times(1)
	state.EXPECT().GetAccruedFees().Return(uint64(0)).Times(1)
	state.EXPECT().NumActiveL1Validators().Return(0).Times(1)

	d, err := NewDiffOn(state)
	require.NoError(err)

	// Put a current delegator
	d.PutCurrentDelegator(currentDelegator)

	// Assert that we get the current delegator back
	// Mock iterator for [state] returns no delegators.
	state.EXPECT().GetCurrentDelegatorIterator(
		currentDelegator.SubnetID,
		currentDelegator.NodeID,
	).Return(iterator.Empty[*Staker]{}, nil).Times(2)
	gotCurrentDelegatorIter, err := d.GetCurrentDelegatorIterator(currentDelegator.SubnetID, currentDelegator.NodeID)
	require.NoError(err)
	// The iterator should have the 1 delegator we put in [d]
	require.True(gotCurrentDelegatorIter.Next())
	require.Equal(gotCurrentDelegatorIter.Value(), currentDelegator)

	// Delete the current delegator
	d.DeleteCurrentDelegator(currentDelegator)

	// Make sure the deletion worked.
	// The iterator should have no elements.
	gotCurrentDelegatorIter, err = d.GetCurrentDelegatorIterator(currentDelegator.SubnetID, currentDelegator.NodeID)
	require.NoError(err)
	require.False(gotCurrentDelegatorIter.Next())
}

func TestDiffPendingDelegator(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	pendingDelegator := &Staker{
		TxID:     ids.GenerateTestID(),
		SubnetID: ids.GenerateTestID(),
		NodeID:   ids.GenerateTestNodeID(),
	}

	state := NewMockState(ctrl)
	// Called in NewDiffOn
	state.EXPECT().GetTimestamp().Return(time.Now()).Times(1)
	state.EXPECT().GetFeeState().Return(gas.State{}).Times(1)
	state.EXPECT().GetL1ValidatorExcess().Return(gas.Gas(0)).Times(1)
	state.EXPECT().GetAccruedFees().Return(uint64(0)).Times(1)
	state.EXPECT().NumActiveL1Validators().Return(0).Times(1)

	d, err := NewDiffOn(state)
	require.NoError(err)

	// Put a pending delegator
	d.PutPendingDelegator(pendingDelegator)

	// Assert that we get the pending delegator back
	// Mock iterator for [state] returns no delegators.
	state.EXPECT().GetPendingDelegatorIterator(
		pendingDelegator.SubnetID,
		pendingDelegator.NodeID,
	).Return(iterator.Empty[*Staker]{}, nil).Times(2)
	gotPendingDelegatorIter, err := d.GetPendingDelegatorIterator(pendingDelegator.SubnetID, pendingDelegator.NodeID)
	require.NoError(err)
	// The iterator should have the 1 delegator we put in [d]
	require.True(gotPendingDelegatorIter.Next())
	require.Equal(gotPendingDelegatorIter.Value(), pendingDelegator)

	// Delete the pending delegator
	d.DeletePendingDelegator(pendingDelegator)

	// Make sure the deletion worked.
	// The iterator should have no elements.
	gotPendingDelegatorIter, err = d.GetPendingDelegatorIterator(pendingDelegator.SubnetID, pendingDelegator.NodeID)
	require.NoError(err)
	require.False(gotPendingDelegatorIter.Next())
}

func TestDiffSubnet(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	state := newTestState(t, memdb.New())

	// Initialize parent with one subnet
	parentStateCreateSubnetTx := &txs.Tx{
		Unsigned: &txs.CreateSubnetTx{
			Owner: fxmock.NewOwner(ctrl),
		},
	}
	state.AddSubnet(parentStateCreateSubnetTx.ID())

	// Verify parent returns one subnet
	subnetIDs, err := state.GetSubnetIDs()
	require.NoError(err)
	require.Equal(
		[]ids.ID{
			parentStateCreateSubnetTx.ID(),
		},
		subnetIDs,
	)

	diff, err := NewDiffOn(state)
	require.NoError(err)

	// Put a subnet
	createSubnetTx := &txs.Tx{
		Unsigned: &txs.CreateSubnetTx{
			Owner: fxmock.NewOwner(ctrl),
		},
	}
	diff.AddSubnet(createSubnetTx.ID())

	// Apply diff to parent state
	require.NoError(diff.Apply(state))

	// Verify parent now returns two subnets
	subnetIDs, err = state.GetSubnetIDs()
	require.NoError(err)
	require.Equal(
		[]ids.ID{
			parentStateCreateSubnetTx.ID(),
			createSubnetTx.ID(),
		},
		subnetIDs,
	)
}

func TestDiffChain(t *testing.T) {
	require := require.New(t)

	state := newTestState(t, memdb.New())
	subnetID := ids.GenerateTestID()

	// Initialize parent with one chain
	parentStateCreateChainTx := &txs.Tx{
		Unsigned: &txs.CreateChainTx{
			SubnetID: subnetID,
		},
	}
	state.AddChain(parentStateCreateChainTx)

	// Verify parent returns one chain
	chains, err := state.GetChains(subnetID)
	require.NoError(err)
	require.Equal(
		[]*txs.Tx{
			parentStateCreateChainTx,
		},
		chains,
	)

	diff, err := NewDiffOn(state)
	require.NoError(err)

	// Put a chain
	createChainTx := &txs.Tx{
		Unsigned: &txs.CreateChainTx{
			SubnetID: subnetID, // note this is the same subnet as [parentStateCreateChainTx]
		},
	}
	diff.AddChain(createChainTx)

	// Apply diff to parent state
	require.NoError(diff.Apply(state))

	// Verify parent now returns two chains
	chains, err = state.GetChains(subnetID)
	require.NoError(err)
	require.Equal(
		[]*txs.Tx{
			parentStateCreateChainTx,
			createChainTx,
		},
		chains,
	)
}

func TestDiffTx(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	state := NewMockState(ctrl)
	// Called in NewDiffOn
	state.EXPECT().GetTimestamp().Return(time.Now()).Times(1)
	state.EXPECT().GetFeeState().Return(gas.State{}).Times(1)
	state.EXPECT().GetL1ValidatorExcess().Return(gas.Gas(0)).Times(1)
	state.EXPECT().GetAccruedFees().Return(uint64(0)).Times(1)
	state.EXPECT().NumActiveL1Validators().Return(0).Times(1)

	d, err := NewDiffOn(state)
	require.NoError(err)

	// Put a tx
	subnetID := ids.GenerateTestID()
	tx := &txs.Tx{
		Unsigned: &txs.CreateChainTx{
			SubnetID: subnetID,
		},
	}
	tx.SetBytes(utils.RandomBytes(16), utils.RandomBytes(16))
	d.AddTx(tx, status.Committed)

	{
		// Assert that we get the tx back
		gotTx, gotStatus, err := d.GetTx(tx.ID())
		require.NoError(err)
		require.Equal(status.Committed, gotStatus)
		require.Equal(tx, gotTx)
	}

	{
		// Assert that we can get a tx from the parent state
		// [state] returns 1 tx.
		parentTx := &txs.Tx{
			Unsigned: &txs.CreateChainTx{
				SubnetID: subnetID,
			},
		}
		parentTx.SetBytes(utils.RandomBytes(16), utils.RandomBytes(16))
		state.EXPECT().GetTx(parentTx.ID()).Return(parentTx, status.Committed, nil).Times(1)
		gotParentTx, gotStatus, err := d.GetTx(parentTx.ID())
		require.NoError(err)
		require.Equal(status.Committed, gotStatus)
		require.Equal(parentTx, gotParentTx)
	}
}

func TestDiffRewardUTXO(t *testing.T) {
	require := require.New(t)

	state := newTestState(t, memdb.New())

	// Initialize parent with one reward UTXO
	var (
		txID             = ids.GenerateTestID()
		parentRewardUTXO = &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID: txID,
			},
		}
	)
	state.AddRewardUTXO(txID, parentRewardUTXO)

	// Verify parent returns the reward UTXO
	rewardUTXOs, err := state.GetRewardUTXOs(txID)
	require.NoError(err)
	require.Equal(
		[]*avax.UTXO{
			parentRewardUTXO,
		},
		rewardUTXOs,
	)

	diff, err := NewDiffOn(state)
	require.NoError(err)

	// Put a reward UTXO
	rewardUTXO := &avax.UTXO{
		UTXOID: avax.UTXOID{TxID: txID},
	}
	diff.AddRewardUTXO(txID, rewardUTXO)

	// Apply diff to parent state
	require.NoError(diff.Apply(state))

	// Verify parent now returns two reward UTXOs
	rewardUTXOs, err = state.GetRewardUTXOs(txID)
	require.NoError(err)
	require.Equal(
		[]*avax.UTXO{
			parentRewardUTXO,
			rewardUTXO,
		},
		rewardUTXOs,
	)
}

func TestDiffUTXO(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	state := NewMockState(ctrl)
	// Called in NewDiffOn
	state.EXPECT().GetTimestamp().Return(time.Now()).Times(1)
	state.EXPECT().GetFeeState().Return(gas.State{}).Times(1)
	state.EXPECT().GetL1ValidatorExcess().Return(gas.Gas(0)).Times(1)
	state.EXPECT().GetAccruedFees().Return(uint64(0)).Times(1)
	state.EXPECT().NumActiveL1Validators().Return(0).Times(1)

	d, err := NewDiffOn(state)
	require.NoError(err)

	// Put a UTXO
	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
	}
	d.AddUTXO(utxo)

	{
		// Assert that we get the UTXO back
		gotUTXO, err := d.GetUTXO(utxo.InputID())
		require.NoError(err)
		require.Equal(utxo, gotUTXO)
	}

	{
		// Assert that we can get a UTXO from the parent state
		// [state] returns 1 UTXO.
		parentUTXO := &avax.UTXO{
			UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
		}
		state.EXPECT().GetUTXO(parentUTXO.InputID()).Return(parentUTXO, nil).Times(1)
		gotParentUTXO, err := d.GetUTXO(parentUTXO.InputID())
		require.NoError(err)
		require.Equal(parentUTXO, gotParentUTXO)
	}

	{
		// Delete the UTXO
		d.DeleteUTXO(utxo.InputID())

		// Make sure it's gone
		_, err = d.GetUTXO(utxo.InputID())
		require.ErrorIs(err, database.ErrNotFound)
	}
}

func assertChainsEqual(t *testing.T, expected, actual Chain) {
	require := require.New(t)

	t.Helper()

	expectedExpiryIterator, expectedErr := expected.GetExpiryIterator()
	actualExpiryIterator, actualErr := actual.GetExpiryIterator()
	require.Equal(expectedErr, actualErr)
	if expectedErr == nil {
		require.Equal(
			iterator.ToSlice(expectedExpiryIterator),
			iterator.ToSlice(actualExpiryIterator),
		)
	}

	expectedActiveL1ValidatorsIterator, expectedErr := expected.GetActiveL1ValidatorsIterator()
	actualActiveL1ValidatorsIterator, actualErr := actual.GetActiveL1ValidatorsIterator()
	require.Equal(expectedErr, actualErr)
	if expectedErr == nil {
		require.Equal(
			iterator.ToSlice(expectedActiveL1ValidatorsIterator),
			iterator.ToSlice(actualActiveL1ValidatorsIterator),
		)
	}

	require.Equal(expected.NumActiveL1Validators(), actual.NumActiveL1Validators())

	expectedCurrentStakerIterator, expectedErr := expected.GetCurrentStakerIterator()
	actualCurrentStakerIterator, actualErr := actual.GetCurrentStakerIterator()
	require.Equal(expectedErr, actualErr)
	if expectedErr == nil {
		require.Equal(
			iterator.ToSlice(expectedCurrentStakerIterator),
			iterator.ToSlice(actualCurrentStakerIterator),
		)
	}

	expectedPendingStakerIterator, expectedErr := expected.GetPendingStakerIterator()
	actualPendingStakerIterator, actualErr := actual.GetPendingStakerIterator()
	require.Equal(expectedErr, actualErr)
	if expectedErr == nil {
		require.Equal(
			iterator.ToSlice(expectedPendingStakerIterator),
			iterator.ToSlice(actualPendingStakerIterator),
		)
	}

	require.Equal(expected.GetTimestamp(), actual.GetTimestamp())
	require.Equal(expected.GetFeeState(), actual.GetFeeState())
	require.Equal(expected.GetL1ValidatorExcess(), actual.GetL1ValidatorExcess())
	require.Equal(expected.GetAccruedFees(), actual.GetAccruedFees())

	expectedCurrentSupply, err := expected.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(err)

	actualCurrentSupply, err := actual.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(err)

	require.Equal(expectedCurrentSupply, actualCurrentSupply)
}

func TestDiffSubnetOwner(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	state := newTestState(t, memdb.New())

	var (
		owner1 = fxmock.NewOwner(ctrl)
		owner2 = fxmock.NewOwner(ctrl)

		createSubnetTx = &txs.Tx{
			Unsigned: &txs.CreateSubnetTx{
				BaseTx: txs.BaseTx{},
				Owner:  owner1,
			},
		}

		subnetID = createSubnetTx.ID()
	)

	// Create subnet on base state
	owner, err := state.GetSubnetOwner(subnetID)
	require.ErrorIs(err, database.ErrNotFound)
	require.Nil(owner)

	state.AddSubnet(subnetID)
	state.SetSubnetOwner(subnetID, owner1)

	owner, err = state.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner1, owner)

	// Create diff and verify that subnet owner returns correctly
	d, err := NewDiffOn(state)
	require.NoError(err)

	owner, err = d.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner1, owner)

	// Transferring subnet ownership on diff should be reflected on diff not state
	d.SetSubnetOwner(subnetID, owner2)
	owner, err = d.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner2, owner)

	owner, err = state.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner1, owner)

	// State should reflect new subnet owner after diff is applied.
	require.NoError(d.Apply(state))

	owner, err = state.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner2, owner)
}

func TestDiffSubnetToL1Conversion(t *testing.T) {
	var (
		require            = require.New(t)
		state              = newTestState(t, memdb.New())
		subnetID           = ids.GenerateTestID()
		expectedConversion = SubnetToL1Conversion{
			ConversionID: ids.GenerateTestID(),
			ChainID:      ids.GenerateTestID(),
			Addr:         []byte{1, 2, 3, 4},
		}
	)

	actualConversion, err := state.GetSubnetToL1Conversion(subnetID)
	require.ErrorIs(err, database.ErrNotFound)
	require.Zero(actualConversion)

	d, err := NewDiffOn(state)
	require.NoError(err)

	actualConversion, err = d.GetSubnetToL1Conversion(subnetID)
	require.ErrorIs(err, database.ErrNotFound)
	require.Zero(actualConversion)

	// Setting a subnet conversion should be reflected on diff not state
	d.SetSubnetToL1Conversion(subnetID, expectedConversion)
	actualConversion, err = d.GetSubnetToL1Conversion(subnetID)
	require.NoError(err)
	require.Equal(expectedConversion, actualConversion)

	actualConversion, err = state.GetSubnetToL1Conversion(subnetID)
	require.ErrorIs(err, database.ErrNotFound)
	require.Zero(actualConversion)

	// State should reflect new subnet conversion after diff is applied
	require.NoError(d.Apply(state))
	actualConversion, err = state.GetSubnetToL1Conversion(subnetID)
	require.NoError(err)
	require.Equal(expectedConversion, actualConversion)
}

func TestDiffStacking(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	state := newTestState(t, memdb.New())

	var (
		owner1 = fxmock.NewOwner(ctrl)
		owner2 = fxmock.NewOwner(ctrl)
		owner3 = fxmock.NewOwner(ctrl)

		createSubnetTx = &txs.Tx{
			Unsigned: &txs.CreateSubnetTx{
				BaseTx: txs.BaseTx{},
				Owner:  owner1,
			},
		}

		subnetID = createSubnetTx.ID()
	)

	// Create subnet on base state
	owner, err := state.GetSubnetOwner(subnetID)
	require.ErrorIs(err, database.ErrNotFound)
	require.Nil(owner)

	state.AddSubnet(subnetID)
	state.SetSubnetOwner(subnetID, owner1)

	owner, err = state.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner1, owner)

	// Create first diff and verify that subnet owner returns correctly
	statesDiff, err := NewDiffOn(state)
	require.NoError(err)

	owner, err = statesDiff.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner1, owner)

	// Transferring subnet ownership on first diff should be reflected on first diff not state
	statesDiff.SetSubnetOwner(subnetID, owner2)
	owner, err = statesDiff.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner2, owner)

	owner, err = state.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner1, owner)

	// Create a second diff on first diff and verify that subnet owner returns correctly
	stackedDiff, err := NewDiffOn(statesDiff)
	require.NoError(err)
	owner, err = stackedDiff.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner2, owner)

	// Transfer ownership on stacked diff and verify it is only reflected on stacked diff
	stackedDiff.SetSubnetOwner(subnetID, owner3)
	owner, err = stackedDiff.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner3, owner)

	owner, err = statesDiff.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner2, owner)

	owner, err = state.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner1, owner)

	// Applying both diffs successively should work as expected.
	require.NoError(stackedDiff.Apply(statesDiff))

	owner, err = statesDiff.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner3, owner)

	owner, err = state.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner1, owner)

	require.NoError(statesDiff.Apply(state))

	owner, err = state.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner3, owner)
}

func TestDiffUpdateValidator(t *testing.T) {
	tests := []struct {
		name            string
		updateValidator func(*Staker)
		updateState     func(*require.Assertions, Diff)
		expectedErr     error
	}{
		{
			name: "invalid mutation",
			updateValidator: func(validator *Staker) {
				validator.Weight = 5
			},
			expectedErr: errInvalidStakerMutation,
		},
		{
			name: "missing validator",
			updateValidator: func(validator *Staker) {
				validator.NodeID = ids.GenerateTestNodeID()
			},
			expectedErr: database.ErrNotFound,
		},
		{
			name: "deleted validator",
			updateState: func(require *require.Assertions, diff Diff) {
				currentStakerIterator, err := diff.GetCurrentStakerIterator()
				require.NoError(err)
				require.True(currentStakerIterator.Next())

				stakerToRemove := currentStakerIterator.Value()
				currentStakerIterator.Release()

				diff.DeleteCurrentValidator(stakerToRemove)
			},
			expectedErr: database.ErrNotFound,
		},
		{
			name: "valid mutation",
			updateValidator: func(validator *Staker) {
				validator.Weight = 15
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			state := newTestState(t, memdb.New())

			blsKey, err := localsigner.New()
			require.NoError(err)

			currentValidator := &Staker{
				TxID:      ids.GenerateTestID(),
				NodeID:    ids.GenerateTestNodeID(),
				PublicKey: blsKey.PublicKey(),
				SubnetID:  ids.GenerateTestID(),
				Weight:    10,
				StartTime: time.Unix(1, 0),
				EndTime:   time.Unix(2, 0),
				Priority:  txs.PrimaryNetworkValidatorCurrentPriority,
			}
			require.NoError(state.PutCurrentValidator(currentValidator))

			d, err := NewDiffOn(state)
			require.NoError(err)

			if test.updateState != nil {
				test.updateState(require, d)
			}

			validator := *currentValidator
			if test.updateValidator != nil {
				test.updateValidator(&validator)
			}

			require.ErrorIs(d.UpdateCurrentValidator(&validator), test.expectedErr)
		})
	}
}

func TestDiffStopContinuousValidator(t *testing.T) {
	require := require.New(t)

	subnetID := ids.GenerateTestID()

	state := newTestState(t, memdb.New())
	d, err := NewDiffOn(state)

	blsKey, err := localsigner.New()
	require.NoError(err)

	fixedValidator := &Staker{
		TxID:      ids.GenerateTestID(),
		NodeID:    ids.GenerateTestNodeID(),
		PublicKey: blsKey.PublicKey(),
		SubnetID:  subnetID,
		Weight:    10,
		StartTime: time.Unix(1, 0),
		EndTime:   time.Unix(2, 0),
		Priority:  txs.PrimaryNetworkValidatorCurrentPriority,
	}
	require.NoError(d.PutCurrentValidator(fixedValidator))

	require.ErrorIs(d.StopContinuousValidator(subnetID, ids.GenerateTestNodeID()), database.ErrNotFound)
	require.ErrorIs(d.StopContinuousValidator(subnetID, fixedValidator.NodeID), errIncompatibleContinuousStaker)

	blsKey, err = localsigner.New()
	require.NoError(err)

	continuousValidator := &Staker{
		TxID:               ids.GenerateTestID(),
		NodeID:             ids.GenerateTestNodeID(),
		PublicKey:          blsKey.PublicKey(),
		SubnetID:           subnetID,
		Weight:             10,
		StartTime:          time.Unix(1, 0),
		EndTime:            time.Unix(2, 0),
		Priority:           txs.PrimaryNetworkValidatorCurrentPriority,
		ContinuationPeriod: 14 * 24 * time.Hour,
	}
	require.NoError(d.PutCurrentValidator(continuousValidator))

	require.NoError(d.StopContinuousValidator(subnetID, continuousValidator.NodeID))

	validator, err := d.GetCurrentValidator(subnetID, continuousValidator.NodeID)
	require.NoError(err)

	require.Equal(continuousValidator.Weight, validator.Weight)
	require.Equal(continuousValidator.PotentialReward, validator.PotentialReward)
	require.Equal(continuousValidator.AccruedRewards, validator.AccruedRewards)
	require.Equal(continuousValidator.AccruedDelegateeRewards, validator.AccruedDelegateeRewards)
	require.Equal(continuousValidator.StartTime, validator.StartTime)
	require.Equal(continuousValidator.EndTime, validator.EndTime)
	require.Equal(time.Duration(0), validator.ContinuationPeriod)

	require.ErrorIs(d.StopContinuousValidator(subnetID, continuousValidator.NodeID), errIncompatibleContinuousStaker)
}

func TestDiffResetContinuousValidatorCycleValidation(t *testing.T) {
	require := require.New(t)

	subnetID := ids.GenerateTestID()

	state := newTestState(t, memdb.New())
	d, err := NewDiffOn(state)

	blsKey, err := localsigner.New()
	require.NoError(err)

	continuousValidator := &Staker{
		TxID:                    ids.GenerateTestID(),
		NodeID:                  ids.GenerateTestNodeID(),
		PublicKey:               blsKey.PublicKey(),
		SubnetID:                subnetID,
		Weight:                  10,
		StartTime:               time.Unix(1, 0),
		EndTime:                 time.Unix(2, 0),
		PotentialReward:         100,
		AccruedRewards:          10,
		AccruedDelegateeRewards: 5,
		NextTime:                time.Time{},
		Priority:                txs.PrimaryNetworkValidatorCurrentPriority,
		ContinuationPeriod:      14 * 24 * time.Hour,
	}
	require.NoError(d.PutCurrentValidator(continuousValidator))

	tests := []struct {
		name                                                               string
		expectedErr                                                        error
		subnetID                                                           ids.ID
		nodeID                                                             ids.NodeID
		weight                                                             uint64
		potentialReward, totalAccruedRewards, totalAccruedDelegateeRewards uint64
	}{
		{
			name:                         "decreased accrued rewards",
			subnetID:                     subnetID,
			nodeID:                       continuousValidator.NodeID,
			weight:                       continuousValidator.Weight,
			potentialReward:              continuousValidator.PotentialReward,
			totalAccruedRewards:          continuousValidator.AccruedRewards - 1,
			totalAccruedDelegateeRewards: continuousValidator.AccruedDelegateeRewards,
			expectedErr:                  errDecreasedAccruedRewards,
		},
		{
			name:                         "decreased accrued delegatee rewards",
			subnetID:                     subnetID,
			nodeID:                       continuousValidator.NodeID,
			weight:                       continuousValidator.Weight,
			potentialReward:              continuousValidator.PotentialReward,
			totalAccruedRewards:          continuousValidator.AccruedRewards,
			totalAccruedDelegateeRewards: continuousValidator.AccruedDelegateeRewards - 1,
			expectedErr:                  errDecreasedAccruedDelegateeRewards,
		},
		{
			name:                         "decreased weight",
			subnetID:                     subnetID,
			nodeID:                       continuousValidator.NodeID,
			weight:                       continuousValidator.Weight - 1,
			potentialReward:              continuousValidator.PotentialReward,
			totalAccruedRewards:          continuousValidator.AccruedRewards,
			totalAccruedDelegateeRewards: continuousValidator.AccruedDelegateeRewards,
			expectedErr:                  errDecreasedWeight,
		},
	}

	for _, test := range tests {
		err = d.ResetContinuousValidatorCycle(
			subnetID,
			test.nodeID,
			test.weight,
			test.potentialReward,
			test.totalAccruedRewards,
			test.totalAccruedDelegateeRewards,
		)

		require.ErrorIs(err, test.expectedErr)
	}
}

func TestDiffResetContinuousValidatorCycle(t *testing.T) {
	require := require.New(t)

	subnetID := ids.GenerateTestID()

	state := newTestState(t, memdb.New())
	d, err := NewDiffOn(state)

	blsKey, err := localsigner.New()
	require.NoError(err)

	continuousValidator := &Staker{
		TxID:                    ids.GenerateTestID(),
		NodeID:                  ids.GenerateTestNodeID(),
		PublicKey:               blsKey.PublicKey(),
		SubnetID:                subnetID,
		Weight:                  10,
		StartTime:               time.Unix(1, 0),
		EndTime:                 time.Unix(2, 0),
		PotentialReward:         100,
		AccruedRewards:          10,
		AccruedDelegateeRewards: 5,
		NextTime:                time.Time{},
		Priority:                txs.PrimaryNetworkValidatorCurrentPriority,
		ContinuationPeriod:      14 * 24 * time.Hour,
	}
	require.NoError(d.PutCurrentValidator(continuousValidator))

	newWeight := continuousValidator.Weight + 10
	newPotentialReward := continuousValidator.PotentialReward + 15
	newAccruedRewards := continuousValidator.AccruedRewards + 20
	newAccruedDelegateeRewards := continuousValidator.AccruedDelegateeRewards + 25

	expectedStartTime := continuousValidator.EndTime
	expectedEndTime := continuousValidator.EndTime.Add(continuousValidator.ContinuationPeriod)
	err = d.ResetContinuousValidatorCycle(
		subnetID,
		continuousValidator.NodeID,
		newWeight,
		newPotentialReward,
		newAccruedRewards,
		newAccruedDelegateeRewards,
	)
	require.NoError(err)

	continuousValidator, err = d.GetCurrentValidator(subnetID, continuousValidator.NodeID)
	require.NoError(err)

	require.Equal(newWeight, continuousValidator.Weight)
	require.Equal(newPotentialReward, continuousValidator.PotentialReward)
	require.Equal(newAccruedRewards, continuousValidator.AccruedRewards)
	require.Equal(newAccruedDelegateeRewards, continuousValidator.AccruedDelegateeRewards)
	require.Equal(expectedStartTime, continuousValidator.StartTime)
	require.Equal(expectedEndTime, continuousValidator.EndTime)
}
