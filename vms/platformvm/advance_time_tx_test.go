// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
)

// Ensure semantic verification fails when proposed timestamp is at or before current timestamp
func TestAdvanceTimeTxTimestampTooEarly(t *testing.T) {
	vm, _ := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.Ctx.Lock.Unlock()
	}()

	if tx, err := vm.newAdvanceTimeTx(defaultGenesisTime); err != nil {
		t.Fatal(err)
	} else if _, _, _, _, err = tx.UnsignedTx.(UnsignedProposalTx).SemanticVerify(vm, vm.DB, tx); err == nil {
		t.Fatal("should've failed verification because proposed timestamp same as current timestamp")
	}
}

// Ensure semantic verification fails when proposed timestamp is after next validator set change time
func TestAdvanceTimeTxTimestampTooLate(t *testing.T) {
	vm, _ := defaultVM()
	vm.Ctx.Lock.Lock()

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := defaultGenesisTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(defaultMinStakingDuration)
	nodeIDKey, _ := vm.factory.NewPrivateKey()
	nodeID := nodeIDKey.PublicKey().Address()
	addPendingValidatorTx, err := vm.newAddValidatorTx(
		vm.minValidatorStake,
		uint64(pendingValidatorStartTime.Unix()),
		uint64(pendingValidatorEndTime.Unix()),
		nodeID,
		nodeID,
		PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.enqueueStaker(vm.DB, constants.PrimaryNetworkID, addPendingValidatorTx); err != nil {
		t.Fatal(err)
	}

	tx, err := vm.newAdvanceTimeTx(pendingValidatorStartTime.Add(1 * time.Second))
	if err != nil {
		t.Fatal(err)
	} else if _, _, _, _, err = tx.UnsignedTx.(UnsignedProposalTx).SemanticVerify(vm, vm.DB, tx); err == nil {
		t.Fatal("should've failed verification because proposed timestamp is after pending validator start time")
	}
	if err := vm.Shutdown(); err != nil {
		t.Fatal(err)
	}
	vm.Ctx.Lock.Unlock()

	// Case: Timestamp is after next validator end time
	vm, _ = defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.Ctx.Lock.Unlock()
	}()

	// fast forward clock to 10 seconds before genesis validators stop validating
	vm.clock.Set(defaultValidateEndTime.Add(-10 * time.Second))

	// Proposes advancing timestamp to 1 second after genesis validators stop validating
	if tx, err := vm.newAdvanceTimeTx(defaultValidateEndTime.Add(1 * time.Second)); err != nil {
		t.Fatal(err)
	} else if _, _, _, _, err = tx.UnsignedTx.(UnsignedProposalTx).SemanticVerify(vm, vm.DB, tx); err == nil {
		t.Fatal("should've failed verification because proposed timestamp is after pending validator start time")
	}
}

// Ensure semantic verification updates the current and pending staker set
// for the primary network
func TestAdvanceTimeTxUpdatePrimaryNetworkStakers(t *testing.T) {
	vm, _ := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.Ctx.Lock.Unlock()
	}()

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := defaultGenesisTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(defaultMinStakingDuration)
	nodeIDKey, _ := vm.factory.NewPrivateKey()
	nodeID := nodeIDKey.PublicKey().Address()
	addPendingValidatorTx, err := vm.newAddValidatorTx(
		vm.minValidatorStake,
		uint64(pendingValidatorStartTime.Unix()),
		uint64(pendingValidatorEndTime.Unix()),
		nodeID,
		nodeID,
		PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.enqueueStaker(vm.DB, constants.PrimaryNetworkID, addPendingValidatorTx); err != nil {
		t.Fatal(err)
	}

	tx, err := vm.newAdvanceTimeTx(pendingValidatorStartTime)
	if err != nil {
		t.Fatal(err)
	}
	onCommit, onAbort, _, _, err := tx.UnsignedTx.(UnsignedProposalTx).SemanticVerify(vm, vm.DB, tx)
	if err != nil {
		t.Fatal(err)
	}

	if validatorTx, isValidator, err := vm.isValidator(onCommit, constants.PrimaryNetworkID, nodeID); err != nil {
		t.Fatal(err)
	} else if !isValidator {
		t.Fatalf("Should have added the validator to the validator set")
	} else if validatorTx.ID() != addPendingValidatorTx.ID() {
		t.Fatalf("Added the wrong tx to the validator set")
	} else if _, willBeValidator, err := vm.willBeValidator(onCommit, constants.PrimaryNetworkID, nodeID); err != nil {
		t.Fatal(err)
	} else if willBeValidator {
		t.Fatalf("Should have removed the validator from the pending validator set")
	} else if tx, err := vm.nextStakerStop(onCommit, constants.PrimaryNetworkID); err != nil {
		t.Fatal(err)
	} else if tx.Reward != 1370 { // See rewards tests
		t.Fatalf("Expected reward of %d but was %d", 1370, tx.Reward)
	}

	if _, isValidator, err := vm.isValidator(onAbort, constants.PrimaryNetworkID, nodeID); err != nil {
		t.Fatal(err)
	} else if isValidator {
		t.Fatalf("Shouldn't have added the validator to the validator set")
	}

	validatorTx, willBeValidator, err := vm.willBeValidator(onAbort, constants.PrimaryNetworkID, nodeID)
	switch {
	case err != nil:
		t.Fatal(err)
	case !willBeValidator:
		t.Fatalf("Shouldn't have removed the validator from the pending validator set")
	case validatorTx.ID() != addPendingValidatorTx.ID():
		t.Fatalf("Added the wrong tx to the pending validator set")
	}
}

