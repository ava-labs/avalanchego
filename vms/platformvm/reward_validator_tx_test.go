// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/platformvm/uptime"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestUnsignedRewardValidatorTxSemanticVerifyOnCommit(t *testing.T) {
	vm, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	currentStakers := vm.internalState.CurrentStakerChainState()
	toRemoveTx, _, err := currentStakers.GetNextStaker()
	if err != nil {
		t.Fatal(err)
	}
	toRemove := toRemoveTx.UnsignedTx.(*UnsignedAddValidatorTx)

	// Case 1: Chain timestamp is wrong
	if tx, err := vm.newRewardValidatorTx(toRemove.ID()); err != nil {
		t.Fatal(err)
	} else if _, _, _, _, err := toRemove.SemanticVerify(vm, vm.internalState, tx); err == nil {
		t.Fatalf("should have failed because validator end time doesn't match chain timestamp")
	}

	// Advance chain timestamp to time that next validator leaves
	vm.internalState.SetTimestamp(toRemove.EndTime())

	// Case 2: Wrong validator
	if tx, err := vm.newRewardValidatorTx(ids.GenerateTestID()); err != nil {
		t.Fatal(err)
	} else if _, _, _, _, err := toRemove.SemanticVerify(vm, vm.internalState, tx); err == nil {
		t.Fatalf("should have failed because validator ID is wrong")
	}

	// Case 3: Happy path
	tx, err := vm.newRewardValidatorTx(toRemove.ID())
	if err != nil {
		t.Fatal(err)
	}

	onCommitState, _, _, _, err := tx.UnsignedTx.(UnsignedProposalTx).SemanticVerify(vm, vm.internalState, tx)
	if err != nil {
		t.Fatal(err)
	}

	onCommitCurrentStakers := onCommitState.CurrentStakerChainState()
	nextToRemoveTx, _, err := onCommitCurrentStakers.GetNextStaker()
	if err != nil {
		t.Fatal(err)
	}
	if toRemove.ID() == nextToRemoveTx.ID() {
		t.Fatalf("Should have removed the previous validator")
	}

	// check that stake/reward is given back
	stakeOwners := toRemove.Stake[0].Out.(*secp256k1fx.TransferOutput).AddressesSet()

	// Get old balances
	oldBalance, err := vm.getBalance(stakeOwners)
	if err != nil {
		t.Fatal(err)
	}

	onCommitState.Apply(vm.internalState)
	if err := vm.internalState.Commit(); err != nil {
		t.Fatal(err)
	}

	if err := vm.updateValidators(false); err != nil {
		t.Fatal(err)
	}

	onCommitBalance, err := vm.getBalance(stakeOwners)
	if err != nil {
		t.Fatal(err)
	}

	if onCommitBalance != oldBalance+toRemove.Validator.Weight()+27 {
		t.Fatalf("on commit, should have old balance (%d) + staked amount (%d) + reward (%d) but have %d",
			oldBalance, toRemove.Validator.Weight(), 27, onCommitBalance)
	}
}

func TestUnsignedRewardValidatorTxSemanticVerifyOnAbort(t *testing.T) {
	vm, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	currentStakers := vm.internalState.CurrentStakerChainState()
	toRemoveTx, _, err := currentStakers.GetNextStaker()
	if err != nil {
		t.Fatal(err)
	}
	toRemove := toRemoveTx.UnsignedTx.(*UnsignedAddValidatorTx)

	// Case 1: Chain timestamp is wrong
	if tx, err := vm.newRewardValidatorTx(toRemove.ID()); err != nil {
		t.Fatal(err)
	} else if _, _, _, _, err := toRemove.SemanticVerify(vm, vm.internalState, tx); err == nil {
		t.Fatalf("should have failed because validator end time doesn't match chain timestamp")
	}

	// Advance chain timestamp to time that next validator leaves
	vm.internalState.SetTimestamp(toRemove.EndTime())

	// Case 2: Wrong validator
	if tx, err := vm.newRewardValidatorTx(ids.GenerateTestID()); err != nil {
		t.Fatal(err)
	} else if _, _, _, _, err := toRemove.SemanticVerify(vm, vm.internalState, tx); err == nil {
		t.Fatalf("should have failed because validator ID is wrong")
	}

	// Case 3: Happy path
	tx, err := vm.newRewardValidatorTx(toRemove.ID())
	if err != nil {
		t.Fatal(err)
	}

	_, onAbortState, _, _, err := tx.UnsignedTx.(UnsignedProposalTx).SemanticVerify(vm, vm.internalState, tx)
	if err != nil {
		t.Fatal(err)
	}

	onAbortCurrentStakers := onAbortState.CurrentStakerChainState()
	nextToRemoveTx, _, err := onAbortCurrentStakers.GetNextStaker()
	if err != nil {
		t.Fatal(err)
	}
	if toRemove.ID() == nextToRemoveTx.ID() {
		t.Fatalf("Should have removed the previous validator")
	}

	// check that stake/reward isn't given back
	stakeOwners := toRemove.Stake[0].Out.(*secp256k1fx.TransferOutput).AddressesSet()

	// Get old balances
	oldBalance, err := vm.getBalance(stakeOwners)
	if err != nil {
		t.Fatal(err)
	}

	onAbortState.Apply(vm.internalState)
	if err := vm.internalState.Commit(); err != nil {
		t.Fatal(err)
	}

	if err := vm.updateValidators(false); err != nil {
		t.Fatal(err)
	}

	onAbortBalance, err := vm.getBalance(stakeOwners)
	if err != nil {
		t.Fatal(err)
	}

	if onAbortBalance != oldBalance+toRemove.Validator.Weight() {
		t.Fatalf("on abort, should have old balance (%d) + staked amount (%d) but have %d",
			oldBalance, toRemove.Validator.Weight(), onAbortBalance)
	}
}

