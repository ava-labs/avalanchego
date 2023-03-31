// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestApricotProposalBlockTimeVerification(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	env := newEnvironment(t, ctrl)
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()

	// create apricotParentBlk. It's a standard one for simplicity
	parentHeight := uint64(2022)

	apricotParentBlk, err := blocks.NewApricotStandardBlock(
		ids.Empty, // does not matter
		parentHeight,
		nil, // txs do not matter in this test
	)
	require.NoError(err)
	parentID := apricotParentBlk.ID()

	// store parent block, with relevant quantities
	onParentAccept := state.NewMockDiff(ctrl)
	env.blkManager.(*manager).blkIDToState[parentID] = &blockState{
		statelessBlock: apricotParentBlk,
		onAcceptState:  onParentAccept,
	}
	env.blkManager.(*manager).lastAccepted = parentID
	chainTime := env.clk.Time().Truncate(time.Second)
	env.mockedState.EXPECT().GetTimestamp().Return(chainTime).AnyTimes()
	env.mockedState.EXPECT().GetLastAccepted().Return(parentID).AnyTimes()

	// create a proposal transaction to be included into proposal block
	utx := &txs.AddValidatorTx{
		BaseTx:    txs.BaseTx{},
		Validator: txs.Validator{End: uint64(chainTime.Unix())},
		StakeOuts: []*avax.TransferableOutput{
			{
				Asset: avax.Asset{
					ID: env.ctx.AVAXAssetID,
				},
				Out: &secp256k1fx.TransferOutput{
					Amt: 1,
				},
			},
		},
		RewardsOwner:     &secp256k1fx.OutputOwners{},
		DelegationShares: uint32(defaultTxFee),
	}
	addValTx := &txs.Tx{Unsigned: utx}
	require.NoError(addValTx.Initialize(txs.Codec))
	blkTx := &txs.Tx{
		Unsigned: &txs.RewardValidatorTx{
			TxID: addValTx.ID(),
		},
	}

	// setup state to validate proposal block transaction
	onParentAccept.EXPECT().GetTimestamp().Return(chainTime).AnyTimes()

	currentStakersIt := state.NewMockStakerIterator(ctrl)
	currentStakersIt.EXPECT().Next().Return(true)
	currentStakersIt.EXPECT().Value().Return(&state.Staker{
		TxID:      addValTx.ID(),
		NodeID:    utx.NodeID(),
		SubnetID:  utx.SubnetID(),
		StartTime: utx.StartTime(),
		EndTime:   chainTime,
	}).Times(2)
	currentStakersIt.EXPECT().Release()
	onParentAccept.EXPECT().GetCurrentStakerIterator().Return(currentStakersIt, nil)
	onParentAccept.EXPECT().GetCurrentValidator(utx.SubnetID(), utx.NodeID()).Return(&state.Staker{
		TxID:      addValTx.ID(),
		NodeID:    utx.NodeID(),
		SubnetID:  utx.SubnetID(),
		StartTime: utx.StartTime(),
		EndTime:   chainTime,
	}, nil)
	onParentAccept.EXPECT().GetTx(addValTx.ID()).Return(addValTx, status.Committed, nil)
	onParentAccept.EXPECT().GetCurrentSupply(constants.PrimaryNetworkID).Return(uint64(1000), nil).AnyTimes()

	env.mockedState.EXPECT().GetUptime(gomock.Any(), constants.PrimaryNetworkID).Return(
		time.Duration(1000), /*upDuration*/
		time.Time{},         /*lastUpdated*/
		nil,                 /*err*/
	).AnyTimes()

	// wrong height
	statelessProposalBlock, err := blocks.NewApricotProposalBlock(
		parentID,
		parentHeight,
		blkTx,
	)
	require.NoError(err)

	block := env.blkManager.NewBlock(statelessProposalBlock)
	require.Error(block.Verify(context.Background()))

	// valid
	statelessProposalBlock, err = blocks.NewApricotProposalBlock(
		parentID,
		parentHeight+1,
		blkTx,
	)
	require.NoError(err)

	block = env.blkManager.NewBlock(statelessProposalBlock)
	require.NoError(block.Verify(context.Background()))
}

