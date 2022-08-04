// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"fmt"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/executor/version"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"

	txexecutor "github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

func TestApricotStandardBlockTimeVerification(t *testing.T) {
	assert := assert.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	env := newEnvironment(t, ctrl)
	defer func() {
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()

	// setup and store parent block
	// it's a standard block for simplicity
	blksVersion := uint16(blocks.ApricotVersion)
	parentTime := time.Time{}
	parentHeight := uint64(2022)

	apricotParentBlk, err := blocks.NewStandardBlock(
		blksVersion,
		parentTime,
		ids.Empty, // does not matter
		parentHeight,
		nil, // txs do not matter in this test
	)
	assert.NoError(err)
	parentID := apricotParentBlk.ID()

	// store parent block, with relevant quantities
	env.blkManager.(*manager).blkIDToState[parentID] = &blockState{
		statelessBlock: apricotParentBlk,
	}
	env.blkManager.(*manager).lastAccepted = parentID
	env.blkManager.(*manager).stateVersions.SetState(parentID, env.mockedState)
	env.mockedState.EXPECT().GetLastAccepted().Return(parentID).AnyTimes()

	chainTime := env.clk.Time().Truncate(time.Second)
	env.mockedState.EXPECT().GetTimestamp().Return(chainTime).AnyTimes()
	env.mockedState.EXPECT().GetCurrentSupply().Return(uint64(1000)).AnyTimes()

	// wrong height
	apricotChildBlk, err := blocks.NewStandardBlock(
		blksVersion,
		parentTime,
		apricotParentBlk.ID(),
		apricotParentBlk.Height(),
		nil, // txs nulled to simplify test
	)
	assert.NoError(err)
	block := env.blkManager.NewBlock(apricotChildBlk)
	assert.Error(block.Verify())

	// valid height
	apricotChildBlk, err = blocks.NewStandardBlock(
		blocks.ApricotVersion,
		parentTime,
		apricotParentBlk.ID(),
		apricotParentBlk.Height()+1,
		nil, // txs nulled to simplify test
	)
	assert.NoError(err)
	block = env.blkManager.NewBlock(apricotChildBlk)
	assert.NoError(block.Verify())
}

func TestBlueberryStandardBlockTimeVerification(t *testing.T) {
	assert := assert.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	env := newEnvironment(t, ctrl)
	defer func() {
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()
	now := env.clk.Time()
	env.clk.Set(now)
	env.config.BlueberryTime = time.Time{} // activate Blueberry

	// setup and store parent block
	// it's a standard block for simplicity
	parentVersion := version.BlueberryBlockVersion
	parentTime := now
	parentHeight := uint64(2022)

	blueberryParentBlk, err := blocks.NewStandardBlock(
		parentVersion,
		parentTime,
		ids.Empty, // does not matter
		parentHeight,
		nil, // txs do not matter in this test
	)
	assert.NoError(err)
	parentID := blueberryParentBlk.ID()

	// store parent block, with relevant quantities
	chainTime := env.clk.Time().Truncate(time.Second)
	env.blkManager.(*manager).blkIDToState[parentID] = &blockState{
		statelessBlock: blueberryParentBlk,
		timestamp:      chainTime,
	}
	env.blkManager.(*manager).lastAccepted = parentID
	env.blkManager.(*manager).stateVersions.SetState(parentID, env.mockedState)
	env.mockedState.EXPECT().GetLastAccepted().Return(parentID).AnyTimes()

	nextStakerTime := chainTime.Add(txexecutor.SyncBound).Add(-1 * time.Second)

	// store just once current staker to mark next staker time.
	currentStakerIt := state.NewMockStakerIterator(ctrl)
	currentStakerIt.EXPECT().Next().Return(true).AnyTimes()
	currentStakerIt.EXPECT().Value().Return(
		&state.Staker{
			NextTime: nextStakerTime,
			Priority: state.PrimaryNetworkValidatorCurrentPriority,
		},
	).AnyTimes()
	currentStakerIt.EXPECT().Release().Return().AnyTimes()
	env.mockedState.EXPECT().GetCurrentStakerIterator().Return(currentStakerIt, nil).AnyTimes()

	// no pending stakers
	pendingIt := state.NewMockStakerIterator(ctrl)
	pendingIt.EXPECT().Next().Return(false).AnyTimes()
	pendingIt.EXPECT().Release().Return().AnyTimes()
	env.mockedState.EXPECT().GetPendingStakerIterator().Return(pendingIt, nil).AnyTimes()

	env.mockedState.EXPECT().GetCurrentSupply().Return(uint64(1000)).AnyTimes()
	env.mockedState.EXPECT().GetTimestamp().Return(chainTime).AnyTimes()

	// wrong version
	childTimestamp := parentTime.Add(time.Second)
	blueberryChildBlk, err := blocks.NewStandardBlock(
		blocks.ApricotVersion,
		childTimestamp,
		blueberryParentBlk.ID(),
		blueberryParentBlk.Height()+1,
		nil, // txs nulled to simplify test
	)
	assert.NoError(err)
	block := env.blkManager.NewBlock(blueberryChildBlk)
	assert.Error(block.Verify())

	// wrong height
	blueberryChildBlk, err = blocks.NewStandardBlock(
		version.BlueberryBlockVersion,
		childTimestamp,
		blueberryParentBlk.ID(),
		blueberryParentBlk.Height(),
		nil, // txs nulled to simplify test
	)
	assert.NoError(err)
	block = env.blkManager.NewBlock(blueberryChildBlk)
	assert.Error(block.Verify())

	// wrong timestamp, earlier than parent
	childTimestamp = parentTime.Add(-1 * time.Second)
	blueberryChildBlk, err = blocks.NewStandardBlock(
		version.BlueberryBlockVersion,
		childTimestamp,
		blueberryParentBlk.ID(),
		blueberryParentBlk.Height()+1,
		nil, // txs nulled to simplify test
	)
	assert.NoError(err)
	block = env.blkManager.NewBlock(blueberryChildBlk)
	assert.Error(block.Verify())

	// wrong timestamp, violated synchrony bound
	childTimestamp = parentTime.Add(txexecutor.SyncBound).Add(time.Second)
	blueberryChildBlk, err = blocks.NewStandardBlock(
		version.BlueberryBlockVersion,
		childTimestamp,
		blueberryParentBlk.ID(),
		blueberryParentBlk.Height()+1,
		nil, // txs nulled to simplify test
	)
	assert.NoError(err)
	block = env.blkManager.NewBlock(blueberryChildBlk)
	assert.Error(block.Verify())

	// wrong timestamp, skipped staker set change event
	childTimestamp = nextStakerTime.Add(time.Second)
	blueberryChildBlk, err = blocks.NewStandardBlock(
		version.BlueberryBlockVersion,
		childTimestamp,
		blueberryParentBlk.ID(),
		blueberryParentBlk.Height()+1,
		nil, // txs nulled to simplify test
	)
	assert.NoError(err)
	block = env.blkManager.NewBlock(blueberryChildBlk)
	assert.Error(block.Verify())

	// valid block, same timestamp as parent block
	childTimestamp = parentTime
	blueberryChildBlk, err = blocks.NewStandardBlock(
		version.BlueberryBlockVersion,
		childTimestamp,
		blueberryParentBlk.ID(),
		blueberryParentBlk.Height()+1,
		nil, // txs nulled to simplify test
	)
	assert.NoError(err)
	block = env.blkManager.NewBlock(blueberryChildBlk)
	assert.NoError(block.Verify())

	// valid
	childTimestamp = nextStakerTime
	blueberryChildBlk, err = blocks.NewStandardBlock(
		version.BlueberryBlockVersion,
		childTimestamp,
		blueberryParentBlk.ID(),
		blueberryParentBlk.Height()+1,
		nil, // txs nulled to simplify test
	)
	assert.NoError(err)
	block = env.blkManager.NewBlock(blueberryChildBlk)
	assert.NoError(block.Verify())
}

func TestBlueberryStandardBlockUpdatePrimaryNetworkStakers(t *testing.T) {
	assert := assert.New(t)

	env := newEnvironment(t, nil)
	defer func() {
		assert.NoError(shutdownEnvironment(env))
	}()
	env.config.BlueberryTime = time.Time{} // activate Blueberry

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := defaultGenesisTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(defaultMinStakingDuration)
	nodeID := ids.GenerateTestNodeID()
	rewardAddress := ids.GenerateTestShortID()
	addPendingValidatorTx, err := addPendingValidator(
		env,
		pendingValidatorStartTime,
		pendingValidatorEndTime,
		nodeID,
		rewardAddress,
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0]},
	)
	assert.NoError(err)

	// build standard block moving ahead chain time
	preferredID := env.state.GetLastAccepted()
	parentBlk, _, err := env.state.GetStatelessBlock(preferredID)
	assert.NoError(err)
	statelessStandardBlock, err := blocks.NewStandardBlock(
		version.BlueberryBlockVersion,
		pendingValidatorStartTime,
		parentBlk.ID(),
		parentBlk.Height()+1,
		nil, // txs nulled to simplify test
	)
	assert.NoError(err)
	block := env.blkManager.NewBlock(statelessStandardBlock)

	// update staker set
	assert.NoError(block.Verify())

	// tests
	blkStateMap := env.blkManager.(*manager).blkIDToState
	updatedState := blkStateMap[block.ID()].onAcceptState
	currentValidator, err := updatedState.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	assert.NoError(err)
	assert.True(currentValidator.TxID == addPendingValidatorTx.ID(), "Added the wrong tx to the validator set")
	assert.EqualValues(1370, currentValidator.PotentialReward) // See rewards tests to explain why 1370

	_, err = updatedState.GetPendingValidator(constants.PrimaryNetworkID, nodeID)
	assert.ErrorIs(err, database.ErrNotFound)

	// Test VM validators
	assert.NoError(block.Accept())
	assert.True(env.config.Validators.Contains(constants.PrimaryNetworkID, nodeID))
}

