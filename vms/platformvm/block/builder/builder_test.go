// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/statetest"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	blockexecutor "github.com/ava-labs/avalanchego/vms/platformvm/block/executor"
	txexecutor "github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

func TestBuildBlockBasic(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t, upgradetest.Latest)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	subnetID := testSubnet1.ID()
	wallet := newWallet(t, env, walletConfig{
		subnetIDs: []ids.ID{subnetID},
	})

	// Create a valid transaction
	tx, err := wallet.IssueCreateChainTx(
		subnetID,
		nil,
		constants.AVMID,
		nil,
		"chain name",
	)
	require.NoError(err)

	// Issue the transaction
	env.ctx.Lock.Unlock()
	require.NoError(env.network.IssueTxFromRPC(tx))
	env.ctx.Lock.Lock()

	txID := tx.ID()
	_, ok := env.mempool.Get(txID)
	require.True(ok)

	// [BuildBlock] should build a block with the transaction
	blkIntf, err := env.Builder.BuildBlock(t.Context())
	require.NoError(err)

	require.IsType(&blockexecutor.Block{}, blkIntf)
	blk := blkIntf.(*blockexecutor.Block)
	require.Len(blk.Txs(), 1)
	require.Equal(txID, blk.Txs()[0].ID())

	// Mempool should not contain the transaction or have marked it as dropped
	_, ok = env.mempool.Get(txID)
	require.False(ok)
	require.NoError(env.mempool.GetDropReason(txID))
}

func TestBuildBlockDoesNotBuildWithEmptyMempool(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t, upgradetest.Latest)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	tx, exists := env.mempool.Peek()
	require.False(exists)
	require.Nil(tx)

	// [BuildBlock] should not build an empty block
	blk, err := env.Builder.BuildBlock(t.Context())
	require.ErrorIs(err, ErrNoPendingBlocks)
	require.Nil(blk)
}

func TestBuildBlockShouldReward(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t, upgradetest.Latest)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	wallet := newWallet(t, env, walletConfig{})

	var (
		now    = env.backend.Clk.Time()
		nodeID = ids.GenerateTestNodeID()

		defaultValidatorStake = 100 * units.MilliAvax
		validatorStartTime    = now.Add(2 * txexecutor.SyncBound)
		validatorEndTime      = validatorStartTime.Add(360 * 24 * time.Hour)
	)

	sk, err := localsigner.New()
	require.NoError(err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(err)

	rewardOwners := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}

	// Create a valid [AddPermissionlessValidatorTx]
	tx, err := wallet.IssueAddPermissionlessValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(validatorStartTime.Unix()),
				End:    uint64(validatorEndTime.Unix()),
				Wght:   defaultValidatorStake,
			},
			Subnet: constants.PrimaryNetworkID,
		},
		pop,
		env.ctx.AVAXAssetID,
		rewardOwners,
		rewardOwners,
		reward.PercentDenominator,
	)
	require.NoError(err)

	// Issue the transaction
	env.ctx.Lock.Unlock()
	require.NoError(env.network.IssueTxFromRPC(tx))
	env.ctx.Lock.Lock()

	txID := tx.ID()
	_, ok := env.mempool.Get(txID)
	require.True(ok)

	// Build and accept a block with the tx
	blk, err := env.Builder.BuildBlock(t.Context())
	require.NoError(err)
	require.IsType(&block.BanffStandardBlock{}, blk.(*blockexecutor.Block).Block)
	require.Equal([]*txs.Tx{tx}, blk.(*blockexecutor.Block).Block.Txs())
	require.NoError(blk.Verify(t.Context()))
	require.NoError(blk.Accept(t.Context()))
	env.blkManager.SetPreference(blk.ID(), nil)

	// Validator should now be current
	staker, err := env.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)
	require.Equal(txID, staker.TxID)

	// Should be rewarded at the end of staking period
	env.backend.Clk.Set(validatorEndTime)

	for {
		iter, err := env.state.GetCurrentStakerIterator()
		require.NoError(err)
		require.True(iter.Next())
		staker := iter.Value()
		iter.Release()

		// Check that the right block was built
		blk, err := env.Builder.BuildBlock(t.Context())
		require.NoError(err)
		require.NoError(blk.Verify(t.Context()))
		require.IsType(&block.BanffProposalBlock{}, blk.(*blockexecutor.Block).Block)

		expectedTx, err := NewRewardValidatorTx(env.ctx, staker.TxID)
		require.NoError(err)
		require.Equal([]*txs.Tx{expectedTx}, blk.(*blockexecutor.Block).Block.Txs())

		// Commit the [ProposalBlock] with a [CommitBlock]
		proposalBlk, ok := blk.(snowman.OracleBlock)
		require.True(ok)
		options, err := proposalBlk.Options(t.Context())
		require.NoError(err)

		commit := options[0].(*blockexecutor.Block)
		require.IsType(&block.BanffCommitBlock{}, commit.Block)

		require.NoError(blk.Accept(t.Context()))
		require.NoError(commit.Verify(t.Context()))
		require.NoError(commit.Accept(t.Context()))
		env.blkManager.SetPreference(commit.ID(), nil)

		// Stop rewarding once our staker is rewarded
		if staker.TxID == txID {
			break
		}
	}

	// Staking rewards should have been issued
	rewardUTXOs, err := env.state.GetRewardUTXOs(txID)
	require.NoError(err)
	require.NotEmpty(rewardUTXOs)
}

