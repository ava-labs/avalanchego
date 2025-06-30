// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	walletcommon "github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

func TestApricotProposalBlockTimeVerification(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	env := newEnvironment(t, ctrl, upgradetest.ApricotPhase5)

	// create apricotParentBlk. It's a standard one for simplicity
	parentHeight := uint64(2022)

	apricotParentBlk, err := block.NewApricotStandardBlock(
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
	onParentAccept.EXPECT().GetFeeState().Return(gas.State{}).AnyTimes()
	onParentAccept.EXPECT().GetL1ValidatorExcess().Return(gas.Gas(0)).AnyTimes()
	onParentAccept.EXPECT().GetAccruedFees().Return(uint64(0)).AnyTimes()
	onParentAccept.EXPECT().NumActiveL1Validators().Return(0).AnyTimes()

	onParentAccept.EXPECT().GetCurrentStakerIterator().Return(
		iterator.FromSlice(&state.Staker{
			TxID:      addValTx.ID(),
			NodeID:    utx.NodeID(),
			SubnetID:  utx.SubnetID(),
			StartTime: utx.StartTime(),
			NextTime:  chainTime,
			EndTime:   chainTime,
		}),
		nil,
	)
	onParentAccept.EXPECT().GetTx(addValTx.ID()).Return(addValTx, status.Committed, nil)
	onParentAccept.EXPECT().GetCurrentSupply(constants.PrimaryNetworkID).Return(uint64(1000), nil).AnyTimes()
	onParentAccept.EXPECT().GetDelegateeReward(constants.PrimaryNetworkID, utx.NodeID()).Return(uint64(0), nil).AnyTimes()

	env.mockedState.EXPECT().GetUptime(gomock.Any()).Return(
		time.Microsecond, /*upDuration*/
		time.Time{},      /*lastUpdated*/
		nil,              /*err*/
	).AnyTimes()

	// wrong height
	statelessProposalBlock, err := block.NewApricotProposalBlock(
		parentID,
		parentHeight,
		blkTx,
	)
	require.NoError(err)

	proposalBlock := env.blkManager.NewBlock(statelessProposalBlock)

	err = proposalBlock.Verify(context.Background())
	require.ErrorIs(err, errIncorrectBlockHeight)

	// valid
	statelessProposalBlock, err = block.NewApricotProposalBlock(
		parentID,
		parentHeight+1,
		blkTx,
	)
	require.NoError(err)

	proposalBlock = env.blkManager.NewBlock(statelessProposalBlock)
	require.NoError(proposalBlock.Verify(context.Background()))
}

func TestBanffProposalBlockTimeVerification(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	env := newEnvironment(t, ctrl, upgradetest.Banff)

	// create parentBlock. It's a standard one for simplicity
	parentTime := genesistest.DefaultValidatorStartTime
	parentHeight := uint64(2022)

	banffParentBlk, err := block.NewApricotStandardBlock(
		ids.GenerateTestID(), // does not matter
		parentHeight,
		nil, // txs do not matter in this test
	)
	require.NoError(err)
	parentID := banffParentBlk.ID()

	// store parent block, with relevant quantities
	chainTime := parentTime
	onParentAccept := state.NewMockDiff(ctrl)
	onParentAccept.EXPECT().GetTimestamp().Return(parentTime).AnyTimes()
	onParentAccept.EXPECT().GetFeeState().Return(gas.State{}).AnyTimes()
	onParentAccept.EXPECT().GetL1ValidatorExcess().Return(gas.Gas(0)).AnyTimes()
	onParentAccept.EXPECT().GetAccruedFees().Return(uint64(0)).AnyTimes()
	onParentAccept.EXPECT().NumActiveL1Validators().Return(0).AnyTimes()
	onParentAccept.EXPECT().GetCurrentSupply(constants.PrimaryNetworkID).Return(uint64(1000), nil).AnyTimes()

	env.blkManager.(*manager).blkIDToState[parentID] = &blockState{
		statelessBlock: banffParentBlk,
		onAcceptState:  onParentAccept,
		timestamp:      parentTime,
	}
	env.blkManager.(*manager).lastAccepted = parentID
	env.mockedState.EXPECT().GetLastAccepted().Return(parentID).AnyTimes()
	env.mockedState.EXPECT().GetStatelessBlock(gomock.Any()).DoAndReturn(
		func(blockID ids.ID) (block.Block, error) {
			if blockID == parentID {
				return banffParentBlk, nil
			}
			return nil, database.ErrNotFound
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
	onParentAccept.EXPECT().GetTx(nextStakerTxID).Return(nextStakerTx, status.Processing, nil)

	onParentAccept.EXPECT().GetCurrentStakerIterator().DoAndReturn(func() (iterator.Iterator[*state.Staker], error) {
		return iterator.FromSlice(
			&state.Staker{
				TxID:     nextStakerTxID,
				EndTime:  nextStakerTime,
				NextTime: nextStakerTime,
				Priority: txs.PrimaryNetworkValidatorCurrentPriority,
			},
		), nil
	}).AnyTimes()
	onParentAccept.EXPECT().GetPendingStakerIterator().Return(iterator.Empty[*state.Staker]{}, nil).AnyTimes()
	onParentAccept.EXPECT().GetActiveL1ValidatorsIterator().Return(iterator.Empty[state.L1Validator]{}, nil).AnyTimes()
	onParentAccept.EXPECT().GetExpiryIterator().Return(iterator.Empty[state.ExpiryEntry]{}, nil).AnyTimes()

	onParentAccept.EXPECT().GetDelegateeReward(constants.PrimaryNetworkID, unsignedNextStakerTx.NodeID()).Return(uint64(0), nil).AnyTimes()

	env.mockedState.EXPECT().GetUptime(gomock.Any).Return(
		time.Microsecond, /*upDuration*/
		time.Time{},      /*lastUpdated*/
		nil,              /*err*/
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
		statelessProposalBlock, err := block.NewBanffProposalBlock(
			parentTime.Add(time.Second),
			parentID,
			banffParentBlk.Height(),
			blkTx,
			[]*txs.Tx{},
		)
		require.NoError(err)

		block := env.blkManager.NewBlock(statelessProposalBlock)
		err = block.Verify(context.Background())
		require.ErrorIs(err, errIncorrectBlockHeight)
	}

	{
		// wrong block version
		statelessProposalBlock, err := block.NewApricotProposalBlock(
			parentID,
			banffParentBlk.Height()+1,
			blkTx,
		)
		require.NoError(err)

		block := env.blkManager.NewBlock(statelessProposalBlock)
		err = block.Verify(context.Background())
		require.ErrorIs(err, errApricotBlockIssuedAfterFork)
	}

	{
		// wrong timestamp, earlier than parent
		statelessProposalBlock, err := block.NewBanffProposalBlock(
			parentTime.Add(-1*time.Second),
			parentID,
			banffParentBlk.Height()+1,
			blkTx,
			[]*txs.Tx{},
		)
		require.NoError(err)

		block := env.blkManager.NewBlock(statelessProposalBlock)
		err = block.Verify(context.Background())
		require.ErrorIs(err, executor.ErrChildBlockEarlierThanParent)
	}

	{
		// wrong timestamp, violated synchrony bound
		initClkTime := env.clk.Time()
		env.clk.Set(parentTime.Add(-executor.SyncBound))
		statelessProposalBlock, err := block.NewBanffProposalBlock(
			parentTime.Add(time.Second),
			parentID,
			banffParentBlk.Height()+1,
			blkTx,
			[]*txs.Tx{},
		)
		require.NoError(err)

		block := env.blkManager.NewBlock(statelessProposalBlock)
		err = block.Verify(context.Background())
		require.ErrorIs(err, executor.ErrChildBlockBeyondSyncBound)
		env.clk.Set(initClkTime)
	}

	{
		// wrong timestamp, skipped staker set change event
		skippedStakerEventTimeStamp := nextStakerTime.Add(time.Second)
		statelessProposalBlock, err := block.NewBanffProposalBlock(
			skippedStakerEventTimeStamp,
			parentID,
			banffParentBlk.Height()+1,
			blkTx,
			[]*txs.Tx{},
		)
		require.NoError(err)

		block := env.blkManager.NewBlock(statelessProposalBlock)
		err = block.Verify(context.Background())
		require.ErrorIs(err, executor.ErrChildBlockAfterStakerChangeTime)
	}

	{
		// wrong tx content (no advance time txs)
		invalidTx := &txs.Tx{
			Unsigned: &txs.AdvanceTimeTx{
				Time: uint64(nextStakerTime.Unix()),
			},
		}
		require.NoError(invalidTx.Initialize(txs.Codec))
		statelessProposalBlock, err := block.NewBanffProposalBlock(
			parentTime.Add(time.Second),
			parentID,
			banffParentBlk.Height()+1,
			invalidTx,
			[]*txs.Tx{},
		)
		require.NoError(err)

		block := env.blkManager.NewBlock(statelessProposalBlock)
		err = block.Verify(context.Background())
		require.ErrorIs(err, executor.ErrAdvanceTimeTxIssuedAfterBanff)
	}

	{
		// valid
		statelessProposalBlock, err := block.NewBanffProposalBlock(
			nextStakerTime,
			parentID,
			banffParentBlk.Height()+1,
			blkTx,
			[]*txs.Tx{},
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

	// In this test multiple stakers may join and leave the staker set at the same time.
	// The order in which they do it is asserted; the order may depend on the staker.TxID,
	// which in turns depend on every feature of the transaction creating the staker.
	// So in this test we avoid ids.GenerateTestNodeID, in favour of ids.BuildTestNodeID
	// so that TxID does not depend on the order we run tests. We also explicitly declare
	// the change address, to avoid picking a random one in case multiple funding keys are set.
	staker0 := staker{
		nodeID:        ids.BuildTestNodeID([]byte{0xf0}),
		rewardAddress: ids.ShortID{0xf0},
		startTime:     genesistest.DefaultValidatorStartTime,
		endTime:       time.Time{}, // actual endTime depends on specific test
	}

	staker1 := staker{
		nodeID:        ids.BuildTestNodeID([]byte{0xf1}),
		rewardAddress: ids.ShortID{0xf1},
		startTime:     genesistest.DefaultValidatorStartTime.Add(1 * time.Minute),
		endTime:       genesistest.DefaultValidatorStartTime.Add(10 * defaultMinStakingDuration).Add(1 * time.Minute),
	}
	staker2 := staker{
		nodeID:        ids.BuildTestNodeID([]byte{0xf2}),
		rewardAddress: ids.ShortID{0xf2},
		startTime:     staker1.startTime.Add(1 * time.Minute),
		endTime:       staker1.startTime.Add(1 * time.Minute).Add(defaultMinStakingDuration),
	}
	staker3 := staker{
		nodeID:        ids.BuildTestNodeID([]byte{0xf3}),
		rewardAddress: ids.ShortID{0xf3},
		startTime:     staker2.startTime.Add(1 * time.Minute),
		endTime:       staker2.endTime.Add(1 * time.Minute),
	}
	staker3Sub := staker{
		nodeID:        ids.BuildTestNodeID([]byte{0xf3}),
		rewardAddress: ids.ShortID{0xff},
		startTime:     staker3.startTime.Add(1 * time.Minute),
		endTime:       staker3.endTime.Add(-1 * time.Minute),
	}
	staker4 := staker{
		nodeID:        ids.BuildTestNodeID([]byte{0xf4}),
		rewardAddress: ids.ShortID{0xf4},
		startTime:     staker3.startTime,
		endTime:       staker3.endTime,
	}
	staker5 := staker{
		nodeID:        ids.BuildTestNodeID([]byte{0xf5}),
		rewardAddress: ids.ShortID{0xf5},
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
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			require := require.New(t)
			env := newEnvironment(t, nil, upgradetest.Banff)

			subnetID := testSubnet1.ID()
			env.config.TrackedSubnets.Add(subnetID)

			for _, staker := range test.stakers {
				wallet := newWallet(t, env, walletConfig{})

				tx, err := wallet.IssueAddValidatorTx(
					&txs.Validator{
						NodeID: staker.nodeID,
						Start:  uint64(staker.startTime.Unix()),
						End:    uint64(staker.endTime.Unix()),
						Wght:   env.config.MinValidatorStake,
					},
					&secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{staker.rewardAddress},
					},
					reward.PercentDenominator,
				)
				require.NoError(err)

				staker, err := state.NewPendingStaker(
					tx.ID(),
					tx.Unsigned.(*txs.AddValidatorTx),
				)
				require.NoError(err)

				require.NoError(env.state.PutPendingValidator(staker))
				env.state.AddTx(tx, status.Committed)
				require.NoError(env.state.Commit())
			}

			for _, subStaker := range test.subnetStakers {
				wallet := newWallet(t, env, walletConfig{
					subnetIDs: []ids.ID{subnetID},
				})

				tx, err := wallet.IssueAddSubnetValidatorTx(
					&txs.SubnetValidator{
						Validator: txs.Validator{
							NodeID: subStaker.nodeID,
							Start:  uint64(subStaker.startTime.Unix()),
							End:    uint64(subStaker.endTime.Unix()),
							Wght:   10,
						},
						Subnet: subnetID,
					},
				)
				require.NoError(err)

				subnetStaker, err := state.NewPendingStaker(
					tx.ID(),
					tx.Unsigned.(*txs.AddSubnetValidatorTx),
				)
				require.NoError(err)

				require.NoError(env.state.PutPendingValidator(subnetStaker))
				env.state.AddTx(tx, status.Committed)
				require.NoError(env.state.Commit())
			}

			for _, newTime := range test.advanceTimeTo {
				env.clk.Set(newTime)

				// add Staker0 (with the right end time) to state
				// so to allow proposalBlk issuance
				staker0.endTime = newTime

				wallet := newWallet(t, env, walletConfig{})

				addStaker0, err := wallet.IssueAddValidatorTx(
					&txs.Validator{
						NodeID: staker0.nodeID,
						Start:  uint64(staker0.startTime.Unix()),
						End:    uint64(staker0.endTime.Unix()),
						Wght:   10,
					},
					&secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{staker0.rewardAddress},
					},
					reward.PercentDenominator,
				)
				require.NoError(err)

				// store Staker0 to state
				addValTx := addStaker0.Unsigned.(*txs.AddValidatorTx)
				staker0, err := state.NewCurrentStaker(
					addStaker0.ID(),
					addValTx,
					addValTx.StartTime(),
					0,
				)
				require.NoError(err)

				require.NoError(env.state.PutCurrentValidator(staker0))
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
				parentBlk, err := env.state.GetStatelessBlock(preferredID)
				require.NoError(err)
				statelessProposalBlock, err := block.NewBanffProposalBlock(
					newTime,
					parentBlk.ID(),
					parentBlk.Height()+1,
					s0RewardTx,
					[]*txs.Tx{},
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
					_, ok := env.config.Validators.GetValidator(constants.PrimaryNetworkID, stakerNodeID)
					require.False(ok)
				case current:
					_, err := env.state.GetCurrentValidator(constants.PrimaryNetworkID, stakerNodeID)
					require.NoError(err)
					_, ok := env.config.Validators.GetValidator(constants.PrimaryNetworkID, stakerNodeID)
					require.True(ok)
				}
			}

			for stakerNodeID, status := range test.expectedSubnetStakers {
				switch status {
				case pending:
					_, ok := env.config.Validators.GetValidator(subnetID, stakerNodeID)
					require.False(ok)
				case current:
					_, ok := env.config.Validators.GetValidator(subnetID, stakerNodeID)
					require.True(ok)
				}
			}
		})
	}
}

func TestBanffProposalBlockRemoveSubnetValidator(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, nil, upgradetest.Banff)

	subnetID := testSubnet1.ID()
	wallet := newWallet(t, env, walletConfig{
		subnetIDs: []ids.ID{subnetID},
	})

	env.config.TrackedSubnets.Add(subnetID)

	// Add a subnet validator to the staker set
	subnetValidatorNodeID := genesistest.DefaultNodeIDs[0]
	subnetVdr1EndTime := genesistest.DefaultValidatorStartTime.Add(defaultMinStakingDuration)
	tx, err := wallet.IssueAddSubnetValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: subnetValidatorNodeID,
				Start:  genesistest.DefaultValidatorStartTimeUnix,
				End:    uint64(subnetVdr1EndTime.Unix()),
				Wght:   1,
			},
			Subnet: subnetID,
		},
	)
	require.NoError(err)

	addSubnetValTx := tx.Unsigned.(*txs.AddSubnetValidatorTx)
	staker, err := state.NewCurrentStaker(
		tx.ID(),
		addSubnetValTx,
		addSubnetValTx.StartTime(),
		0,
	)
	require.NoError(err)

	require.NoError(env.state.PutCurrentValidator(staker))
	env.state.AddTx(tx, status.Committed)
	require.NoError(env.state.Commit())

	// The above validator is now part of the staking set

	// Queue a staker that joins the staker set after the above validator leaves
	subnetVdr2NodeID := genesistest.DefaultNodeIDs[1]
	tx, err = wallet.IssueAddSubnetValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: subnetVdr2NodeID,
				Start:  uint64(subnetVdr1EndTime.Add(time.Second).Unix()),
				End:    uint64(subnetVdr1EndTime.Add(time.Second).Add(defaultMinStakingDuration).Unix()),
				Wght:   1,
			},
			Subnet: subnetID,
		},
	)
	require.NoError(err)

	staker, err = state.NewPendingStaker(
		tx.ID(),
		tx.Unsigned.(*txs.AddSubnetValidatorTx),
	)
	require.NoError(err)

	require.NoError(env.state.PutPendingValidator(staker))
	env.state.AddTx(tx, status.Committed)
	require.NoError(env.state.Commit())

	// The above validator is now in the pending staker set

	// Advance time to the first staker's end time.
	env.clk.Set(subnetVdr1EndTime)

	// add Staker0 (with the right end time) to state
	// so to allow proposalBlk issuance
	staker0EndTime := subnetVdr1EndTime
	addStaker0, err := wallet.IssueAddValidatorTx(
		&txs.Validator{
			NodeID: ids.GenerateTestNodeID(),
			Start:  genesistest.DefaultValidatorStartTimeUnix,
			End:    uint64(staker0EndTime.Unix()),
			Wght:   10,
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		},
		reward.PercentDenominator,
		walletcommon.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.ShortEmpty},
		}),
	)
	require.NoError(err)

	// store Staker0 to state
	addValTx := addStaker0.Unsigned.(*txs.AddValidatorTx)
	staker, err = state.NewCurrentStaker(
		addStaker0.ID(),
		addValTx,
		addValTx.StartTime(),
		0,
	)
	require.NoError(err)

	require.NoError(env.state.PutCurrentValidator(staker))
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
	parentBlk, err := env.state.GetStatelessBlock(preferredID)
	require.NoError(err)
	statelessProposalBlock, err := block.NewBanffProposalBlock(
		subnetVdr1EndTime,
		parentBlk.ID(),
		parentBlk.Height()+1,
		s0RewardTx,
		[]*txs.Tx{},
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
	_, ok := env.config.Validators.GetValidator(subnetID, subnetVdr2NodeID)
	require.False(ok)
	_, ok = env.config.Validators.GetValidator(subnetID, subnetValidatorNodeID)
	require.False(ok)
}