// Ensure semantic verification updates the current and pending staker sets correctly.
// Namely, it should add pending stakers whose start time is at or before the timestamp.
// It will not remove primary network stakers; that happens in rewardTxs.
func TestBlueberryStandardBlockUpdateStakers(t *testing.T) {
	// Chronological order (not in scale):
	// Staker0:    |--- ??? // Staker0 end time depends on the test
	// Staker1:        |------------------------------------------------------------------------|
	// Staker2:            |------------------------|
	// Staker3:                |------------------------|
	// Staker3sub:                 |----------------|
	// Staker4:                |------------------------|
	// Staker5:                                     |------------------------|
	staker1 := staker{
		nodeID:        ids.GenerateTestNodeID(),
		rewardAddress: ids.GenerateTestShortID(),
		startTime:     defaultGenesisTime.Add(1 * time.Minute),
		endTime:       defaultGenesisTime.Add(10 * defaultMinStakingDuration).Add(1 * time.Minute),
	}
	staker2 := staker{
		nodeID:        ids.GenerateTestNodeID(),
		rewardAddress: ids.GenerateTestShortID(),
		startTime:     staker1.startTime.Add(1 * time.Minute),
		endTime:       staker1.startTime.Add(1 * time.Minute).Add(defaultMinStakingDuration),
	}
	staker3 := staker{
		nodeID:        ids.GenerateTestNodeID(),
		rewardAddress: ids.GenerateTestShortID(),
		startTime:     staker2.startTime.Add(1 * time.Minute),
		endTime:       staker2.endTime.Add(1 * time.Minute),
	}
	staker3Sub := staker{
		nodeID:        staker3.nodeID,
		rewardAddress: ids.GenerateTestShortID(),
		startTime:     staker3.startTime.Add(1 * time.Minute),
		endTime:       staker3.endTime.Add(-1 * time.Minute),
	}
	staker4 := staker{
		nodeID:        ids.GenerateTestNodeID(),
		rewardAddress: ids.GenerateTestShortID(),
		startTime:     staker3.startTime,
		endTime:       staker3.endTime,
	}
	staker5 := staker{
		nodeID:        ids.GenerateTestNodeID(),
		rewardAddress: ids.GenerateTestShortID(),
		startTime:     staker2.endTime,
		endTime:       staker2.endTime.Add(defaultMinStakingDuration),
	}

	tests := []test{
		{
			description:   "advance time to before staker1 start with subnet",
			stakers:       []staker{staker1, staker2, staker3, staker4, staker5},
			subnetStakers: []staker{staker1, staker2, staker3, staker4, staker5},
			advanceTimeTo: []time.Time{staker1.startTime.Add(-1 * time.Second)},
			expectedStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: pending,
				staker2.nodeID: pending,
				staker3.nodeID: pending,
				staker4.nodeID: pending,
				staker5.nodeID: pending,
			},
			expectedSubnetStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: pending,
				staker2.nodeID: pending,
				staker3.nodeID: pending,
				staker4.nodeID: pending,
				staker5.nodeID: pending,
			},
		},
		{
			description:   "advance time to staker 1 start with subnet",
			stakers:       []staker{staker1, staker2, staker3, staker4, staker5},
			subnetStakers: []staker{staker1},
			advanceTimeTo: []time.Time{staker1.startTime},
			expectedStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: current,
				staker2.nodeID: pending,
				staker3.nodeID: pending,
				staker4.nodeID: pending,
				staker5.nodeID: pending,
			},
			expectedSubnetStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: current,
				staker2.nodeID: pending,
				staker3.nodeID: pending,
				staker4.nodeID: pending,
				staker5.nodeID: pending,
			},
		},
		{
			description:   "advance time to the staker2 start",
			stakers:       []staker{staker1, staker2, staker3, staker4, staker5},
			advanceTimeTo: []time.Time{staker1.startTime, staker2.startTime},
			expectedStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: current,
				staker2.nodeID: current,
				staker3.nodeID: pending,
				staker4.nodeID: pending,
				staker5.nodeID: pending,
			},
		},
		{
			description:   "staker3 should validate only primary network",
			stakers:       []staker{staker1, staker2, staker3, staker4, staker5},
			subnetStakers: []staker{staker1, staker2, staker3Sub, staker4, staker5},
			advanceTimeTo: []time.Time{staker1.startTime, staker2.startTime, staker3.startTime},
			expectedStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: current,
				staker2.nodeID: current,
				staker3.nodeID: current,
				staker4.nodeID: current,
				staker5.nodeID: pending,
			},
			expectedSubnetStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID:    current,
				staker2.nodeID:    current,
				staker3Sub.nodeID: pending,
				staker4.nodeID:    current,
				staker5.nodeID:    pending,
			},
		},
		{
			description:   "advance time to staker3 start with subnet",
			stakers:       []staker{staker1, staker2, staker3, staker4, staker5},
			subnetStakers: []staker{staker1, staker2, staker3Sub, staker4, staker5},
			advanceTimeTo: []time.Time{staker1.startTime, staker2.startTime, staker3.startTime, staker3Sub.startTime},
			expectedStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: current,
				staker2.nodeID: current,
				staker3.nodeID: current,
				staker4.nodeID: current,
				staker5.nodeID: pending,
			},
			expectedSubnetStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: current,
				staker2.nodeID: current,
				staker3.nodeID: current,
				staker4.nodeID: current,
				staker5.nodeID: pending,
			},
		},
		{
			description:   "advance time to staker5 end",
			stakers:       []staker{staker1, staker2, staker3, staker4, staker5},
			advanceTimeTo: []time.Time{staker1.startTime, staker2.startTime, staker3.startTime, staker5.startTime},
			expectedStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: current,
				staker2.nodeID: current,
				staker3.nodeID: current,
				staker4.nodeID: current,
				staker5.nodeID: current,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(ts *testing.T) {
			assert := assert.New(ts)
			env := newEnvironment(t, nil)
			defer func() {
				if err := shutdownEnvironment(env); err != nil {
					t.Fatal(err)
				}
			}()
			env.config.BlueberryTime = time.Time{} // activate Blueberry
			env.config.WhitelistedSubnets.Add(testSubnet1.ID())

			for _, staker := range test.stakers {
				_, err := addPendingValidator(
					env,
					staker.startTime,
					staker.endTime,
					staker.nodeID,
					staker.rewardAddress,
					[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0]},
				)
				assert.NoError(err)
			}

			for _, staker := range test.subnetStakers {
				tx, err := env.txBuilder.NewAddSubnetValidatorTx(
					10, // Weight
					uint64(staker.startTime.Unix()),
					uint64(staker.endTime.Unix()),
					staker.nodeID,    // validator ID
					testSubnet1.ID(), // Subnet ID
					[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
					ids.ShortEmpty,
				)
				assert.NoError(err)

				staker := state.NewSubnetStaker(tx.ID(), &tx.Unsigned.(*txs.AddSubnetValidatorTx).Validator)
				staker.NextTime = staker.StartTime
				staker.Priority = state.SubnetValidatorPendingPriority

				env.state.PutPendingValidator(staker)
				env.state.AddTx(tx, status.Committed)
			}
			env.state.SetHeight( /*dummyHeight*/ 1)
			assert.NoError(env.state.Commit())

			for _, newTime := range test.advanceTimeTo {
				env.clk.Set(newTime)

				// build standard block moving ahead chain time
				preferredID := env.state.GetLastAccepted()
				parentBlk, _, err := env.state.GetStatelessBlock(preferredID)
				assert.NoError(err)
				statelessStandardBlock, err := blocks.NewStandardBlock(
					version.BlueberryBlockVersion,
					newTime,
					parentBlk.ID(),
					parentBlk.Height()+1,
					nil, // txs nulled to simplify test
				)
				block := env.blkManager.NewBlock(statelessStandardBlock)

				assert.NoError(err)

				// update staker set
				assert.NoError(block.Verify())
				assert.NoError(block.Accept())
			}

			for stakerNodeID, status := range test.expectedStakers {
				switch status {
				case pending:
					_, err := env.state.GetPendingValidator(constants.PrimaryNetworkID, stakerNodeID)
					assert.NoError(err)
					assert.False(env.config.Validators.Contains(constants.PrimaryNetworkID, stakerNodeID))
				case current:
					_, err := env.state.GetCurrentValidator(constants.PrimaryNetworkID, stakerNodeID)
					assert.NoError(err)
					assert.True(env.config.Validators.Contains(constants.PrimaryNetworkID, stakerNodeID))
				}
			}

			for stakerNodeID, status := range test.expectedSubnetStakers {
				switch status {
				case pending:
					assert.False(env.config.Validators.Contains(testSubnet1.ID(), stakerNodeID))
				case current:
					assert.True(env.config.Validators.Contains(testSubnet1.ID(), stakerNodeID))
				}
			}
		})
	}
}