// Ensure semantic verification updates the current and pending staker sets correctly.
// Namely, it should add pending stakers whose start time is at or before the timestamp.
// It will not remove primary network stakers; that happens in rewardTxs.
func TestAdvanceTimeTxUpdatePrimaryNetworkStakers2(t *testing.T) {
	type staker struct {
		nodeID             ids.ShortID
		startTime, endTime time.Time
	}
	type test struct {
		description     string
		stakers         []staker
		advanceTimeTo   []time.Time
		expectedCurrent []ids.ShortID
		expectedPending []ids.ShortID
	}

	// Chronological order: staker1 start, staker2 start, staker3 start and staker 4 start,
	//  staker3 and staker4 end, staker2 end and staker5 start, staker1 end
	staker1 := staker{
		nodeID:    ids.GenerateTestShortID(),
		startTime: defaultGenesisTime.Add(1 * time.Minute),
		endTime:   defaultGenesisTime.Add(10 * defaultMinStakingDuration).Add(1 * time.Minute),
	}
	staker2 := staker{
		nodeID:    ids.GenerateTestShortID(),
		startTime: staker1.startTime.Add(1 * time.Minute),
		endTime:   staker1.startTime.Add(1 * time.Minute).Add(defaultMinStakingDuration),
	}
	staker3 := staker{
		nodeID:    ids.GenerateTestShortID(),
		startTime: staker2.startTime.Add(1 * time.Minute),
		endTime:   staker2.endTime.Add(1 * time.Minute),
	}
	staker4 := staker{
		nodeID:    ids.GenerateTestShortID(),
		startTime: staker3.startTime,
		endTime:   staker3.endTime,
	}
	staker5 := staker{
		nodeID:    ids.GenerateTestShortID(),
		startTime: staker2.endTime,
		endTime:   staker2.endTime.Add(defaultMinStakingDuration),
	}

	tests := []test{
		{
			description: "advance time to before staker1 start",
			stakers: []staker{
				staker1,
				staker2,
				staker3,
				staker4,
				staker5,
			},
			advanceTimeTo:   []time.Time{staker1.startTime.Add(-1 * time.Second)},
			expectedPending: []ids.ShortID{staker1.nodeID, staker2.nodeID, staker3.nodeID, staker4.nodeID, staker5.nodeID},
		},
		{
			description: "advance time to staker 1 start",
			stakers: []staker{
				staker1,
				staker2,
				staker3,
				staker4,
				staker5,
			},
			advanceTimeTo:   []time.Time{staker1.startTime},
			expectedCurrent: []ids.ShortID{staker1.nodeID},
			expectedPending: []ids.ShortID{staker2.nodeID, staker3.nodeID, staker4.nodeID, staker5.nodeID},
		},
		{
			description: "advance time to the staker2 start",
			stakers: []staker{
				staker1,
				staker2,
				staker3,
				staker4,
				staker5,
			},
			advanceTimeTo:   []time.Time{staker1.startTime, staker2.startTime},
			expectedCurrent: []ids.ShortID{staker1.nodeID},
			expectedPending: []ids.ShortID{staker3.nodeID, staker4.nodeID, staker5.nodeID},
		},
		{
			description: "advance time to staker3 and staker4 start",
			stakers: []staker{
				staker1,
				staker2,
				staker3,
				staker4,
				staker5,
			},
			advanceTimeTo:   []time.Time{staker1.startTime, staker2.startTime, staker3.startTime},
			expectedCurrent: []ids.ShortID{staker2.nodeID, staker3.nodeID, staker4.nodeID},
			expectedPending: []ids.ShortID{staker5.nodeID},
		},
		{
			description: "advance time to staker5 start",
			stakers: []staker{
				staker1,
				staker2,
				staker3,
				staker4,
				staker5,
			},
			advanceTimeTo:   []time.Time{staker1.startTime, staker2.startTime, staker3.startTime, staker5.startTime},
			expectedCurrent: []ids.ShortID{staker3.nodeID, staker4.nodeID, staker5.nodeID},
		},
	}

	for _, tt := range tests {
		vm, _ := defaultVM()
		vm.Ctx.Lock.Lock()
		defer func() {
			if err := vm.Shutdown(); err != nil {
				t.Fatal(err)
			}
			vm.Ctx.Lock.Unlock()
		}()

		for _, staker := range tt.stakers {
			tx, err := vm.newAddValidatorTx(
				vm.minValidatorStake,
				uint64(staker.startTime.Unix()),
				uint64(staker.endTime.Unix()),
				staker.nodeID,  // validator ID
				ids.ShortEmpty, // reward address
				PercentDenominator,
				[]*crypto.PrivateKeySECP256K1R{keys[0]},
				ids.ShortEmpty, // change addr
			)
			if err != nil {
				t.Fatal(err)
			}
			if err := vm.enqueueStaker(vm.DB, constants.PrimaryNetworkID, tx); err != nil {
				t.Fatal(err)
			}
		}

		db := vm.DB
		for _, newTime := range tt.advanceTimeTo {
			vm.clock.Set(newTime)
			tx, err := vm.newAdvanceTimeTx(newTime)
			if err != nil {
				t.Fatal(err)
			}

			db, _, _, _, err = tx.UnsignedTx.(UnsignedProposalTx).SemanticVerify(vm, db, tx)
			if err != nil {
				t.Fatalf("failed test '%s': %s", tt.description, err)
			}
		}

		// Check that the validators we expect to be in the current staker set are there
		for _, stakerNodeID := range tt.expectedCurrent {
			_, isValidator, err := vm.isValidator(db, constants.PrimaryNetworkID, stakerNodeID)
			if err != nil {
				t.Fatal(err)
			} else if !isValidator {
				t.Fatalf("failed test '%s': expected validator to be in current validator set but it isn't", tt.description)
			}
		}
		// Check that the validators we expect to be in the pending staker set are there
		for _, stakerNodeID := range tt.expectedPending {
			_, willBeValidator, err := vm.willBeValidator(db, constants.PrimaryNetworkID, stakerNodeID)
			if err != nil {
				t.Fatal(err)
			} else if !willBeValidator {
				t.Fatalf("failed test '%s': expected validator to be in pending validator set but it isn't", tt.description)
			}
		}
	}
}