func TestBanffProposalBlockTrackedSubnet(t *testing.T) {
	for _, tracked := range []bool{true, false} {
		t.Run(fmt.Sprintf("tracked %t", tracked), func(t *testing.T) {
			require := require.New(t)
			env := newEnvironment(t, nil, upgradetest.Banff)

			subnetID := testSubnet1.ID()
			if tracked {
				env.config.TrackedSubnets.Add(subnetID)
			}

			wallet := newWallet(t, env, walletConfig{
				subnetIDs: []ids.ID{subnetID},
			})

			// Add a subnet validator to the staker set
			subnetValidatorNodeID := genesistest.DefaultNodeIDs[0]
			subnetVdr1StartTime := genesistest.DefaultValidatorStartTime.Add(1 * time.Minute)
			subnetVdr1EndTime := genesistest.DefaultValidatorStartTime.Add(10 * defaultMinStakingDuration).Add(1 * time.Minute)

			tx, err := wallet.IssueAddSubnetValidatorTx(
				&txs.SubnetValidator{
					Validator: txs.Validator{
						NodeID: subnetValidatorNodeID,
						Start:  uint64(subnetVdr1StartTime.Unix()),
						End:    uint64(subnetVdr1EndTime.Unix()),
						Wght:   1,
					},
					Subnet: subnetID,
				},
			)
			require.NoError(err)

			staker, err := state.NewPendingStaker(
				tx.ID(),
				tx.Unsigned.(*txs.AddSubnetValidatorTx),
			)
			require.NoError(err)

			require.NoError(env.state.PutPendingValidator(staker))
			env.state.AddTx(tx, status.Committed)
			require.NoError(env.state.Commit())

			// Advance time to the staker's start time.
			env.clk.Set(subnetVdr1StartTime)

			// add Staker0 (with the right end time) to state
			// so to allow proposalBlk issuance
			staker0StartTime := genesistest.DefaultValidatorStartTime
			staker0EndTime := subnetVdr1StartTime

			addStaker0, err := wallet.IssueAddValidatorTx(
				&txs.Validator{
					NodeID: ids.GenerateTestNodeID(),
					Start:  uint64(staker0StartTime.Unix()),
					End:    uint64(staker0EndTime.Unix()),
					Wght:   10,
				},
				&secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
				},
				reward.PercentDenominator,
			)
			require.NoError(err)

			// store Staker0 to state
			addValTx := addStaker0.Unsigned.(*txs.AddValidatorTx)
			staker, err = state.NewCurrentStaker(
				addStaker0.ID(),
				addValTx,
				addValTx.StartTime(),
				0,
			)
			require.NoError(err)

			require.NoError(env.state.PutCurrentValidator(staker))
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
			parentBlk, err := env.state.GetStatelessBlock(preferredID)
			require.NoError(err)
			statelessProposalBlock, err := block.NewBanffProposalBlock(
				subnetVdr1StartTime,
				parentBlk.ID(),
				parentBlk.Height()+1,
				s0RewardTx,
				[]*txs.Tx{},
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
			_, ok := env.config.Validators.GetValidator(subnetID, subnetValidatorNodeID)
			require.True(ok)
		})
	}
}