func TestBuildBlockShouldRewardAutoRenewedValidator(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t, upgradetest.Latest)

	// Remove genesis validators so our auto-renewed validator is the only staker
	currentStakerIterator, err := env.state.GetCurrentStakerIterator()
	require.NoError(err)
	for _, staker := range iterator.ToSlice(currentStakerIterator) {
		require.NoError(env.state.DeleteCurrentValidator(staker))
	}

	var (
		nodeID      = ids.GenerateTestNodeID()
		stakePeriod = 24 * time.Hour
		startTime   = genesistest.DefaultValidatorStartTime
		endTime     = startTime.Add(stakePeriod)
	)

	sk, err := localsigner.New()
	require.NoError(err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(err)

	// Build the AddAutoRenewedValidatorTx directly
	addTx, err := txs.NewSigned(&txs.AddAutoRenewedValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    env.ctx.NetworkID,
			BlockchainID: env.ctx.ChainID,
		}},
		ValidatorNodeID:          nodeID,
		Signer:                   pop,
		StakeOuts:                []*avax.TransferableOutput{},
		ValidatorRewardsOwner:    &secp256k1fx.OutputOwners{},
		DelegatorRewardsOwner:    &secp256k1fx.OutputOwners{},
		Owner:                    &secp256k1fx.OutputOwners{},
		DelegationShares:         reward.PercentDenominator,
		Wght:                     env.config.MinValidatorStake,
		AutoCompoundRewardShares: reward.PercentDenominator,
		Period:                   uint64(stakePeriod / time.Second),
	}, txs.Codec, nil)
	require.NoError(err)

	txID := addTx.ID()
	validatorTx := addTx.Unsigned.(*txs.AddAutoRenewedValidatorTx)

	// Add the tx and staker directly to state
	env.state.AddTx(addTx, status.Committed)

	staker, err := state.NewStaker(txID, validatorTx, startTime, endTime, validatorTx.Weight(), 0)
	require.NoError(err)

	require.NoError(env.state.PutCurrentValidator(staker))
	require.NoError(env.state.SetStakingInfo(staker.SubnetID, staker.NodeID, state.StakingInfo{Period: stakePeriod}))
	require.NoError(env.state.Commit())

	// Advance time to the validator's end time so it should be rewarded
	env.state.SetTimestamp(endTime)
	env.backend.Clk.Set(endTime)

	// Build the block
	blk, err := env.Builder.BuildBlock(t.Context())
	require.NoError(err)

	proposalBlk := blk.(*blockexecutor.Block).Block
	require.IsType(&block.BanffProposalBlock{}, proposalBlk)

	proposalTxs := proposalBlk.Txs()
	require.Len(proposalTxs, 1)

	rewardTx, ok := proposalTxs[0].Unsigned.(*txs.RewardAutoRenewedValidatorTx)
	require.True(ok)
	require.Equal(txID, rewardTx.TxID)
	require.Equal(uint64(endTime.Unix()), rewardTx.Timestamp)
}