func TestRewardDelegatorTxSemanticVerifyOnCommit(t *testing.T) {
	assert := assert.New(t)

	vm, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)

		vm.ctx.Lock.Unlock()
	}()

	vdrRewardAddress := ids.GenerateTestShortID()
	delRewardAddress := ids.GenerateTestShortID()

	vdrStartTime := uint64(defaultValidateStartTime.Unix()) + 1
	vdrEndTime := uint64(defaultValidateStartTime.Add(2 * defaultMinStakingDuration).Unix())
	vdrNodeID := ids.GenerateTestShortID()
	vdrTx, err := vm.newAddValidatorTx(
		vm.MinValidatorStake, // stakeAmt
		vdrStartTime,
		vdrEndTime,
		vdrNodeID,        // node ID
		vdrRewardAddress, // reward address
		PercentDenominator/4,
		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // fee payer
		ids.ShortEmpty,                          // change addr
	)
	assert.NoError(err)

	delStartTime := vdrStartTime
	delEndTime := vdrEndTime
	delTx, err := vm.newAddDelegatorTx(
		vm.MinDelegatorStake, // stakeAmt
		delStartTime,
		delEndTime,
		vdrNodeID,                               // node ID
		delRewardAddress,                        // reward address
		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // fee payer
		ids.ShortEmpty,                          // change addr
	)
	assert.NoError(err)

	vm.internalState.AddCurrentStaker(vdrTx, 0)
	vm.internalState.AddTx(vdrTx, Committed)
	vm.internalState.AddCurrentStaker(delTx, 1000000)
	vm.internalState.AddTx(delTx, Committed)
	vm.internalState.SetTimestamp(time.Unix(int64(delEndTime), 0))
	err = vm.internalState.Commit()
	assert.NoError(err)
	err = vm.internalState.(*internalStateImpl).loadCurrentValidators()
	assert.NoError(err)

	tx, err := vm.newRewardValidatorTx(delTx.ID())
	assert.NoError(err)

	onCommitState, _, _, _, err := tx.UnsignedTx.(UnsignedProposalTx).SemanticVerify(vm, vm.internalState, tx)
	assert.NoError(err)

	vdrDestSet := ids.ShortSet{}
	vdrDestSet.Add(vdrRewardAddress)
	delDestSet := ids.ShortSet{}
	delDestSet.Add(delRewardAddress)

	expectedReward := uint64(1000000)

	oldVdrBalance, err := vm.getBalance(vdrDestSet)
	assert.NoError(err)
	oldDelBalance, err := vm.getBalance(delDestSet)
	assert.NoError(err)

	onCommitState.Apply(vm.internalState)
	err = vm.internalState.Commit()
	assert.NoError(err)

	// If tx is committed, delegator and delegatee should get reward
	// and the delegator's reward should be greater because the delegatee's share is 25%
	commitVdrBalance, err := vm.getBalance(vdrDestSet)
	assert.NoError(err)
	vdrReward, err := math.Sub64(commitVdrBalance, oldVdrBalance)
	assert.NoError(err)
	assert.NotZero(vdrReward, "expected delegatee balance to increase because of reward")

	commitDelBalance, err := vm.getBalance(delDestSet)
	assert.NoError(err)
	delReward, err := math.Sub64(commitDelBalance, oldDelBalance)
	assert.NoError(err)
	assert.NotZero(delReward, "expected delegator balance to increase because of reward")

	assert.Less(vdrReward, delReward, "the delegator's reward should be greater than the delegatee's because the delegatee's share is 25%")
	assert.Equal(expectedReward, delReward+vdrReward, "expected total reward to be %d but is %d", expectedReward, delReward+vdrReward)
}