// Regression test for https://github.com/ava-labs/avalanchego/pull/584
// that ensures it fixes a bug where subnet validators are not removed
// when timestamp is advanced and there is a pending staker whose start time
// is after the new timestamp
func TestBlueberryStandardBlockRemoveSubnetValidator(t *testing.T) {
	assert := assert.New(t)
	env := newEnvironment(t, nil)
	defer func() {
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()
	env.config.BlueberryTime = time.Time{} // activate Blueberry
	env.config.WhitelistedSubnets.Add(testSubnet1.ID())

	// Add a subnet validator to the staker set
	subnetValidatorNodeID := ids.NodeID(preFundedKeys[0].PublicKey().Address())
	// Starts after the corre
	subnetVdr1StartTime := defaultValidateStartTime
	subnetVdr1EndTime := defaultValidateStartTime.Add(defaultMinStakingDuration)
	tx, err := env.txBuilder.NewAddSubnetValidatorTx(
		1,                                  // Weight
		uint64(subnetVdr1StartTime.Unix()), // Start time
		uint64(subnetVdr1EndTime.Unix()),   // end time
		subnetValidatorNodeID,              // Node ID
		testSubnet1.ID(),                   // Subnet ID
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
		ids.ShortEmpty,
	)
	assert.NoError(err)

	staker := state.NewSubnetStaker(tx.ID(), &tx.Unsigned.(*txs.AddSubnetValidatorTx).Validator)
	staker.NextTime = staker.EndTime
	staker.Priority = state.SubnetValidatorCurrentPriority

	env.state.PutCurrentValidator(staker)
	env.state.AddTx(tx, status.Committed)
	assert.NoError(env.state.Commit())

	// The above validator is now part of the staking set

	// Queue a staker that joins the staker set after the above validator leaves
	subnetVdr2NodeID := ids.NodeID(preFundedKeys[1].PublicKey().Address())
	tx, err = env.txBuilder.NewAddSubnetValidatorTx(
		1, // Weight
		uint64(subnetVdr1EndTime.Add(time.Second).Unix()),                                // Start time
		uint64(subnetVdr1EndTime.Add(time.Second).Add(defaultMinStakingDuration).Unix()), // end time
		subnetVdr2NodeID, // Node ID
		testSubnet1.ID(), // Subnet ID
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
		ids.ShortEmpty,
	)
	assert.NoError(err)

	staker = state.NewSubnetStaker(tx.ID(), &tx.Unsigned.(*txs.AddSubnetValidatorTx).Validator)
	staker.NextTime = staker.StartTime
	staker.Priority = state.SubnetValidatorPendingPriority

	env.state.PutPendingValidator(staker)
	env.state.AddTx(tx, status.Committed)
	assert.NoError(env.state.Commit())

	// The above validator is now in the pending staker set

	// Advance time to the first staker's end time.
	env.clk.Set(subnetVdr1EndTime)
	// build standard block moving ahead chain time
	preferredID := env.state.GetLastAccepted()
	parentBlk, _, err := env.state.GetStatelessBlock(preferredID)
	assert.NoError(err)
	statelessStandardBlock, err := blocks.NewStandardBlock(
		version.BlueberryBlockVersion,
		subnetVdr1EndTime,
		parentBlk.ID(),
		parentBlk.Height()+1,
		nil, // txs nulled to simplify test
	)
	assert.NoError(err)
	block := env.blkManager.NewBlock(statelessStandardBlock)

	// update staker set
	assert.NoError(block.Verify())

	blkStateMap := env.blkManager.(*manager).blkIDToState
	updatedState := blkStateMap[block.ID()].onAcceptState
	_, err = updatedState.GetCurrentValidator(testSubnet1.ID(), subnetValidatorNodeID)
	assert.ErrorIs(err, database.ErrNotFound)

	// Check VM Validators are removed successfully
	assert.NoError(block.Accept())
	assert.False(env.config.Validators.Contains(testSubnet1.ID(), subnetVdr2NodeID))
	assert.False(env.config.Validators.Contains(testSubnet1.ID(), subnetValidatorNodeID))
}

func TestBlueberryStandardBlockWhitelistedSubnet(t *testing.T) {
	assert := assert.New(t)

	for _, whitelist := range []bool{true, false} {
		t.Run(fmt.Sprintf("whitelisted %t", whitelist), func(ts *testing.T) {
			env := newEnvironment(t, nil)
			defer func() {
				if err := shutdownEnvironment(env); err != nil {
					t.Fatal(err)
				}
			}()
			env.config.BlueberryTime = time.Time{} // activate Blueberry
			if whitelist {
				env.config.WhitelistedSubnets.Add(testSubnet1.ID())
			}

			// Add a subnet validator to the staker set
			subnetValidatorNodeID := ids.NodeID(preFundedKeys[0].PublicKey().Address())

			subnetVdr1StartTime := defaultGenesisTime.Add(1 * time.Minute)
			subnetVdr1EndTime := defaultGenesisTime.Add(10 * defaultMinStakingDuration).Add(1 * time.Minute)
			tx, err := env.txBuilder.NewAddSubnetValidatorTx(
				1,                                  // Weight
				uint64(subnetVdr1StartTime.Unix()), // Start time
				uint64(subnetVdr1EndTime.Unix()),   // end time
				subnetValidatorNodeID,              // Node ID
				testSubnet1.ID(),                   // Subnet ID
				[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
				ids.ShortEmpty,
			)
			assert.NoError(err)

			staker := state.NewSubnetStaker(tx.ID(), &tx.Unsigned.(*txs.AddSubnetValidatorTx).Validator)
			staker.NextTime = staker.StartTime
			staker.Priority = state.SubnetValidatorPendingPriority

			env.state.PutPendingValidator(staker)
			env.state.AddTx(tx, status.Committed)
			assert.NoError(env.state.Commit())

			// Advance time to the staker's start time.
			env.clk.Set(subnetVdr1StartTime)

			// build standard block moving ahead chain time
			preferredID := env.state.GetLastAccepted()
			parentBlk, _, err := env.state.GetStatelessBlock(preferredID)
			assert.NoError(err)
			statelessStandardBlock, err := blocks.NewStandardBlock(
				version.BlueberryBlockVersion,
				subnetVdr1StartTime,
				parentBlk.ID(),
				parentBlk.Height()+1,
				nil, // txs nulled to simplify test
			)
			assert.NoError(err)
			block := env.blkManager.NewBlock(statelessStandardBlock)

			// update staker set
			assert.NoError(block.Verify())
			assert.NoError(block.Accept())
			assert.Equal(whitelist, env.config.Validators.Contains(testSubnet1.ID(), subnetValidatorNodeID))
		})
	}
}

func TestBlueberryStandardBlockDelegatorStakerWeight(t *testing.T) {
	assert := assert.New(t)
	env := newEnvironment(t, nil)
	defer func() {
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()
	env.config.BlueberryTime = time.Time{} // activate Blueberry

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := defaultGenesisTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(defaultMaxStakingDuration)
	nodeID := ids.GenerateTestNodeID()
	rewardAddress := ids.GenerateTestShortID()
	_, err := addPendingValidator(
		env,
		pendingValidatorStartTime,
		pendingValidatorEndTime,
		nodeID,
		rewardAddress,
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0]},
	)
	assert.NoError(err)

	// build standard block moving ahead chain time
	preferredID := env.state.GetLastAccepted()
	parentBlk, _, err := env.state.GetStatelessBlock(preferredID)
	assert.NoError(err)
	statelessStandardBlock, err := blocks.NewStandardBlock(
		version.BlueberryBlockVersion,
		pendingValidatorStartTime,
		parentBlk.ID(),
		parentBlk.Height()+1,
		nil, // txs nulled to simplify test
	)
	assert.NoError(err)
	block := env.blkManager.NewBlock(statelessStandardBlock)
	assert.NoError(block.Verify())
	assert.NoError(block.Accept())
	assert.NoError(env.state.Commit())

	// Test validator weight before delegation
	primarySet, ok := env.config.Validators.GetValidators(constants.PrimaryNetworkID)
	assert.True(ok)
	vdrWeight, _ := primarySet.GetWeight(nodeID)
	assert.Equal(env.config.MinValidatorStake, vdrWeight)

	// Add delegator
	pendingDelegatorStartTime := pendingValidatorStartTime.Add(1 * time.Second)
	pendingDelegatorEndTime := pendingDelegatorStartTime.Add(1 * time.Second)

	addDelegatorTx, err := env.txBuilder.NewAddDelegatorTx(
		env.config.MinDelegatorStake,
		uint64(pendingDelegatorStartTime.Unix()),
		uint64(pendingDelegatorEndTime.Unix()),
		nodeID,
		preFundedKeys[0].PublicKey().Address(),
		[]*crypto.PrivateKeySECP256K1R{
			preFundedKeys[0],
			preFundedKeys[1],
			preFundedKeys[4],
		},
		ids.ShortEmpty,
	)
	assert.NoError(err)

	staker := state.NewPrimaryNetworkStaker(addDelegatorTx.ID(), &addDelegatorTx.Unsigned.(*txs.AddDelegatorTx).Validator)
	staker.NextTime = staker.StartTime
	staker.Priority = state.PrimaryNetworkDelegatorPendingPriority

	env.state.PutPendingDelegator(staker)
	env.state.AddTx(addDelegatorTx, status.Committed)
	env.state.SetHeight( /*dummyHeight*/ uint64(1))
	assert.NoError(env.state.Commit())

	// Advance Time
	preferredID = env.state.GetLastAccepted()
	parentBlk, _, err = env.state.GetStatelessBlock(preferredID)
	assert.NoError(err)
	statelessStandardBlock, err = blocks.NewStandardBlock(
		version.BlueberryBlockVersion,
		pendingDelegatorStartTime,
		parentBlk.ID(),
		parentBlk.Height()+1,
		nil, // txs nulled to simplify test
	)
	assert.NoError(err)
	block = env.blkManager.NewBlock(statelessStandardBlock)
	assert.NoError(block.Verify())
	assert.NoError(block.Accept())
	assert.NoError(env.state.Commit())

	// Test validator weight after delegation
	vdrWeight, _ = primarySet.GetWeight(nodeID)
	assert.Equal(env.config.MinDelegatorStake+env.config.MinValidatorStake, vdrWeight)
}

// Helpers

type stakerStatus uint

const (
	pending stakerStatus = iota
	current
)

type staker struct {
	nodeID             ids.NodeID
	rewardAddress      ids.ShortID
	startTime, endTime time.Time
}

type test struct {
	description           string
	stakers               []staker
	subnetStakers         []staker
	advanceTimeTo         []time.Time
	expectedStakers       map[ids.NodeID]stakerStatus
	expectedSubnetStakers map[ids.NodeID]stakerStatus
}

func addPendingValidator(
	env *environment,
	startTime time.Time,
	endTime time.Time,
	nodeID ids.NodeID,
	rewardAddress ids.ShortID,
	keys []*crypto.PrivateKeySECP256K1R,
) (*txs.Tx, error) {
	addPendingValidatorTx, err := env.txBuilder.NewAddValidatorTx(
		env.config.MinValidatorStake,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		nodeID,
		rewardAddress,
		reward.PercentDenominator,
		keys,
		ids.ShortEmpty,
	)
	if err != nil {
		return nil, err
	}

	staker := state.NewPrimaryNetworkStaker(
		addPendingValidatorTx.ID(),
		&addPendingValidatorTx.Unsigned.(*txs.AddValidatorTx).Validator,
	)
	staker.NextTime = staker.StartTime
	staker.Priority = state.PrimaryNetworkValidatorPendingPriority

	env.state.PutPendingValidator(staker)
	env.state.AddTx(addPendingValidatorTx, status.Committed)
	dummyHeight := uint64(1)
	env.state.SetHeight(dummyHeight)
	if err := env.state.Commit(); err != nil {
		return nil, err
	}
	return addPendingValidatorTx, nil
}