func TestBuildBlockAdvanceTime(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t, upgradetest.Latest)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	var (
		now      = env.backend.Clk.Time()
		nextTime = now.Add(2 * txexecutor.SyncBound)
	)

	// Add a staker to [env.state]
	require.NoError(env.state.PutCurrentValidator(&state.Staker{
		NextTime: nextTime,
		Priority: txs.PrimaryNetworkValidatorCurrentPriority,
	}))

	// Advance wall clock to [nextTime]
	env.backend.Clk.Set(nextTime)

	// [BuildBlock] should build a block advancing the time to [NextTime]
	blkIntf, err := env.Builder.BuildBlock(t.Context())
	require.NoError(err)

	require.IsType(&blockexecutor.Block{}, blkIntf)
	blk := blkIntf.(*blockexecutor.Block)
	require.Empty(blk.Txs())
	require.IsType(&block.BanffStandardBlock{}, blk.Block)
	standardBlk := blk.Block.(*block.BanffStandardBlock)
	require.Equal(nextTime.Unix(), standardBlk.Timestamp().Unix())
}

func TestBuildBlockForceAdvanceTime(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t, upgradetest.Latest)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	subnetID := testSubnet1.ID()
	wallet := newWallet(t, env, walletConfig{
		subnetIDs: []ids.ID{subnetID},
	})

	// Create a valid transaction
	tx, err := wallet.IssueCreateChainTx(
		subnetID,
		nil,
		constants.AVMID,
		nil,
		"chain name",
	)
	require.NoError(err)

	// Issue the transaction
	env.ctx.Lock.Unlock()
	require.NoError(env.network.IssueTxFromRPC(tx))
	env.ctx.Lock.Lock()

	txID := tx.ID()
	_, ok := env.mempool.Get(txID)
	require.True(ok)

	var (
		now      = env.backend.Clk.Time()
		nextTime = now.Add(2 * txexecutor.SyncBound)
	)

	// Add a staker to [env.state]
	require.NoError(env.state.PutCurrentValidator(&state.Staker{
		NextTime: nextTime,
		Priority: txs.PrimaryNetworkValidatorCurrentPriority,
	}))

	// Advance wall clock to [nextTime] + [txexecutor.SyncBound]
	env.backend.Clk.Set(nextTime.Add(txexecutor.SyncBound))

	// [BuildBlock] should build a block advancing the time to [nextTime],
	// not the current wall clock.
	blkIntf, err := env.Builder.BuildBlock(t.Context())
	require.NoError(err)

	require.IsType(&blockexecutor.Block{}, blkIntf)
	blk := blkIntf.(*blockexecutor.Block)
	require.Equal([]*txs.Tx{tx}, blk.Txs())
	require.IsType(&block.BanffStandardBlock{}, blk.Block)
	standardBlk := blk.Block.(*block.BanffStandardBlock)
	require.Equal(nextTime.Unix(), standardBlk.Timestamp().Unix())
}

