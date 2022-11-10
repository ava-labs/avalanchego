// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	txexecutor "github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

func TestApricotStandardBlockTimeVerification(t *testing.T) {
	require := require.New(t)
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
	env.mockedState.EXPECT().GetLastAccepted().Return(parentID).AnyTimes()
	env.mockedState.EXPECT().GetTimestamp().Return(chainTime).AnyTimes()
	onParentAccept.EXPECT().GetTimestamp().Return(chainTime).AnyTimes()

	// wrong height
	apricotChildBlk, err := blocks.NewApricotStandardBlock(
		apricotParentBlk.ID(),
		apricotParentBlk.Height(),
		nil, // txs nulled to simplify test
	)
	require.NoError(err)
	block := env.blkManager.NewBlock(apricotChildBlk)
	require.Error(block.Verify(context.Background()))

	// valid height
	apricotChildBlk, err = blocks.NewApricotStandardBlock(
		apricotParentBlk.ID(),
		apricotParentBlk.Height()+1,
		nil, // txs nulled to simplify test
	)
	require.NoError(err)
	block = env.blkManager.NewBlock(apricotChildBlk)
	require.NoError(block.Verify(context.Background()))
}

func TestBanffStandardBlockTimeVerification(t *testing.T) {
	require := require.New(t)
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
	env.config.BanffTime = time.Time{} // activate Banff

	// setup and store parent block
	// it's a standard block for simplicity
	parentTime := now
	parentHeight := uint64(2022)

	banffParentBlk, err := blocks.NewBanffStandardBlock(
		parentTime,
		ids.Empty, // does not matter
		parentHeight,
		nil, // txs do not matter in this test
	)
	require.NoError(err)
	parentID := banffParentBlk.ID()

	// store parent block, with relevant quantities
	onParentAccept := state.NewMockDiff(ctrl)
	chainTime := env.clk.Time().Truncate(time.Second)
	env.blkManager.(*manager).blkIDToState[parentID] = &blockState{
		statelessBlock: banffParentBlk,
		onAcceptState:  onParentAccept,
		timestamp:      chainTime,
	}
	env.blkManager.(*manager).lastAccepted = parentID
	env.mockedState.EXPECT().GetLastAccepted().Return(parentID).AnyTimes()
	env.mockedState.EXPECT().GetTimestamp().Return(chainTime).AnyTimes()

	nextStakerTime := chainTime.Add(txexecutor.SyncBound).Add(-1 * time.Second)

	// store just once current staker to mark next staker time.
	currentStakerIt := state.NewMockStakerIterator(ctrl)
	currentStakerIt.EXPECT().Next().Return(true).AnyTimes()
	currentStakerIt.EXPECT().Value().Return(
		&state.Staker{
			NextTime: nextStakerTime,
			Priority: txs.PrimaryNetworkValidatorCurrentPriority,
		},
	).AnyTimes()
	currentStakerIt.EXPECT().Release().Return().AnyTimes()
	onParentAccept.EXPECT().GetCurrentStakerIterator().Return(currentStakerIt, nil).AnyTimes()

	// no pending stakers
	pendingIt := state.NewMockStakerIterator(ctrl)
	pendingIt.EXPECT().Next().Return(false).AnyTimes()
	pendingIt.EXPECT().Release().Return().AnyTimes()
	onParentAccept.EXPECT().GetPendingStakerIterator().Return(pendingIt, nil).AnyTimes()

	onParentAccept.EXPECT().GetTimestamp().Return(chainTime).AnyTimes()

	txID := ids.GenerateTestID()
	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID: txID,
		},
		Asset: avax.Asset{
			ID: avaxAssetID,
		},
		Out: &secp256k1fx.TransferOutput{
			Amt: env.config.CreateSubnetTxFee,
		},
	}
	utxoID := utxo.InputID()
	onParentAccept.EXPECT().GetUTXO(utxoID).Return(utxo, nil).AnyTimes()

	// Create the tx
	utx := &txs.CreateSubnetTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    env.ctx.NetworkID,
			BlockchainID: env.ctx.ChainID,
			Ins: []*avax.TransferableInput{{
				UTXOID: utxo.UTXOID,
				Asset:  utxo.Asset,
				In: &secp256k1fx.TransferInput{
					Amt: env.config.CreateSubnetTxFee,
				},
			}},
		}},
		Owner: &secp256k1fx.OutputOwners{},
	}
	tx := &txs.Tx{Unsigned: utx}
	require.NoError(tx.Sign(txs.Codec, [][]*crypto.PrivateKeySECP256K1R{{}}))

	{
		// wrong version
		banffChildBlk, err := blocks.NewApricotStandardBlock(
			banffParentBlk.ID(),
			banffParentBlk.Height()+1,
			[]*txs.Tx{tx},
		)
		require.NoError(err)
		block := env.blkManager.NewBlock(banffChildBlk)
		require.Error(block.Verify(context.Background()))
	}

	{
		// wrong height
		childTimestamp := parentTime.Add(time.Second)
		banffChildBlk, err := blocks.NewBanffStandardBlock(
			childTimestamp,
			banffParentBlk.ID(),
			banffParentBlk.Height(),
			[]*txs.Tx{tx},
		)
		require.NoError(err)
		block := env.blkManager.NewBlock(banffChildBlk)
		require.Error(block.Verify(context.Background()))
	}

	{
		// wrong timestamp, earlier than parent
		childTimestamp := parentTime.Add(-1 * time.Second)
		banffChildBlk, err := blocks.NewBanffStandardBlock(
			childTimestamp,
			banffParentBlk.ID(),
			banffParentBlk.Height()+1,
			[]*txs.Tx{tx},
		)
		require.NoError(err)
		block := env.blkManager.NewBlock(banffChildBlk)
		require.Error(block.Verify(context.Background()))
	}

	{
		// wrong timestamp, violated synchrony bound
		childTimestamp := parentTime.Add(txexecutor.SyncBound).Add(time.Second)
		banffChildBlk, err := blocks.NewBanffStandardBlock(
			childTimestamp,
			banffParentBlk.ID(),
			banffParentBlk.Height()+1,
			[]*txs.Tx{tx},
		)
		require.NoError(err)
		block := env.blkManager.NewBlock(banffChildBlk)
		require.Error(block.Verify(context.Background()))
	}

	{
		// wrong timestamp, skipped staker set change event
		childTimestamp := nextStakerTime.Add(time.Second)
		banffChildBlk, err := blocks.NewBanffStandardBlock(
			childTimestamp,
			banffParentBlk.ID(),
			banffParentBlk.Height()+1,
			[]*txs.Tx{tx},
		)
		require.NoError(err)
		block := env.blkManager.NewBlock(banffChildBlk)
		require.Error(block.Verify(context.Background()))
	}

	{
		// no state changes
		childTimestamp := parentTime
		banffChildBlk, err := blocks.NewBanffStandardBlock(
			childTimestamp,
			banffParentBlk.ID(),
			banffParentBlk.Height()+1,
			nil,
		)
		require.NoError(err)
		block := env.blkManager.NewBlock(banffChildBlk)
		require.ErrorIs(block.Verify(context.Background()), errBanffStandardBlockWithoutChanges)
	}

	{
		// valid block, same timestamp as parent block
		childTimestamp := parentTime
		banffChildBlk, err := blocks.NewBanffStandardBlock(
			childTimestamp,
			banffParentBlk.ID(),
			banffParentBlk.Height()+1,
			[]*txs.Tx{tx},
		)
		require.NoError(err)
		block := env.blkManager.NewBlock(banffChildBlk)
		require.NoError(block.Verify(context.Background()))
	}

	{
		// valid
		childTimestamp := nextStakerTime
		banffChildBlk, err := blocks.NewBanffStandardBlock(
			childTimestamp,
			banffParentBlk.ID(),
			banffParentBlk.Height()+1,
			[]*txs.Tx{tx},
		)
		require.NoError(err)
		block := env.blkManager.NewBlock(banffChildBlk)
		require.NoError(block.Verify(context.Background()))
	}
}

