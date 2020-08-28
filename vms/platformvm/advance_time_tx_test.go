// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"testing"
	"time"

	"github.com/ava-labs/gecko/utils/constants"
	"github.com/ava-labs/gecko/utils/crypto"
)

// Ensure semantic verification fails when proposed timestamp is at or before current timestamp
func TestAdvanceTimeTxTimestampTooEarly(t *testing.T) {
	vm, _ := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
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
	pendingValidatorEndTime := pendingValidatorStartTime.Add(MinimumStakingDuration)
	nodeIDKey, _ := vm.factory.NewPrivateKey()
	nodeID := nodeIDKey.PublicKey().Address()
	addPendingValidatorTx, err := vm.newAddValidatorTx(
		vm.minStake,
		uint64(pendingValidatorStartTime.Unix()),
		uint64(pendingValidatorEndTime.Unix()),
		nodeID,
		nodeID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}

	err = vm.putPendingValidators(
		vm.DB,
		&EventHeap{
			SortByStartTime: true,
			Txs:             []*Tx{addPendingValidatorTx},
		},
		constants.PrimaryNetworkID,
	)
	if err != nil {
		t.Fatal(err)
	}

	tx, err := vm.newAdvanceTimeTx(pendingValidatorStartTime.Add(1 * time.Second))
	if err != nil {
		t.Fatal(err)
	} else if _, _, _, _, err = tx.UnsignedTx.(UnsignedProposalTx).SemanticVerify(vm, vm.DB, tx); err == nil {
		t.Fatal("should've failed verification because proposed timestamp is after pending validator start time")
	}
	vm.Shutdown()
	vm.Ctx.Lock.Unlock()

	// Case: Timestamp is after next validator end time
	vm, _ = defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
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

// Ensure semantic verification updates the current and pending validator sets correctly
func TestAdvanceTimeTxUpdateValidators(t *testing.T) {
	vm, _ := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := defaultGenesisTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(MinimumStakingDuration)
	nodeIDKey, _ := vm.factory.NewPrivateKey()
	nodeID := nodeIDKey.PublicKey().Address()
	addPendingValidatorTx, err := vm.newAddValidatorTx(
		vm.minStake,
		uint64(pendingValidatorStartTime.Unix()),
		uint64(pendingValidatorEndTime.Unix()),
		nodeID,
		nodeID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.putPendingValidators(
		vm.DB,
		&EventHeap{
			SortByStartTime: true,
			Txs:             []*Tx{addPendingValidatorTx},
		},
		constants.PrimaryNetworkID,
	); err != nil {
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

	if onCommitCurrentEvents, err := vm.getCurrentValidators(onCommit, constants.PrimaryNetworkID); err != nil {
		t.Fatal(err)
	} else if onCommitCurrentEvents.Len() != len(keys)+1 { // Each key in [keys] is a validator to start with...then we added a validator
		t.Fatalf("Should have added the validator to the validator set")
	}

	if onCommitPendingEvents, err := vm.getPendingValidators(onCommit, constants.PrimaryNetworkID); err != nil {
		t.Fatal(err)
	} else if onCommitPendingEvents.Len() != 0 {
		t.Fatalf("Should have removed the validator from the pending validator set")
	}

	if onAbortCurrentEvents, err := vm.getCurrentValidators(onAbort, constants.PrimaryNetworkID); err != nil {
		t.Fatal(err)
	} else if onAbortCurrentEvents.Len() != len(keys) {
		t.Fatalf("Shouldn't have added the validator to the validator set")
	}

	if onAbortPendingEvents, err := vm.getPendingValidators(onAbort, constants.PrimaryNetworkID); err != nil {
		t.Fatal(err)
	} else if onAbortPendingEvents.Len() != 1 {
		t.Fatalf("Shouldn't have removed the validator from the pending validator set")
	}
}

// Test method InitiallyPrefersCommit
func TestAdvanceTimeTxInitiallyPrefersCommit(t *testing.T) {
	vm, _ := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	// Proposed advancing timestamp to 1 second after current timestamp
	tx, err := vm.newAdvanceTimeTx(defaultGenesisTime.Add(1 * time.Second))
	if err != nil {
		t.Fatal(err)
	}

	if tx.UnsignedTx.(UnsignedProposalTx).InitiallyPrefersCommit(vm) {
		t.Fatal("should not prefer to commit this tx because its proposed timestamp is after wall clock time")
	}

	// advance wall clock time
	vm.clock.Set(defaultGenesisTime.Add(2 * time.Second))
	if !tx.UnsignedTx.(UnsignedProposalTx).InitiallyPrefersCommit(vm) {
		t.Fatal("should prefer to commit this tx because its proposed timestamp is before wall clock time")
	}
}

// Ensure marshaling/unmarshaling works
func TestAdvanceTimeTxUnmarshal(t *testing.T) {
	vm, _ := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
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