func TestBuildBlockInvalidStakingDurations(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t, upgradetest.Latest)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	// Post-Durango, [StartTime] is no longer validated. Staking durations are
	// based on the current chain timestamp and must be validated.
	env.config.UpgradeConfig.DurangoTime = time.Time{}

	wallet := newWallet(t, env, walletConfig{})

	var (
		now                   = env.backend.Clk.Time()
		defaultValidatorStake = 100 * units.MilliAvax

		// Add a validator ending in [MaxStakeDuration]
		validatorEndTime = now.Add(env.config.MaxStakeDuration)
	)

	sk, err := localsigner.New()
	require.NoError(err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(err)

	rewardsOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}
	tx1, err := wallet.IssueAddPermissionlessValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: ids.GenerateTestNodeID(),
				Start:  uint64(now.Unix()),
				End:    uint64(validatorEndTime.Unix()),
				Wght:   defaultValidatorStake,
			},
			Subnet: constants.PrimaryNetworkID,
		},
		pop,
		env.ctx.AVAXAssetID,
		rewardsOwner,
		rewardsOwner,
		reward.PercentDenominator,
	)
	require.NoError(err)
	require.NoError(env.mempool.Add(tx1))

	tx1ID := tx1.ID()
	_, ok := env.mempool.Get(tx1ID)
	require.True(ok)

	// Add a validator ending past [MaxStakeDuration]
	validator2EndTime := now.Add(env.config.MaxStakeDuration + time.Second)

	sk, err = localsigner.New()
	require.NoError(err)
	pop, err = signer.NewProofOfPossession(sk)
	require.NoError(err)

	tx2, err := wallet.IssueAddPermissionlessValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: ids.GenerateTestNodeID(),
				Start:  uint64(now.Unix()),
				End:    uint64(validator2EndTime.Unix()),
				Wght:   defaultValidatorStake,
			},
			Subnet: constants.PrimaryNetworkID,
		},
		pop,
		env.ctx.AVAXAssetID,
		rewardsOwner,
		rewardsOwner,
		reward.PercentDenominator,
	)
	require.NoError(err)
	require.NoError(env.mempool.Add(tx2))

	tx2ID := tx2.ID()
	_, ok = env.mempool.Get(tx2ID)
	require.True(ok)

	// Only tx1 should be in a built block since [MaxStakeDuration] is satisfied.
	blkIntf, err := env.Builder.BuildBlock(t.Context())
	require.NoError(err)

	require.IsType(&blockexecutor.Block{}, blkIntf)
	blk := blkIntf.(*blockexecutor.Block)
	require.Len(blk.Txs(), 1)
	require.Equal(tx1ID, blk.Txs()[0].ID())

	// Mempool should have none of the txs
	_, ok = env.mempool.Get(tx1ID)
	require.False(ok)
	_, ok = env.mempool.Get(tx2ID)
	require.False(ok)

	// Only tx2 should be dropped
	require.NoError(env.mempool.GetDropReason(tx1ID))

	tx2DropReason := env.mempool.GetDropReason(tx2ID)
	require.ErrorIs(tx2DropReason, txexecutor.ErrStakeTooLong)
}

func TestPreviouslyDroppedTxsCannotBeReAddedToMempool(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t, upgradetest.Latest)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	subnetID := testSubnet1.ID()
	wallet := newWallet(t, env, walletConfig{
		subnetIDs: []ids.ID{subnetID},
	})

	// Create a valid transaction
	tx, err := wallet.IssueCreateChainTx(
		testSubnet1.ID(),
		nil,
		constants.AVMID,
		nil,
		"chain name",
	)
	require.NoError(err)

	// Transaction should not be marked as dropped before being added to the
	// mempool
	txID := tx.ID()
	require.NoError(env.mempool.GetDropReason(txID))

	// Mark the transaction as dropped
	errTestingDropped := errors.New("testing dropped")
	env.mempool.MarkDropped(txID, errTestingDropped)
	err = env.mempool.GetDropReason(txID)
	require.ErrorIs(err, errTestingDropped)

	// Issue the transaction
	env.ctx.Lock.Unlock()
	err = env.network.IssueTxFromRPC(tx)
	require.ErrorIs(err, errTestingDropped)
	env.ctx.Lock.Lock()
	_, ok := env.mempool.Get(txID)
	require.False(ok)

	// When issued again, the mempool should still be marked as dropped
	err = env.mempool.GetDropReason(txID)
	require.ErrorIs(err, errTestingDropped)
}

func TestNoErrorOnUnexpectedSetPreferenceDuringBootstrapping(t *testing.T) {
	env := newEnvironment(t, upgradetest.Latest)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	env.isBootstrapped.Set(false)
	env.blkManager.SetPreference(ids.GenerateTestID(), nil) // should not panic
}