func TestBanffStandardBlockUpdatePrimaryNetworkStakers(t *testing.T) {
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
	require.NoError(err)

	// build standard block moving ahead chain time
	preferredID := env.state.GetLastAccepted()
	parentBlk, _, err := env.state.GetStatelessBlock(preferredID)
	require.NoError(err)
	statelessStandardBlock, err := blocks.NewBanffStandardBlock(
		pendingValidatorStartTime,
		parentBlk.ID(),
		parentBlk.Height()+1,
		nil, // txs nulled to simplify test
	)
	require.NoError(err)
	block := env.blkManager.NewBlock(statelessStandardBlock)

	// update staker set
	require.NoError(block.Verify(context.Background()))

	// tests
	blkStateMap := env.blkManager.(*manager).blkIDToState
	updatedState := blkStateMap[block.ID()].onAcceptState
	currentValidator, err := updatedState.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)
	require.True(currentValidator.TxID == addPendingValidatorTx.ID(), "Added the wrong tx to the validator set")
	require.EqualValues(1370, currentValidator.PotentialReward) // See rewards tests to explain why 1370

	_, err = updatedState.GetPendingValidator(constants.PrimaryNetworkID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	// Test VM validators
	require.NoError(block.Accept(context.Background()))
	require.True(env.config.Validators.Contains(constants.PrimaryNetworkID, nodeID))
}

