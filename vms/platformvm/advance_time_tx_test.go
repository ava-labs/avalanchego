// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ava-labs/gecko/utils/constants"
	"github.com/ava-labs/gecko/utils/crypto"
)

func TestAdvanceTimeTxSyntacticVerify(t *testing.T) {
	// Case: Tx is nil
	var tx *advanceTimeTx
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have failed verification because tx is nil")
	}

	// Case: Timestamp is ahead of synchrony bound
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

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
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

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
	vm.Ctx.Lock.Lock()

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := defaultGenesisTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(MinimumStakingDuration)
	nodeIDKey, _ := vm.factory.NewPrivateKey()
	nodeID := nodeIDKey.PublicKey().Address()
	addPendingValidatorTx, err := vm.newAddDefaultSubnetValidatorTx(
		MinimumStakeAmount,
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
			Txs:             []TimedTx{addPendingValidatorTx},
		},
		constants.DefaultSubnetID,
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
	vm.Shutdown()
	vm.Ctx.Lock.Unlock()

	// Case: Timestamp is after next validator end time
	vm = defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

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
	addPendingValidatorTx, err := vm.newAddDefaultSubnetValidatorTx(
		MinimumStakeAmount,
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
			Txs:             []TimedTx{addPendingValidatorTx},
		},
		constants.DefaultSubnetID,
	); err != nil {
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

	if onCommitCurrentEvents, err := vm.getCurrentValidators(onCommit, constants.DefaultSubnetID); err != nil {
		t.Fatal(err)
	} else if onCommitCurrentEvents.Len() != len(keys)+1 { // Each key in [keys] is a validator to start with...then we added a validator
		t.Fatalf("Should have added the validator to the validator set")
	}

	if onCommitPendingEvents, err := vm.getPendingValidators(onCommit, constants.DefaultSubnetID); err != nil {
		t.Fatal(err)
	} else if onCommitPendingEvents.Len() != 0 {
		t.Fatalf("Should have removed the validator from the pending validator set")
	}

	if onAbortCurrentEvents, err := vm.getCurrentValidators(onAbort, constants.DefaultSubnetID); err != nil {
		t.Fatal(err)
	} else if onAbortCurrentEvents.Len() != len(keys) {
		t.Fatalf("Shouldn't have added the validator to the validator set")
	}

	if onAbortPendingEvents, err := vm.getPendingValidators(onAbort, constants.DefaultSubnetID); err != nil {
		t.Fatal(err)
	} else if onAbortPendingEvents.Len() != 1 {
		t.Fatalf("Shouldn't have removed the validator from the pending validator set")
	}
}

// Test method InitiallyPrefersCommit
func TestAdvanceTimeTxInitiallyPrefersCommit(t *testing.T) {
	vm := defaultVM()
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

	var unmarshaledTx advanceTimeTx
	if err := Codec.Unmarshal(bytes, &unmarshaledTx); err != nil {
		t.Fatal(err)
	} else if tx.Time != unmarshaledTx.Time {
		t.Fatal("should have same timestamp")
	}
}

func TestAdvanceTimeTxMarshalJSON(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	tx, err := vm.newAdvanceTimeTx(defaultGenesisTime)
	if err != nil {
		t.Fatal(err)
	}

	asJSON, err := json.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	asString := string(asJSON)
	t.Log(asString)
	if !strings.Contains(asString, fmt.Sprintf("\"id\":\"%s\"", tx.ID())) {
		t.Fatal("id is wrong")
	} else if !strings.Contains(asString, fmt.Sprintf("\"time\":%d", defaultGenesisTime.Unix())) {
		t.Fatal("time is wrong")
	}
}
