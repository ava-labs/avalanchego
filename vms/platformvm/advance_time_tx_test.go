// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"testing"
	"time"
)

func TestAdvanceTimeTxSyntacticVerify(t *testing.T) {
	// Case 1: Tx is nil
	var tx *advanceTimeTx
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have failed verification because tx is nil")
	}

	// Case 2: Timestamp is ahead of synchrony bound
	vm := defaultVM()
	defer func() { vm.Ctx.Lock.Lock(); vm.Shutdown(); vm.Ctx.Lock.Unlock() }()

	tx = &advanceTimeTx{
		Time: uint64(defaultGenesisTime.Add(Delta).Add(1 * time.Second).Unix()),
		vm:   vm,
	}

	err := tx.SyntacticVerify()
	if err == nil {
		t.Fatal("should've failed verification because timestamp is ahead of synchrony bound")
	}

	// Case 3: Valid
	tx.Time = uint64(defaultGenesisTime.Add(Delta).Unix())
	err = tx.SyntacticVerify()
	if err != nil {
		t.Fatalf("should've passed verification but got: %v", err)
	}
}

// Ensure semantic verification fails when proposed timestamp is at or before current timestamp
func TestAdvanceTimeTxTimestampTooEarly(t *testing.T) {
	vm := defaultVM()
	defer func() { vm.Ctx.Lock.Lock(); vm.Shutdown(); vm.Ctx.Lock.Unlock() }()

	tx := &advanceTimeTx{
		Time: uint64(defaultGenesisTime.Unix()),
		vm:   vm,
	}
	_, _, _, _, err := tx.SemanticVerify(vm.DB)
	if err == nil {
		t.Fatal("should've failed verification because proposed timestamp same as current timestamp")
	}
}

// Ensure semantic verification fails when proposed timestamp is after next validator set change time
func TestAdvanceTimeTxTimestampTooLate(t *testing.T) {
	vm := defaultVM()
	defer func() { vm.Ctx.Lock.Lock(); vm.Shutdown(); vm.Ctx.Lock.Unlock() }()

	// Case 1: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := defaultGenesisTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(MinimumStakingDuration)
	nodeIDKey, _ := vm.factory.NewPrivateKey()
	nodeID := nodeIDKey.PublicKey().Address()
	addPendingValidatorTx, err := vm.newAddDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultStakeAmount,
		uint64(pendingValidatorStartTime.Unix()),
		uint64(pendingValidatorEndTime.Unix()),
		nodeID,
		nodeID,
		NumberOfShares,
		testNetworkID,
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}

	err = vm.putPendingValidators(
		vm.DB,
		&EventHeap{
			SortByStartTime: true,
			Txs:             []TimedTx{addPendingValidatorTx},
		},
		DefaultSubnetID,
	)
	if err != nil {
		t.Fatal(err)
	}

	tx := &advanceTimeTx{
		Time: uint64(pendingValidatorStartTime.Add(1 * time.Second).Unix()),
		vm:   vm,
	}
	_, _, _, _, err = tx.SemanticVerify(vm.DB)
	if err == nil {
		t.Fatal("should've failed verification because proposed timestamp is after pending validator start time")
	}

	// Case 2: Timestamp is after next validator end time
	vm = defaultVM()

	// fast forward clock to 10 seconds before genesis validators stop validating
	vm.clock.Set(defaultValidateEndTime.Add(-10 * time.Second))

	// Proposes advancing timestamp to 1 second after genesis validators stop validating
	tx = &advanceTimeTx{
		Time: uint64(defaultValidateEndTime.Add(1 * time.Second).Unix()),
		vm:   vm,
	}

	_, _, _, _, err = tx.SemanticVerify(vm.DB)
	t.Log(err)
	if err == nil {
		t.Fatal("should've failed verification because proposed timestamp is after pending validator start time")
	}
}