func TestBanffProposalBlockTimeVerification(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	env := newEnvironment(t, ctrl)
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()
	env.clk.Set(defaultGenesisTime)
	env.config.BanffTime = time.Time{} // activate Banff

	// create parentBlock. It's a standard one for simplicity
	parentTime := defaultGenesisTime
	parentHeight := uint64(2022)

	banffParentBlk, err := blocks.NewApricotStandardBlock(
		genesisBlkID, // does not matter
		parentHeight,
		nil, // txs do not matter in this test
	)
	require.NoError(err)
	parentID := banffParentBlk.ID()

	// store parent block, with relevant quantities
	chainTime := parentTime
	env.mockedState.EXPECT().GetTimestamp().Return(chainTime).AnyTimes()

	onParentAccept := state.NewMockDiff(ctrl)
	onParentAccept.EXPECT().GetTimestamp().Return(parentTime).AnyTimes()
	onParentAccept.EXPECT().GetCurrentSupply(constants.PrimaryNetworkID).Return(uint64(1000), nil).AnyTimes()

	env.blkManager.(*manager).blkIDToState[parentID] = &blockState{
		statelessBlock: banffParentBlk,
		onAcceptState:  onParentAccept,
		timestamp:      parentTime,
	}
	env.blkManager.(*manager).lastAccepted = parentID
	env.mockedState.EXPECT().GetLastAccepted().Return(parentID).AnyTimes()
	env.mockedState.EXPECT().GetStatelessBlock(gomock.Any()).DoAndReturn(
		func(blockID ids.ID) (blocks.Block, choices.Status, error) {
			if blockID == parentID {
				return banffParentBlk, choices.Accepted, nil
			}
			return nil, choices.Rejected, database.ErrNotFound
		}).AnyTimes()

	// setup state to validate proposal block transaction
	nextStakerTime := chainTime.Add(executor.SyncBound).Add(-1 * time.Second)
	unsignedNextStakerTx := &txs.AddValidatorTx{
		BaseTx:    txs.BaseTx{},
		Validator: txs.Validator{End: uint64(nextStakerTime.Unix())},
		StakeOuts: []*avax.TransferableOutput{
			{
				Asset: avax.Asset{
					ID: env.ctx.AVAXAssetID,
				},
				Out: &secp256k1fx.TransferOutput{
					Amt: 1,
				},
			},
		},
		RewardsOwner:     &secp256k1fx.OutputOwners{},
		DelegationShares: uint32(defaultTxFee),
	}
	nextStakerTx := &txs.Tx{Unsigned: unsignedNextStakerTx}
	require.NoError(nextStakerTx.Initialize(txs.Codec))

	nextStakerTxID := nextStakerTx.ID()
	onParentAccept.EXPECT().GetCurrentValidator(unsignedNextStakerTx.SubnetID(), unsignedNextStakerTx.NodeID()).Return(&state.Staker{
		TxID:      nextStakerTxID,
		NodeID:    unsignedNextStakerTx.NodeID(),
		SubnetID:  unsignedNextStakerTx.SubnetID(),
		StartTime: unsignedNextStakerTx.StartTime(),
		EndTime:   chainTime,
	}, nil)
	onParentAccept.EXPECT().GetTx(nextStakerTxID).Return(nextStakerTx, status.Processing, nil)

	currentStakersIt := state.NewMockStakerIterator(ctrl)
	currentStakersIt.EXPECT().Next().Return(true).AnyTimes()
	currentStakersIt.EXPECT().Value().Return(&state.Staker{
		TxID:     nextStakerTxID,
		EndTime:  nextStakerTime,
		NextTime: nextStakerTime,
		Priority: txs.PrimaryNetworkValidatorCurrentPriority,
	}).AnyTimes()
	currentStakersIt.EXPECT().Release().AnyTimes()
	onParentAccept.EXPECT().GetCurrentStakerIterator().Return(currentStakersIt, nil).AnyTimes()

	pendingStakersIt := state.NewMockStakerIterator(ctrl)
	pendingStakersIt.EXPECT().Next().Return(false).AnyTimes() // no pending stakers
	pendingStakersIt.EXPECT().Release().AnyTimes()
	onParentAccept.EXPECT().GetPendingStakerIterator().Return(pendingStakersIt, nil).AnyTimes()

	env.mockedState.EXPECT().GetUptime(gomock.Any(), gomock.Any()).Return(
		time.Duration(1000), /*upDuration*/
		time.Time{},         /*lastUpdated*/
		nil,                 /*err*/
	).AnyTimes()

	// create proposal tx to be included in the proposal block
	blkTx := &txs.Tx{
		Unsigned: &txs.RewardValidatorTx{
			TxID: nextStakerTxID,
		},
	}
	require.NoError(blkTx.Initialize(txs.Codec))

	{
		// wrong height
		statelessProposalBlock, err := blocks.NewBanffProposalBlock(
			parentTime.Add(time.Second),
			parentID,
			banffParentBlk.Height(),
			blkTx,
		)
		require.NoError(err)

		block := env.blkManager.NewBlock(statelessProposalBlock)
		require.Error(block.Verify(context.Background()))
	}

	{
		// wrong version
		statelessProposalBlock, err := blocks.NewApricotProposalBlock(
			parentID,
			banffParentBlk.Height()+1,
			blkTx,
		)
		require.NoError(err)

		block := env.blkManager.NewBlock(statelessProposalBlock)
		require.Error(block.Verify(context.Background()))
	}

	{
		// wrong timestamp, earlier than parent
		statelessProposalBlock, err := blocks.NewBanffProposalBlock(
			parentTime.Add(-1*time.Second),
			parentID,
			banffParentBlk.Height()+1,
			blkTx,
		)
		require.NoError(err)

		block := env.blkManager.NewBlock(statelessProposalBlock)
		require.Error(block.Verify(context.Background()))
	}

	{
		// wrong timestamp, violated synchrony bound
		beyondSyncBoundTimeStamp := env.clk.Time().Add(executor.SyncBound).Add(time.Second)
		statelessProposalBlock, err := blocks.NewBanffProposalBlock(
			beyondSyncBoundTimeStamp,
			parentID,
			banffParentBlk.Height()+1,
			blkTx,
		)
		require.NoError(err)

		block := env.blkManager.NewBlock(statelessProposalBlock)
		require.Error(block.Verify(context.Background()))
	}

	{
		// wrong timestamp, skipped staker set change event
		skippedStakerEventTimeStamp := nextStakerTime.Add(time.Second)
		statelessProposalBlock, err := blocks.NewBanffProposalBlock(
			skippedStakerEventTimeStamp,
			parentID,
			banffParentBlk.Height()+1,
			blkTx,
		)
		require.NoError(err)

		block := env.blkManager.NewBlock(statelessProposalBlock)
		require.Error(block.Verify(context.Background()))
	}

	{
		// wrong tx content (no advance time txs)
		invalidTx := &txs.Tx{
			Unsigned: &txs.AdvanceTimeTx{
				Time: uint64(nextStakerTime.Unix()),
			},
		}
		require.NoError(invalidTx.Initialize(txs.Codec))
		statelessProposalBlock, err := blocks.NewBanffProposalBlock(
			parentTime.Add(time.Second),
			parentID,
			banffParentBlk.Height()+1,
			invalidTx,
		)
		require.NoError(err)

		block := env.blkManager.NewBlock(statelessProposalBlock)
		require.Error(block.Verify(context.Background()))
	}

	{
		// include too many transactions
		statelessProposalBlock, err := blocks.NewBanffProposalBlock(
			nextStakerTime,
			parentID,
			banffParentBlk.Height()+1,
			blkTx,
		)
		require.NoError(err)

		statelessProposalBlock.Transactions = []*txs.Tx{blkTx}
		block := env.blkManager.NewBlock(statelessProposalBlock)
		require.ErrorIs(block.Verify(context.Background()), errBanffProposalBlockWithMultipleTransactions)
	}

	{
		// valid
		statelessProposalBlock, err := blocks.NewBanffProposalBlock(
			nextStakerTime,
			parentID,
			banffParentBlk.Height()+1,
			blkTx,
		)
		require.NoError(err)

		block := env.blkManager.NewBlock(statelessProposalBlock)
		require.NoError(block.Verify(context.Background()))
	}
}

