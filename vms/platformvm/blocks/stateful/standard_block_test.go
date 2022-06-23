// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"fmt"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

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

func TestPreForkStandardBlockTimeVerification(t *testing.T) {
	assert := assert.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	h := newTestHelpersCollection(t, ctrl)
	defer func() {
		if err := internalStateShutdown(h); err != nil {
			t.Fatal(err)
		}
	}()

	// setup and store parent block
	// it's a standard block for simplicity
	blksVersion := uint16(stateless.PreForkVersion)
	parentTime := time.Time{}
	parentHeight := uint64(2022)

	preForkParentBlk, err := stateless.NewStandardBlock(
		blksVersion,
		uint64(parentTime.Unix()),
		ids.Empty, // does not matter
		parentHeight,
		nil, // txs do not matter in this test
	)
	assert.NoError(err)

	// Parent state
	chainTime := h.clk.Time().Truncate(time.Second)
	currentSupply := uint64(1000)
	h.mockedFullState.EXPECT().GetStatelessBlock(gomock.Any()).DoAndReturn(
		func(blockID ids.ID) (stateless.CommonBlockIntf, choices.Status, error) {
			if blockID == preForkParentBlk.ID() {
				return preForkParentBlk, choices.Accepted, nil
			}
			return nil, choices.Rejected, database.ErrNotFound
		}).AnyTimes()
	h.mockedFullState.EXPECT().GetLastAccepted().Return(preForkParentBlk.ID()).AnyTimes()
	h.mockedFullState.EXPECT().CurrentStakers().AnyTimes()
	h.mockedFullState.EXPECT().PendingStakers().AnyTimes()
	h.mockedFullState.EXPECT().GetTimestamp().Return(chainTime).AnyTimes()
	h.mockedFullState.EXPECT().GetCurrentSupply().Return(currentSupply).AnyTimes()

	// wrong height
	preForkChildBlk, err := NewStandardBlock(
		blksVersion,
		uint64(parentTime.Unix()),
		h.blkVerifier,
		h.txExecBackend,
		preForkParentBlk.ID(),
		preForkParentBlk.Height(),
		nil, // txs nulled to simplify test
	)
	assert.NoError(err)
	assert.Error(preForkChildBlk.Verify())

	// valid height
	preForkChildBlk, err = NewStandardBlock(
		stateless.PreForkVersion,
		uint64(parentTime.Unix()),
		h.blkVerifier,
		h.txExecBackend,
		preForkParentBlk.ID(),
		preForkParentBlk.Height()+1,
		nil, // txs nulled to simplify test
	)
	assert.NoError(err)
	assert.NoError(preForkChildBlk.Verify())
}