// Ensure semantic verification updates the current and pending validator sets correctly
func TestAdvanceTimeTxUpdateValidators(t *testing.T) {
	vm := defaultVM()
	defer func() { vm.Ctx.Lock.Lock(); vm.Shutdown(); vm.Ctx.Lock.Unlock() }()

	// Case 1: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := defaultGenesisTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(MinimumStakingDuration)
	nodeIDKey, _ := vm.factory.NewPrivateKey()
	nodeID := nodeIDKey.PublicKey().Address()
	addPendingValidatorTx, err := vm.newAddDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultStakeAmount,
		uint64(pendingValidatorStartTime.Unix()),
		uint64(pendingValidatorEndTime.Unix()),
		nodeID,
		nodeID,
		NumberOfShares,
		testNetworkID,
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}

	err = vm.putPendingValidators(
		vm.DB,
		&EventHeap{
			SortByStartTime: true,
			Txs:             []TimedTx{addPendingValidatorTx},
		},
		DefaultSubnetID,
	)
	if err != nil {
		t.Fatal(err)
	}

	tx := &advanceTimeTx{
		Time: uint64(pendingValidatorStartTime.Unix()),
		vm:   vm,
	}
	onCommit, onAbort, _, _, err := tx.SemanticVerify(vm.DB)
	if err != nil {
		t.Fatal(err)
	}

	onCommitCurrentEvents, err := vm.getCurrentValidators(onCommit, DefaultSubnetID)
	if err != nil {
		t.Fatal(err)
	}
	if onCommitCurrentEvents.Len() != len(keys)+1 { // Each key in [keys] is a validator to start with...then we added a validator
		t.Fatalf("Should have added the validator to the validator set")
	}

	onCommitPendingEvents, err := vm.getPendingValidators(onCommit, DefaultSubnetID)
	if err != nil {
		t.Fatal(err)
	}
	if onCommitPendingEvents.Len() != 0 {
		t.Fatalf("Should have removed the validator from the pending validator set")
	}

	onAbortCurrentEvents, err := vm.getCurrentValidators(onAbort, DefaultSubnetID)
	if err != nil {
		t.Fatal(err)
	}
	if onAbortCurrentEvents.Len() != len(keys) {
		t.Fatalf("Shouldn't have added the validator to the validator set")
	}

	onAbortPendingEvents, err := vm.getPendingValidators(onAbort, DefaultSubnetID)
	if err != nil {
		t.Fatal(err)
	}
	if onAbortPendingEvents.Len() != 1 {
		t.Fatalf("Shouldn't have removed the validator from the pending validator set")
	}
}

// Test method InitiallyPrefersCommit
func TestAdvanceTimeTxInitiallyPrefersCommit(t *testing.T) {
	vm := defaultVM()
	defer func() { vm.Ctx.Lock.Lock(); vm.Shutdown(); vm.Ctx.Lock.Unlock() }()

	// Proposed advancing timestamp to 1 second after current timestamp
	tx, err := vm.newAdvanceTimeTx(defaultGenesisTime.Add(1 * time.Second))
	if err != nil {
		t.Fatal(err)
	}

	if tx.InitiallyPrefersCommit() {
		t.Fatal("should not prefer to commit this tx because its proposed timestamp is after wall clock time")
	}

	// advance wall clock time
	vm.clock.Set(defaultGenesisTime.Add(2 * time.Second))
	if !tx.InitiallyPrefersCommit() {
		t.Fatal("should prefer to commit this tx because its proposed timestamp is before wall clock time")
	}
}

// Ensure marshaling/unmarshaling works
func TestAdvanceTimeTxUnmarshal(t *testing.T) {
	vm := defaultVM()
	defer func() { vm.Ctx.Lock.Lock(); vm.Shutdown(); vm.Ctx.Lock.Unlock() }()

	tx, err := vm.newAdvanceTimeTx(defaultGenesisTime)
	if err != nil {
		t.Fatal(err)
	}

	bytes, err := Codec.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}

	var unmarshaledTx advanceTimeTx
	err = Codec.Unmarshal(bytes, &unmarshaledTx)
	if err != nil {
		t.Fatal(err)
	}

	if tx.Time != unmarshaledTx.Time {
		t.Fatal("should have same timestamp")
	}
}
