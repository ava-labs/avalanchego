// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"testing"
	"time"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/constants"
	"github.com/ava-labs/gecko/utils/crypto"
)

func TestRewardValidatorTxSyntacticVerify(t *testing.T) {
	type test struct {
		tx        *rewardValidatorTx
		shouldErr bool
	}

	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	txID := ids.NewID([32]byte{1, 2, 3, 4, 5, 6, 7})

	tests := []test{
		{
			tx:        nil,
			shouldErr: true,
		},
		{
			tx: &rewardValidatorTx{
				vm:   vm,
				TxID: txID,
			},
			shouldErr: false,
		},
		{
			tx: &rewardValidatorTx{
				vm:   vm,
				TxID: ids.ID{},
			},
			shouldErr: true,
		},
	}

	for _, test := range tests {
		err := test.tx.SyntacticVerify()
		if err != nil && !test.shouldErr {
			t.Fatalf("expected nil error but got: %v", err)
		}
		if err == nil && test.shouldErr {
			t.Fatalf("expected error but got nil")
		}
	}
}

func TestRewardValidatorTxSemanticVerify(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	var nextToRemove *addDefaultSubnetValidatorTx
	currentValidators, err := vm.getCurrentValidators(vm.DB, constants.DefaultSubnetID)
	if err != nil {
		t.Fatal(err)
	}
	// ID of validator that should leave DS validator set next
	nextToRemove = currentValidators.Peek().(*addDefaultSubnetValidatorTx)

	// Case 1: Chain timestamp is wrong
	tx, err := vm.newRewardValidatorTx(nextToRemove.ID())
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, _, err = tx.SemanticVerify(vm.DB)
	t.Log(err)
	if err == nil {
		t.Fatalf("should have failed because validator end time doesn't match chain timestamp")
	}

	// Case 2: Wrong validator
	tx, err = vm.newRewardValidatorTx(ids.Empty)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, _, err = tx.SemanticVerify(vm.DB)
	t.Log(err)
	if err == nil {
		t.Fatalf("should have failed because validator ID is wrong")
	}

	// Case 3: Happy path
	// Advance chain timestamp to time that genesis validators leave
	if err := vm.putTimestamp(vm.DB, defaultValidateEndTime); err != nil {
		t.Fatal(err)
	}
	tx, err = vm.newRewardValidatorTx(nextToRemove.ID())
	if err != nil {
		t.Fatal(err)
	}
	onCommitDB, onAbortDB, _, _, err := tx.SemanticVerify(vm.DB)
	t.Log(err)
	if err != nil {
		t.Fatal(err)
	}

	// there should be no validators of default subnet in [onCommitDB] or [onAbortDB]
	// (as specified in defaultVM's init)
	currentValidators, err = vm.getCurrentValidators(onCommitDB, constants.DefaultSubnetID)
	t.Log(currentValidators)
	if err != nil {
		t.Fatal(err)
	}
	if numValidators := currentValidators.Len(); numValidators != len(keys)-1 {
		t.Fatalf("Should be %d validators but are %d", len(keys)-1, numValidators)
	}

	currentValidators, err = vm.getCurrentValidators(onAbortDB, constants.DefaultSubnetID)
	if err != nil {
		t.Fatal(err)
	}
	if numValidators := currentValidators.Len(); numValidators != len(keys)-1 {
		t.Fatalf("Should be %d validators but there are %d", len(keys)-1, numValidators)
	}

	// account should have gotten validator reward
	account, err := vm.getAccount(onCommitDB, nextToRemove.Destination)
	if err != nil {
		t.Fatal(err)
	}
	if account.Balance <= defaultBalance-txFee {
		t.Fatal("expected account balance to have increased due to receiving validator reward")
	}
}

func TestRewardDelegatorTxSemanticVerify(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	keyIntf1, err := vm.factory.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	key1 := keyIntf1.(*crypto.PrivateKeySECP256K1R)

	keyIntf2, err := vm.factory.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	key2 := keyIntf2.(*crypto.PrivateKeySECP256K1R)

	vdrTx, err := vm.newAddDefaultSubnetValidatorTx(
		defaultNonce+1,     // nonce
		defaultStakeAmount, // stakeAmt
		uint64(defaultValidateEndTime.Add(-365*24*time.Hour).Unix())-1,
		uint64(defaultValidateEndTime.Unix())-1,
		key1.PublicKey().Address(), // node ID
		key1.PublicKey().Address(), // destination
		NumberOfShares/4,
		testNetworkID,
		key1,
	)
	if err != nil {
		t.Fatal(err)
	}

	delTx, err := vm.newAddDefaultSubnetDelegatorTx(
		defaultNonce+1,     // nonce
		defaultStakeAmount, // stakeAmt
		uint64(defaultValidateEndTime.Add(-365*24*time.Hour).Unix())-1,
		uint64(defaultValidateEndTime.Unix())-1,
		key1.PublicKey().Address(), // node ID
		key2.PublicKey().Address(), // destination
		testNetworkID,
		key2,
	)
	if err != nil {
		t.Fatal(err)
	}

	currentValidators, err := vm.getCurrentValidators(vm.DB, constants.DefaultSubnetID)
	if err != nil {
		t.Fatal(err)
	}
	currentValidators.Add(vdrTx)
	currentValidators.Add(delTx)
	vm.putCurrentValidators(vm.DB, currentValidators, constants.DefaultSubnetID)

	if err := vm.putTimestamp(vm.DB, defaultValidateEndTime.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}

	tx, err := vm.newRewardValidatorTx(delTx.ID())
	if err != nil {
		t.Fatal(err)
	}
	onCommitDB, _, _, _, err := tx.SemanticVerify(vm.DB)
	t.Log(err)
	if err != nil {
		t.Fatal(err)
	}

	// account should have gotten validator reward
	account, err := vm.getAccount(onCommitDB, vdrTx.Destination)
	if err != nil {
		t.Fatal(err)
	}
	if expectedBalance := defaultStakeAmount / 100; account.Balance != expectedBalance {
		t.Fatalf("expected account balance to be %d was %d", expectedBalance, account.Balance)
	}

	// account should have gotten validator reward
	account, err = vm.getAccount(onCommitDB, delTx.Destination)
	if err != nil {
		t.Fatal(err)
	}
	if expectedBalance := (defaultStakeAmount * 103) / 100; account.Balance != expectedBalance {
		t.Fatalf("expected account balance to be %d was %d", expectedBalance, account.Balance)
	}

	tx, err = vm.newRewardValidatorTx(vdrTx.ID())
	if err != nil {
		t.Fatal(err)
	}
	onCommitDB, _, _, _, err = tx.SemanticVerify(onCommitDB)
	t.Log(err)
	if err != nil {
		t.Fatal(err)
	}

	// account should have gotten validator reward
	account, err = vm.getAccount(onCommitDB, vdrTx.Destination)
	if err != nil {
		t.Fatal(err)
	}
	if expectedBalance := (defaultStakeAmount * 21) / 20; account.Balance != expectedBalance {
		t.Fatalf("expected account balance to be %d was %d", expectedBalance, account.Balance)
	}
}