func TestBanffProposalBlockUpdateStakers(t *testing.T) {
	// Chronological order (not in scale):
	// Staker0:    |--- ??? // Staker0 end time depends on the test
	// Staker1:        |------------------------------------------------------|
	// Staker2:            |------------------------|
	// Staker3:                |------------------------|
	// Staker3sub:                 |----------------|
	// Staker4:                |------------------------|
	// Staker5:                                     |--------------------|

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
		t.Run(test.description, func(t *testing.T) {
			require := require.New(t)
			env := newEnvironment(t, nil)
			defer func() {
				require.NoError(shutdownEnvironment(env))
			}()

			env.config.BanffTime = time.Time{} // activate Banff

			subnetID := testSubnet1.ID()
			env.config.TrackedSubnets.Add(subnetID)
			env.config.Validators.Add(subnetID, validators.NewSet())

			for _, staker := range test.stakers {
				tx, err := env.txBuilder.NewAddValidatorTx(
					env.config.MinValidatorStake,
					uint64(staker.startTime.Unix()),
					uint64(staker.endTime.Unix()),
					staker.nodeID,
					staker.rewardAddress,
					reward.PercentDenominator,
					[]*secp256k1.PrivateKey{preFundedKeys[0]},
					ids.ShortEmpty,
				)
				require.NoError(err)

				staker, err := state.NewPendingStaker(
					tx.ID(),
					tx.Unsigned.(*txs.AddValidatorTx),
				)
				require.NoError(err)

				env.state.PutPendingValidator(staker)
				env.state.AddTx(tx, status.Committed)
				require.NoError(env.state.Commit())
			}

			for _, subStaker := range test.subnetStakers {
				tx, err := env.txBuilder.NewAddSubnetValidatorTx(
					10, // Weight
					uint64(subStaker.startTime.Unix()),
					uint64(subStaker.endTime.Unix()),
					subStaker.nodeID, // validator ID
					subnetID,         // Subnet ID
					[]*secp256k1.PrivateKey{preFundedKeys[0], preFundedKeys[1]},
					ids.ShortEmpty,
				)
				require.NoError(err)

				subnetStaker, err := state.NewPendingStaker(
					tx.ID(),
					tx.Unsigned.(*txs.AddSubnetValidatorTx),
				)
				require.NoError(err)

				env.state.PutPendingValidator(subnetStaker)
				env.state.AddTx(tx, status.Committed)
				require.NoError(env.state.Commit())
			}

			for _, newTime := range test.advanceTimeTo {
				env.clk.Set(newTime)

				// add Staker0 (with the right end time) to state
				// so to allow proposalBlk issuance
				staker0.endTime = newTime
				addStaker0, err := env.txBuilder.NewAddValidatorTx(
					10,
					uint64(staker0.startTime.Unix()),
					uint64(staker0.endTime.Unix()),
					staker0.nodeID,
					staker0.rewardAddress,
					reward.PercentDenominator,
					[]*secp256k1.PrivateKey{preFundedKeys[0], preFundedKeys[1]},
					ids.ShortEmpty,
				)
				require.NoError(err)

				// store Staker0 to state
				staker0, err := state.NewCurrentStaker(
					addStaker0.ID(),
					addStaker0.Unsigned.(*txs.AddValidatorTx),
					0,
				)
				require.NoError(err)

				env.state.PutCurrentValidator(staker0)
				env.state.AddTx(addStaker0, status.Committed)
				require.NoError(env.state.Commit())

				s0RewardTx := &txs.Tx{
					Unsigned: &txs.RewardValidatorTx{
						TxID: staker0.TxID,
					},
				}
				require.NoError(s0RewardTx.Initialize(txs.Codec))

				// build proposal block moving ahead chain time
				// as well as rewarding staker0
				preferredID := env.state.GetLastAccepted()
				parentBlk, _, err := env.state.GetStatelessBlock(preferredID)
				require.NoError(err)
				statelessProposalBlock, err := blocks.NewBanffProposalBlock(
					newTime,
					parentBlk.ID(),
					parentBlk.Height()+1,
					s0RewardTx,
				)
				require.NoError(err)

				// verify and accept the block
				block := env.blkManager.NewBlock(statelessProposalBlock)
				require.NoError(block.Verify(context.Background()))
				options, err := block.(snowman.OracleBlock).Options(context.Background())
				require.NoError(err)

				require.NoError(options[0].Verify(context.Background()))

				require.NoError(block.Accept(context.Background()))
				require.NoError(options[0].Accept(context.Background()))
			}
			require.NoError(env.state.Commit())

			for stakerNodeID, status := range test.expectedStakers {
				switch status {
				case pending:
					_, err := env.state.GetPendingValidator(constants.PrimaryNetworkID, stakerNodeID)
					require.NoError(err)
					require.False(validators.Contains(env.config.Validators, constants.PrimaryNetworkID, stakerNodeID))
				case current:
					_, err := env.state.GetCurrentValidator(constants.PrimaryNetworkID, stakerNodeID)
					require.NoError(err)
					require.True(validators.Contains(env.config.Validators, constants.PrimaryNetworkID, stakerNodeID))
				}
			}

			for stakerNodeID, status := range test.expectedSubnetStakers {
				switch status {
				case pending:
					require.False(validators.Contains(env.config.Validators, subnetID, stakerNodeID))
				case current:
					require.True(validators.Contains(env.config.Validators, subnetID, stakerNodeID))
				}
			}
		})
	}
}