func TestGetNextStakerToReward(t *testing.T) {
	var (
		now  = time.Now()
		txID = ids.GenerateTestID()
	)

	type test struct {
		name                 string
		timestamp            time.Time
		state                *state.State
		expectedTxID         ids.ID
		expectedShouldReward bool
		expectedErr          error
	}

	tests := []test{
		{
			name:        "end of time",
			timestamp:   mockable.MaxTime,
			state:       statetest.New(t, statetest.Config{}),
			expectedErr: ErrEndOfTime,
		},
		{
			name:      "no stakers",
			timestamp: now,
			state: func() *state.State {
				s := statetest.New(t, statetest.Config{})
				// statetest.New initializes the state with a genesis that contains validators.
				// To test the case where there are no stakers, we need to delete the genesis validators.
				currentStakerIterator, err := s.GetCurrentStakerIterator()
				require.NoError(t, err)
				for _, staker := range iterator.ToSlice(currentStakerIterator) {
					require.NoError(t, s.DeleteCurrentValidator(staker))
				}
				return s
			}(),
		},
		{
			name:      "expired subnet validator/delegator",
			timestamp: now,
			state: func() *state.State {
				s := statetest.New(t, statetest.Config{})
				staker1 := &state.Staker{
					Priority: txs.SubnetPermissionedValidatorCurrentPriority,
					EndTime:  now,
					NodeID:   ids.GenerateTestNodeID(),
				}
				staker2 := &state.Staker{
					TxID:     txID,
					Priority: txs.SubnetPermissionlessDelegatorCurrentPriority,
					EndTime:  now,
					NodeID:   staker1.NodeID,
				}
				require.NoError(t, s.PutCurrentValidator(staker1))
				require.NoError(t, s.PutCurrentDelegator(staker2))
				return s
			}(),
			expectedTxID:         txID,
			expectedShouldReward: true,
		},
		{
			name:      "expired primary network validator after subnet expired subnet validator",
			timestamp: now,
			state: func() *state.State {
				s := statetest.New(t, statetest.Config{})
				staker1 := &state.Staker{
					Priority: txs.SubnetPermissionedValidatorCurrentPriority,
					EndTime:  now,
					NodeID:   ids.GenerateTestNodeID(),
				}
				staker2 := &state.Staker{
					TxID:     txID,
					Priority: txs.PrimaryNetworkValidatorCurrentPriority,
					EndTime:  now,
					NodeID:   ids.GenerateTestNodeID(),
				}
				require.NoError(t, s.PutCurrentValidator(staker1))
				require.NoError(t, s.PutCurrentValidator(staker2))
				return s
			}(),
			expectedTxID:         txID,
			expectedShouldReward: true,
		},
		{
			name:      "expired primary network delegator after subnet expired subnet validator",
			timestamp: now,
			state: func() *state.State {
				s := statetest.New(t, statetest.Config{})
				staker1 := &state.Staker{
					Priority: txs.SubnetPermissionedValidatorCurrentPriority,
					EndTime:  now,
					NodeID:   ids.GenerateTestNodeID(),
				}
				staker2 := &state.Staker{
					TxID:     txID,
					Priority: txs.PrimaryNetworkDelegatorCurrentPriority,
					EndTime:  now,
					NodeID:   staker1.NodeID,
				}
				require.NoError(t, s.PutCurrentValidator(staker1))
				require.NoError(t, s.PutCurrentDelegator(staker2))
				return s
			}(),
			expectedTxID:         txID,
			expectedShouldReward: true,
		},
		{
			name:      "non-expired primary network delegator",
			timestamp: now,
			state: func() *state.State {
				s := statetest.New(t, statetest.Config{})
				require.NoError(t, s.PutCurrentDelegator(&state.Staker{
					TxID:     txID,
					NodeID:   genesistest.DefaultNodeIDs[0],
					SubnetID: constants.PrimaryNetworkID,
					Priority: txs.PrimaryNetworkDelegatorCurrentPriority,
					EndTime:  now.Add(time.Second),
				}))
				return s
			}(),
			expectedTxID:         txID,
			expectedShouldReward: false,
		},
		{
			name:      "non-expired primary network validator",
			timestamp: now,
			state: func() *state.State {
				s := statetest.New(t, statetest.Config{})
				require.NoError(t, s.PutCurrentValidator(&state.Staker{
					TxID:     txID,
					Priority: txs.PrimaryNetworkValidatorCurrentPriority,
					EndTime:  now.Add(time.Second),
					NodeID:   ids.GenerateTestNodeID(),
				}))
				return s
			}(),
			expectedTxID:         txID,
			expectedShouldReward: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			txID, shouldReward, err := getNextStakerToReward(tt.timestamp, tt.state)
			require.ErrorIs(err, tt.expectedErr)
			if tt.expectedErr != nil {
				return
			}
			require.Equal(tt.expectedTxID, txID)
			require.Equal(tt.expectedShouldReward, shouldReward)
		})
	}
}

