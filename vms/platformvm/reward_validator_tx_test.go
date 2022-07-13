// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestUnsignedRewardValidatorTxExecuteOnCommit(t *testing.T) {
	assert := assert.New(t)
	vm, _, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
		vm.ctx.Lock.Unlock()
	}()

	currentStakerIterator, err := vm.internalState.GetCurrentStakerIterator()
	assert.NoError(err)
	assert.True(currentStakerIterator.Next())
	stakerToRemove := currentStakerIterator.Value()
	currentStakerIterator.Release()

	{
		// Case 1: Chain timestamp is wrong
		tx, err := vm.txBuilder.NewRewardValidatorTx(stakerToRemove.TxID)
		if err != nil {
			t.Fatal(err)
		}

		executor := proposalTxExecutor{
			vm:            vm,
			parentID:      vm.preferred,
			stateVersions: vm.stateVersions,
			tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
		if err == nil {
			t.Fatalf("should have failed because validator end time doesn't match chain timestamp")
		}
	}

	// Advance chain timestamp to time that next validator leaves
	vm.internalState.SetTimestamp(stakerToRemove.EndTime)

	{
		// Case 2: Wrong validator
		tx, err := vm.txBuilder.NewRewardValidatorTx(ids.GenerateTestID())
		if err != nil {
			t.Fatal(err)
		}

		executor := proposalTxExecutor{
			vm:            vm,
			parentID:      vm.preferred,
			stateVersions: vm.stateVersions,
			tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
		if err == nil {
			t.Fatalf("should have failed because validator ID is wrong")
		}
	}

	// Case 3: Happy path
	tx, err := vm.txBuilder.NewRewardValidatorTx(stakerToRemove.TxID)
	if err != nil {
		t.Fatal(err)
	}

	executor := proposalTxExecutor{
		vm:            vm,
		parentID:      vm.preferred,
		stateVersions: vm.stateVersions,
		tx:            tx,
	}
	err = tx.Unsigned.Visit(&executor)
	if err != nil {
		t.Fatal(err)
	}

	currentStakerIterator, err = executor.onCommit.GetCurrentStakerIterator()
	assert.NoError(err)
	assert.True(currentStakerIterator.Next())
	nextStakerToRemove := currentStakerIterator.Value()
	currentStakerIterator.Release()
	assert.NotEqual(stakerToRemove.TxID, nextStakerToRemove.TxID)

	toRemoveTxIntf, _, err := vm.internalState.GetTx(stakerToRemove.TxID)
	assert.NoError(err)

	toRemoveTx, ok := toRemoveTxIntf.Unsigned.(*txs.AddValidatorTx)
	assert.True(ok)

	// check that stake/reward is given back
	stakeOwners := toRemoveTx.Stake[0].Out.(*secp256k1fx.TransferOutput).AddressesSet()

	// Get old balances
	oldBalance, err := avax.GetBalance(vm.internalState, stakeOwners)
	assert.NoError(err)

	executor.onCommit.Apply(vm.internalState)
	assert.NoError(vm.internalState.Commit())

	onCommitBalance, err := avax.GetBalance(vm.internalState, stakeOwners)
	assert.NoError(err)
	assert.EqualValues(oldBalance+stakerToRemove.Weight+stakerToRemove.PotentialReward, onCommitBalance)
}

func TestUnsignedRewardValidatorTxExecuteOnAbort(t *testing.T) {
	assert := assert.New(t)
	vm, _, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
		vm.ctx.Lock.Unlock()
	}()

	currentStakerIterator, err := vm.internalState.GetCurrentStakerIterator()
	assert.NoError(err)
	assert.True(currentStakerIterator.Next())
	stakerToRemove := currentStakerIterator.Value()
	currentStakerIterator.Release()

	toRemoveTxIntf, _, err := vm.internalState.GetTx(stakerToRemove.TxID)
	assert.NoError(err)

	toRemoveTx, ok := toRemoveTxIntf.Unsigned.(*txs.AddValidatorTx)
	assert.True(ok)

	{
		// Case 1: Chain timestamp is wrong
		tx, err := vm.txBuilder.NewRewardValidatorTx(stakerToRemove.TxID)
		if err != nil {
			t.Fatal(err)
		}

		executor := proposalTxExecutor{
			vm:            vm,
			parentID:      vm.preferred,
			stateVersions: vm.stateVersions,
			tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
		if err == nil {
			t.Fatalf("should have failed because validator end time doesn't match chain timestamp")
		}
	}

	// Advance chain timestamp to time that next validator leaves
	vm.internalState.SetTimestamp(stakerToRemove.EndTime)

	{
		// Case 2: Wrong validator
		tx, err := vm.txBuilder.NewRewardValidatorTx(ids.GenerateTestID())
		if err != nil {
			t.Fatal(err)
		}

		executor := proposalTxExecutor{
			vm:            vm,
			parentID:      vm.preferred,
			stateVersions: vm.stateVersions,
			tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
		if err == nil {
			t.Fatalf("should have failed because validator ID is wrong")
		}
	}

	{
		// Case 3: Happy path
		tx, err := vm.txBuilder.NewRewardValidatorTx(stakerToRemove.TxID)
		if err != nil {
			t.Fatal(err)
		}

		executor := proposalTxExecutor{
			vm:            vm,
			parentID:      vm.preferred,
			stateVersions: vm.stateVersions,
			tx:            tx,
		}
		err = tx.Unsigned.Visit(&executor)
		if err != nil {
			t.Fatal(err)
		}

		currentStakerIterator, err = executor.onAbort.GetCurrentStakerIterator()
		assert.NoError(err)
		assert.True(currentStakerIterator.Next())
		nextStakerToRemove := currentStakerIterator.Value()
		currentStakerIterator.Release()
		assert.NotEqual(stakerToRemove.TxID, nextStakerToRemove.TxID)

		// check that stake is given back but the reward is not provided
		stakeOwners := toRemoveTx.Stake[0].Out.(*secp256k1fx.TransferOutput).AddressesSet()

		// Get old balances
		oldBalance, err := avax.GetBalance(vm.internalState, stakeOwners)
		assert.NoError(err)

		executor.onAbort.Apply(vm.internalState)
		assert.NoError(vm.internalState.Commit())

		onAbortBalance, err := avax.GetBalance(vm.internalState, stakeOwners)
		assert.NoError(err)
		assert.EqualValues(oldBalance+stakerToRemove.Weight, onAbortBalance)
	}
}

func TestRewardDelegatorTxExecuteOnCommit(t *testing.T) {
	assert := assert.New(t)
	vm, _, _, _ := defaultVM()
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
	vdrNodeID := ids.GenerateTestNodeID()
	vdrTx, err := vm.txBuilder.NewAddValidatorTx(
		vm.MinValidatorStake, // stakeAmount
		vdrStartTime,
		vdrEndTime,
		vdrNodeID,        // node ID
		vdrRewardAddress, // reward address
		reward.PercentDenominator/4,
		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // fee payer
		ids.ShortEmpty,                          // change addr
	)
	assert.NoError(err)

	delStartTime := vdrStartTime
	delEndTime := vdrEndTime
	delTx, err := vm.txBuilder.NewAddDelegatorTx(
		vm.MinDelegatorStake, // stakeAmount
		delStartTime,
		delEndTime,
		vdrNodeID,                               // node ID
		delRewardAddress,                        // reward address
		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // fee payer
		ids.ShortEmpty,                          // change addr
	)
	assert.NoError(err)

	stakerVdr := state.NewPrimaryNetworkStaker(vdrTx.ID(), &vdrTx.Unsigned.(*txs.AddValidatorTx).Validator)
	stakerVdr.PotentialReward = 0
	stakerVdr.NextTime = stakerVdr.EndTime
	stakerVdr.Priority = state.PrimaryNetworkValidatorCurrentPriority

	stakerDel := state.NewPrimaryNetworkStaker(delTx.ID(), &delTx.Unsigned.(*txs.AddDelegatorTx).Validator)
	stakerDel.PotentialReward = 1000000
	stakerDel.NextTime = stakerDel.EndTime
	stakerDel.Priority = state.PrimaryNetworkDelegatorCurrentPriority

	vm.internalState.PutCurrentValidator(stakerVdr)
	vm.internalState.AddTx(vdrTx, status.Committed)
	vm.internalState.PutCurrentDelegator(stakerDel)
	vm.internalState.AddTx(delTx, status.Committed)
	vm.internalState.SetTimestamp(time.Unix(int64(delEndTime), 0))
	assert.NoError(vm.internalState.Commit())
	assert.NoError(vm.internalState.Load())
	// test validator stake
	set, ok := vm.Validators.GetValidators(constants.PrimaryNetworkID)
	assert.True(ok)
	stake, ok := set.GetWeight(vdrNodeID)
	assert.True(ok)
	assert.Equal(vm.MinValidatorStake+vm.MinDelegatorStake, stake)

	tx, err := vm.txBuilder.NewRewardValidatorTx(delTx.ID())
	assert.NoError(err)

	executor := proposalTxExecutor{
		vm:            vm,
		parentID:      vm.preferred,
		stateVersions: vm.stateVersions,
		tx:            tx,
	}
	err = tx.Unsigned.Visit(&executor)
	assert.NoError(err)

	vdrDestSet := ids.ShortSet{}
	vdrDestSet.Add(vdrRewardAddress)
	delDestSet := ids.ShortSet{}
	delDestSet.Add(delRewardAddress)

	expectedReward := uint64(1000000)

	oldVdrBalance, err := avax.GetBalance(vm.internalState, vdrDestSet)
	assert.NoError(err)
	oldDelBalance, err := avax.GetBalance(vm.internalState, delDestSet)
	assert.NoError(err)

	executor.onCommit.Apply(vm.internalState)
	err = vm.internalState.Commit()
	assert.NoError(err)

	// If tx is committed, delegator and delegatee should get reward
	// and the delegator's reward should be greater because the delegatee's share is 25%
	commitVdrBalance, err := avax.GetBalance(vm.internalState, vdrDestSet)
	assert.NoError(err)
	vdrReward, err := math.Sub64(commitVdrBalance, oldVdrBalance)
	assert.NoError(err)
	assert.NotZero(vdrReward, "expected delegatee balance to increase because of reward")

	commitDelBalance, err := avax.GetBalance(vm.internalState, delDestSet)
	assert.NoError(err)
	delReward, err := math.Sub64(commitDelBalance, oldDelBalance)
	assert.NoError(err)
	assert.NotZero(delReward, "expected delegator balance to increase because of reward")

	assert.Less(vdrReward, delReward, "the delegator's reward should be greater than the delegatee's because the delegatee's share is 25%")
	assert.Equal(expectedReward, delReward+vdrReward, "expected total reward to be %d but is %d", expectedReward, delReward+vdrReward)

	stake, ok = set.GetWeight(vdrNodeID)
	assert.True(ok)
	assert.Equal(vm.MinValidatorStake, stake)
}

func TestRewardDelegatorTxExecuteOnAbort(t *testing.T) {
	assert := assert.New(t)

	vm, _, _, _ := defaultVM()
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
	vdrNodeID := ids.GenerateTestNodeID()
	vdrTx, err := vm.txBuilder.NewAddValidatorTx(
		vm.MinValidatorStake, // stakeAmount
		vdrStartTime,
		vdrEndTime,
		vdrNodeID,        // node ID
		vdrRewardAddress, // reward address
		reward.PercentDenominator/4,
		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // fee payer
		ids.ShortEmpty,                          // change addr
	)
	assert.NoError(err)

	delStartTime := vdrStartTime
	delEndTime := vdrEndTime
	delTx, err := vm.txBuilder.NewAddDelegatorTx(
		vm.MinDelegatorStake, // stakeAmount
		delStartTime,
		delEndTime,
		vdrNodeID,                               // node ID
		delRewardAddress,                        // reward address
		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // fee payer
		ids.ShortEmpty,                          // change addr
	)
	assert.NoError(err)

	stakerVdr := state.NewPrimaryNetworkStaker(vdrTx.ID(), &vdrTx.Unsigned.(*txs.AddValidatorTx).Validator)
	stakerVdr.PotentialReward = 0
	stakerVdr.NextTime = stakerVdr.EndTime
	stakerVdr.Priority = state.PrimaryNetworkValidatorCurrentPriority

	stakerDel := state.NewPrimaryNetworkStaker(delTx.ID(), &delTx.Unsigned.(*txs.AddDelegatorTx).Validator)
	stakerDel.PotentialReward = 1000000
	stakerDel.NextTime = stakerDel.EndTime
	stakerDel.Priority = state.PrimaryNetworkDelegatorCurrentPriority

	vm.internalState.PutCurrentValidator(stakerVdr)
	vm.internalState.AddTx(vdrTx, status.Committed)
	vm.internalState.PutCurrentDelegator(stakerDel)
	vm.internalState.AddTx(delTx, status.Committed)
	vm.internalState.SetTimestamp(time.Unix(int64(delEndTime), 0))
	assert.NoError(vm.internalState.Commit())
	assert.NoError(vm.internalState.Load())

	tx, err := vm.txBuilder.NewRewardValidatorTx(delTx.ID())
	assert.NoError(err)

	executor := proposalTxExecutor{
		vm:            vm,
		parentID:      vm.preferred,
		stateVersions: vm.stateVersions,
		tx:            tx,
	}
	err = tx.Unsigned.Visit(&executor)
	assert.NoError(err)

	vdrDestSet := ids.ShortSet{}
	vdrDestSet.Add(vdrRewardAddress)
	delDestSet := ids.ShortSet{}
	delDestSet.Add(delRewardAddress)

	expectedReward := uint64(1000000)

	oldVdrBalance, err := avax.GetBalance(vm.internalState, vdrDestSet)
	assert.NoError(err)
	oldDelBalance, err := avax.GetBalance(vm.internalState, delDestSet)
	assert.NoError(err)

	executor.onAbort.Apply(vm.internalState)
	err = vm.internalState.Commit()
	assert.NoError(err)

	// If tx is aborted, delegator and delegatee shouldn't get reward
	newVdrBalance, err := avax.GetBalance(vm.internalState, vdrDestSet)
	assert.NoError(err)
	vdrReward, err := math.Sub64(newVdrBalance, oldVdrBalance)
	assert.NoError(err)
	assert.Zero(vdrReward, "expected delegatee balance not to increase")

	newDelBalance, err := avax.GetBalance(vm.internalState, delDestSet)
	assert.NoError(err)
	delReward, err := math.Sub64(newDelBalance, oldDelBalance)
	assert.NoError(err)
	assert.Zero(delReward, "expected delegator balance not to increase")

	newSupply := vm.internalState.GetCurrentSupply()
	assert.Equal(initialSupply-expectedReward, newSupply, "should have removed un-rewarded tokens from the potential supply")
}

func TestUptimeDisallowedWithRestart(t *testing.T) {
	assert := assert.New(t)
	_, genesisBytes := defaultGenesis()
	db := manager.NewMemDB(version.Semantic1_0_0)

	firstDB := db.NewPrefixDBManager([]byte{})
	firstVM := &VM{Factory: Factory{
		Config: config.Config{
			Chains:                 chains.MockManager{},
			UptimePercentage:       .2,
			RewardConfig:           defaultRewardConfig,
			Validators:             validators.NewManager(),
			UptimeLockedCalculator: uptime.NewLockedCalculator(),
		},
	}}

	firstCtx := defaultContext()
	firstCtx.Lock.Lock()

	firstMsgChan := make(chan common.Message, 1)
	err := firstVM.Initialize(firstCtx, firstDB, genesisBytes, nil, nil, firstMsgChan, nil, nil)
	assert.NoError(err)

	firstVM.clock.Set(defaultGenesisTime)
	firstVM.uptimeManager.(uptime.TestManager).SetTime(defaultGenesisTime)

	err = firstVM.SetState(snow.Bootstrapping)
	assert.NoError(err)

	err = firstVM.SetState(snow.NormalOp)
	assert.NoError(err)

	// Fast forward clock to time for genesis validators to leave
	firstVM.uptimeManager.(uptime.TestManager).SetTime(defaultValidateEndTime)

	err = firstVM.Shutdown()
	assert.NoError(err)

	firstCtx.Lock.Unlock()

	secondDB := db.NewPrefixDBManager([]byte{})
	secondVM := &VM{Factory: Factory{
		Config: config.Config{
			Chains:                 chains.MockManager{},
			UptimePercentage:       .21,
			Validators:             validators.NewManager(),
			UptimeLockedCalculator: uptime.NewLockedCalculator(),
		},
	}}

	secondCtx := defaultContext()
	secondCtx.Lock.Lock()
	defer func() {
		err := secondVM.Shutdown()
		assert.NoError(err)
		secondCtx.Lock.Unlock()
	}()

	secondMsgChan := make(chan common.Message, 1)
	err = secondVM.Initialize(secondCtx, secondDB, genesisBytes, nil, nil, secondMsgChan, nil, nil)
	assert.NoError(err)

	secondVM.clock.Set(defaultValidateStartTime.Add(2 * defaultMinStakingDuration))
	secondVM.uptimeManager.(uptime.TestManager).SetTime(defaultValidateStartTime.Add(2 * defaultMinStakingDuration))

	err = secondVM.SetState(snow.Bootstrapping)
	assert.NoError(err)

	err = secondVM.SetState(snow.NormalOp)
	assert.NoError(err)

	secondVM.clock.Set(defaultValidateEndTime)
	secondVM.uptimeManager.(uptime.TestManager).SetTime(defaultValidateEndTime)

	blk, err := secondVM.BuildBlock() // should contain proposal to advance time
	assert.NoError(err)

	err = blk.Verify()
	assert.NoError(err)

	// Assert preferences are correct
	block := blk.(*ProposalBlock)
	options, err := block.Options()
	assert.NoError(err)

	commit, ok := options[0].(*CommitBlock)
	assert.True(ok)

	abort, ok := options[1].(*AbortBlock)
	assert.True(ok)

	err = block.Accept()
	assert.NoError(err)

	err = commit.Verify()
	assert.NoError(err)

	err = abort.Verify()
	assert.NoError(err)

	_, txStatus, err := abort.onAcceptState.GetTx(block.Tx.ID())
	assert.NoError(err)
	assert.Equal(status.Aborted, txStatus)

	err = commit.Accept() // advance the timestamp
	assert.NoError(err)

	err = secondVM.SetPreference(secondVM.lastAcceptedID)
	assert.NoError(err)

	_, txStatus, err = secondVM.internalState.GetTx(block.Tx.ID())
	assert.NoError(err)
	assert.Equal(status.Committed, txStatus)

	// Verify that chain's timestamp has advanced
	timestamp := secondVM.internalState.GetTimestamp()
	assert.Equal(defaultValidateEndTime.Unix(), timestamp.Unix())

	blk, err = secondVM.BuildBlock() // should contain proposal to reward genesis validator
	assert.NoError(err)

	err = blk.Verify()
	assert.NoError(err)

	block = blk.(*ProposalBlock)
	options, err = block.Options()
	assert.NoError(err)

	commit, ok = options[1].(*CommitBlock)
	assert.True(ok)

	abort, ok = options[0].(*AbortBlock)
	assert.True(ok)

	err = blk.Accept()
	assert.NoError(err)

	err = commit.Verify()
	assert.NoError(err)

	_, txStatus, err = commit.onAcceptState.GetTx(block.Tx.ID())
	assert.NoError(err)
	assert.Equal(status.Committed, txStatus)

	err = abort.Verify()
	assert.NoError(err)

	err = abort.Accept() // do not reward the genesis validator
	assert.NoError(err)

	_, txStatus, err = secondVM.internalState.GetTx(block.Tx.ID())
	assert.NoError(err)
	assert.Equal(status.Aborted, txStatus)

	_, err = secondVM.internalState.GetCurrentValidator(constants.PrimaryNetworkID, ids.NodeID(keys[1].PublicKey().Address()))
	assert.ErrorIs(err, database.ErrNotFound)
}

func TestUptimeDisallowedAfterNeverConnecting(t *testing.T) {
	assert := assert.New(t)
	_, genesisBytes := defaultGenesis()
	db := manager.NewMemDB(version.Semantic1_0_0)

	vm := &VM{Factory: Factory{
		Config: config.Config{
			Chains:                 chains.MockManager{},
			UptimePercentage:       .2,
			RewardConfig:           defaultRewardConfig,
			Validators:             validators.NewManager(),
			UptimeLockedCalculator: uptime.NewLockedCalculator(),
		},
	}}

	ctx := defaultContext()
	ctx.Lock.Lock()

	msgChan := make(chan common.Message, 1)
	appSender := &common.SenderTest{T: t}
	err := vm.Initialize(ctx, db, genesisBytes, nil, nil, msgChan, nil, appSender)
	assert.NoError(err)
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
		ctx.Lock.Unlock()
	}()

	vm.clock.Set(defaultGenesisTime)
	vm.uptimeManager.(uptime.TestManager).SetTime(defaultGenesisTime)

	err = vm.SetState(snow.Bootstrapping)
	assert.NoError(err)

	err = vm.SetState(snow.NormalOp)
	assert.NoError(err)

	// Fast forward clock to time for genesis validators to leave
	vm.clock.Set(defaultValidateEndTime)
	vm.uptimeManager.(uptime.TestManager).SetTime(defaultValidateEndTime)

	blk, err := vm.BuildBlock() // should contain proposal to advance time
	assert.NoError(err)

	err = blk.Verify()
	assert.NoError(err)

	// first the time will be advanced.
	block := blk.(*ProposalBlock)
	options, err := block.Options()
	assert.NoError(err)

	commit, ok := options[0].(*CommitBlock)
	assert.True(ok)

	abort, ok := options[1].(*AbortBlock)
	assert.True(ok)

	err = block.Accept()
	assert.NoError(err)

	err = commit.Verify()
	assert.NoError(err)

	err = abort.Verify()
	assert.NoError(err)

	// advance the timestamp
	err = commit.Accept()
	assert.NoError(err)

	err = vm.SetPreference(vm.lastAcceptedID)
	assert.NoError(err)

	// Verify that chain's timestamp has advanced
	timestamp := vm.internalState.GetTimestamp()
	assert.Equal(defaultValidateEndTime.Unix(), timestamp.Unix())

	// should contain proposal to reward genesis validator
	blk, err = vm.BuildBlock()
	assert.NoError(err)

	err = blk.Verify()
	assert.NoError(err)

	block = blk.(*ProposalBlock)
	options, err = block.Options()
	assert.NoError(err)

	abort, ok = options[0].(*AbortBlock)
	assert.True(ok)

	commit, ok = options[1].(*CommitBlock)
	assert.True(ok)

	err = blk.Accept()
	assert.NoError(err)

	err = commit.Verify()
	assert.NoError(err)

	err = abort.Verify()
	assert.NoError(err)

	// do not reward the genesis validator
	err = abort.Accept()
	assert.NoError(err)

	_, err = vm.internalState.GetCurrentValidator(constants.PrimaryNetworkID, ids.NodeID(keys[1].PublicKey().Address()))
	assert.ErrorIs(err, database.ErrNotFound)
}