func TestBanffProposalBlockRemoveSubnetValidator(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, nil)
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()
	env.config.BanffTime = time.Time{} // activate Banff

	subnetID := testSubnet1.ID()
	env.config.TrackedSubnets.Add(subnetID)
	env.config.Validators.Add(subnetID, validators.NewSet())

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
		subnetID,                           // Subnet ID
		[]*secp256k1.PrivateKey{preFundedKeys[0], preFundedKeys[1]},
		ids.ShortEmpty,
	)
	require.NoError(err)

	staker, err := state.NewCurrentStaker(
		tx.ID(),
		tx.Unsigned.(*txs.AddSubnetValidatorTx),
		0,
	)
	require.NoError(err)

	env.state.PutCurrentValidator(staker)
	env.state.AddTx(tx, status.Committed)
	require.NoError(env.state.Commit())

	// The above validator is now part of the staking set

	// Queue a staker that joins the staker set after the above validator leaves
	subnetVdr2NodeID := ids.NodeID(preFundedKeys[1].PublicKey().Address())
	tx, err = env.txBuilder.NewAddSubnetValidatorTx(
		1, // Weight
		uint64(subnetVdr1EndTime.Add(time.Second).Unix()),                                // Start time
		uint64(subnetVdr1EndTime.Add(time.Second).Add(defaultMinStakingDuration).Unix()), // end time
		subnetVdr2NodeID, // Node ID
		subnetID,         // Subnet ID
		[]*secp256k1.PrivateKey{preFundedKeys[0], preFundedKeys[1]},
		ids.ShortEmpty,
	)
	require.NoError(err)

	staker, err = state.NewPendingStaker(
		tx.ID(),
		tx.Unsigned.(*txs.AddSubnetValidatorTx),
	)
	require.NoError(err)

	env.state.PutPendingValidator(staker)
	env.state.AddTx(tx, status.Committed)
	require.NoError(env.state.Commit())

	// The above validator is now in the pending staker set

	// Advance time to the first staker's end time.
	env.clk.Set(subnetVdr1EndTime)

	// add Staker0 (with the right end time) to state
	// so to allow proposalBlk issuance
	staker0StartTime := defaultValidateStartTime
	staker0EndTime := subnetVdr1EndTime
	addStaker0, err := env.txBuilder.NewAddValidatorTx(
		10,
		uint64(staker0StartTime.Unix()),
		uint64(staker0EndTime.Unix()),
		ids.GenerateTestNodeID(),
		ids.GenerateTestShortID(),
		reward.PercentDenominator,
		[]*secp256k1.PrivateKey{preFundedKeys[0], preFundedKeys[1]},
		ids.ShortEmpty,
	)
	require.NoError(err)

	// store Staker0 to state
	staker, err = state.NewCurrentStaker(
		addStaker0.ID(),
		addStaker0.Unsigned.(*txs.AddValidatorTx),
		0,
	)
	require.NoError(err)

	env.state.PutCurrentValidator(staker)
	env.state.AddTx(addStaker0, status.Committed)
	require.NoError(env.state.Commit())

	// create rewardTx for staker0
	s0RewardTx := &txs.Tx{
		Unsigned: &txs.RewardValidatorTx{
			TxID: addStaker0.ID(),
		},
	}
	require.NoError(s0RewardTx.Initialize(txs.Codec))

	// build proposal block moving ahead chain time
	preferredID := env.state.GetLastAccepted()
	parentBlk, _, err := env.state.GetStatelessBlock(preferredID)
	require.NoError(err)
	statelessProposalBlock, err := blocks.NewBanffProposalBlock(
		subnetVdr1EndTime,
		parentBlk.ID(),
		parentBlk.Height()+1,
		s0RewardTx,
	)
	require.NoError(err)
	propBlk := env.blkManager.NewBlock(statelessProposalBlock)
	require.NoError(propBlk.Verify(context.Background())) // verify and update staker set

	options, err := propBlk.(snowman.OracleBlock).Options(context.Background())
	require.NoError(err)
	commitBlk := options[0]
	require.NoError(commitBlk.Verify(context.Background()))

	blkStateMap := env.blkManager.(*manager).blkIDToState
	updatedState := blkStateMap[commitBlk.ID()].onAcceptState
	_, err = updatedState.GetCurrentValidator(subnetID, subnetValidatorNodeID)
	require.ErrorIs(err, database.ErrNotFound)

	// Check VM Validators are removed successfully
	require.NoError(propBlk.Accept(context.Background()))
	require.NoError(commitBlk.Accept(context.Background()))
	require.False(validators.Contains(env.config.Validators, subnetID, subnetVdr2NodeID))
	require.False(validators.Contains(env.config.Validators, subnetID, subnetValidatorNodeID))
}