// Test method InitiallyPrefersCommit
func TestAdvanceTimeTxInitiallyPrefersCommit(t *testing.T) {
	vm, _ := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.Ctx.Lock.Unlock()
	}()

	vm.clock.Set(defaultGenesisTime) // VM's clock reads the genesis time

	// Proposed advancing timestamp to 1 second after sync bound
	tx, err := vm.newAdvanceTimeTx(defaultGenesisTime.Add(1 * time.Second).Add(syncBound))
	if err != nil {
		t.Fatal(err)
	}

	if tx.UnsignedTx.(UnsignedProposalTx).InitiallyPrefersCommit(vm) {
		t.Fatal("should not prefer to commit this tx because its proposed timestamp is outside of sync bound")
	}

	// advance wall clock time
	vm.clock.Set(defaultGenesisTime.Add(1 * time.Second))
	if !tx.UnsignedTx.(UnsignedProposalTx).InitiallyPrefersCommit(vm) {
		t.Fatal("should prefer to commit this tx because its proposed timestamp it's within sync bound")
	}
}

// Ensure marshaling/unmarshaling works
func TestAdvanceTimeTxUnmarshal(t *testing.T) {
	vm, _ := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.Ctx.Lock.Unlock()
	}()

	tx, err := vm.newAdvanceTimeTx(defaultGenesisTime)
	if err != nil {
		t.Fatal(err)
	}

	bytes, err := Codec.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}

	var unmarshaledTx Tx
	if err := Codec.Unmarshal(bytes, &unmarshaledTx); err != nil {
		t.Fatal(err)
	} else if tx.UnsignedTx.(*UnsignedAdvanceTimeTx).Time != unmarshaledTx.UnsignedTx.(*UnsignedAdvanceTimeTx).Time {
		t.Fatal("should have same timestamp")
	}
}
