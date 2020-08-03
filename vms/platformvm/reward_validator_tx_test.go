// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"testing"
	"time"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/constants"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/math"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

func TestUnsignedewardValidatorTxSyntacticVerify(t *testing.T) {
	type test struct {
		tx        *UnsignedRewardValidatorTx
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
			tx: &UnsignedRewardValidatorTx{
				vm:   vm,
				TxID: txID,
			},
			shouldErr: false,
		},
		{
			tx: &UnsignedRewardValidatorTx{
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

func TestUnsignedRewardValidatorTxSemanticVerify(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	currentValidators, err := vm.getCurrentValidators(vm.DB, constants.DefaultSubnetID)
	if err != nil {
		t.Fatal(err)
	}
	// ID of validator that should leave DS validator set next
	nextToRemove := currentValidators.Peek().UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx)

	// Case 1: Chain timestamp is wrong
	if tx, err := vm.newRewardValidatorTx(nextToRemove.ID()); err != nil {
		t.Fatal(err)
	} else if _, _, _, _, err := tx.SemanticVerify(vm.DB, tx); err == nil {
		t.Fatalf("should have failed because validator end time doesn't match chain timestamp")
	}

	// Case 2: Wrong validator
	if tx, err := vm.newRewardValidatorTx(ids.GenerateTestID()); err != nil {
		t.Fatal(err)
	} else if _, _, _, _, err := tx.SemanticVerify(vm.DB, tx); err == nil {
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
	onCommitDB, onAbortDB, _, _, err := tx.SemanticVerify(vm.DB, tx)
	if err != nil {
		t.Fatal(err)
	}

	// Should be one less validator than before
	oldNumValidators := len(currentValidators.Txs)
	if currentValidators, err := vm.getCurrentValidators(onCommitDB, constants.DefaultSubnetID); err != nil {
		t.Fatal(err)
	} else if numValidators := currentValidators.Len(); numValidators != oldNumValidators-1 {
		t.Fatalf("Should be %d validators but are %d", oldNumValidators-1, numValidators)
	} else if currentValidators, err = vm.getCurrentValidators(onAbortDB, constants.DefaultSubnetID); err != nil {
		t.Fatal(err)
	} else if numValidators := currentValidators.Len(); numValidators != oldNumValidators-1 {
		t.Fatalf("Should be %d validators but there are %d", oldNumValidators-1, numValidators)
	}

	// check that stake/reward is given back
	stakeOwners := nextToRemove.Stake[0].Out.(*secp256k1fx.TransferOutput).AddressesSet()
	// Get old balances, balances if tx abort, balances if tx committed
	for _, stakeOwner := range stakeOwners.List() {
		oldBalance, err := vm.getBalance(vm.DB, [][]byte{stakeOwner.Bytes()})
		if err != nil {
			t.Fatal(err)
		}
		onAbortBalance, err := vm.getBalance(onAbortDB, [][]byte{stakeOwner.Bytes()})
		if err != nil {
			t.Fatal(err)
		}
		onCommitBalance, err := vm.getBalance(onCommitDB, [][]byte{stakeOwner.Bytes()})
		if err != nil {
			t.Fatal(err)
		}
		if onAbortBalance != oldBalance+nextToRemove.Weight() {
			t.Fatalf("on abort, should have got back staked amount")
		}
		expectedReward := reward(nextToRemove.Duration(), nextToRemove.Weight(), InflationRate)
		if onCommitBalance != oldBalance+expectedReward+nextToRemove.Weight() {
			t.Fatalf("on commit, should have old balance (%d) + staked amount (%d) + reward (%d) but have %d",
				oldBalance, nextToRemove.Weight(), expectedReward, onCommitBalance)
		}
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
		MinimumStakeAmount, // stakeAmt
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
	unsignedVdrTx := vdrTx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx)

	delStartTime := vdrStartTime + 1
	delEndTime := vdrEndTime - 1
	delDestination := key2.PublicKey().Address()
	delTx, err := vm.newAddDefaultSubnetDelegatorTx(
		MinimumStakeAmount, // stakeAmt
		delStartTime,
		delEndTime,
		vdrDestination,                          // node ID
		delDestination,                          // destination
		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // fee payer
	)
	if err != nil {
		t.Fatal(err)
	}
	unsignedDelTx := delTx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetDelegatorTx)

	currentValidators, err := vm.getCurrentValidators(vm.DB, constants.DefaultSubnetID)
	if err != nil {
		t.Fatal(err)
	}
	currentValidators.Add(vdrTx)
	currentValidators.Add(delTx)
	if err := vm.putCurrentValidators(vm.DB, currentValidators, constants.DefaultSubnetID); err != nil {
		t.Fatal(err)
		// Advance timestamp to when delegator should leave validator set
	} else if err := vm.putTimestamp(vm.DB, time.Unix(int64(delEndTime), 0)); err != nil {
		t.Fatal(err)
	}

	tx, err := vm.newRewardValidatorTx(delTx.ID())
	if err != nil {
		t.Fatal(err)
	}
	onCommitDB, onAbortDB, _, _, err := tx.SemanticVerify(vm.DB, tx)
	if err != nil {
		t.Fatal(err)
	}

	vdrDestSlice := [][]byte{vdrDestination.Bytes()}
	delDestSlice := [][]byte{keys[0].PublicKey().Address().Bytes()}

	expectedReward := reward(
		time.Unix(int64(delEndTime), 0).Sub(time.Unix(int64(delStartTime), 0)), // duration
		unsignedDelTx.Weight(),                                                 // amount
		InflationRate,                                                          // inflation rate
	)

	// If tx is committed, delegator and delegatee should get reward
	// and the delegator's reward should be greater because the delegatee's share is 25%
	if oldVdrBalance, err := vm.getBalance(vm.DB, vdrDestSlice); err != nil {
		t.Fatal(err)
	} else if commitVdrBalance, err := vm.getBalance(onCommitDB, vdrDestSlice); err != nil {
		t.Fatal(err)
	} else if vdrReward, err := math.Sub64(commitVdrBalance, oldVdrBalance); err != nil || vdrReward == 0 && InflationRate > 1.0 {
		t.Fatal("expected delgatee balance to increase because of reward")
	} else if oldDelBalance, err := vm.getBalance(vm.DB, delDestSlice); err != nil {
		t.Fatal(err)
	} else if commitDelBalance, err := vm.getBalance(onCommitDB, delDestSlice); err != nil {
		t.Fatal(err)
	} else if delBalanceChange, err := math.Sub64(commitDelBalance, oldDelBalance); err != nil || delBalanceChange == 0 {
		t.Fatal("expected delgator balance to increase upon commit")
	} else if delReward, err := math.Sub64(delBalanceChange, unsignedVdrTx.Weight()); err != nil || delReward == 0 && InflationRate > 1.0 {
		t.Fatal("expected delegator reward to be non-zero")
	} else if delReward < vdrReward {
		t.Fatal("the delegator's reward should be greater than the delegatee's because the delegatee's share is 25%")
	} else if delReward+vdrReward != expectedReward {
		t.Fatalf("expected total reward to be %d but is %d", expectedReward, delReward+vdrReward)
	} else if abortVdrBalance, err := vm.getBalance(onAbortDB, vdrDestSlice); err != nil {
		t.Fatal(err)
	} else if vdrReward, err = math.Sub64(abortVdrBalance, oldVdrBalance); err != nil || vdrReward != 0 && InflationRate > 1.0 {
		t.Fatal("expected delgatee balance to stay the same upon abort")
	} else if abortDelBalance, err := vm.getBalance(onAbortDB, delDestSlice); err != nil {
		t.Fatal(err)
	} else if delReward, err = math.Sub64(abortDelBalance, oldDelBalance); err != nil || delReward != unsignedDelTx.Weight() {
		t.Fatal("expected delgator balance to increase by stake amount upon abort")
	}
}