func TestPostForkStandardBlockTimeVerification(t *testing.T) {
	assert := assert.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	h := newTestHelpersCollection(t, ctrl)
	defer func() {
		if err := internalStateShutdown(h); err != nil {
			t.Fatal(err)
		}
	}()
	now := h.clk.Time()
	h.clk.Set(now)
	h.cfg.AdvanceTimeTxRemovalTime = time.Time{} // activate advance time tx removal fork

	// setup and store parent block
	// it's a standard block for simplicity
	parentVersion := uint16(stateless.PostForkVersion)
	parentTime := now
	parentHeight := uint64(2022)

	postForkParentBlk, err := stateless.NewStandardBlock(
		parentVersion,
		uint64(parentTime.Unix()),
		ids.Empty, // does not matter
		parentHeight,
		nil, // txs do not matter in this test
	)
	assert.NoError(err)

	// Parent state
	chainTime := h.clk.Time().Truncate(time.Second)
	nextStakerTime := chainTime.Add(executor.SyncBound).Add(-1 * time.Second)
	currentSupply := uint64(1000)
	h.mockedFullState.EXPECT().GetStatelessBlock(gomock.Any()).DoAndReturn(
		func(blockID ids.ID) (stateless.CommonBlockIntf, choices.Status, error) {
			if blockID == postForkParentBlk.ID() {
				return postForkParentBlk, choices.Accepted, nil
			}
			return nil, choices.Rejected, database.ErrNotFound
		}).AnyTimes()
	h.mockedFullState.EXPECT().GetLastAccepted().Return(postForkParentBlk.ID()).AnyTimes()

	// currentStaker is set just so UpdateStakerSet goes through doing nothing
	currentStaker := state.NewMockCurrentStakers(ctrl)
	currentStaker.EXPECT().Stakers().AnyTimes()
	currentStaker.EXPECT().UpdateStakers(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	h.mockedFullState.EXPECT().CurrentStakers().
		DoAndReturn(func() state.CurrentStakers { return currentStaker }).AnyTimes()

	// pendingStaker is set just so UpdateStakerSet goes through doing nothing
	pendingStaker := state.NewMockPendingStakers(ctrl)
	pendingStaker.EXPECT().Stakers().AnyTimes()
	pendingStaker.EXPECT().DeleteStakers(gomock.Any()).AnyTimes()
	h.mockedFullState.EXPECT().PendingStakers().
		DoAndReturn(func() state.PendingStakers { return pendingStaker }).AnyTimes()

	h.mockedFullState.EXPECT().GetNextStakerChangeTime().Return(nextStakerTime, nil).AnyTimes()
	h.mockedFullState.EXPECT().GetTimestamp().Return(chainTime).AnyTimes()
	h.mockedFullState.EXPECT().GetCurrentSupply().Return(currentSupply).AnyTimes()

	// wrong version
	childTimestamp := uint64(parentTime.Add(time.Second).Unix())
	postForkChildBlk, err := NewStandardBlock(
		stateless.PreForkVersion,
		childTimestamp,
		h.blkVerifier,
		h.txExecBackend,
		postForkParentBlk.ID(),
		postForkParentBlk.Height()+1,
		nil, // txs nulled to simplify test
	)
	assert.NoError(err)
	assert.Error(postForkChildBlk.Verify())

	// wrong height
	postForkChildBlk, err = NewStandardBlock(
		stateless.PostForkVersion,
		childTimestamp,
		h.blkVerifier,
		h.txExecBackend,
		postForkParentBlk.ID(),
		postForkParentBlk.Height(),
		nil, // txs nulled to simplify test
	)
	assert.NoError(err)
	assert.Error(postForkChildBlk.Verify())

	// wrong timestamp, non increasing wrt parent
	childTimestamp = uint64(parentTime.Unix())
	postForkChildBlk, err = NewStandardBlock(
		stateless.PostForkVersion,
		childTimestamp,
		h.blkVerifier,
		h.txExecBackend,
		postForkParentBlk.ID(),
		postForkParentBlk.Height()+1,
		nil, // txs nulled to simplify test
	)
	assert.NoError(err)
	assert.Error(postForkChildBlk.Verify())

	// wrong timestamp, violated synchrony bound
	childTimestamp = uint64(parentTime.Add(executor.SyncBound).Add(time.Second).Unix())
	postForkChildBlk, err = NewStandardBlock(
		stateless.PostForkVersion,
		childTimestamp,
		h.blkVerifier,
		h.txExecBackend,
		postForkParentBlk.ID(),
		postForkParentBlk.Height()+1,
		nil, // txs nulled to simplify test
	)
	assert.NoError(err)
	assert.Error(postForkChildBlk.Verify())

	// wrong timestamp, skipped staker set change event
	childTimestamp = uint64(nextStakerTime.Add(time.Second).Unix())
	postForkChildBlk, err = NewStandardBlock(
		stateless.PostForkVersion,
		childTimestamp,
		h.blkVerifier,
		h.txExecBackend,
		postForkParentBlk.ID(),
		postForkParentBlk.Height()+1,
		nil, // txs nulled to simplify test
	)
	assert.NoError(err)
	assert.Error(postForkChildBlk.Verify())

	// valid
	childTimestamp = uint64(parentTime.Add(time.Second).Unix())
	postForkChildBlk, err = NewStandardBlock(
		stateless.PostForkVersion,
		childTimestamp,
		h.blkVerifier,
		h.txExecBackend,
		postForkParentBlk.ID(),
		postForkParentBlk.Height()+1,
		nil, // txs nulled to simplify test
	)
	assert.NoError(err)
	assert.NoError(postForkChildBlk.Verify())

	// valid
	childTimestamp = uint64(nextStakerTime.Unix())
	postForkChildBlk, err = NewStandardBlock(
		stateless.PostForkVersion,
		childTimestamp,
		h.blkVerifier,
		h.txExecBackend,
		postForkParentBlk.ID(),
		postForkParentBlk.Height()+1,
		nil, // txs nulled to simplify test
	)
	assert.NoError(err)
	assert.NoError(postForkChildBlk.Verify())
}

func TestPostForkStandardBlockUpdatePrimaryNetworkStakers(t *testing.T) {
	assert := assert.New(t)

	h := newTestHelpersCollection(t, nil)
	defer func() {
		if err := internalStateShutdown(h); err != nil {
			t.Fatal(err)
		}
	}()
	h.cfg.AdvanceTimeTxRemovalTime = time.Time{} // activate advance time tx removal fork

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := defaultGenesisTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(defaultMinStakingDuration)

	factory := crypto.FactorySECP256K1R{}
	nodeIDKey, _ := factory.NewPrivateKey()
	rewardAddress := nodeIDKey.PublicKey().Address()
	nodeID := ids.NodeID(rewardAddress)

	addPendingValidatorTx, err := addPendingValidator(
		h,
		pendingValidatorStartTime,
		pendingValidatorEndTime,
		nodeID,
		rewardAddress,
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0]},
	)
	assert.NoError(err)

	// build standard block moving ahead chain time
	preferredID := h.fullState.GetLastAccepted()
	parentBlk, _, err := h.fullState.GetStatelessBlock(preferredID)
	assert.NoError(err)
	block, err := NewStandardBlock(
		stateless.PostForkVersion,
		uint64(pendingValidatorStartTime.Unix()),
		h.blkVerifier,
		h.txExecBackend,
		parentBlk.ID(),
		parentBlk.Height()+1,
		nil, // txs nulled to simplify test
	)
	assert.NoError(err)

	// update staker set
	assert.NoError(block.Verify())

	// tests
	updatedState := block.onAcceptState
	onCommitCurrentStakers := updatedState.CurrentStakers()
	validator, err := onCommitCurrentStakers.GetValidator(nodeID)
	assert.NoError(err)

	_, vdrID := validator.AddValidatorTx()
	assert.True(vdrID == addPendingValidatorTx.ID(), "Added the wrong tx to the validator set")

	onCommitPendingStakers := updatedState.PendingStakers()
	_, _, err = onCommitPendingStakers.GetValidatorTx(nodeID)
	assert.Error(err, "Should have removed the validator from the pending validator set")

	_, reward, err := onCommitCurrentStakers.GetNextStaker()
	assert.NoError(err)

	// See rewards tests
	assert.True(reward == 1370, fmt.Errorf("Expected reward of %d but was %d", 1370, reward))

	// Test VM validators
	updatedState.Apply(h.fullState)
	assert.NoError(h.fullState.Commit())
	assert.True(h.cfg.Validators.Contains(constants.PrimaryNetworkID, nodeID))
}