func TestBanffProposalBlockTrackedSubnet(t *testing.T) {
	for _, tracked := range []bool{true, false} {
		t.Run(fmt.Sprintf("tracked %t", tracked), func(ts *testing.T) {
			require := require.New(t)
			env := newEnvironment(t, nil)
			defer func() {
				require.NoError(shutdownEnvironment(env))
			}()
			env.config.BanffTime = time.Time{} // activate Banff

			subnetID := testSubnet1.ID()
			if tracked {
				env.config.TrackedSubnets.Add(subnetID)
				env.config.Validators.Add(subnetID, validators.NewSet())
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
				subnetID,                           // Subnet ID
				[]*secp256k1.PrivateKey{preFundedKeys[0], preFundedKeys[1]},
				ids.ShortEmpty,
			)
			require.NoError(err)

			staker, err := state.NewPendingStaker(
				tx.ID(),
				tx.Unsigned.(*txs.AddSubnetValidatorTx),
			)
			require.NoError(err)

			env.state.PutPendingValidator(staker)
			env.state.AddTx(tx, status.Committed)
			require.NoError(env.state.Commit())

			// Advance time to the staker's start time.
			env.clk.Set(subnetVdr1StartTime)

			// add Staker0 (with the right end time) to state
			// so to allow proposalBlk issuance
			staker0StartTime := defaultGenesisTime
			staker0EndTime := subnetVdr1StartTime
			addStaker0, err := env.txBuilder.NewAddValidatorTx(
				10,
				uint64(staker0StartTime.Unix()),
				uint64(staker0EndTime.Unix()),
				ids.GenerateTestNodeID(),
				ids.GenerateTestShortID(),
				reward.PercentDenominator,
				[]*secp256k1.PrivateKey{preFundedKeys[0], preFundedKeys[1]},
				ids.ShortEmpty,
			)
			require.NoError(err)

			// store Staker0 to state
			staker, err = state.NewCurrentStaker(
				addStaker0.ID(),
				addStaker0.Unsigned.(*txs.AddValidatorTx),
				0,
			)
			require.NoError(err)

			env.state.PutCurrentValidator(staker)
			env.state.AddTx(addStaker0, status.Committed)
			require.NoError(env.state.Commit())

			// create rewardTx for staker0
			s0RewardTx := &txs.Tx{
				Unsigned: &txs.RewardValidatorTx{
					TxID: addStaker0.ID(),
				},
			}
			require.NoError(s0RewardTx.Initialize(txs.Codec))

			// build proposal block moving ahead chain time
			preferredID := env.state.GetLastAccepted()
			parentBlk, _, err := env.state.GetStatelessBlock(preferredID)
			require.NoError(err)
			statelessProposalBlock, err := blocks.NewBanffProposalBlock(
				subnetVdr1StartTime,
				parentBlk.ID(),
				parentBlk.Height()+1,
				s0RewardTx,
			)
			require.NoError(err)
			propBlk := env.blkManager.NewBlock(statelessProposalBlock)
			require.NoError(propBlk.Verify(context.Background())) // verify update staker set
			options, err := propBlk.(snowman.OracleBlock).Options(context.Background())
			require.NoError(err)
			commitBlk := options[0]
			require.NoError(commitBlk.Verify(context.Background()))

			require.NoError(propBlk.Accept(context.Background()))
			require.NoError(commitBlk.Accept(context.Background()))
			require.Equal(tracked, validators.Contains(env.config.Validators, subnetID, subnetValidatorNodeID))
		})
	}
}