// Ensure semantic verification updates the current and pending staker sets correctly.
// Namely, it should add pending stakers whose start time is at or before the timestamp.
// It will not remove primary network stakers; that happens in rewardTxs.
func TestBanffStandardBlockUpdateStakers(t *testing.T) {
	// Chronological order (not in scale):
	// Staker1:    |----------------------------------------------------------|
	// Staker2:        |------------------------|
	// Staker3:            |------------------------|
	// Staker3sub:             |----------------|
	// Staker4:            |------------------------|
	// Staker5:                                 |--------------------|
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
			require := require.New(ts)
			env := newEnvironment(t, nil)
			defer func() {
				if err := shutdownEnvironment(env); err != nil {
					t.Fatal(err)
				}
			}()
			env.config.BanffTime = time.Time{} // activate Banff
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
				require.NoError(err)
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
				require.NoError(err)

				staker, err := state.NewPendingStaker(
					tx.ID(),
					tx.Unsigned.(*txs.AddSubnetValidatorTx),
				)
				require.NoError(err)

				env.state.PutPendingValidator(staker)
				env.state.AddTx(tx, status.Committed)
			}
			env.state.SetHeight( /*dummyHeight*/ 1)
			require.NoError(env.state.Commit())

			for _, newTime := range test.advanceTimeTo {
				env.clk.Set(newTime)

				// build standard block moving ahead chain time
				preferredID := env.state.GetLastAccepted()
				parentBlk, _, err := env.state.GetStatelessBlock(preferredID)
				require.NoError(err)
				statelessStandardBlock, err := blocks.NewBanffStandardBlock(
					newTime,
					parentBlk.ID(),
					parentBlk.Height()+1,
					nil, // txs nulled to simplify test
				)
				block := env.blkManager.NewBlock(statelessStandardBlock)

				require.NoError(err)

				// update staker set
				require.NoError(block.Verify(context.Background()))
				require.NoError(block.Accept(context.Background()))
			}

			for stakerNodeID, status := range test.expectedStakers {
				switch status {
				case pending:
					_, err := env.state.GetPendingValidator(constants.PrimaryNetworkID, stakerNodeID)
					require.NoError(err)
					require.False(env.config.Validators.Contains(constants.PrimaryNetworkID, stakerNodeID))
				case current:
					_, err := env.state.GetCurrentValidator(constants.PrimaryNetworkID, stakerNodeID)
					require.NoError(err)
					require.True(env.config.Validators.Contains(constants.PrimaryNetworkID, stakerNodeID))
				}
			}

			for stakerNodeID, status := range test.expectedSubnetStakers {
				switch status {
				case pending:
					require.False(env.config.Validators.Contains(testSubnet1.ID(), stakerNodeID))
				case current:
					require.True(env.config.Validators.Contains(testSubnet1.ID(), stakerNodeID))
				}
			}
		})
	}
}

// Regression test for https://github.com/ava-labs/avalanchego/pull/584
// that ensures it fixes a bug where subnet validators are not removed
// when timestamp is advanced and there is a pending staker whose start time
// is after the new timestamp
func TestBanffStandardBlockRemoveSubnetValidator(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, nil)
	defer func() {
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()
	env.config.BanffTime = time.Time{} // activate Banff
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
		testSubnet1.ID(), // Subnet ID
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0], preFundedKeys[1]},
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
	// build standard block moving ahead chain time
	preferredID := env.state.GetLastAccepted()
	parentBlk, _, err := env.state.GetStatelessBlock(preferredID)
	require.NoError(err)
	statelessStandardBlock, err := blocks.NewBanffStandardBlock(
		subnetVdr1EndTime,
		parentBlk.ID(),
		parentBlk.Height()+1,
		nil, // txs nulled to simplify test
	)
	require.NoError(err)
	block := env.blkManager.NewBlock(statelessStandardBlock)

	// update staker set
	require.NoError(block.Verify(context.Background()))

	blkStateMap := env.blkManager.(*manager).blkIDToState
	updatedState := blkStateMap[block.ID()].onAcceptState
	_, err = updatedState.GetCurrentValidator(testSubnet1.ID(), subnetValidatorNodeID)
	require.ErrorIs(err, database.ErrNotFound)

	// Check VM Validators are removed successfully
	require.NoError(block.Accept(context.Background()))
	require.False(env.config.Validators.Contains(testSubnet1.ID(), subnetVdr2NodeID))
	require.False(env.config.Validators.Contains(testSubnet1.ID(), subnetValidatorNodeID))
}

