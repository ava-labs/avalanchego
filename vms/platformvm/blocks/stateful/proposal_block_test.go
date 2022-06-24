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
	"github.com/ava-labs/avalanchego/vms/platformvm/validator"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

func TestPreForkProposalBlockTimeVerification(t *testing.T) {
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

	// create a proposal transaction to be included into proposal block
	utx := &txs.AddValidatorTx{
		BaseTx:       txs.BaseTx{},
		Validator:    validator.Validator{End: uint64(chainTime.Unix())},
		Stake:        nil,
		RewardsOwner: &secp256k1fx.OutputOwners{},
		Shares:       uint32(defaultTxFee),
	}
	addValTx := &txs.Tx{Unsigned: utx}
	assert.NoError(addValTx.Sign(txs.Codec, nil))
	blkTx := &txs.Tx{
		Unsigned: &txs.RewardValidatorTx{
			TxID: addValTx.ID(),
		},
	}

	// setup state to validate proposal block transaction
	currentStaker := state.NewMockCurrentStakers(ctrl)
	currentStaker.EXPECT().GetNextStaker().
		Return(addValTx, uint64(0), nil).AnyTimes()
	currentStaker.EXPECT().DeleteNextStaker().AnyTimes()

	h.mockedFullState.EXPECT().CurrentStakers().Return(currentStaker).AnyTimes()
	h.mockedFullState.EXPECT().PendingStakers().AnyTimes()
	h.mockedFullState.EXPECT().GetTimestamp().Return(chainTime).AnyTimes()
	h.mockedFullState.EXPECT().GetCurrentSupply().Return(currentSupply).AnyTimes()
	h.mockedFullState.EXPECT().GetUptime(gomock.Any()).
		Return(time.Duration(1000) /*upDuration*/, time.Time{} /*lastUpdated*/, nil /*err*/).AnyTimes()

	// wrong height
	block, err := NewProposalBlock(
		blksVersion,
		uint64(parentTime.Unix()),
		h.blkVerifier,
		h.txExecBackend,
		preForkParentBlk.ID(),
		preForkParentBlk.Height(),
		blkTx,
	)
	assert.NoError(err)
	assert.NoError(err)
	assert.Error(block.Verify())

	// valid
	block, err = NewProposalBlock(
		blksVersion,
		uint64(parentTime.Unix()),
		h.blkVerifier,
		h.txExecBackend,
		preForkParentBlk.ID(),
		preForkParentBlk.Height()+1,
		blkTx,
	)
	assert.NoError(err)
	assert.NoError(block.Verify())
}