func TestBanffProposalBlockDelegatorStakerWeight(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, nil)
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()
	env.config.BanffTime = time.Time{} // activate Banff

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
		[]*secp256k1.PrivateKey{preFundedKeys[0]},
	)
	require.NoError(err)

	// add Staker0 (with the right end time) to state
	// just to allow proposalBlk issuance (with a reward Tx)
	staker0StartTime := defaultGenesisTime
	staker0EndTime := pendingValidatorStartTime
	addStaker0, err := env.txBuilder.NewAddValidatorTx(
		10,
		uint64(staker0StartTime.Unix()),
		uint64(staker0EndTime.Unix()),
		ids.GenerateTestNodeID(),
		ids.GenerateTestShortID(),
		reward.PercentDenominator,
		[]*secp256k1.PrivateKey{preFundedKeys[0], preFundedKeys[1]},
		ids.ShortEmpty,
	)
	require.NoError(err)

	// store Staker0 to state
	staker, err := state.NewCurrentStaker(
		addStaker0.ID(),
		addStaker0.Unsigned.(*txs.AddValidatorTx),
		0,
	)
	require.NoError(err)

	env.state.PutCurrentValidator(staker)
	env.state.AddTx(addStaker0, status.Committed)
	require.NoError(env.state.Commit())

	// create rewardTx for staker0
	s0RewardTx := &txs.Tx{
		Unsigned: &txs.RewardValidatorTx{
			TxID: addStaker0.ID(),
		},
	}
	require.NoError(s0RewardTx.Initialize(txs.Codec))

	// build proposal block moving ahead chain time
	preferredID := env.state.GetLastAccepted()
	parentBlk, _, err := env.state.GetStatelessBlock(preferredID)
	require.NoError(err)
	statelessProposalBlock, err := blocks.NewBanffProposalBlock(
		pendingValidatorStartTime,
		parentBlk.ID(),
		parentBlk.Height()+1,
		s0RewardTx,
	)
	require.NoError(err)
	propBlk := env.blkManager.NewBlock(statelessProposalBlock)
	require.NoError(propBlk.Verify(context.Background()))

	options, err := propBlk.(snowman.OracleBlock).Options(context.Background())
	require.NoError(err)
	commitBlk := options[0]
	require.NoError(commitBlk.Verify(context.Background()))

	require.NoError(propBlk.Accept(context.Background()))
	require.NoError(commitBlk.Accept(context.Background()))

	// Test validator weight before delegation
	primarySet, ok := env.config.Validators.Get(constants.PrimaryNetworkID)
	require.True(ok)
	vdrWeight := primarySet.GetWeight(nodeID)
	require.Equal(env.config.MinValidatorStake, vdrWeight)

	// Add delegator
	pendingDelegatorStartTime := pendingValidatorStartTime.Add(1 * time.Second)
	pendingDelegatorEndTime := pendingDelegatorStartTime.Add(1 * time.Second)

	addDelegatorTx, err := env.txBuilder.NewAddDelegatorTx(
		env.config.MinDelegatorStake,
		uint64(pendingDelegatorStartTime.Unix()),
		uint64(pendingDelegatorEndTime.Unix()),
		nodeID,
		preFundedKeys[0].PublicKey().Address(),
		[]*secp256k1.PrivateKey{
			preFundedKeys[0],
			preFundedKeys[1],
			preFundedKeys[4],
		},
		ids.ShortEmpty,
	)
	require.NoError(err)

	staker, err = state.NewPendingStaker(
		addDelegatorTx.ID(),
		addDelegatorTx.Unsigned.(*txs.AddDelegatorTx),
	)
	require.NoError(err)

	env.state.PutPendingDelegator(staker)
	env.state.AddTx(addDelegatorTx, status.Committed)
	env.state.SetHeight( /*dummyHeight*/ uint64(1))
	require.NoError(env.state.Commit())

	// add Staker0 (with the right end time) to state
	// so to allow proposalBlk issuance
	staker0EndTime = pendingDelegatorStartTime
	addStaker0, err = env.txBuilder.NewAddValidatorTx(
		10,
		uint64(staker0StartTime.Unix()),
		uint64(staker0EndTime.Unix()),
		ids.GenerateTestNodeID(),
		ids.GenerateTestShortID(),
		reward.PercentDenominator,
		[]*secp256k1.PrivateKey{preFundedKeys[0], preFundedKeys[1]},
		ids.ShortEmpty,
	)
	require.NoError(err)

	// store Staker0 to state
	staker, err = state.NewCurrentStaker(
		addStaker0.ID(),
		addStaker0.Unsigned.(*txs.AddValidatorTx),
		0,
	)
	require.NoError(err)

	env.state.PutCurrentValidator(staker)
	env.state.AddTx(addStaker0, status.Committed)
	require.NoError(env.state.Commit())

	// create rewardTx for staker0
	s0RewardTx = &txs.Tx{
		Unsigned: &txs.RewardValidatorTx{
			TxID: addStaker0.ID(),
		},
	}
	require.NoError(s0RewardTx.Initialize(txs.Codec))

	// Advance Time
	preferredID = env.state.GetLastAccepted()
	parentBlk, _, err = env.state.GetStatelessBlock(preferredID)
	require.NoError(err)
	statelessProposalBlock, err = blocks.NewBanffProposalBlock(
		pendingDelegatorStartTime,
		parentBlk.ID(),
		parentBlk.Height()+1,
		s0RewardTx,
	)
	require.NoError(err)

	propBlk = env.blkManager.NewBlock(statelessProposalBlock)
	require.NoError(propBlk.Verify(context.Background()))

	options, err = propBlk.(snowman.OracleBlock).Options(context.Background())
	require.NoError(err)
	commitBlk = options[0]
	require.NoError(commitBlk.Verify(context.Background()))

	require.NoError(propBlk.Accept(context.Background()))
	require.NoError(commitBlk.Accept(context.Background()))

	// Test validator weight after delegation
	vdrWeight = primarySet.GetWeight(nodeID)
	require.Equal(env.config.MinDelegatorStake+env.config.MinValidatorStake, vdrWeight)
}