func TestBanffProposalBlockDelegatorStakerWeight(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, nil, upgradetest.Banff)

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := genesistest.DefaultValidatorStartTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(defaultMaxStakingDuration)
	nodeID := ids.GenerateTestNodeID()
	rewardAddress := ids.GenerateTestShortID()
	addPendingValidator(
		t,
		env,
		pendingValidatorStartTime,
		pendingValidatorEndTime,
		nodeID,
		rewardAddress,
		[]*secp256k1.PrivateKey{genesistest.DefaultFundedKeys[0]},
	)

	wallet := newWallet(t, env, walletConfig{})

	rewardsOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}

	// add Staker0 (with the right end time) to state
	// just to allow proposalBlk issuance (with a reward Tx)
	staker0StartTime := genesistest.DefaultValidatorStartTime
	staker0EndTime := pendingValidatorStartTime
	addStaker0, err := wallet.IssueAddValidatorTx(
		&txs.Validator{
			NodeID: ids.GenerateTestNodeID(),
			Start:  uint64(staker0StartTime.Unix()),
			End:    uint64(staker0EndTime.Unix()),
			Wght:   10,
		},
		rewardsOwner,
		reward.PercentDenominator,
	)
	require.NoError(err)

	// store Staker0 to state
	addValTx := addStaker0.Unsigned.(*txs.AddValidatorTx)
	staker, err := state.NewCurrentStaker(
		addStaker0.ID(),
		addValTx,
		addValTx.StartTime(),
		0,
	)
	require.NoError(err)

	require.NoError(env.state.PutCurrentValidator(staker))
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
	parentBlk, err := env.state.GetStatelessBlock(preferredID)
	require.NoError(err)
	statelessProposalBlock, err := block.NewBanffProposalBlock(
		pendingValidatorStartTime,
		parentBlk.ID(),
		parentBlk.Height()+1,
		s0RewardTx,
		[]*txs.Tx{},
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
	vdrWeight := env.config.Validators.GetWeight(constants.PrimaryNetworkID, nodeID)
	require.Equal(env.config.MinValidatorStake, vdrWeight)

	// Add delegator
	pendingDelegatorStartTime := pendingValidatorStartTime.Add(1 * time.Second)
	pendingDelegatorEndTime := pendingDelegatorStartTime.Add(1 * time.Second)
	addDelegatorTx, err := wallet.IssueAddDelegatorTx(
		&txs.Validator{
			NodeID: nodeID,
			Start:  uint64(pendingDelegatorStartTime.Unix()),
			End:    uint64(pendingDelegatorEndTime.Unix()),
			Wght:   env.config.MinDelegatorStake,
		},
		rewardsOwner,
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
	addStaker0, err = wallet.IssueAddValidatorTx(
		&txs.Validator{
			NodeID: ids.GenerateTestNodeID(),
			Start:  uint64(staker0StartTime.Unix()),
			End:    uint64(staker0EndTime.Unix()),
			Wght:   10,
		},
		rewardsOwner,
		reward.PercentDenominator,
	)
	require.NoError(err)

	// store Staker0 to state
	addValTx = addStaker0.Unsigned.(*txs.AddValidatorTx)
	staker, err = state.NewCurrentStaker(
		addStaker0.ID(),
		addValTx,
		addValTx.StartTime(),
		0,
	)
	require.NoError(err)

	require.NoError(env.state.PutCurrentValidator(staker))
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
	parentBlk, err = env.state.GetStatelessBlock(preferredID)
	require.NoError(err)
	statelessProposalBlock, err = block.NewBanffProposalBlock(
		pendingDelegatorStartTime,
		parentBlk.ID(),
		parentBlk.Height()+1,
		s0RewardTx,
		[]*txs.Tx{},
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
	vdrWeight = env.config.Validators.GetWeight(constants.PrimaryNetworkID, nodeID)
	require.Equal(env.config.MinDelegatorStake+env.config.MinValidatorStake, vdrWeight)
}

func TestBanffProposalBlockDelegatorStakers(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, nil, upgradetest.Banff)

	// Case: Timestamp is after next validator start time
	// Add a pending validator
	pendingValidatorStartTime := genesistest.DefaultValidatorStartTime.Add(1 * time.Second)
	pendingValidatorEndTime := pendingValidatorStartTime.Add(defaultMinStakingDuration)
	nodeIDKey, _ := secp256k1.NewPrivateKey()
	rewardAddress := nodeIDKey.Address()
	nodeID := ids.BuildTestNodeID(rewardAddress[:])

	addPendingValidator(
		t,
		env,
		pendingValidatorStartTime,
		pendingValidatorEndTime,
		nodeID,
		rewardAddress,
		[]*secp256k1.PrivateKey{genesistest.DefaultFundedKeys[0]},
	)

	wallet := newWallet(t, env, walletConfig{})

	rewardsOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}

	// add Staker0 (with the right end time) to state
	// so to allow proposalBlk issuance
	staker0StartTime := genesistest.DefaultValidatorStartTime
	staker0EndTime := pendingValidatorStartTime
	addStaker0, err := wallet.IssueAddValidatorTx(
		&txs.Validator{
			NodeID: ids.GenerateTestNodeID(),
			Start:  uint64(staker0StartTime.Unix()),
			End:    uint64(staker0EndTime.Unix()),
			Wght:   10,
		},
		rewardsOwner,
		reward.PercentDenominator,
	)
	require.NoError(err)

	// store Staker0 to state
	addValTx := addStaker0.Unsigned.(*txs.AddValidatorTx)
	staker, err := state.NewCurrentStaker(
		addStaker0.ID(),
		addValTx,
		addValTx.StartTime(),
		0,
	)
	require.NoError(err)

	require.NoError(env.state.PutCurrentValidator(staker))
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
	parentBlk, err := env.state.GetStatelessBlock(preferredID)
	require.NoError(err)
	statelessProposalBlock, err := block.NewBanffProposalBlock(
		pendingValidatorStartTime,
		parentBlk.ID(),
		parentBlk.Height()+1,
		s0RewardTx,
		[]*txs.Tx{},
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
	vdrWeight := env.config.Validators.GetWeight(constants.PrimaryNetworkID, nodeID)
	require.Equal(env.config.MinValidatorStake, vdrWeight)

	// Add delegator
	pendingDelegatorStartTime := pendingValidatorStartTime.Add(1 * time.Second)
	pendingDelegatorEndTime := pendingDelegatorStartTime.Add(defaultMinStakingDuration)
	addDelegatorTx, err := wallet.IssueAddDelegatorTx(
		&txs.Validator{
			NodeID: nodeID,
			Start:  uint64(pendingDelegatorStartTime.Unix()),
			End:    uint64(pendingDelegatorEndTime.Unix()),
			Wght:   env.config.MinDelegatorStake,
		},
		rewardsOwner,
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
	addStaker0, err = wallet.IssueAddValidatorTx(
		&txs.Validator{
			NodeID: ids.GenerateTestNodeID(),
			Start:  uint64(staker0StartTime.Unix()),
			End:    uint64(staker0EndTime.Unix()),
			Wght:   10,
		},
		rewardsOwner,
		reward.PercentDenominator,
	)
	require.NoError(err)

	// store Staker0 to state
	addValTx = addStaker0.Unsigned.(*txs.AddValidatorTx)
	staker, err = state.NewCurrentStaker(
		addStaker0.ID(),
		addValTx,
		addValTx.StartTime(),
		0,
	)
	require.NoError(err)

	require.NoError(env.state.PutCurrentValidator(staker))
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
	parentBlk, err = env.state.GetStatelessBlock(preferredID)
	require.NoError(err)
	statelessProposalBlock, err = block.NewBanffProposalBlock(
		pendingDelegatorStartTime,
		parentBlk.ID(),
		parentBlk.Height()+1,
		s0RewardTx,
		[]*txs.Tx{},
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
	vdrWeight = env.config.Validators.GetWeight(constants.PrimaryNetworkID, nodeID)
	require.Equal(env.config.MinDelegatorStake+env.config.MinValidatorStake, vdrWeight)
}

func TestAddValidatorProposalBlock(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, nil, upgradetest.Durango)

	wallet := newWallet(t, env, walletConfig{})

	now := env.clk.Time()

	// Create validator tx
	var (
		validatorStartTime = now.Add(2 * executor.SyncBound)
		validatorEndTime   = validatorStartTime.Add(env.config.MinStakeDuration)
		nodeID             = ids.GenerateTestNodeID()
	)

	sk, err := localsigner.New()
	require.NoError(err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(err)

	rewardsOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}

	addValidatorTx, err := wallet.IssueAddPermissionlessValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(validatorStartTime.Unix()),
				End:    uint64(validatorEndTime.Unix()),
				Wght:   env.config.MinValidatorStake,
			},
			Subnet: constants.PrimaryNetworkID,
		},
		pop,
		env.ctx.AVAXAssetID,
		rewardsOwner,
		rewardsOwner,
		10000,
	)
	require.NoError(err)

	// Add validator through a [StandardBlock]
	preferredID := env.blkManager.Preferred()
	preferred, err := env.blkManager.GetStatelessBlock(preferredID)
	require.NoError(err)

	statelessBlk, err := block.NewBanffStandardBlock(
		now.Add(executor.SyncBound),
		preferredID,
		preferred.Height()+1,
		[]*txs.Tx{addValidatorTx},
	)
	require.NoError(err)
	blk := env.blkManager.NewBlock(statelessBlk)
	require.NoError(blk.Verify(context.Background()))
	require.NoError(blk.Accept(context.Background()))
	env.blkManager.SetPreference(statelessBlk.ID())

	// Should be current
	staker, err := env.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)
	require.NotNil(staker)

	// Advance time until next staker change time is [validatorEndTime]
	for {
		nextStakerChangeTime, err := state.GetNextStakerChangeTime(
			env.config.ValidatorFeeConfig,
			env.state,
			mockable.MaxTime,
		)
		require.NoError(err)
		if nextStakerChangeTime.Equal(validatorEndTime) {
			break
		}

		preferredID = env.blkManager.Preferred()
		preferred, err = env.blkManager.GetStatelessBlock(preferredID)
		require.NoError(err)

		statelessBlk, err = block.NewBanffStandardBlock(
			nextStakerChangeTime,
			preferredID,
			preferred.Height()+1,
			nil,
		)
		require.NoError(err)
		blk = env.blkManager.NewBlock(statelessBlk)
		require.NoError(blk.Verify(context.Background()))
		require.NoError(blk.Accept(context.Background()))
		env.blkManager.SetPreference(statelessBlk.ID())
	}

	env.clk.Set(validatorEndTime)
	now = env.clk.Time()

	// Create another validator tx
	validatorStartTime = now.Add(2 * executor.SyncBound)
	validatorEndTime = validatorStartTime.Add(env.config.MinStakeDuration)
	nodeID = ids.GenerateTestNodeID()

	sk, err = localsigner.New()
	require.NoError(err)
	pop, err = signer.NewProofOfPossession(sk)
	require.NoError(err)

	addValidatorTx2, err := wallet.IssueAddPermissionlessValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(validatorStartTime.Unix()),
				End:    uint64(validatorEndTime.Unix()),
				Wght:   env.config.MinValidatorStake,
			},
			Subnet: constants.PrimaryNetworkID,
		},
		pop,
		env.ctx.AVAXAssetID,
		rewardsOwner,
		rewardsOwner,
		10000,
	)
	require.NoError(err)

	// Add validator through a [ProposalBlock] and reward the last one
	preferredID = env.blkManager.Preferred()
	preferred, err = env.blkManager.GetStatelessBlock(preferredID)
	require.NoError(err)

	rewardValidatorTx, err := newRewardValidatorTx(t, addValidatorTx.ID())
	require.NoError(err)

	statelessProposalBlk, err := block.NewBanffProposalBlock(
		now,
		preferredID,
		preferred.Height()+1,
		rewardValidatorTx,
		[]*txs.Tx{addValidatorTx2},
	)
	require.NoError(err)
	blk = env.blkManager.NewBlock(statelessProposalBlk)
	require.NoError(blk.Verify(context.Background()))

	options, err := blk.(snowman.OracleBlock).Options(context.Background())
	require.NoError(err)
	commitBlk := options[0]
	require.NoError(commitBlk.Verify(context.Background()))

	require.NoError(blk.Accept(context.Background()))
	require.NoError(commitBlk.Accept(context.Background()))

	// Should be current
	staker, err = env.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)
	require.NotNil(staker)

	rewardUTXOs, err := env.state.GetRewardUTXOs(addValidatorTx.ID())
	require.NoError(err)
	require.NotEmpty(rewardUTXOs)
}

func newRewardValidatorTx(t testing.TB, txID ids.ID) (*txs.Tx, error) {
	utx := &txs.RewardValidatorTx{TxID: txID}
	tx, err := txs.NewSigned(utx, txs.Codec, nil)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(snowtest.Context(t, snowtest.PChainID))
}