// Ensure semantic verification updates the current and pending staker sets correctly.
// Namely, it should add pending stakers whose start time is at or before the timestamp.
// It will not remove primary network stakers; that happens in rewardTxs.
func TestPostForkStandardBlockUpdateStakers(t *testing.T) {
	// Chronological order: staker1 start, staker2 start, staker3 start and staker 4 start,
	//  staker3 and staker4 end, staker2 end and staker5 start, staker1 end
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
				staker1.nodeID: pending, staker2.nodeID: pending, staker3.nodeID: pending, staker4.nodeID: pending, staker5.nodeID: pending,
			},
			expectedSubnetStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: pending, staker2.nodeID: pending, staker3.nodeID: pending, staker4.nodeID: pending, staker5.nodeID: pending,
			},
		},
		{
			description:   "advance time to staker 1 start with subnet",
			stakers:       []staker{staker1, staker2, staker3, staker4, staker5},
			subnetStakers: []staker{staker1},
			advanceTimeTo: []time.Time{staker1.startTime},
			expectedStakers: map[ids.NodeID]stakerStatus{
				staker2.nodeID: pending, staker3.nodeID: pending, staker4.nodeID: pending, staker5.nodeID: pending,
				staker1.nodeID: current,
			},
			expectedSubnetStakers: map[ids.NodeID]stakerStatus{
				staker2.nodeID: pending, staker3.nodeID: pending, staker4.nodeID: pending, staker5.nodeID: pending,
				staker1.nodeID: current,
			},
		},
		{
			description:   "advance time to the staker2 start",
			stakers:       []staker{staker1, staker2, staker3, staker4, staker5},
			advanceTimeTo: []time.Time{staker1.startTime, staker2.startTime},
			expectedStakers: map[ids.NodeID]stakerStatus{
				staker3.nodeID: pending, staker4.nodeID: pending, staker5.nodeID: pending,
				staker1.nodeID: current, staker2.nodeID: current,
			},
		},
		{
			description:   "staker3 should validate only primary network",
			stakers:       []staker{staker1, staker2, staker3, staker4, staker5},
			subnetStakers: []staker{staker1, staker2, staker3Sub, staker4, staker5},
			advanceTimeTo: []time.Time{staker1.startTime, staker2.startTime, staker3.startTime},
			expectedStakers: map[ids.NodeID]stakerStatus{
				staker5.nodeID: pending,
				staker1.nodeID: current, staker2.nodeID: current, staker3.nodeID: current, staker4.nodeID: current,
			},
			expectedSubnetStakers: map[ids.NodeID]stakerStatus{
				staker5.nodeID: pending, staker3Sub.nodeID: pending,
				staker1.nodeID: current, staker2.nodeID: current, staker4.nodeID: current,
			},
		},
		{
			description:   "advance time to staker3 start with subnet",
			stakers:       []staker{staker1, staker2, staker3, staker4, staker5},
			subnetStakers: []staker{staker1, staker2, staker3Sub, staker4, staker5},
			advanceTimeTo: []time.Time{staker1.startTime, staker2.startTime, staker3.startTime, staker3Sub.startTime},
			expectedStakers: map[ids.NodeID]stakerStatus{
				staker5.nodeID: pending,
				staker1.nodeID: current, staker2.nodeID: current, staker3.nodeID: current, staker4.nodeID: current,
			},
			expectedSubnetStakers: map[ids.NodeID]stakerStatus{
				staker5.nodeID: pending,
				staker1.nodeID: current, staker2.nodeID: current, staker3.nodeID: current, staker4.nodeID: current,
			},
		},
		{
			description:   "advance time to staker5 end",
			stakers:       []staker{staker1, staker2, staker3, staker4, staker5},
			advanceTimeTo: []time.Time{staker1.startTime, staker2.startTime, staker3.startTime, staker5.startTime},
			expectedStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: current, staker2.nodeID: current, staker3.nodeID: current, staker4.nodeID: current, staker5.nodeID: current,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(ts *testing.T) {
			assert := assert.New(ts)
			h := newTestHelpersCollection(t, nil)
			defer func() {
				if err := internalStateShutdown(h); err != nil {
					t.Fatal(err)
				}
			}()
			h.cfg.AdvanceTimeTxRemovalTime = time.Time{} // activate advance time tx removal fork
			h.cfg.WhitelistedSubnets.Add(testSubnet1.ID())

			for _, staker := range test.stakers {
				_, err := addPendingValidator(
					h,
					staker.startTime,
					staker.endTime,
					staker.nodeID,
					staker.rewardAddress,
					[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0]},
				)
				assert.NoError(err)
			}

			for _, staker := range test.subnetStakers {
				tx, err := h.txBuilder.NewAddSubnetValidatorTx(
					10, // Weight
					uint64(staker.startTime.Unix()),
					uint64(staker.endTime.Unix()),
					staker.nodeID,    // validator ID
					testSubnet1.ID(), // Subnet ID
					[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
					ids.ShortEmpty,
				)
				assert.NoError(err)
				h.fullState.AddPendingStaker(tx)
				h.fullState.AddTx(tx, status.Committed)
			}
			assert.NoError(h.fullState.Commit())
			assert.NoError(h.fullState.Load())

			for _, newTime := range test.advanceTimeTo {
				h.clk.Set(newTime)

				// build standard block moving ahead chain time
				preferredID := h.fullState.GetLastAccepted()
				parentBlk, _, err := h.fullState.GetStatelessBlock(preferredID)
				assert.NoError(err)
				block, err := NewStandardBlock(
					stateless.PostForkVersion,
					uint64(newTime.Unix()),
					h.blkVerifier,
					h.txExecBackend,
					parentBlk.ID(),
					parentBlk.Height()+1,
					nil, // txs nulled to simplify test
				)

				assert.NoError(err)

				// update staker set
				assert.NoError(block.Verify())
				block.onAcceptState.Apply(h.fullState)
			}
			assert.NoError(h.fullState.Commit())

			// Check that the validators we expect to be in the current staker set are there
			currentStakers := h.fullState.CurrentStakers()
			// Check that the validators we expect to be in the pending staker set are there
			pendingStakers := h.fullState.PendingStakers()
			for stakerNodeID, status := range test.expectedStakers {
				switch status {
				case pending:
					_, _, err := pendingStakers.GetValidatorTx(stakerNodeID)
					assert.NoError(err)
					assert.False(h.cfg.Validators.Contains(constants.PrimaryNetworkID, stakerNodeID))
				case current:
					_, err := currentStakers.GetValidator(stakerNodeID)
					assert.NoError(err)
					assert.True(h.cfg.Validators.Contains(constants.PrimaryNetworkID, stakerNodeID))
				}
			}

			for stakerNodeID, status := range test.expectedSubnetStakers {
				switch status {
				case pending:
					assert.False(h.cfg.Validators.Contains(testSubnet1.ID(), stakerNodeID))
				case current:
					assert.True(h.cfg.Validators.Contains(testSubnet1.ID(), stakerNodeID))
				}
			}
		})
	}
}