func TestBanffProposalBlockDelegatorStakers(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, nil)
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()
	env.config.BanffTime = time.Time{} // activate Banff

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := defaultGenesisTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(defaultMinStakingDuration)
	factory := secp256k1.Factory{}
	nodeIDKey, _ := factory.NewPrivateKey()
	rewardAddress := nodeIDKey.PublicKey().Address()
	nodeID := ids.NodeID(rewardAddress)

	_, err := addPendingValidator(
		env,
		pendingValidatorStartTime,
		pendingValidatorEndTime,
		nodeID,
		rewardAddress,
		[]*secp256k1.PrivateKey{preFundedKeys[0]},
	)
	require.NoError(err)

	// add Staker0 (with the right end time) to state
	// so to allow proposalBlk issuance
	staker0StartTime := defaultGenesisTime
	staker0EndTime := pendingValidatorStartTime
	addStaker0, err := env.txBuilder.NewAddValidatorTx(
		10,
		uint64(staker0StartTime.Unix()),
		uint64(staker0EndTime.Unix()),
		ids.GenerateTestNodeID(),
		ids.GenerateTestShortID(),
		reward.PercentDenominator,
		[]*secp256k1.PrivateKey{preFundedKeys[0], preFundedKeys[1]},
		ids.ShortEmpty,
	)
	require.NoError(err)

	// store Staker0 to state
	staker, err := state.NewCurrentStaker(
		addStaker0.ID(),
		addStaker0.Unsigned.(*txs.AddValidatorTx),
		0,
	)
	require.NoError(err)

	env.state.PutCurrentValidator(staker)
	env.state.AddTx(addStaker0, status.Committed)
	require.NoError(env.state.Commit())

	// create rewardTx for staker0
	s0RewardTx := &txs.Tx{
		Unsigned: &txs.RewardValidatorTx{
			TxID: addStaker0.ID(),
		},
	}
	require.NoError(s0RewardTx.Initialize(txs.Codec))

	// build proposal block moving ahead chain time
	preferredID := env.state.GetLastAccepted()
	parentBlk, _, err := env.state.GetStatelessBlock(preferredID)
	require.NoError(err)
	statelessProposalBlock, err := blocks.NewBanffProposalBlock(
		pendingValidatorStartTime,
		parentBlk.ID(),
		parentBlk.Height()+1,
		s0RewardTx,
	)
	require.NoError(err)
	propBlk := env.blkManager.NewBlock(statelessProposalBlock)
	require.NoError(propBlk.Verify(context.Background()))

	options, err := propBlk.(snowman.OracleBlock).Options(context.Background())
	require.NoError(err)
	commitBlk := options[0]
	require.NoError(commitBlk.Verify(context.Background()))

	require.NoError(propBlk.Accept(context.Background()))
	require.NoError(commitBlk.Accept(context.Background()))

	// Test validator weight before delegation
	primarySet, ok := env.config.Validators.Get(constants.PrimaryNetworkID)
	require.True(ok)
	vdrWeight := primarySet.GetWeight(nodeID)
	require.Equal(env.config.MinValidatorStake, vdrWeight)

	// Add delegator
	pendingDelegatorStartTime := pendingValidatorStartTime.Add(1 * time.Second)
	pendingDelegatorEndTime := pendingDelegatorStartTime.Add(defaultMinStakingDuration)
	addDelegatorTx, err := env.txBuilder.NewAddDelegatorTx(
		env.config.MinDelegatorStake,
		uint64(pendingDelegatorStartTime.Unix()),
		uint64(pendingDelegatorEndTime.Unix()),
		nodeID,
		preFundedKeys[0].PublicKey().Address(),
		[]*secp256k1.PrivateKey{
			preFundedKeys[0],
			preFundedKeys[1],
			preFundedKeys[4],
		},
		ids.ShortEmpty,
	)
	require.NoError(err)

	staker, err = state.NewPendingStaker(
		addDelegatorTx.ID(),
		addDelegatorTx.Unsigned.(*txs.AddDelegatorTx),
	)
	require.NoError(err)

	env.state.PutPendingDelegator(staker)
	env.state.AddTx(addDelegatorTx, status.Committed)
	env.state.SetHeight( /*dummyHeight*/ uint64(1))
	require.NoError(env.state.Commit())

	// add Staker0 (with the right end time) to state
	// so to allow proposalBlk issuance
	staker0EndTime = pendingDelegatorStartTime
	addStaker0, err = env.txBuilder.NewAddValidatorTx(
		10,
		uint64(staker0StartTime.Unix()),
		uint64(staker0EndTime.Unix()),
		ids.GenerateTestNodeID(),
		ids.GenerateTestShortID(),
		reward.PercentDenominator,
		[]*secp256k1.PrivateKey{preFundedKeys[0], preFundedKeys[1]},
		ids.ShortEmpty,
	)
	require.NoError(err)

	// store Staker0 to state
	staker, err = state.NewCurrentStaker(
		addStaker0.ID(),
		addStaker0.Unsigned.(*txs.AddValidatorTx),
		0,
	)
	require.NoError(err)

	env.state.PutCurrentValidator(staker)
	env.state.AddTx(addStaker0, status.Committed)
	require.NoError(env.state.Commit())

	// create rewardTx for staker0
	s0RewardTx = &txs.Tx{
		Unsigned: &txs.RewardValidatorTx{
			TxID: addStaker0.ID(),
		},
	}
	require.NoError(s0RewardTx.Initialize(txs.Codec))

	// Advance Time
	preferredID = env.state.GetLastAccepted()
	parentBlk, _, err = env.state.GetStatelessBlock(preferredID)
	require.NoError(err)
	statelessProposalBlock, err = blocks.NewBanffProposalBlock(
		pendingDelegatorStartTime,
		parentBlk.ID(),
		parentBlk.Height()+1,
		s0RewardTx,
	)
	require.NoError(err)
	propBlk = env.blkManager.NewBlock(statelessProposalBlock)
	require.NoError(propBlk.Verify(context.Background()))

	options, err = propBlk.(snowman.OracleBlock).Options(context.Background())
	require.NoError(err)
	commitBlk = options[0]
	require.NoError(commitBlk.Verify(context.Background()))

	require.NoError(propBlk.Accept(context.Background()))
	require.NoError(commitBlk.Accept(context.Background()))

	// Test validator weight after delegation
	vdrWeight = primarySet.GetWeight(nodeID)
	require.Equal(env.config.MinDelegatorStake+env.config.MinValidatorStake, vdrWeight)
}
