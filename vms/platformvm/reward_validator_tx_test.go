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
	"github.com/stretchr/testify/assert"
)

func TestUnsignedRewardValidatorTxSemanticVerify(t *testing.T) {
	vm, _ := defaultVM(t)
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	// ID of validator that should leave DS validator set next
	toRemoveIntf, err := vm.nextStakerStop(vm.DB, constants.PrimaryNetworkID)
	if err != nil {
		t.Fatal(err)
	}
	toRemove := toRemoveIntf.UnsignedTx.(*UnsignedAddValidatorTx)

	// Case 1: Chain timestamp is wrong
	if tx, err := vm.newRewardValidatorTx(toRemove.ID()); err != nil {
		t.Fatal(err)
	} else if _, _, _, _, err := toRemove.SemanticVerify(vm, vm.DB, tx); err == nil {
		t.Fatalf("should have failed because validator end time doesn't match chain timestamp")
	}

	// Case 2: Wrong validator
	if tx, err := vm.newRewardValidatorTx(ids.GenerateTestID()); err != nil {
		t.Fatal(err)
	} else if _, _, _, _, err := toRemove.SemanticVerify(vm, vm.DB, tx); err == nil {
		t.Fatalf("should have failed because validator ID is wrong")
	}

	// Case 3: Happy path
	// Advance chain timestamp to time that next validator leaves
	if err := vm.putTimestamp(vm.DB, toRemove.EndTime()); err != nil {
		t.Fatal(err)
	}
	tx, err := vm.newRewardValidatorTx(toRemove.ID())
	if err != nil {
		t.Fatal(err)
	}
	onCommitDB, onAbortDB, _, _, err := tx.UnsignedTx.(UnsignedProposalTx).SemanticVerify(vm, vm.DB, tx)
	if err != nil {
		t.Fatal(err)
	}

	// ID of validator that should leave DS validator set next

	if nextToRemove, err := vm.nextStakerStop(onCommitDB, constants.PrimaryNetworkID); err != nil {
		t.Fatal(err)
	} else if toRemove.ID().Equals(nextToRemove.ID()) {
		t.Fatalf("Should have removed the previous validator")
	}

	// check that stake/reward is given back
	stakeOwners := toRemove.Stake[0].Out.(*secp256k1fx.TransferOutput).AddressesSet()
	// Get old balances, balances if tx abort, balances if tx committed
	for _, stakeOwner := range stakeOwners.List() {
		stakeOwnerSet := ids.ShortSet{}
		stakeOwnerSet.Add(stakeOwner)

		oldBalance, err := vm.getBalance(vm.DB, stakeOwnerSet)
		if err != nil {
			t.Fatal(err)
		}
		onAbortBalance, err := vm.getBalance(onAbortDB, stakeOwnerSet)
		if err != nil {
			t.Fatal(err)
		}
		onCommitBalance, err := vm.getBalance(onCommitDB, stakeOwnerSet)
		if err != nil {
			t.Fatal(err)
		}
		if onAbortBalance != oldBalance+toRemove.Validator.Weight() {
			t.Fatalf("on abort, should have got back staked amount")
		}
		expectedReward := reward(toRemove.Validator.Duration(), toRemove.Validator.Weight(), InflationRate)
		if onCommitBalance != oldBalance+expectedReward+toRemove.Validator.Weight() {
			t.Fatalf("on commit, should have old balance (%d) + staked amount (%d) + reward (%d) but have %d",
				oldBalance, toRemove.Validator.Weight(), expectedReward, onCommitBalance)
		}
	}
}