// Regression test for https://github.com/ava-labs/avalanchego/pull/584
// that ensures it fixes a bug where subnet validators are not removed
// when timestamp is advanced and there is a pending staker whose start time
// is after the new timestamp
func TestPostForkStandardBlockRemoveSubnetValidator(t *testing.T) {
	assert := assert.New(t)
	h := newTestHelpersCollection(t, nil)
	defer func() {
		if err := internalStateShutdown(h); err != nil {
			t.Fatal(err)
		}
	}()
	h.cfg.AdvanceTimeTxRemovalTime = time.Time{} // activate advance time tx removal fork
	h.cfg.WhitelistedSubnets.Add(testSubnet1.ID())

	// Add a subnet validator to the staker set
	subnetValidatorNodeID := ids.NodeID(preFundedKeys[0].PublicKey().Address())
	// Starts after the corre
	subnetVdr1StartTime := defaultValidateStartTime
	subnetVdr1EndTime := defaultValidateStartTime.Add(defaultMinStakingDuration)
	tx, err := h.txBuilder.NewAddSubnetValidatorTx(
		1,                                  // Weight
		uint64(subnetVdr1StartTime.Unix()), // Start time
		uint64(subnetVdr1EndTime.Unix()),   // end time
		subnetValidatorNodeID,              // Node ID
		testSubnet1.ID(),                   // Subnet ID
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
		ids.ShortEmpty,
	)
	assert.NoError(err)

	h.fullState.AddCurrentStaker(tx, 0)
	h.fullState.AddTx(tx, status.Committed)
	assert.NoError(h.fullState.Commit())
	assert.NoError(h.fullState.Load())

	// The above validator is now part of the staking set

	// Queue a staker that joins the staker set after the above validator leaves
	subnetVdr2NodeID := ids.NodeID(preFundedKeys[1].PublicKey().Address())
	tx, err = h.txBuilder.NewAddSubnetValidatorTx(
		1, // Weight
		uint64(subnetVdr1EndTime.Add(time.Second).Unix()),                                // Start time
		uint64(subnetVdr1EndTime.Add(time.Second).Add(defaultMinStakingDuration).Unix()), // end time
		subnetVdr2NodeID, // Node ID
		testSubnet1.ID(), // Subnet ID
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
		ids.ShortEmpty,
	)
	assert.NoError(err)

	h.fullState.AddPendingStaker(tx)
	h.fullState.AddTx(tx, status.Committed)
	assert.NoError(h.fullState.Commit())
	assert.NoError(h.fullState.Load())

	// The above validator is now in the pending staker set

	// Advance time to the first staker's end time.
	h.clk.Set(subnetVdr1EndTime)
	// build standard block moving ahead chain time
	preferredID := h.fullState.GetLastAccepted()
	parentBlk, _, err := h.fullState.GetStatelessBlock(preferredID)
	assert.NoError(err)
	block, err := NewStandardBlock(
		stateless.PostForkVersion,
		uint64(subnetVdr1EndTime.Unix()),
		h.blkVerifier,
		h.txExecBackend,
		parentBlk.ID(),
		parentBlk.Height()+1,
		nil, // txs nulled to simplify test
	)
	assert.NoError(err)

	// update staker set
	assert.NoError(block.Verify())

	currentStakers := block.onAcceptState.CurrentStakers()
	vdr, err := currentStakers.GetValidator(subnetValidatorNodeID)
	assert.NoError(err)
	_, exists := vdr.SubnetValidators()[testSubnet1.ID()]

	// The first staker should now be removed. Verify that is the case.
	assert.False(exists, "should have been removed from validator set")

	// Check VM Validators are removed successfully
	block.onAcceptState.Apply(h.fullState)
	assert.NoError(h.fullState.Commit())
	assert.False(h.cfg.Validators.Contains(testSubnet1.ID(), subnetVdr2NodeID))
	assert.False(h.cfg.Validators.Contains(testSubnet1.ID(), subnetValidatorNodeID))
}