func TestRewardDelegatorTxSemanticVerifyOnAbort(t *testing.T) {
	assert := assert.New(t)

	vm, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)

		vm.ctx.Lock.Unlock()
	}()

	initialSupply := vm.internalState.GetCurrentSupply()

	vdrRewardAddress := ids.GenerateTestShortID()
	delRewardAddress := ids.GenerateTestShortID()

	vdrStartTime := uint64(defaultValidateStartTime.Unix()) + 1
	vdrEndTime := uint64(defaultValidateStartTime.Add(2 * defaultMinStakingDuration).Unix())
	vdrNodeID := ids.GenerateTestShortID()
	vdrTx, err := vm.newAddValidatorTx(
		vm.MinValidatorStake, // stakeAmt
		vdrStartTime,
		vdrEndTime,
		vdrNodeID,        // node ID
		vdrRewardAddress, // reward address
		PercentDenominator/4,
		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // fee payer
		ids.ShortEmpty,                          // change addr
	)
	assert.NoError(err)

	delStartTime := vdrStartTime
	delEndTime := vdrEndTime
	delTx, err := vm.newAddDelegatorTx(
		vm.MinDelegatorStake, // stakeAmt
		delStartTime,
		delEndTime,
		vdrNodeID,                               // node ID
		delRewardAddress,                        // reward address
		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // fee payer
		ids.ShortEmpty,                          // change addr
	)
	assert.NoError(err)

	vm.internalState.AddCurrentStaker(vdrTx, 0)
	vm.internalState.AddTx(vdrTx, Committed)
	vm.internalState.AddCurrentStaker(delTx, 1000000)
	vm.internalState.AddTx(delTx, Committed)
	vm.internalState.SetTimestamp(time.Unix(int64(delEndTime), 0))
	err = vm.internalState.Commit()
	assert.NoError(err)
	err = vm.internalState.(*internalStateImpl).loadCurrentValidators()
	assert.NoError(err)

	tx, err := vm.newRewardValidatorTx(delTx.ID())
	assert.NoError(err)

	_, onAbortState, _, _, err := tx.UnsignedTx.(UnsignedProposalTx).SemanticVerify(vm, vm.internalState, tx)
	assert.NoError(err)

	vdrDestSet := ids.ShortSet{}
	vdrDestSet.Add(vdrRewardAddress)
	delDestSet := ids.ShortSet{}
	delDestSet.Add(delRewardAddress)

	expectedReward := uint64(1000000)

	oldVdrBalance, err := vm.getBalance(vdrDestSet)
	assert.NoError(err)
	oldDelBalance, err := vm.getBalance(delDestSet)
	assert.NoError(err)

	onAbortState.Apply(vm.internalState)
	err = vm.internalState.Commit()
	assert.NoError(err)

	// If tx is aborted, delegator and delegatee shouldn't get reward
	newVdrBalance, err := vm.getBalance(vdrDestSet)
	assert.NoError(err)
	vdrReward, err := math.Sub64(newVdrBalance, oldVdrBalance)
	assert.NoError(err)
	assert.Zero(vdrReward, "expected delegatee balance not to increase")

	newDelBalance, err := vm.getBalance(delDestSet)
	assert.NoError(err)
	delReward, err := math.Sub64(newDelBalance, oldDelBalance)
	assert.NoError(err)
	assert.Zero(delReward, "expected delegator balance not to increase")

	newSupply := vm.internalState.GetCurrentSupply()
	assert.Equal(initialSupply-expectedReward, newSupply, "should have removed un-rewarded tokens from the potential supply")
}