func TestNewRewardTxForStaker(t *testing.T) {
	var (
		networkID = uint32(1337)
		chainID   = ids.GenerateTestID()
	)

	ctx := &snow.Context{
		ChainID:   chainID,
		NetworkID: networkID,
	}

	validBaseTx := txs.BaseTx{
		BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
		},
	}

	blsSK, err := localsigner.New()
	require.NoError(t, err)

	blsPOP, err := signer.NewProofOfPossession(blsSK)
	require.NoError(t, err)

	tests := []struct {
		name         string
		stakerTxFunc func(t testing.TB) *txs.Tx
		timestamp    time.Time
		wantTxType   any
	}{
		{
			name: "AddAutoRenewedValidatorTx returns RewardAutoRenewedValidatorTx",
			stakerTxFunc: func(t testing.TB) *txs.Tx {
				utx := &txs.AddAutoRenewedValidatorTx{
					BaseTx:                validBaseTx,
					ValidatorNodeID:       ids.GenerateTestNodeID(),
					Period:                1,
					Wght:                  2,
					Signer:                blsPOP,
					StakeOuts:             []*avax.TransferableOutput{},
					ValidatorRewardsOwner: &secp256k1fx.OutputOwners{},
					DelegatorRewardsOwner: &secp256k1fx.OutputOwners{},
					DelegationShares:      reward.PercentDenominator,
					Owner:                 &secp256k1fx.OutputOwners{},
				}

				tx, err := txs.NewSigned(utx, txs.Codec, nil)
				require.NoError(t, err)
				return tx
			},
			timestamp:  time.Unix(1000, 0),
			wantTxType: &txs.RewardAutoRenewedValidatorTx{},
		},
		{
			name: "AddPermissionlessValidatorTx returns RewardValidatorTx",
			stakerTxFunc: func(t testing.TB) *txs.Tx {
				utx := &txs.AddPermissionlessValidatorTx{
					BaseTx: validBaseTx,
					Validator: txs.Validator{
						NodeID: ids.GenerateTestNodeID(),
						End:    uint64(time.Now().Add(time.Hour).Unix()),
						Wght:   2,
					},
					Subnet:                ids.GenerateTestID(),
					Signer:                blsPOP,
					StakeOuts:             []*avax.TransferableOutput{},
					ValidatorRewardsOwner: &secp256k1fx.OutputOwners{},
					DelegatorRewardsOwner: &secp256k1fx.OutputOwners{},
					DelegationShares:      reward.PercentDenominator,
				}

				tx, err := txs.NewSigned(utx, txs.Codec, nil)
				require.NoError(t, err)
				return tx
			},
			timestamp:  time.Unix(1000, 0),
			wantTxType: &txs.RewardValidatorTx{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stakerTx := tt.stakerTxFunc(t)

			rewardTx, err := NewRewardTxForStaker(ctx, stakerTx, tt.timestamp)
			require.NoError(t, err)
			require.NotNil(t, rewardTx)
			require.IsType(t, tt.wantTxType, rewardTx.Unsigned)

			switch utx := rewardTx.Unsigned.(type) {
			case *txs.RewardAutoRenewedValidatorTx:
				require.Equal(t, stakerTx.ID(), utx.TxID)
				require.Equal(t, uint64(tt.timestamp.Unix()), utx.Timestamp)
			case *txs.RewardValidatorTx:
				require.Equal(t, stakerTx.ID(), utx.TxID)
			}
		})
	}
}