func TestBanffStandardBlockWhitelistedSubnet(t *testing.T) {
	require := require.New(t)

	for _, whitelist := range []bool{true, false} {
		t.Run(fmt.Sprintf("whitelisted %t", whitelist), func(ts *testing.T) {
			env := newEnvironment(t, nil)
			defer func() {
				if err := shutdownEnvironment(env); err != nil {
					t.Fatal(err)
				}
			}()
			env.config.BanffTime = time.Time{} // activate Banff
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

			// build standard block moving ahead chain time
			preferredID := env.state.GetLastAccepted()
			parentBlk, _, err := env.state.GetStatelessBlock(preferredID)
			require.NoError(err)
			statelessStandardBlock, err := blocks.NewBanffStandardBlock(
				subnetVdr1StartTime,
				parentBlk.ID(),
				parentBlk.Height()+1,
				nil, // txs nulled to simplify test
			)
			require.NoError(err)
			block := env.blkManager.NewBlock(statelessStandardBlock)

			// update staker set
			require.NoError(block.Verify(context.Background()))
			require.NoError(block.Accept(context.Background()))
			require.Equal(whitelist, env.config.Validators.Contains(testSubnet1.ID(), subnetValidatorNodeID))
		})
	}
}

func TestBanffStandardBlockDelegatorStakerWeight(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, nil)
	defer func() {
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
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
		[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0]},
	)
	require.NoError(err)

	// build standard block moving ahead chain time
	preferredID := env.state.GetLastAccepted()
	parentBlk, _, err := env.state.GetStatelessBlock(preferredID)
	require.NoError(err)
	statelessStandardBlock, err := blocks.NewBanffStandardBlock(
		pendingValidatorStartTime,
		parentBlk.ID(),
		parentBlk.Height()+1,
		nil, // txs nulled to simplify test
	)
	require.NoError(err)
	block := env.blkManager.NewBlock(statelessStandardBlock)
	require.NoError(block.Verify(context.Background()))
	require.NoError(block.Accept(context.Background()))
	require.NoError(env.state.Commit())

	// Test validator weight before delegation
	primarySet, ok := env.config.Validators.GetValidators(constants.PrimaryNetworkID)
	require.True(ok)
	vdrWeight, _ := primarySet.GetWeight(nodeID)
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
		[]*crypto.PrivateKeySECP256K1R{
			preFundedKeys[0],
			preFundedKeys[1],
			preFundedKeys[4],
		},
		ids.ShortEmpty,
	)
	require.NoError(err)

	staker, err := state.NewPendingStaker(
		addDelegatorTx.ID(),
		addDelegatorTx.Unsigned.(*txs.AddDelegatorTx),
	)
	require.NoError(err)

	env.state.PutPendingDelegator(staker)
	env.state.AddTx(addDelegatorTx, status.Committed)
	env.state.SetHeight( /*dummyHeight*/ uint64(1))
	require.NoError(env.state.Commit())

	// Advance Time
	preferredID = env.state.GetLastAccepted()
	parentBlk, _, err = env.state.GetStatelessBlock(preferredID)
	require.NoError(err)
	statelessStandardBlock, err = blocks.NewBanffStandardBlock(
		pendingDelegatorStartTime,
		parentBlk.ID(),
		parentBlk.Height()+1,
		nil, // txs nulled to simplify test
	)
	require.NoError(err)
	block = env.blkManager.NewBlock(statelessStandardBlock)
	require.NoError(block.Verify(context.Background()))
	require.NoError(block.Accept(context.Background()))
	require.NoError(env.state.Commit())

	// Test validator weight after delegation
	vdrWeight, _ = primarySet.GetWeight(nodeID)
	require.Equal(env.config.MinDelegatorStake+env.config.MinValidatorStake, vdrWeight)
}
