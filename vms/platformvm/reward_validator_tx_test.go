// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"testing"
	"time"

	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/math"
	"github.com/ava-labs/gecko/vms/secp256k1fx"

	"github.com/ava-labs/gecko/ids"
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
	currentValidators, err := vm.getCurrentValidators(vm.DB, DefaultSubnetID)
	if err != nil {
		t.Fatal(err)
	}
	// ID of validator that should leave DS validator set next
	nextToRemove = currentValidators.Peek().(*addDefaultSubnetValidatorTx)

	// Case 1: Chain timestamp is wrong
	if tx, err := vm.newRewardValidatorTx(nextToRemove.ID()); err != nil {
		t.Fatal(err)
	} else if _, _, _, _, err := tx.SemanticVerify(vm.DB); err == nil {
		t.Fatalf("should have failed because validator end time doesn't match chain timestamp")
	}

	// Case 2: Wrong validator
	if tx, err := vm.newRewardValidatorTx(ids.Empty); err != nil {
		t.Fatal(err)
	} else if _, _, _, _, err := tx.SemanticVerify(vm.DB); err == nil {
		t.Fatalf("should have failed because validator ID is wrong")
	}

	// Case 3: Happy path
	// Advance chain timestamp to time that next validator leaves
	if err := vm.putTimestamp(vm.DB, nextToRemove.EndTime()); err != nil {
		t.Fatal(err)
	}
	tx, err := vm.newRewardValidatorTx(nextToRemove.ID())
	if err != nil {
		t.Fatal(err)
	}
	onCommitDB, onAbortDB, _, _, err := tx.SemanticVerify(vm.DB)
	if err != nil {
		t.Fatal(err)
	}

	// there should be no validators of default subnet in [onCommitDB] or [onAbortDB]
	// (as specified in defaultVM's init)
	if currentValidators, err := vm.getCurrentValidators(onCommitDB, DefaultSubnetID); err != nil {
		t.Fatal(err)
	} else if numValidators := currentValidators.Len(); numValidators != len(keys)-1 {
		t.Fatalf("Should be %d validators but are %d", len(keys)-1, numValidators)
	} else if currentValidators, err = vm.getCurrentValidators(onAbortDB, DefaultSubnetID); err != nil {
		t.Fatal(err)
	} else if numValidators := currentValidators.Len(); numValidators != len(keys)-1 {
		t.Fatalf("Should be %d validators but there are %d", len(keys)-1, numValidators)
	}

	// check that address got validator reward
	addrSet := ids.ShortSet{}
	addrSet.Add(nextToRemove.Destination)
	oldBalance := uint64(nextToRemove.Weight())
	utxos, err := vm.getUTXOs(vm.DB, addrSet)
	if err != nil {
		t.Fatal(err)
	}
	for _, utxo := range utxos {
		oldBalance += utxo.Out.(*secp256k1fx.TransferOutput).Amount()
	}

	commitBalance := uint64(0)
	utxos, err = vm.getUTXOs(onCommitDB, addrSet)
	if err != nil {
		t.Fatal(err)
	}
	for _, utxo := range utxos {
		commitBalance += utxo.Out.(*secp256k1fx.TransferOutput).Amount()
	}
	if commitBalance <= oldBalance {
		t.Fatal("expected address balance to have increased due to receiving validator reward")
	}

	abortBalance := uint64(0)
	utxos, err = vm.getUTXOs(onAbortDB, addrSet)
	if err != nil {
		t.Fatal(err)
	}
	for _, utxo := range utxos {
		abortBalance += utxo.Out.(*secp256k1fx.TransferOutput).Amount()
	}
	if abortBalance != oldBalance {
		t.Fatal("expected address balance to have remained same due to not receiving validator reward")
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

	vdrStartTime := uint64(defaultValidateStartTime.Unix()) + 1
	vdrEndTime := uint64(defaultValidateStartTime.Add(2 * MinimumStakingDuration).Unix())
	vdrDestination := key1.PublicKey().Address()
	vdrTx, err := vm.newAddDefaultSubnetValidatorTx(
		defaultStakeAmount, // stakeAmt
		vdrStartTime,
		vdrEndTime,
		vdrDestination, // node ID
		vdrDestination, // destination
		NumberOfShares/4,
		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // fee payer
	)
	if err != nil {
		t.Fatal(err)
	}

	delStartTime := vdrStartTime + 1
	delEndTime := vdrEndTime - 1
	delDestination := key2.PublicKey().Address()
	delTx, err := vm.newAddDefaultSubnetDelegatorTx(
		defaultStakeAmount, // stakeAmt
		delStartTime,
		delEndTime,
		vdrDestination,                          // node ID
		delDestination,                          // destination
		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // fee payer
	)
	if err != nil {
		t.Fatal(err)
	}

	currentValidators, err := vm.getCurrentValidators(vm.DB, DefaultSubnetID)
	if err != nil {
		t.Fatal(err)
	}
	currentValidators.Add(vdrTx)
	currentValidators.Add(delTx)
	if err := vm.putCurrentValidators(vm.DB, currentValidators, DefaultSubnetID); err != nil {
		t.Fatal(err)
		// Advance timestamp to when delegator should leave validator set
	} else if err := vm.putTimestamp(vm.DB, time.Unix(int64(delEndTime), 0)); err != nil {
		t.Fatal(err)
	}

	tx, err := vm.newRewardValidatorTx(delTx.ID())
	if err != nil {
		t.Fatal(err)
	}
	onCommitDB, onAbortDB, _, _, err := tx.SemanticVerify(vm.DB)
	if err != nil {
		t.Fatal(err)
	}

	vdrDestSet := ids.ShortSet{}
	vdrDestSet.Add(vdrDestination) // vdr destination as a set
	delDestSet := ids.ShortSet{}
	delDestSet.Add(delDestination)

	expectedReward := reward(
		time.Unix(int64(delEndTime), 0).Sub(time.Unix(int64(delStartTime), 0)), // duration
		delTx.Weight(), // amount
		InflationRate)  // inflation rate

	// If tx is committed, delegator and delegatee should get reward
	// and the delegator's reward should be greater because the delegatee's share is 25%
	if oldVdrBalance, err := vm.getBalance(vm.DB, vdrDestSet); err != nil {
		t.Fatal(err)
	} else if commitVdrBalance, err := vm.getBalance(onCommitDB, vdrDestSet); err != nil {
		t.Fatal(err)
	} else if vdrReward, err := math.Sub64(commitVdrBalance, oldVdrBalance); err != nil || vdrReward == 0 {
		t.Fatal("expected delgatee balance to increase because of reward")
	} else if oldDelBalance, err := vm.getBalance(vm.DB, delDestSet); err != nil {
		t.Fatal(err)
	} else if commitDelBalance, err := vm.getBalance(onCommitDB, delDestSet); err != nil {
		t.Fatal(err)
	} else if delBalanceChange, err := math.Sub64(commitDelBalance, oldDelBalance); err != nil || delBalanceChange == 0 {
		t.Fatal("expected delgator balance to increase upon commit")
	} else if delReward, err := math.Sub64(delBalanceChange, vdrTx.Weight()); err != nil || delReward == 0 {
		t.Fatal("expected delegator reward to be non-zero")
	} else if delReward < vdrReward {
		t.Fatal("the delegator's reward should be greater than the delegatee's because the delegatee's share is 25%")
	} else if delReward+vdrReward != expectedReward {
		t.Fatalf("expected total reward to be %d but is %d", expectedReward, delReward+vdrReward)
	} else if abortVdrBalance, err := vm.getBalance(onAbortDB, vdrDestSet); err != nil {
		t.Fatal(err)
	} else if vdrReward, err = math.Sub64(abortVdrBalance, oldVdrBalance); err != nil || vdrReward != 0 {
		t.Fatal("expected delgatee balance to stay the same upon abort")
	} else if abortDelBalance, err := vm.getBalance(onAbortDB, delDestSet); err != nil {
		t.Fatal(err)
	} else if delReward, err = math.Sub64(abortDelBalance, oldDelBalance); err != nil || delReward != delTx.Weight() {
		t.Fatal("expected delgator balance to just increase by stake amount upon abort")
	}
}