func TestRewardDelegatorTxSemanticVerify(t *testing.T) {
	vm, _ := defaultVM(t)
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	vdrRewardAddress := ids.GenerateTestShortID()
	delRewardAddress := ids.GenerateTestShortID()

	vdrStartTime := uint64(defaultValidateStartTime.Unix()) + 1
	vdrEndTime := uint64(defaultValidateStartTime.Add(2 * MinimumStakingDuration).Unix())
	vdrNodeID := ids.GenerateTestShortID()
	vdrTx, err := vm.newAddValidatorTx(
		vm.minStake, // stakeAmt
		vdrStartTime,
		vdrEndTime,
		vdrNodeID,        // node ID
		vdrRewardAddress, // reward address
		NumberOfShares/4,
		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // fee payer
	)
	if err != nil {
		t.Fatal(err)
	}

	delStartTime := vdrStartTime
	delEndTime := vdrEndTime
	delTx, err := vm.newAddDelegatorTx(
		vm.minStake, // stakeAmt
		delStartTime,
		delEndTime,
		vdrNodeID,                               // node ID
		delRewardAddress,                        // reward address
		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // fee payer
	)
	if err != nil {
		t.Fatal(err)
	}
	unsignedDelTx := delTx.UnsignedTx.(*UnsignedAddDelegatorTx)

	if err := vm.addStaker(vm.DB, constants.PrimaryNetworkID, vdrTx); err != nil {
		t.Fatal(err)
	}
	if err := vm.addStaker(vm.DB, constants.PrimaryNetworkID, delTx); err != nil {
		t.Fatal(err)
	}
	if err := vm.putTimestamp(vm.DB, time.Unix(int64(delEndTime), 0)); err != nil {
		t.Fatal(err)
	}

	tx, err := vm.newRewardValidatorTx(delTx.ID())
	if err != nil {
		t.Fatal(err)
	}
	onCommitDB, onAbortDB, _, _, err := tx.UnsignedTx.(UnsignedProposalTx).SemanticVerify(vm, vm.DB, tx)
	if err != nil {
		t.Fatal(err)
	}

	vdrDestSet := ids.ShortSet{}
	vdrDestSet.Add(vdrRewardAddress)
	delDestSet := ids.ShortSet{}
	delDestSet.Add(delRewardAddress)

	expectedReward := reward(
		time.Unix(int64(delEndTime), 0).Sub(time.Unix(int64(delStartTime), 0)), // duration
		unsignedDelTx.Validator.Weight(),                                       // amount
		InflationRate,                                                          // inflation rate
	)

	// If tx is committed, delegator and delegatee should get reward
	// and the delegator's reward should be greater because the delegatee's share is 25%
	oldVdrBalance, err := vm.getBalance(vm.DB, vdrDestSet)
	assert.NoError(t, err)
	commitVdrBalance, err := vm.getBalance(onCommitDB, vdrDestSet)
	assert.NoError(t, err)
	vdrReward, err := math.Sub64(commitVdrBalance, oldVdrBalance)
	assert.NoError(t, err)
	if vdrReward == 0 && InflationRate > 1.0 {
		t.Fatal("expected delegatee balance to increase because of reward")
	}

	oldDelBalance, err := vm.getBalance(vm.DB, delDestSet)
	assert.NoError(t, err)
	commitDelBalance, err := vm.getBalance(onCommitDB, delDestSet)
	assert.NoError(t, err)
	delReward, err := math.Sub64(commitDelBalance, oldDelBalance)
	assert.NoError(t, err)
	if delReward == 0 && InflationRate > 1.0 {
		t.Fatal("expected delegator balance to increase because of reward")
	}

	if delReward < vdrReward {
		t.Fatal("the delegator's reward should be greater than the delegatee's because the delegatee's share is 25%")
	}
	if delReward+vdrReward != expectedReward {
		t.Fatalf("expected total reward to be %d but is %d", expectedReward, delReward+vdrReward)
	}

	abortVdrBalance, err := vm.getBalance(onAbortDB, vdrDestSet)
	assert.NoError(t, err)
	vdrReward, err = math.Sub64(abortVdrBalance, oldVdrBalance)
	assert.NoError(t, err)
	if vdrReward != 0 {
		t.Fatal("expected delegatee balance to stay the same")
	}

	abortDelBalance, err := vm.getBalance(onAbortDB, delDestSet)
	assert.NoError(t, err)
	delReward, err = math.Sub64(abortDelBalance, oldDelBalance)
	assert.NoError(t, err)
	if delReward != 0 {
		t.Fatal("expected delegatee balance to stay the same")
	}
}