func TestPostForkStandardBlockWhitelistedSubnet(t *testing.T) {
	assert := assert.New(t)

	for _, whitelist := range []bool{true, false} {
		t.Run(fmt.Sprintf("whitelisted %t", whitelist), func(ts *testing.T) {
			h := newTestHelpersCollection(t, nil)
			defer func() {
				if err := internalStateShutdown(h); err != nil {
					t.Fatal(err)
				}
			}()
			h.cfg.AdvanceTimeTxRemovalTime = time.Time{} // activate advance time tx removal fork
			if whitelist {
				h.cfg.WhitelistedSubnets.Add(testSubnet1.ID())
			}

			// Add a subnet validator to the staker set
			subnetValidatorNodeID := ids.NodeID(preFundedKeys[0].PublicKey().Address())

			subnetVdr1StartTime := defaultGenesisTime.Add(1 * time.Minute)
			subnetVdr1EndTime := defaultGenesisTime.Add(10 * defaultMinStakingDuration).Add(1 * time.Minute)
			tx, err := h.txBuilder.NewAddSubnetValidatorTx(
				1,                                  // Weight
				uint64(subnetVdr1StartTime.Unix()), // Start time
				uint64(subnetVdr1EndTime.Unix()),   // end time
				subnetValidatorNodeID,              // Node ID
				testSubnet1.ID(),                   // Subnet ID
				[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
				ids.ShortEmpty,
			)
			assert.NoError(err)

			h.fullState.AddPendingStaker(tx)
			h.fullState.AddTx(tx, status.Committed)
			assert.NoError(h.fullState.Commit())
			assert.NoError(h.fullState.Load())

			// Advance time to the staker's start time.
			h.clk.Set(subnetVdr1StartTime)

			// build standard block moving ahead chain time
			preferredID := h.fullState.GetLastAccepted()
			parentBlk, _, err := h.fullState.GetStatelessBlock(preferredID)
			assert.NoError(err)
			block, err := NewStandardBlock(
				stateless.PostForkVersion,
				uint64(subnetVdr1StartTime.Unix()),
				h.blkVerifier,
				h.txExecBackend,
				parentBlk.ID(),
				parentBlk.Height()+1,
				nil, // txs nulled to simplify test
			)
			assert.NoError(err)

			// update staker set
			assert.NoError(block.Verify())
			block.onAcceptState.Apply(h.fullState)

			assert.NoError(h.fullState.Commit())
			assert.Equal(whitelist, h.cfg.Validators.Contains(testSubnet1.ID(), subnetValidatorNodeID))
		})
	}
}

func TestPostForkStandardBlockDelegatorStakerWeight(t *testing.T) {
	assert := assert.New(t)
	h := newTestHelpersCollection(t, nil)
	defer func() {
		if err := internalStateShutdown(h); err != nil {
			t.Fatal(err)
		}
	}()
	h.cfg.AdvanceTimeTxRemovalTime = time.Time{} // activate advance time tx removal fork

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := defaultGenesisTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(defaultMaxStakingDuration)
	factory := crypto.FactorySECP256K1R{}
	nodeIDKey, _ := factory.NewPrivateKey()
	rewardAddress := nodeIDKey.PublicKey().Address()
	nodeID := ids.NodeID(rewardAddress)
	_, err := addPendingValidator(
		h,
		pendingValidatorStartTime,
		pendingValidatorEndTime,
		nodeID,
		rewardAddress,
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0]},
	)
	assert.NoError(err)

	// build standard block moving ahead chain time
	preferredID := h.fullState.GetLastAccepted()
	parentBlk, _, err := h.fullState.GetStatelessBlock(preferredID)
	assert.NoError(err)
	block, err := NewStandardBlock(
		stateless.PostForkVersion,
		uint64(pendingValidatorStartTime.Unix()),
		h.blkVerifier,
		h.txExecBackend,
		parentBlk.ID(),
		parentBlk.Height()+1,
		nil, // txs nulled to simplify test
	)
	assert.NoError(err)
	assert.NoError(block.Verify())

	block.onAcceptState.Apply(h.fullState)
	assert.NoError(h.fullState.Commit())

	// Test validator weight before delegation
	primarySet, ok := h.cfg.Validators.GetValidators(constants.PrimaryNetworkID)
	assert.True(ok)
	vdrWeight, _ := primarySet.GetWeight(nodeID)
	assert.Equal(h.cfg.MinValidatorStake, vdrWeight)

	// Add delegator
	pendingDelegatorStartTime := pendingValidatorStartTime.Add(1 * time.Second)
	pendingDelegatorEndTime := pendingDelegatorStartTime.Add(1 * time.Second)

	addDelegatorTx, err := h.txBuilder.NewAddDelegatorTx(
		h.cfg.MinDelegatorStake,
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
	h.fullState.AddPendingStaker(addDelegatorTx)
	h.fullState.AddTx(addDelegatorTx, status.Committed)
	assert.NoError(h.fullState.Commit())
	assert.NoError(h.fullState.Load())

	// Advance Time
	block, err = NewStandardBlock(
		stateless.PostForkVersion,
		uint64(pendingDelegatorStartTime.Unix()),
		h.blkVerifier,
		h.txExecBackend,
		parentBlk.ID(),
		parentBlk.Height()+1,
		nil, // txs nulled to simplify test
	)
	assert.NoError(err)
	assert.NoError(block.Verify())

	block.onAcceptState.Apply(h.fullState)
	assert.NoError(h.fullState.Commit())

	// Test validator weight after delegation
	vdrWeight, _ = primarySet.GetWeight(nodeID)
	assert.Equal(h.cfg.MinDelegatorStake+h.cfg.MinValidatorStake, vdrWeight)
}

func addPendingValidator(
	h *testHelpersCollection,
	startTime time.Time,
	endTime time.Time,
	nodeID ids.NodeID,
	rewardAddress ids.ShortID,
	keys []*crypto.PrivateKeySECP256K1R,
) (*txs.Tx, error) {
	addPendingValidatorTx, err := h.txBuilder.NewAddValidatorTx(
		h.cfg.MinValidatorStake,
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

	h.fullState.AddPendingStaker(addPendingValidatorTx)
	h.fullState.AddTx(addPendingValidatorTx, status.Committed)
	if err := h.fullState.Commit(); err != nil {
		return nil, err
	}
	if err := h.fullState.Load(); err != nil {
		return nil, err
	}
	return addPendingValidatorTx, err
}