func TestUptimeDisallowed(t *testing.T) {
	_, genesisBytes := defaultGenesis()
	db := manager.NewMemDB(version.DefaultVersion1_0_0)

	firstDB := db.NewPrefixDBManager([]byte{})
	firstVM := &VM{Factory: Factory{
		Chains:             chains.MockManager{},
		UptimePercentage:   .2,
		StakeMintingPeriod: defaultMaxStakingDuration,
		Validators:         validators.NewManager(),
	}}

	firstCtx := defaultContext()
	firstCtx.Lock.Lock()

	firstMsgChan := make(chan common.Message, 1)
	if err := firstVM.Initialize(firstCtx, firstDB, genesisBytes, nil, nil, firstMsgChan, nil, nil); err != nil {
		t.Fatal(err)
	}

	firstVM.clock.Set(defaultGenesisTime)
	firstVM.Manager.(uptime.TestManager).SetTime(defaultGenesisTime)

	if err := firstVM.Bootstrapping(); err != nil {
		t.Fatal(err)
	}

	if err := firstVM.Bootstrapped(); err != nil {
		t.Fatal(err)
	}

	// Fast forward clock to time for genesis validators to leave
	firstVM.Manager.(uptime.TestManager).SetTime(defaultValidateEndTime)

	if err := firstVM.Shutdown(); err != nil {
		t.Fatal(err)
	}
	firstCtx.Lock.Unlock()

	secondDB := db.NewPrefixDBManager([]byte{})
	secondVM := &VM{Factory: Factory{
		Chains:           chains.MockManager{},
		UptimePercentage: .21,
		Validators:       validators.NewManager(),
	}}

	secondCtx := defaultContext()
	secondCtx.Lock.Lock()
	defer func() {
		if err := secondVM.Shutdown(); err != nil {
			t.Fatal(err)
		}
		secondCtx.Lock.Unlock()
	}()

	secondMsgChan := make(chan common.Message, 1)
	if err := secondVM.Initialize(secondCtx, secondDB, genesisBytes, nil, nil, secondMsgChan, nil, nil); err != nil {
		t.Fatal(err)
	}

	secondVM.clock.Set(defaultValidateStartTime.Add(2 * defaultMinStakingDuration))
	secondVM.Manager.(uptime.TestManager).SetTime(defaultValidateStartTime.Add(2 * defaultMinStakingDuration))

	if err := secondVM.Bootstrapping(); err != nil {
		t.Fatal(err)
	}

	if err := secondVM.Bootstrapped(); err != nil {
		t.Fatal(err)
	}

	secondVM.clock.Set(defaultValidateEndTime)
	secondVM.Manager.(uptime.TestManager).SetTime(defaultValidateEndTime)

	blk, err := secondVM.BuildBlock() // should contain proposal to advance time
	if err != nil {
		t.Fatal(err)
	} else if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	// Assert preferences are correct
	block := blk.(*ProposalBlock)
	options, err := block.Options()
	if err != nil {
		t.Fatal(err)
	}

	commit, ok := options[0].(*CommitBlock)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	}

	abort, ok := options[1].(*AbortBlock)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	}

	if err := block.Accept(); err != nil {
		t.Fatal(err)
	}
	if err := commit.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := abort.Verify(); err != nil {
		t.Fatal(err)
	}

	onAbortState := abort.onAccept()
	_, status, err := onAbortState.GetTx(block.Tx.ID())
	if err != nil {
		t.Fatal(err)
	}
	if status != Aborted {
		t.Fatalf("status should be Aborted but is %s", status)
	}

	if err := commit.Accept(); err != nil { // advance the timestamp
		t.Fatal(err)
	}

	_, status, err = secondVM.internalState.GetTx(block.Tx.ID())
	if err != nil {
		t.Fatal(err)
	}
	if status != Committed {
		t.Fatalf("status should be Committed but is %s", status)
	}

	// Verify that chain's timestamp has advanced
	timestamp := secondVM.internalState.GetTimestamp()
	if !timestamp.Equal(defaultValidateEndTime) {
		t.Fatal("expected timestamp to have advanced")
	}

	blk, err = secondVM.BuildBlock() // should contain proposal to reward genesis validator
	if err != nil {
		t.Fatal(err)
	}
	if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	block = blk.(*ProposalBlock)
	options, err = block.Options()
	if err != nil {
		t.Fatal(err)
	}

	commit, ok = options[1].(*CommitBlock)
	if !ok {
		t.Fatal(errShouldPrefAbort)
	}

	abort, ok = options[0].(*AbortBlock)
	if !ok {
		t.Fatal(errShouldPrefAbort)
	}

	if err := blk.Accept(); err != nil {
		t.Fatal(err)
	}
	if err := commit.Verify(); err != nil {
		t.Fatal(err)
	}

	onCommitState := commit.onAccept()
	_, status, err = onCommitState.GetTx(block.Tx.ID())
	if err != nil {
		t.Fatal(err)
	}
	if status != Committed {
		t.Fatalf("status should be Committed but is %s", status)
	}

	if err := abort.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := abort.Accept(); err != nil { // do not reward the genesis validator
		t.Fatal(err)
	}

	_, status, err = secondVM.internalState.GetTx(block.Tx.ID())
	if err != nil {
		t.Fatal(err)
	}
	if status != Aborted {
		t.Fatalf("status should be Aborted but is %s", status)
	}

	currentStakers := secondVM.internalState.CurrentStakerChainState()
	_, err = currentStakers.GetValidator(keys[1].PublicKey().Address())
	if err == nil {
		t.Fatal("should have removed a genesis validator")
	}
}