func TestPostForkProposalBlockTimeVerification(t *testing.T) {
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
	parentTime := time.Time{}
	parentHeight := uint64(2022)

	parentBlk, err := stateless.NewStandardBlock(
		stateless.PostForkVersion,
		uint64(parentTime.Unix()),
		ids.Empty, // does not matter
		parentHeight,
		nil, // txs do not matter in this test
	)
	assert.NoError(err)

	chainTime := parentTime
	currentSupply := uint64(1000)

	h.mockedFullState.EXPECT().GetStatelessBlock(gomock.Any()).DoAndReturn(
		func(blockID ids.ID) (stateless.CommonBlockIntf, choices.Status, error) {
			if blockID == parentBlk.ID() {
				return parentBlk, choices.Accepted, nil
			}
			return nil, choices.Rejected, database.ErrNotFound
		}).AnyTimes()
	h.mockedFullState.EXPECT().GetLastAccepted().Return(parentBlk.ID()).AnyTimes()

	nextStakerTime := chainTime.Add(executor.SyncBound).Add(-1 * time.Second)
	h.mockedFullState.EXPECT().GetNextStakerChangeTime().
		Return(nextStakerTime, nil).AnyTimes()

	// Timestamp Update - Pending Stakers
	pendingStaker := state.NewMockPendingStakers(ctrl)
	pendingStaker.EXPECT().Stakers().AnyTimes()
	pendingStaker.EXPECT().DeleteStakers(gomock.Any()).Return(pendingStaker).AnyTimes()
	h.mockedFullState.EXPECT().PendingStakers().Return(pendingStaker).AnyTimes()

	// Timestamp Update - Current Stakers
	currentStaker := state.NewMockCurrentStakers(ctrl)
	currentStaker.EXPECT().Stakers().AnyTimes()
	currentStaker.EXPECT().
		UpdateStakers(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(currentStaker, nil).AnyTimes()
	h.mockedFullState.EXPECT().CurrentStakers().Return(currentStaker).AnyTimes()

	// Other Timestamp updates
	h.mockedFullState.EXPECT().GetCurrentSupply().Return(currentSupply).AnyTimes()
	h.mockedFullState.EXPECT().GetTimestamp().Return(chainTime).AnyTimes()
	h.mockedFullState.EXPECT().GetUptime(gomock.Any()).
		Return(time.Duration(1000) /*upDuration*/, time.Time{} /*lastUpdated*/, nil /*err*/).
		AnyTimes()

	// create proposal tx
	validatorEndTime := parentTime.Add(time.Second)
	addValTx := &txs.Tx{
		Unsigned: &txs.AddValidatorTx{
			BaseTx:       txs.BaseTx{},
			Validator:    validator.Validator{End: uint64(validatorEndTime.Unix())},
			Stake:        nil,
			RewardsOwner: &secp256k1fx.OutputOwners{},
			Shares:       uint32(defaultTxFee),
		},
	}
	assert.NoError(addValTx.Sign(txs.Codec, nil))
	blkTx := &txs.Tx{
		Unsigned: &txs.RewardValidatorTx{
			TxID: addValTx.ID(),
		},
	}

	// Tx Execute
	currentStaker.EXPECT().GetNextStaker().
		Return(addValTx, uint64(0), nil).AnyTimes()
	currentStaker.EXPECT().DeleteNextStaker().AnyTimes()

	// wrong version
	block, err := NewProposalBlock(
		stateless.PreForkVersion,
		uint64(parentTime.Add(time.Second).Unix()),
		h.blkVerifier,
		h.txExecBackend,
		parentBlk.ID(),
		parentBlk.Height()+1,
		blkTx,
	)
	assert.NoError(err)
	assert.Error(block.Verify())

	// wrong height
	block, err = NewProposalBlock(
		stateless.PostForkVersion,
		uint64(parentTime.Add(time.Second).Unix()),
		h.blkVerifier,
		h.txExecBackend,
		parentBlk.ID(),
		parentBlk.Height(),
		blkTx,
	)
	assert.NoError(err)
	assert.Error(block.Verify())

	// wrong tx content (no advance time txs)
	invalidTx := &txs.Tx{
		Unsigned: &txs.AdvanceTimeTx{
			Time: uint64(now.Unix()),
		},
	}
	assert.NoError(invalidTx.Sign(txs.Codec, nil))
	block, err = NewProposalBlock(
		stateless.PostForkVersion,
		uint64(parentTime.Add(time.Second).Unix()),
		h.blkVerifier,
		h.txExecBackend,
		parentBlk.ID(),
		parentBlk.Height()+1,
		invalidTx,
	)
	assert.NoError(err)
	assert.Error(block.Verify())

	// wrong timestamp, non increasing wrt parent
	block, err = NewProposalBlock(
		stateless.PostForkVersion,
		uint64(parentTime.Unix()),
		h.blkVerifier,
		h.txExecBackend,
		parentBlk.ID(),
		parentBlk.Height()+1,
		blkTx,
	)
	assert.NoError(err)
	assert.Error(block.Verify())

	// wrong timestamp, violated synchrony bound
	beyondSyncBoundTimeStamp := h.clk.Time().Add(executor.SyncBound).Add(time.Second)
	block, err = NewProposalBlock(
		stateless.PostForkVersion,
		uint64(beyondSyncBoundTimeStamp.Unix()),
		h.blkVerifier,
		h.txExecBackend,
		parentBlk.ID(),
		parentBlk.Height()+1,
		blkTx,
	)
	assert.NoError(err)
	assert.Error(block.Verify())

	// wrong timestamp, skipped staker set change event
	skippedStakerEventTimeStamp := nextStakerTime.Add(time.Second)
	block, err = NewProposalBlock(
		stateless.PostForkVersion,
		uint64(skippedStakerEventTimeStamp.Unix()),
		h.blkVerifier,
		h.txExecBackend,
		parentBlk.ID(),
		parentBlk.Height()+1,
		blkTx,
	)
	assert.NoError(err)
	assert.Error(block.Verify())

	block, err = NewProposalBlock(
		stateless.PostForkVersion,
		uint64(validatorEndTime.Unix()),
		h.blkVerifier,
		h.txExecBackend,
		parentBlk.ID(),
		parentBlk.Height()+1,
		blkTx,
	)
	assert.NoError(err)
	assert.NoError(block.Verify())

	// valid
	blkTimeStamp := nextStakerTime

	addValTx.Unsigned.(*txs.AddValidatorTx).Validator = validator.Validator{
		End: uint64(blkTimeStamp.Unix()),
	}
	assert.NoError(addValTx.Sign(txs.Codec, nil))
	blkTx = &txs.Tx{
		Unsigned: &txs.RewardValidatorTx{
			TxID: addValTx.ID(),
		},
	}

	block, err = NewProposalBlock(
		stateless.PostForkVersion,
		uint64(nextStakerTime.Unix()),
		h.blkVerifier,
		h.txExecBackend,
		parentBlk.ID(),
		parentBlk.Height()+1,
		blkTx,
	)
	assert.NoError(err)
	assert.NoError(block.Verify())
}

func TestPostForkProposalBlockUpdateStakers(t *testing.T) {
	// Chronological order (not in scale):
	// Staker0:    |--- ??? // Staker0 end time depends on the test
	// Staker1:        |------------------------------------------------------------------------|
	// Staker2:            |------------------------|
	// Staker3:                |------------------------|
	// Staker3sub:                 |----------------|
	// Staker4:                |------------------------|
	// Staker5:                                     |------------------------|

	// Staker0 it's here just to allow to issue a proposal block with the chosen endTime.
	staker0RewardAddress := ids.GenerateTestShortID()
	staker0 := staker{
		nodeID:        ids.NodeID(staker0RewardAddress),
		rewardAddress: staker0RewardAddress,
		startTime:     defaultGenesisTime,
		endTime:       time.Time{}, // actual endTime depends on specific test
	}

	staker1RewardAddress := ids.GenerateTestShortID()
	staker1 := staker{
		nodeID:        ids.NodeID(staker1RewardAddress),
		rewardAddress: staker1RewardAddress,
		startTime:     defaultGenesisTime.Add(1 * time.Minute),
		endTime:       defaultGenesisTime.Add(10 * defaultMinStakingDuration).Add(1 * time.Minute),
	}

	staker2RewardAddress := ids.GenerateTestShortID()
	staker2 := staker{
		nodeID:        ids.NodeID(staker2RewardAddress),
		rewardAddress: staker2RewardAddress,
		startTime:     staker1.startTime.Add(1 * time.Minute),
		endTime:       staker1.startTime.Add(1 * time.Minute).Add(defaultMinStakingDuration),
	}

	staker3RewardAddress := ids.GenerateTestShortID()
	staker3 := staker{
		nodeID:        ids.NodeID(staker3RewardAddress),
		rewardAddress: staker3RewardAddress,
		startTime:     staker2.startTime.Add(1 * time.Minute),
		endTime:       staker2.endTime.Add(1 * time.Minute),
	}

	staker3Sub := staker{
		nodeID:        staker3.nodeID,
		rewardAddress: staker3.rewardAddress,
		startTime:     staker3.startTime.Add(1 * time.Minute),
		endTime:       staker3.endTime.Add(-1 * time.Minute),
	}

	staker4RewardAddress := ids.GenerateTestShortID()
	staker4 := staker{
		nodeID:        ids.NodeID(staker4RewardAddress),
		rewardAddress: staker4RewardAddress,
		startTime:     staker3.startTime,
		endTime:       staker3.endTime,
	}

	staker5RewardAddress := ids.GenerateTestShortID()
	staker5 := staker{
		nodeID:        ids.NodeID(staker5RewardAddress),
		rewardAddress: staker5RewardAddress,
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
				staker4.nodeID:    current,
				staker3Sub.nodeID: pending,
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

				// given its txID, staker2 will be
				// rewarded and moved out of current stakers set
				// staker2.nodeID: current,
				staker3.nodeID: current,
				staker4.nodeID: current,
				staker5.nodeID: current,
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

			addedStakerTxsByEndTime := make(map[time.Time]*txs.Tx, 0)
			for _, staker := range test.stakers {
				addPendingValidatorTx, err := h.txBuilder.NewAddValidatorTx(
					h.cfg.MinValidatorStake,
					uint64(staker.startTime.Unix()),
					uint64(staker.endTime.Unix()),
					staker.nodeID,
					staker.rewardAddress,
					reward.PercentDenominator,
					[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0]},
					ids.ShortEmpty,
				)
				assert.NoError(err)
				h.fullState.AddPendingStaker(addPendingValidatorTx)
				h.fullState.AddTx(addPendingValidatorTx, status.Committed)
				addedStakerTxsByEndTime[staker.endTime] = addPendingValidatorTx
			}

			for _, subnetStkr := range test.subnetStakers {
				tx, err := h.txBuilder.NewAddSubnetValidatorTx(
					10, // Weight
					uint64(subnetStkr.startTime.Unix()),
					uint64(subnetStkr.endTime.Unix()),
					subnetStkr.nodeID, // validator ID
					testSubnet1.ID(),  // Subnet ID
					[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
					ids.ShortEmpty,
				)
				assert.NoError(err)
				h.fullState.AddPendingStaker(tx)
				h.fullState.AddTx(tx, status.Committed)
				addedStakerTxsByEndTime[subnetStkr.endTime] = tx
			}
			assert.NoError(h.fullState.Commit())
			assert.NoError(h.fullState.Load())

			for _, newTime := range test.advanceTimeTo {
				h.clk.Set(newTime)

				// add Staker0 (with the right end time) to state
				// so to allow proposalBlk issuance
				staker0.endTime = newTime
				addStaker0, err := h.txBuilder.NewAddValidatorTx(
					10,
					uint64(staker0.startTime.Unix()),
					uint64(staker0.endTime.Unix()),
					staker0.nodeID,
					staker0.rewardAddress,
					reward.PercentDenominator,
					[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
					ids.ShortEmpty,
				)
				assert.NoError(err)
				h.fullState.AddCurrentStaker(addStaker0, uint64(2022))
				h.fullState.AddTx(addStaker0, status.Committed)
				assert.NoError(h.fullState.Commit())
				assert.NoError(h.fullState.Load())

				// multiple stakers may finish their staking period
				// at the same time and be entitled to reward. These
				// staker are sorted deterministically and we need to
				// pick the very first.
				toReward := []*txs.Tx{addStaker0}
				for endTime, stakerTx := range addedStakerTxsByEndTime {
					if newTime.Equal(endTime) {
						toReward = append(toReward, stakerTx)
					}
				}
				state.SortValidatorsByRemoval(toReward)

				for _, rewardTx := range toReward {
					s0RewardTx := &txs.Tx{
						Unsigned: &txs.RewardValidatorTx{
							TxID: rewardTx.ID(),
						},
					}
					assert.NoError(s0RewardTx.Sign(txs.Codec, nil))

					// build proposal block moving ahead chain time
					// as well as rewarding staker0
					preferredID := h.fullState.GetLastAccepted()
					parentBlk, _, err := h.fullState.GetStatelessBlock(preferredID)
					assert.NoError(err)
					block, err := NewProposalBlock(
						stateless.PostForkVersion,
						uint64(newTime.Unix()),
						h.blkVerifier,
						h.txExecBackend,
						parentBlk.ID(),
						parentBlk.Height()+1,
						s0RewardTx,
					)
					assert.NoError(err)

					// verify and accept the block
					assert.NoError(block.Verify())
					options, err := block.Options()
					assert.NoError(err)

					assert.NoError(options[0].Verify())

					assert.NoError(block.Accept())
					assert.NoError(options[0].Accept())
				}
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

func TestPostForkProposalBlockRemoveSubnetValidator(t *testing.T) {
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

	// add Staker0 (with the right end time) to state
	// so to allow proposalBlk issuance
	staker0StartTime := defaultValidateStartTime
	staker0EndTime := subnetVdr1EndTime
	addStaker0, err := h.txBuilder.NewAddValidatorTx(
		10,
		uint64(staker0StartTime.Unix()),
		uint64(staker0EndTime.Unix()),
		ids.GenerateTestNodeID(),
		ids.GenerateTestShortID(),
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
		ids.ShortEmpty,
	)
	assert.NoError(err)
	h.fullState.AddCurrentStaker(addStaker0, uint64(2022))
	h.fullState.AddTx(addStaker0, status.Committed)
	assert.NoError(h.fullState.Commit())
	assert.NoError(h.fullState.Load())

	// create rewardTx for staker0
	s0RewardTx := &txs.Tx{
		Unsigned: &txs.RewardValidatorTx{
			TxID: addStaker0.ID(),
		},
	}
	assert.NoError(s0RewardTx.Sign(txs.Codec, nil))

	// build proposal block moving ahead chain time
	preferredID := h.fullState.GetLastAccepted()
	parentBlk, _, err := h.fullState.GetStatelessBlock(preferredID)
	assert.NoError(err)
	block, err := NewProposalBlock(
		stateless.PostForkVersion,
		uint64(subnetVdr1EndTime.Unix()),
		h.blkVerifier,
		h.txExecBackend,
		parentBlk.ID(),
		parentBlk.Height()+1,
		s0RewardTx,
	)
	assert.NoError(err)

	// update staker set
	assert.NoError(block.Verify())

	currentStakers := block.onCommitState.CurrentStakers()
	vdr, err := currentStakers.GetValidator(subnetValidatorNodeID)
	assert.NoError(err)
	_, exists := vdr.SubnetValidators()[testSubnet1.ID()]

	// The first staker should now be removed. Verify that is the case.
	assert.False(exists, "should have been removed from validator set")

	// Check VM Validators are removed successfully
	block.onPostForkBaseOptionsState.Apply(h.fullState)
	block.onCommitState.Apply(h.fullState)
	assert.NoError(h.fullState.Commit())
	assert.False(h.cfg.Validators.Contains(testSubnet1.ID(), subnetVdr2NodeID))
	assert.False(h.cfg.Validators.Contains(testSubnet1.ID(), subnetValidatorNodeID))
}

func TestPostForkProposalBlockWhitelistedSubnet(t *testing.T) {
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

			// add Staker0 (with the right end time) to state
			// so to allow proposalBlk issuance
			staker0StartTime := defaultGenesisTime
			staker0EndTime := subnetVdr1StartTime
			addStaker0, err := h.txBuilder.NewAddValidatorTx(
				10,
				uint64(staker0StartTime.Unix()),
				uint64(staker0EndTime.Unix()),
				ids.GenerateTestNodeID(),
				ids.GenerateTestShortID(),
				reward.PercentDenominator,
				[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
				ids.ShortEmpty,
			)
			assert.NoError(err)
			h.fullState.AddCurrentStaker(addStaker0, uint64(2022))
			h.fullState.AddTx(addStaker0, status.Committed)
			assert.NoError(h.fullState.Commit())
			assert.NoError(h.fullState.Load())

			// create rewardTx for staker0
			s0RewardTx := &txs.Tx{
				Unsigned: &txs.RewardValidatorTx{
					TxID: addStaker0.ID(),
				},
			}
			assert.NoError(s0RewardTx.Sign(txs.Codec, nil))

			// build proposal block moving ahead chain time
			preferredID := h.fullState.GetLastAccepted()
			parentBlk, _, err := h.fullState.GetStatelessBlock(preferredID)
			assert.NoError(err)
			block, err := NewProposalBlock(
				stateless.PostForkVersion,
				uint64(subnetVdr1StartTime.Unix()),
				h.blkVerifier,
				h.txExecBackend,
				parentBlk.ID(),
				parentBlk.Height()+1,
				s0RewardTx,
			)
			assert.NoError(err)

			// update staker set
			assert.NoError(block.Verify())
			block.onPostForkBaseOptionsState.Apply(h.fullState)
			block.onCommitState.Apply(h.fullState)

			assert.NoError(h.fullState.Commit())
			assert.Equal(whitelist, h.cfg.Validators.Contains(testSubnet1.ID(), subnetValidatorNodeID))
		})
	}
}

func TestPostForkProposalBlockDelegatorStakerWeight(t *testing.T) {
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

	// add Staker0 (with the right end time) to state
	// so to allow proposalBlk issuance
	staker0StartTime := defaultGenesisTime
	staker0EndTime := pendingValidatorStartTime
	addStaker0, err := h.txBuilder.NewAddValidatorTx(
		10,
		uint64(staker0StartTime.Unix()),
		uint64(staker0EndTime.Unix()),
		ids.GenerateTestNodeID(),
		ids.GenerateTestShortID(),
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
		ids.ShortEmpty,
	)
	assert.NoError(err)
	h.fullState.AddCurrentStaker(addStaker0, uint64(2022))
	h.fullState.AddTx(addStaker0, status.Committed)
	assert.NoError(h.fullState.Commit())
	assert.NoError(h.fullState.Load())

	// create rewardTx for staker0
	s0RewardTx := &txs.Tx{
		Unsigned: &txs.RewardValidatorTx{
			TxID: addStaker0.ID(),
		},
	}
	assert.NoError(s0RewardTx.Sign(txs.Codec, nil))

	// build proposal block moving ahead chain time
	preferredID := h.fullState.GetLastAccepted()
	parentBlk, _, err := h.fullState.GetStatelessBlock(preferredID)
	assert.NoError(err)
	block, err := NewProposalBlock(
		stateless.PostForkVersion,
		uint64(pendingValidatorStartTime.Unix()),
		h.blkVerifier,
		h.txExecBackend,
		parentBlk.ID(),
		parentBlk.Height()+1,
		s0RewardTx,
	)
	assert.NoError(err)
	assert.NoError(block.Verify())

	block.onPostForkBaseOptionsState.Apply(h.fullState)
	block.onCommitState.Apply(h.fullState)
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

	// add Staker0 (with the right end time) to state
	// so to allow proposalBlk issuance
	staker0EndTime = pendingDelegatorStartTime
	addStaker0, err = h.txBuilder.NewAddValidatorTx(
		10,
		uint64(staker0StartTime.Unix()),
		uint64(staker0EndTime.Unix()),
		ids.GenerateTestNodeID(),
		ids.GenerateTestShortID(),
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
		ids.ShortEmpty,
	)
	assert.NoError(err)
	h.fullState.AddCurrentStaker(addStaker0, uint64(2022))
	h.fullState.AddTx(addStaker0, status.Committed)
	assert.NoError(h.fullState.Commit())
	assert.NoError(h.fullState.Load())

	// create rewardTx for staker0
	s0RewardTx = &txs.Tx{
		Unsigned: &txs.RewardValidatorTx{
			TxID: addStaker0.ID(),
		},
	}
	assert.NoError(s0RewardTx.Sign(txs.Codec, nil))

	// Advance Time
	block, err = NewProposalBlock(
		stateless.PostForkVersion,
		uint64(pendingDelegatorStartTime.Unix()),
		h.blkVerifier,
		h.txExecBackend,
		parentBlk.ID(),
		parentBlk.Height()+1,
		s0RewardTx,
	)
	assert.NoError(err)
	assert.NoError(block.Verify())

	block.onPostForkBaseOptionsState.Apply(h.fullState)
	block.onCommitState.Apply(h.fullState)
	assert.NoError(h.fullState.Commit())

	// Test validator weight after delegation
	vdrWeight, _ = primarySet.GetWeight(nodeID)
	assert.Equal(h.cfg.MinDelegatorStake+h.cfg.MinValidatorStake, vdrWeight)
}

func TestPostForkProposalBlockDelegatorStakers(t *testing.T) {
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

	_, err := addPendingValidator(
		h,
		pendingValidatorStartTime,
		pendingValidatorEndTime,
		nodeID,
		rewardAddress,
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0]},
	)
	assert.NoError(err)

	// add Staker0 (with the right end time) to state
	// so to allow proposalBlk issuance
	staker0StartTime := defaultGenesisTime
	staker0EndTime := pendingValidatorStartTime
	addStaker0, err := h.txBuilder.NewAddValidatorTx(
		10,
		uint64(staker0StartTime.Unix()),
		uint64(staker0EndTime.Unix()),
		ids.GenerateTestNodeID(),
		ids.GenerateTestShortID(),
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
		ids.ShortEmpty,
	)
	assert.NoError(err)
	h.fullState.AddCurrentStaker(addStaker0, uint64(2022))
	h.fullState.AddTx(addStaker0, status.Committed)
	assert.NoError(h.fullState.Commit())
	assert.NoError(h.fullState.Load())

	// create rewardTx for staker0
	s0RewardTx := &txs.Tx{
		Unsigned: &txs.RewardValidatorTx{
			TxID: addStaker0.ID(),
		},
	}
	assert.NoError(s0RewardTx.Sign(txs.Codec, nil))

	// build proposal block moving ahead chain time
	preferredID := h.fullState.GetLastAccepted()
	parentBlk, _, err := h.fullState.GetStatelessBlock(preferredID)
	assert.NoError(err)
	block, err := NewProposalBlock(
		stateless.PostForkVersion,
		uint64(pendingValidatorStartTime.Unix()),
		h.blkVerifier,
		h.txExecBackend,
		parentBlk.ID(),
		parentBlk.Height()+1,
		s0RewardTx,
	)
	assert.NoError(err)
	assert.NoError(block.Verify())

	block.onPostForkBaseOptionsState.Apply(h.fullState)
	block.onCommitState.Apply(h.fullState)
	assert.NoError(h.fullState.Commit())

	// Test validator weight before delegation
	primarySet, ok := h.cfg.Validators.GetValidators(constants.PrimaryNetworkID)
	assert.True(ok)
	vdrWeight, _ := primarySet.GetWeight(nodeID)
	assert.Equal(h.cfg.MinValidatorStake, vdrWeight)

	// Add delegator
	pendingDelegatorStartTime := pendingValidatorStartTime.Add(1 * time.Second)
	pendingDelegatorEndTime := pendingDelegatorStartTime.Add(defaultMinStakingDuration)
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

	// add Staker0 (with the right end time) to state
	// so to allow proposalBlk issuance
	staker0EndTime = pendingDelegatorStartTime
	addStaker0, err = h.txBuilder.NewAddValidatorTx(
		10,
		uint64(staker0StartTime.Unix()),
		uint64(staker0EndTime.Unix()),
		ids.GenerateTestNodeID(),
		ids.GenerateTestShortID(),
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
		ids.ShortEmpty,
	)
	assert.NoError(err)
	h.fullState.AddCurrentStaker(addStaker0, uint64(2022))
	h.fullState.AddTx(addStaker0, status.Committed)
	assert.NoError(h.fullState.Commit())
	assert.NoError(h.fullState.Load())

	// create rewardTx for staker0
	s0RewardTx = &txs.Tx{
		Unsigned: &txs.RewardValidatorTx{
			TxID: addStaker0.ID(),
		},
	}
	assert.NoError(s0RewardTx.Sign(txs.Codec, nil))

	// Advance Time
	block, err = NewProposalBlock(
		stateless.PostForkVersion,
		uint64(pendingDelegatorStartTime.Unix()),
		h.blkVerifier,
		h.txExecBackend,
		parentBlk.ID(),
		parentBlk.Height()+1,
		s0RewardTx,
	)
	assert.NoError(err)
	assert.NoError(block.Verify())

	block.onPostForkBaseOptionsState.Apply(h.fullState)
	block.onCommitState.Apply(h.fullState)
	assert.NoError(h.fullState.Commit())

	// Test validator weight after delegation
	vdrWeight, _ = primarySet.GetWeight(nodeID)
	assert.Equal(h.cfg.MinDelegatorStake+h.cfg.MinValidatorStake, vdrWeight)
}
