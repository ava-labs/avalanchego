// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/statetest"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	blockexecutor "github.com/ava-labs/avalanchego/vms/platformvm/block/executor"
	walletcommon "github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

func TestAddDelegatorTxOverDelegatedRegression(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, upgradetest.Cortina)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	wallet := newWallet(t, vm, walletConfig{})

	validatorStartTime := vm.clock.Time().Add(executor.SyncBound).Add(1 * time.Second)
	validatorEndTime := validatorStartTime.Add(360 * 24 * time.Hour)

	nodeID := ids.GenerateTestNodeID()
	rewardsOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}

	// create valid tx
	addValidatorTx, err := wallet.IssueAddValidatorTx(
		&txs.Validator{
			NodeID: nodeID,
			Start:  uint64(validatorStartTime.Unix()),
			End:    uint64(validatorEndTime.Unix()),
			Wght:   vm.MinValidatorStake,
		},
		rewardsOwner,
		reward.PercentDenominator,
	)
	require.NoError(err)

	lastAcceptedID, err := vm.LastAccepted(t.Context())
	require.NoError(err)
	lastAccepted, err := vm.GetBlock(t.Context(), lastAcceptedID)
	require.NoError(err)

	statelessBlk0, err := block.NewBanffStandardBlock(
		lastAccepted.Timestamp().Add(time.Second),
		lastAccepted.ID(),
		lastAccepted.Height()+1,
		[]*txs.Tx{addValidatorTx},
	)
	require.NoError(err)

	blk0, err := vm.ParseBlock(t.Context(), statelessBlk0.Bytes())
	require.NoError(err)
	require.NoError(blk0.Verify(t.Context()))
	require.NoError(blk0.Accept(t.Context()))

	// Advance the time
	vm.clock.Set(validatorStartTime)

	firstDelegatorStartTime := validatorStartTime.Add(executor.SyncBound).Add(1 * time.Second)
	firstDelegatorEndTime := firstDelegatorStartTime.Add(vm.MinStakeDuration)

	// create valid tx
	addFirstDelegatorTx, err := wallet.IssueAddDelegatorTx(
		&txs.Validator{
			NodeID: nodeID,
			Start:  uint64(firstDelegatorStartTime.Unix()),
			End:    uint64(firstDelegatorEndTime.Unix()),
			Wght:   4 * vm.MinValidatorStake, // maximum amount of stake this delegator can provide
		},
		rewardsOwner,
	)
	require.NoError(err)

	statelessBlk1, err := block.NewBanffStandardBlock(
		blk0.Timestamp().Add(time.Second),
		blk0.ID(),
		blk0.Height()+1,
		[]*txs.Tx{addFirstDelegatorTx},
	)
	require.NoError(err)

	blk1, err := vm.ParseBlock(t.Context(), statelessBlk1.Bytes())
	require.NoError(err)
	require.NoError(blk1.Verify(t.Context()))
	require.NoError(blk1.Accept(t.Context()))

	// Advance the time
	vm.clock.Set(firstDelegatorStartTime)

	secondDelegatorStartTime := firstDelegatorEndTime.Add(2 * time.Second)
	secondDelegatorEndTime := secondDelegatorStartTime.Add(vm.MinStakeDuration)

	vm.clock.Set(secondDelegatorStartTime.Add(-10 * executor.SyncBound))

	// create valid tx
	addSecondDelegatorTx, err := wallet.IssueAddDelegatorTx(
		&txs.Validator{
			NodeID: nodeID,
			Start:  uint64(secondDelegatorStartTime.Unix()),
			End:    uint64(secondDelegatorEndTime.Unix()),
			Wght:   vm.MinDelegatorStake,
		},
		rewardsOwner,
	)
	require.NoError(err)

	statelessBlk2, err := block.NewBanffStandardBlock(
		blk1.Timestamp().Add(time.Second),
		blk1.ID(),
		blk1.Height()+1,
		[]*txs.Tx{addSecondDelegatorTx},
	)
	require.NoError(err)

	blk2, err := vm.ParseBlock(t.Context(), statelessBlk2.Bytes())
	require.NoError(err)
	require.NoError(blk2.Verify(t.Context()))
	require.NoError(blk2.Accept(t.Context()))

	thirdDelegatorStartTime := firstDelegatorEndTime.Add(-time.Second)
	thirdDelegatorEndTime := thirdDelegatorStartTime.Add(vm.MinStakeDuration)

	// create invalid tx
	addThirdDelegatorTx, err := wallet.IssueAddDelegatorTx(
		&txs.Validator{
			NodeID: nodeID,
			Start:  uint64(thirdDelegatorStartTime.Unix()),
			End:    uint64(thirdDelegatorEndTime.Unix()),
			Wght:   vm.MinDelegatorStake,
		},
		rewardsOwner,
	)
	require.NoError(err)

	statelessBlk3, err := block.NewBanffStandardBlock(
		blk2.Timestamp().Add(time.Second),
		blk2.ID(),
		blk2.Height()+1,
		[]*txs.Tx{addThirdDelegatorTx},
	)
	require.NoError(err)

	blk3, err := vm.ParseBlock(t.Context(), statelessBlk3.Bytes())
	require.NoError(err)
	err = blk3.Verify(t.Context())
	require.ErrorIs(err, executor.ErrOverDelegated)
}

func TestAddDelegatorTxHeapCorruption(t *testing.T) {
	validatorStartTime := latestForkTime.Add(executor.SyncBound).Add(1 * time.Second)
	validatorEndTime := validatorStartTime.Add(360 * 24 * time.Hour)
	validatorStake := defaultMaxValidatorStake / 5

	delegator1StartTime := validatorStartTime
	delegator1EndTime := delegator1StartTime.Add(3 * defaultMinStakingDuration)
	delegator1Stake := defaultMinValidatorStake

	delegator2StartTime := validatorStartTime.Add(1 * defaultMinStakingDuration)
	delegator2EndTime := delegator1StartTime.Add(6 * defaultMinStakingDuration)
	delegator2Stake := defaultMinValidatorStake

	delegator3StartTime := validatorStartTime.Add(2 * defaultMinStakingDuration)
	delegator3EndTime := delegator1StartTime.Add(4 * defaultMinStakingDuration)
	delegator3Stake := defaultMaxValidatorStake - validatorStake - 2*defaultMinValidatorStake

	delegator4StartTime := validatorStartTime.Add(5 * defaultMinStakingDuration)
	delegator4EndTime := delegator1StartTime.Add(7 * defaultMinStakingDuration)
	delegator4Stake := defaultMaxValidatorStake - validatorStake - defaultMinValidatorStake

	tests := []struct {
		name    string
		ap3Time time.Time
	}{
		{
			name:    "pre-upgrade is no longer restrictive",
			ap3Time: validatorEndTime,
		},
		{
			name:    "post-upgrade calculate max stake correctly",
			ap3Time: genesistest.DefaultValidatorStartTime,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			vm, _, _ := defaultVM(t, upgradetest.ApricotPhase3)
			vm.UpgradeConfig.ApricotPhase3Time = test.ap3Time

			vm.ctx.Lock.Lock()
			defer vm.ctx.Lock.Unlock()

			wallet := newWallet(t, vm, walletConfig{})

			nodeID := ids.GenerateTestNodeID()
			rewardsOwner := &secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
			}

			// create valid tx
			addValidatorTx, err := wallet.IssueAddValidatorTx(
				&txs.Validator{
					NodeID: nodeID,
					Start:  uint64(validatorStartTime.Unix()),
					End:    uint64(validatorEndTime.Unix()),
					Wght:   validatorStake,
				},
				rewardsOwner,
				reward.PercentDenominator,
			)
			require.NoError(err)

			lastAcceptedID, err := vm.LastAccepted(t.Context())
			require.NoError(err)
			lastAccepted, err := vm.GetBlock(t.Context(), lastAcceptedID)
			require.NoError(err)

			statelessBlk0, err := block.NewBanffStandardBlock(
				lastAccepted.Timestamp().Add(time.Second),
				lastAccepted.ID(),
				lastAccepted.Height()+1,
				[]*txs.Tx{addValidatorTx},
			)
			require.NoError(err)

			blk0, err := vm.ParseBlock(t.Context(), statelessBlk0.Bytes())
			require.NoError(err)
			require.NoError(blk0.Verify(t.Context()))
			require.NoError(blk0.Accept(t.Context()))

			// create valid tx
			addFirstDelegatorTx, err := wallet.IssueAddDelegatorTx(
				&txs.Validator{
					NodeID: nodeID,
					Start:  uint64(delegator1StartTime.Unix()),
					End:    uint64(delegator1EndTime.Unix()),
					Wght:   delegator1Stake,
				},
				rewardsOwner,
			)
			require.NoError(err)

			statelessBlk1, err := block.NewBanffStandardBlock(
				blk0.Timestamp().Add(time.Second),
				blk0.ID(),
				blk0.Height()+1,
				[]*txs.Tx{addFirstDelegatorTx},
			)
			require.NoError(err)

			blk1, err := vm.ParseBlock(t.Context(), statelessBlk1.Bytes())
			require.NoError(err)
			require.NoError(blk1.Verify(t.Context()))
			require.NoError(blk1.Accept(t.Context()))

			// create valid tx
			addSecondDelegatorTx, err := wallet.IssueAddDelegatorTx(
				&txs.Validator{
					NodeID: nodeID,
					Start:  uint64(delegator2StartTime.Unix()),
					End:    uint64(delegator2EndTime.Unix()),
					Wght:   delegator2Stake,
				},
				rewardsOwner,
			)
			require.NoError(err)

			statelessBlk2, err := block.NewBanffStandardBlock(
				blk1.Timestamp().Add(time.Second),
				blk1.ID(),
				blk1.Height()+1,
				[]*txs.Tx{addSecondDelegatorTx},
			)
			require.NoError(err)

			blk2, err := vm.ParseBlock(t.Context(), statelessBlk2.Bytes())
			require.NoError(err)
			require.NoError(blk2.Verify(t.Context()))
			require.NoError(blk2.Accept(t.Context()))

			// create valid tx
			addThirdDelegatorTx, err := wallet.IssueAddDelegatorTx(
				&txs.Validator{
					NodeID: nodeID,
					Start:  uint64(delegator3StartTime.Unix()),
					End:    uint64(delegator3EndTime.Unix()),
					Wght:   delegator3Stake,
				},
				rewardsOwner,
			)
			require.NoError(err)

			statelessBlk3, err := block.NewBanffStandardBlock(
				blk2.Timestamp().Add(time.Second),
				blk2.ID(),
				blk2.Height()+1,
				[]*txs.Tx{addThirdDelegatorTx},
			)
			require.NoError(err)

			blk3, err := vm.ParseBlock(t.Context(), statelessBlk3.Bytes())
			require.NoError(err)
			require.NoError(blk3.Verify(t.Context()))
			require.NoError(blk3.Accept(t.Context()))

			// create valid tx
			addFourthDelegatorTx, err := wallet.IssueAddDelegatorTx(
				&txs.Validator{
					NodeID: nodeID,
					Start:  uint64(delegator4StartTime.Unix()),
					End:    uint64(delegator4EndTime.Unix()),
					Wght:   delegator4Stake,
				},
				rewardsOwner,
			)
			require.NoError(err)

			statelessBlk4, err := block.NewBanffStandardBlock(
				blk3.Timestamp().Add(time.Second),
				blk3.ID(),
				blk3.Height()+1,
				[]*txs.Tx{addFourthDelegatorTx},
			)
			require.NoError(err)

			blk4, err := vm.ParseBlock(t.Context(), statelessBlk4.Bytes())
			require.NoError(err)
			require.NoError(blk4.Verify(t.Context()))
			require.NoError(blk4.Accept(t.Context()))
		})
	}
}

// Test that calling Verify on a block with an unverified parent doesn't cause a
// panic.
func TestUnverifiedParentPanicRegression(t *testing.T) {
	require := require.New(t)

	baseDB := memdb.New()
	atomicDB := prefixdb.New([]byte{1}, baseDB)

	vm := &VM{Internal: config.Internal{
		Chains:                 chains.TestManager,
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
		UpgradeConfig:          upgradetest.GetConfigWithUpgradeTime(upgradetest.Banff, latestForkTime),
	}}

	ctx := snowtest.Context(t, snowtest.PChainID)
	ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
		ctx.Lock.Unlock()
	}()

	require.NoError(vm.Initialize(
		t.Context(),
		ctx,
		baseDB,
		genesistest.NewBytes(t, genesistest.Config{}),
		nil,
		nil,
		nil,
		nil,
	))

	m := atomic.NewMemory(atomicDB)
	vm.ctx.SharedMemory = m.NewSharedMemory(ctx.ChainID)

	// set time to post Banff fork
	vm.clock.Set(latestForkTime.Add(time.Second))
	vm.state.SetTimestamp(latestForkTime.Add(time.Second))

	var (
		key0    = genesistest.DefaultFundedKeys[0]
		addr0   = key0.Address()
		owners0 = &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{addr0},
		}

		key1    = genesistest.DefaultFundedKeys[1]
		addr1   = key1.Address()
		owners1 = &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{addr1},
		}
	)

	wallet := newWallet(t, vm, walletConfig{})
	addSubnetTx0, err := wallet.IssueCreateSubnetTx(
		owners0,
		walletcommon.WithCustomAddresses(set.Of(
			addr0,
		)),
	)
	require.NoError(err)

	addSubnetTx1, err := wallet.IssueCreateSubnetTx(
		owners1,
		walletcommon.WithCustomAddresses(set.Of(
			addr1,
		)),
	)
	require.NoError(err)

	// Wallet needs to be re-created to generate a conflicting transaction
	wallet = newWallet(t, vm, walletConfig{})
	addSubnetTx2, err := wallet.IssueCreateSubnetTx(
		owners1,
		walletcommon.WithCustomAddresses(set.Of(
			addr0,
		)),
	)
	require.NoError(err)

	preferredID := vm.manager.Preferred()
	preferred, err := vm.manager.GetBlock(preferredID)
	require.NoError(err)
	preferredChainTime := preferred.Timestamp()
	preferredHeight := preferred.Height()

	statelessStandardBlk, err := block.NewBanffStandardBlock(
		preferredChainTime,
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{addSubnetTx0},
	)
	require.NoError(err)
	addSubnetBlk0 := vm.manager.NewBlock(statelessStandardBlk)

	statelessStandardBlk, err = block.NewBanffStandardBlock(
		preferredChainTime,
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{addSubnetTx1},
	)
	require.NoError(err)
	addSubnetBlk1 := vm.manager.NewBlock(statelessStandardBlk)

	statelessStandardBlk, err = block.NewBanffStandardBlock(
		preferredChainTime,
		addSubnetBlk1.ID(),
		preferredHeight+2,
		[]*txs.Tx{addSubnetTx2},
	)
	require.NoError(err)
	addSubnetBlk2 := vm.manager.NewBlock(statelessStandardBlk)

	_, err = vm.ParseBlock(t.Context(), addSubnetBlk0.Bytes())
	require.NoError(err)

	_, err = vm.ParseBlock(t.Context(), addSubnetBlk1.Bytes())
	require.NoError(err)

	_, err = vm.ParseBlock(t.Context(), addSubnetBlk2.Bytes())
	require.NoError(err)

	require.NoError(addSubnetBlk0.Verify(t.Context()))
	require.NoError(addSubnetBlk0.Accept(t.Context()))

	// Doesn't matter what verify returns as long as it's not panicking.
	_ = addSubnetBlk2.Verify(t.Context())
}

func TestRejectedStateRegressionInvalidValidatorTimestamp(t *testing.T) {
	require := require.New(t)

	vm, baseDB, mutableSharedMemory := defaultVM(t, upgradetest.Cortina)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	wallet := newWallet(t, vm, walletConfig{})

	nodeID := ids.GenerateTestNodeID()
	newValidatorStartTime := vm.clock.Time().Add(executor.SyncBound).Add(1 * time.Second)
	newValidatorEndTime := newValidatorStartTime.Add(defaultMinStakingDuration)

	// Create the tx to add a new validator
	addValidatorTx, err := wallet.IssueAddValidatorTx(
		&txs.Validator{
			NodeID: nodeID,
			Start:  uint64(newValidatorStartTime.Unix()),
			End:    uint64(newValidatorEndTime.Unix()),
			Wght:   vm.MinValidatorStake,
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		},
		reward.PercentDenominator,
	)
	require.NoError(err)

	// Create the standard block to add the new validator
	preferredID := vm.manager.Preferred()
	preferred, err := vm.manager.GetBlock(preferredID)
	require.NoError(err)
	preferredChainTime := preferred.Timestamp()
	preferredHeight := preferred.Height()

	statelessBlk, err := block.NewBanffStandardBlock(
		preferredChainTime,
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{addValidatorTx},
	)
	require.NoError(err)

	addValidatorStandardBlk := vm.manager.NewBlock(statelessBlk)
	require.NoError(addValidatorStandardBlk.Verify(t.Context()))

	// Verify that the new validator now in pending validator set
	{
		onAccept, found := vm.manager.GetState(addValidatorStandardBlk.ID())
		require.True(found)

		_, err := onAccept.GetPendingValidator(constants.PrimaryNetworkID, nodeID)
		require.NoError(err)
	}

	// Create the UTXO that will be added to shared memory
	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID: ids.GenerateTestID(),
		},
		Asset: avax.Asset{
			ID: vm.ctx.AVAXAssetID,
		},
		Out: &secp256k1fx.TransferOutput{
			Amt:          1,
			OutputOwners: secp256k1fx.OutputOwners{},
		},
	}

	// Create the import tx that will fail verification
	unsignedImportTx := &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    vm.ctx.NetworkID,
			BlockchainID: vm.ctx.ChainID,
		}},
		SourceChain: vm.ctx.XChainID,
		ImportedInputs: []*avax.TransferableInput{
			{
				UTXOID: utxo.UTXOID,
				Asset:  utxo.Asset,
				In: &secp256k1fx.TransferInput{
					Amt: 1,
				},
			},
		},
	}
	signedImportTx := &txs.Tx{Unsigned: unsignedImportTx}
	require.NoError(signedImportTx.Sign(txs.Codec, [][]*secp256k1.PrivateKey{
		{}, // There is one input, with no required signers
	}))

	// Create the standard block that will fail verification, and then be
	// re-verified.
	preferredChainTime = addValidatorStandardBlk.Timestamp()
	preferredID = addValidatorStandardBlk.ID()
	preferredHeight = addValidatorStandardBlk.Height()

	statelessImportBlk, err := block.NewBanffStandardBlock(
		preferredChainTime,
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{signedImportTx},
	)
	require.NoError(err)

	importBlk := vm.manager.NewBlock(statelessImportBlk)

	// Because the shared memory UTXO hasn't been populated, this block is
	// currently invalid.
	err = importBlk.Verify(t.Context())
	require.ErrorIs(err, database.ErrNotFound)

	// Populate the shared memory UTXO.
	m := atomic.NewMemory(prefixdb.New([]byte{5}, baseDB))

	mutableSharedMemory.SharedMemory = m.NewSharedMemory(vm.ctx.ChainID)
	peerSharedMemory := m.NewSharedMemory(vm.ctx.XChainID)

	utxoBytes, err := txs.Codec.Marshal(txs.CodecVersion, utxo)
	require.NoError(err)

	inputID := utxo.InputID()
	require.NoError(peerSharedMemory.Apply(
		map[ids.ID]*atomic.Requests{
			vm.ctx.ChainID: {
				PutRequests: []*atomic.Element{
					{
						Key:   inputID[:],
						Value: utxoBytes,
					},
				},
			},
		},
	))

	// Because the shared memory UTXO has now been populated, the block should
	// pass verification.
	require.NoError(importBlk.Verify(t.Context()))

	// Move chain time ahead to bring the new validator from the pending
	// validator set into the current validator set.
	vm.clock.Set(newValidatorStartTime)

	// Create the proposal block that should have moved the new validator from
	// the pending validator set into the current validator set.
	preferredID = importBlk.ID()
	preferredHeight = importBlk.Height()

	statelessAdvanceTimeStandardBlk, err := block.NewBanffStandardBlock(
		newValidatorStartTime,
		preferredID,
		preferredHeight+1,
		nil,
	)
	require.NoError(err)

	advanceTimeStandardBlk := vm.manager.NewBlock(statelessAdvanceTimeStandardBlk)
	require.NoError(advanceTimeStandardBlk.Verify(t.Context()))

	// Accept all the blocks
	allBlocks := []snowman.Block{
		addValidatorStandardBlk,
		importBlk,
		advanceTimeStandardBlk,
	}
	for _, blk := range allBlocks {
		require.NoError(blk.Accept(t.Context()))
	}

	// Force a reload of the state from the database.
	vm.Internal.Validators = validators.NewManager()
	newState := statetest.New(t, statetest.Config{
		DB:         vm.db,
		Validators: vm.Internal.Validators,
		Upgrades:   vm.Internal.UpgradeConfig,
		Context:    vm.ctx,
		Rewards:    reward.NewCalculator(vm.Internal.RewardConfig),
	})

	// Verify that new validator is now in the current validator set.
	{
		_, err := newState.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
		require.NoError(err)

		_, err = newState.GetPendingValidator(constants.PrimaryNetworkID, nodeID)
		require.ErrorIs(err, database.ErrNotFound)

		currentTimestamp := newState.GetTimestamp()
		require.Equal(newValidatorStartTime.Unix(), currentTimestamp.Unix())
	}
}

func TestRejectedStateRegressionInvalidValidatorReward(t *testing.T) {
	require := require.New(t)

	vm, baseDB, mutableSharedMemory := defaultVM(t, upgradetest.Cortina)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	vm.state.SetCurrentSupply(constants.PrimaryNetworkID, defaultRewardConfig.SupplyCap/2)

	wallet := newWallet(t, vm, walletConfig{})

	nodeID0 := ids.GenerateTestNodeID()
	newValidatorStartTime0 := vm.clock.Time().Add(executor.SyncBound).Add(1 * time.Second)
	newValidatorEndTime0 := newValidatorStartTime0.Add(defaultMaxStakingDuration)

	rewardsOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}

	// Create the tx to add the first new validator
	addValidatorTx0, err := wallet.IssueAddValidatorTx(
		&txs.Validator{
			NodeID: nodeID0,
			Start:  uint64(newValidatorStartTime0.Unix()),
			End:    uint64(newValidatorEndTime0.Unix()),
			Wght:   vm.MaxValidatorStake,
		},
		rewardsOwner,
		reward.PercentDenominator,
	)
	require.NoError(err)

	// Create the standard block to add the first new validator
	preferredID := vm.manager.Preferred()
	preferred, err := vm.manager.GetBlock(preferredID)
	require.NoError(err)
	preferredChainTime := preferred.Timestamp()
	preferredHeight := preferred.Height()

	statelessAddValidatorStandardBlk0, err := block.NewBanffStandardBlock(
		preferredChainTime,
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{addValidatorTx0},
	)
	require.NoError(err)

	addValidatorStandardBlk0 := vm.manager.NewBlock(statelessAddValidatorStandardBlk0)
	require.NoError(addValidatorStandardBlk0.Verify(t.Context()))

	// Verify that first new validator now in pending validator set
	{
		onAccept, ok := vm.manager.GetState(addValidatorStandardBlk0.ID())
		require.True(ok)

		_, err := onAccept.GetPendingValidator(constants.PrimaryNetworkID, nodeID0)
		require.NoError(err)
	}

	// Move chain time to bring the first new validator from the pending
	// validator set into the current validator set.
	vm.clock.Set(newValidatorStartTime0)

	// Create the proposal block that moves the first new validator from the
	// pending validator set into the current validator set.
	preferredID = addValidatorStandardBlk0.ID()
	preferredHeight = addValidatorStandardBlk0.Height()

	statelessAdvanceTimeStandardBlk0, err := block.NewBanffStandardBlock(
		newValidatorStartTime0,
		preferredID,
		preferredHeight+1,
		nil,
	)
	require.NoError(err)

	advanceTimeStandardBlk0 := vm.manager.NewBlock(statelessAdvanceTimeStandardBlk0)
	require.NoError(advanceTimeStandardBlk0.Verify(t.Context()))

	// Verify that the first new validator is now in the current validator set.
	{
		onAccept, ok := vm.manager.GetState(advanceTimeStandardBlk0.ID())
		require.True(ok)

		_, err := onAccept.GetCurrentValidator(constants.PrimaryNetworkID, nodeID0)
		require.NoError(err)

		_, err = onAccept.GetPendingValidator(constants.PrimaryNetworkID, nodeID0)
		require.ErrorIs(err, database.ErrNotFound)

		currentTimestamp := onAccept.GetTimestamp()
		require.Equal(newValidatorStartTime0.Unix(), currentTimestamp.Unix())
	}

	// Create the UTXO that will be added to shared memory
	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID: ids.GenerateTestID(),
		},
		Asset: avax.Asset{
			ID: vm.ctx.AVAXAssetID,
		},
		Out: &secp256k1fx.TransferOutput{
			Amt:          1,
			OutputOwners: secp256k1fx.OutputOwners{},
		},
	}

	// Create the import tx that will fail verification
	unsignedImportTx := &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    vm.ctx.NetworkID,
			BlockchainID: vm.ctx.ChainID,
		}},
		SourceChain: vm.ctx.XChainID,
		ImportedInputs: []*avax.TransferableInput{
			{
				UTXOID: utxo.UTXOID,
				Asset:  utxo.Asset,
				In: &secp256k1fx.TransferInput{
					Amt: 1,
				},
			},
		},
	}
	signedImportTx := &txs.Tx{Unsigned: unsignedImportTx}
	require.NoError(signedImportTx.Sign(txs.Codec, [][]*secp256k1.PrivateKey{
		{}, // There is one input, with no required signers
	}))

	// Create the standard block that will fail verification, and then be
	// re-verified.
	preferredChainTime = advanceTimeStandardBlk0.Timestamp()
	preferredID = advanceTimeStandardBlk0.ID()
	preferredHeight = advanceTimeStandardBlk0.Height()

	statelessImportBlk, err := block.NewBanffStandardBlock(
		preferredChainTime,
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{signedImportTx},
	)
	require.NoError(err)

	importBlk := vm.manager.NewBlock(statelessImportBlk)
	// Because the shared memory UTXO hasn't been populated, this block is
	// currently invalid.
	err = importBlk.Verify(t.Context())
	require.ErrorIs(err, database.ErrNotFound)

	// Populate the shared memory UTXO.
	m := atomic.NewMemory(prefixdb.New([]byte{5}, baseDB))

	mutableSharedMemory.SharedMemory = m.NewSharedMemory(vm.ctx.ChainID)
	peerSharedMemory := m.NewSharedMemory(vm.ctx.XChainID)

	utxoBytes, err := txs.Codec.Marshal(txs.CodecVersion, utxo)
	require.NoError(err)

	inputID := utxo.InputID()
	require.NoError(peerSharedMemory.Apply(
		map[ids.ID]*atomic.Requests{
			vm.ctx.ChainID: {
				PutRequests: []*atomic.Element{
					{
						Key:   inputID[:],
						Value: utxoBytes,
					},
				},
			},
		},
	))

	// Because the shared memory UTXO has now been populated, the block should
	// pass verification.
	require.NoError(importBlk.Verify(t.Context()))

	newValidatorStartTime1 := newValidatorStartTime0.Add(executor.SyncBound).Add(1 * time.Second)
	newValidatorEndTime1 := newValidatorStartTime1.Add(defaultMaxStakingDuration)

	nodeID1 := ids.GenerateTestNodeID()

	// Create the tx to add the second new validator
	addValidatorTx1, err := wallet.IssueAddValidatorTx(
		&txs.Validator{
			NodeID: nodeID1,
			Start:  uint64(newValidatorStartTime1.Unix()),
			End:    uint64(newValidatorEndTime1.Unix()),
			Wght:   vm.MaxValidatorStake,
		},
		rewardsOwner,
		reward.PercentDenominator,
	)
	require.NoError(err)

	// Create the standard block to add the second new validator
	preferredChainTime = importBlk.Timestamp()
	preferredID = importBlk.ID()
	preferredHeight = importBlk.Height()

	statelessAddValidatorStandardBlk1, err := block.NewBanffStandardBlock(
		preferredChainTime,
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{addValidatorTx1},
	)
	require.NoError(err)

	addValidatorStandardBlk1 := vm.manager.NewBlock(statelessAddValidatorStandardBlk1)

	require.NoError(addValidatorStandardBlk1.Verify(t.Context()))

	// Verify that the second new validator now in pending validator set
	{
		onAccept, ok := vm.manager.GetState(addValidatorStandardBlk1.ID())
		require.True(ok)

		_, err := onAccept.GetPendingValidator(constants.PrimaryNetworkID, nodeID1)
		require.NoError(err)
	}

	// Move chain time to bring the second new validator from the pending
	// validator set into the current validator set.
	vm.clock.Set(newValidatorStartTime1)

	// Create the proposal block that moves the second new validator from the
	// pending validator set into the current validator set.
	preferredID = addValidatorStandardBlk1.ID()
	preferredHeight = addValidatorStandardBlk1.Height()

	statelessAdvanceTimeStandardBlk1, err := block.NewBanffStandardBlock(
		newValidatorStartTime1,
		preferredID,
		preferredHeight+1,
		nil,
	)
	require.NoError(err)

	advanceTimeStandardBlk1 := vm.manager.NewBlock(statelessAdvanceTimeStandardBlk1)
	require.NoError(advanceTimeStandardBlk1.Verify(t.Context()))

	// Verify that the second new validator is now in the current validator set.
	{
		onAccept, ok := vm.manager.GetState(advanceTimeStandardBlk1.ID())
		require.True(ok)

		_, err := onAccept.GetCurrentValidator(constants.PrimaryNetworkID, nodeID1)
		require.NoError(err)

		_, err = onAccept.GetPendingValidator(constants.PrimaryNetworkID, nodeID1)
		require.ErrorIs(err, database.ErrNotFound)

		currentTimestamp := onAccept.GetTimestamp()
		require.Equal(newValidatorStartTime1.Unix(), currentTimestamp.Unix())
	}

	// Accept all the blocks
	allBlocks := []snowman.Block{
		addValidatorStandardBlk0,
		advanceTimeStandardBlk0,
		importBlk,
		addValidatorStandardBlk1,
		advanceTimeStandardBlk1,
	}
	for _, blk := range allBlocks {
		require.NoError(blk.Accept(t.Context()))
	}

	// Force a reload of the state from the database.
	vm.Internal.Validators = validators.NewManager()
	newState := statetest.New(t, statetest.Config{
		DB:         vm.db,
		Validators: vm.Internal.Validators,
		Upgrades:   vm.Internal.UpgradeConfig,
		Context:    vm.ctx,
		Rewards:    reward.NewCalculator(vm.Internal.RewardConfig),
	})

	// Verify that validators are in the current validator set with the correct
	// reward calculated.
	{
		staker0, err := newState.GetCurrentValidator(constants.PrimaryNetworkID, nodeID0)
		require.NoError(err)
		require.Equal(uint64(60000000), staker0.PotentialReward)

		staker1, err := newState.GetCurrentValidator(constants.PrimaryNetworkID, nodeID1)
		require.NoError(err)
		require.Equal(uint64(59999999), staker1.PotentialReward)

		_, err = newState.GetPendingValidator(constants.PrimaryNetworkID, nodeID0)
		require.ErrorIs(err, database.ErrNotFound)

		_, err = newState.GetPendingValidator(constants.PrimaryNetworkID, nodeID1)
		require.ErrorIs(err, database.ErrNotFound)

		currentTimestamp := newState.GetTimestamp()
		require.Equal(newValidatorStartTime1.Unix(), currentTimestamp.Unix())
	}
}

func TestValidatorSetAtCacheOverwriteRegression(t *testing.T) {
	require := require.New(t)

	vm, _, _ := defaultVM(t, upgradetest.Cortina)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	currentHeight, err := vm.GetCurrentHeight(t.Context())
	require.NoError(err)
	require.Equal(uint64(1), currentHeight)

	expectedValidators1 := map[ids.NodeID]uint64{
		genesistest.DefaultNodeIDs[0]: genesistest.DefaultValidatorWeight,
		genesistest.DefaultNodeIDs[1]: genesistest.DefaultValidatorWeight,
		genesistest.DefaultNodeIDs[2]: genesistest.DefaultValidatorWeight,
		genesistest.DefaultNodeIDs[3]: genesistest.DefaultValidatorWeight,
		genesistest.DefaultNodeIDs[4]: genesistest.DefaultValidatorWeight,
	}
	validators, err := vm.GetValidatorSet(t.Context(), 1, constants.PrimaryNetworkID)
	require.NoError(err)
	for nodeID, weight := range expectedValidators1 {
		require.Equal(weight, validators[nodeID].Weight)
	}

	wallet := newWallet(t, vm, walletConfig{})

	newValidatorStartTime0 := vm.clock.Time().Add(executor.SyncBound).Add(1 * time.Second)
	newValidatorEndTime0 := newValidatorStartTime0.Add(defaultMaxStakingDuration)

	extraNodeID := ids.GenerateTestNodeID()

	// Create the tx to add the first new validator
	addValidatorTx0, err := wallet.IssueAddValidatorTx(
		&txs.Validator{
			NodeID: extraNodeID,
			Start:  uint64(newValidatorStartTime0.Unix()),
			End:    uint64(newValidatorEndTime0.Unix()),
			Wght:   vm.MaxValidatorStake,
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		},
		reward.PercentDenominator,
	)
	require.NoError(err)

	// Create the standard block to add the first new validator
	preferredID := vm.manager.Preferred()
	preferred, err := vm.manager.GetBlock(preferredID)
	require.NoError(err)
	preferredChainTime := preferred.Timestamp()
	preferredHeight := preferred.Height()

	statelessStandardBlk, err := block.NewBanffStandardBlock(
		preferredChainTime,
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{addValidatorTx0},
	)
	require.NoError(err)
	addValidatorProposalBlk0 := vm.manager.NewBlock(statelessStandardBlk)
	require.NoError(addValidatorProposalBlk0.Verify(t.Context()))
	require.NoError(addValidatorProposalBlk0.Accept(t.Context()))
	require.NoError(vm.SetPreference(t.Context(), vm.manager.LastAccepted()))

	currentHeight, err = vm.GetCurrentHeight(t.Context())
	require.NoError(err)
	require.Equal(uint64(2), currentHeight)

	for i := uint64(1); i <= 2; i++ {
		validators, err = vm.GetValidatorSet(t.Context(), i, constants.PrimaryNetworkID)
		require.NoError(err)
		for nodeID, weight := range expectedValidators1 {
			require.Equal(weight, validators[nodeID].Weight)
		}
	}

	// Advance chain time to move the first new validator from the pending
	// validator set into the current validator set.
	vm.clock.Set(newValidatorStartTime0)

	// Create the standard block that moves the first new validator from the
	// pending validator set into the current validator set.
	preferredID = vm.manager.Preferred()
	preferred, err = vm.manager.GetBlock(preferredID)
	require.NoError(err)
	preferredID = preferred.ID()
	preferredHeight = preferred.Height()

	statelessStandardBlk, err = block.NewBanffStandardBlock(
		newValidatorStartTime0,
		preferredID,
		preferredHeight+1,
		nil,
	)
	require.NoError(err)
	advanceTimeProposalBlk0 := vm.manager.NewBlock(statelessStandardBlk)
	require.NoError(advanceTimeProposalBlk0.Verify(t.Context()))
	require.NoError(advanceTimeProposalBlk0.Accept(t.Context()))
	require.NoError(vm.SetPreference(t.Context(), vm.manager.LastAccepted()))

	currentHeight, err = vm.GetCurrentHeight(t.Context())
	require.NoError(err)
	require.Equal(uint64(3), currentHeight)

	for i := uint64(1); i <= 2; i++ {
		validators, err = vm.GetValidatorSet(t.Context(), i, constants.PrimaryNetworkID)
		require.NoError(err)
		for nodeID, weight := range expectedValidators1 {
			require.Equal(weight, validators[nodeID].Weight)
		}
	}

	expectedValidators2 := map[ids.NodeID]uint64{
		genesistest.DefaultNodeIDs[0]: genesistest.DefaultValidatorWeight,
		genesistest.DefaultNodeIDs[1]: genesistest.DefaultValidatorWeight,
		genesistest.DefaultNodeIDs[2]: genesistest.DefaultValidatorWeight,
		genesistest.DefaultNodeIDs[3]: genesistest.DefaultValidatorWeight,
		genesistest.DefaultNodeIDs[4]: genesistest.DefaultValidatorWeight,
		extraNodeID:                   vm.MaxValidatorStake,
	}
	validators, err = vm.GetValidatorSet(t.Context(), 3, constants.PrimaryNetworkID)
	require.NoError(err)
	for nodeID, weight := range expectedValidators2 {
		require.Equal(weight, validators[nodeID].Weight)
	}
}

func TestAddDelegatorTxAddBeforeRemove(t *testing.T) {
	require := require.New(t)

	validatorStartTime := latestForkTime.Add(executor.SyncBound).Add(1 * time.Second)
	validatorEndTime := validatorStartTime.Add(360 * 24 * time.Hour)
	validatorStake := defaultMaxValidatorStake / 5

	delegator1StartTime := validatorStartTime
	delegator1EndTime := delegator1StartTime.Add(3 * defaultMinStakingDuration)
	delegator1Stake := defaultMaxValidatorStake - validatorStake

	delegator2StartTime := delegator1EndTime
	delegator2EndTime := delegator2StartTime.Add(3 * defaultMinStakingDuration)
	delegator2Stake := defaultMaxValidatorStake - validatorStake

	vm, _, _ := defaultVM(t, upgradetest.Cortina)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	wallet := newWallet(t, vm, walletConfig{})

	nodeID := ids.GenerateTestNodeID()
	rewardsOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}

	// create valid tx
	addValidatorTx, err := wallet.IssueAddValidatorTx(
		&txs.Validator{
			NodeID: nodeID,
			Start:  uint64(validatorStartTime.Unix()),
			End:    uint64(validatorEndTime.Unix()),
			Wght:   validatorStake,
		},
		rewardsOwner,
		reward.PercentDenominator,
	)
	require.NoError(err)

	lastAcceptedID, err := vm.LastAccepted(t.Context())
	require.NoError(err)
	lastAccepted, err := vm.GetBlock(t.Context(), lastAcceptedID)
	require.NoError(err)

	statelessBlk0, err := block.NewBanffStandardBlock(
		lastAccepted.Timestamp().Add(time.Second),
		lastAccepted.ID(),
		lastAccepted.Height()+1,
		[]*txs.Tx{addValidatorTx},
	)
	require.NoError(err)

	blk0, err := vm.ParseBlock(t.Context(), statelessBlk0.Bytes())
	require.NoError(err)
	require.NoError(blk0.Verify(t.Context()))
	require.NoError(blk0.Accept(t.Context()))

	// create valid tx
	addFirstDelegatorTx, err := wallet.IssueAddDelegatorTx(
		&txs.Validator{
			NodeID: nodeID,
			Start:  uint64(delegator1StartTime.Unix()),
			End:    uint64(delegator1EndTime.Unix()),
			Wght:   delegator1Stake,
		},
		rewardsOwner,
	)
	require.NoError(err)

	statelessBlk1, err := block.NewBanffStandardBlock(
		blk0.Timestamp().Add(time.Second),
		blk0.ID(),
		blk0.Height()+1,
		[]*txs.Tx{addFirstDelegatorTx},
	)
	require.NoError(err)

	blk1, err := vm.ParseBlock(t.Context(), statelessBlk1.Bytes())
	require.NoError(err)
	require.NoError(blk1.Verify(t.Context()))
	require.NoError(blk1.Accept(t.Context()))

	// create invalid tx
	addSecondDelegatorTx, err := wallet.IssueAddDelegatorTx(
		&txs.Validator{
			NodeID: nodeID,
			Start:  uint64(delegator2StartTime.Unix()),
			End:    uint64(delegator2EndTime.Unix()),
			Wght:   delegator2Stake,
		},
		rewardsOwner,
	)
	require.NoError(err)

	statelessBlk2, err := block.NewBanffStandardBlock(
		blk1.Timestamp().Add(time.Second),
		blk1.ID(),
		blk1.Height()+1,
		[]*txs.Tx{addSecondDelegatorTx},
	)
	require.NoError(err)

	blk2, err := vm.ParseBlock(t.Context(), statelessBlk2.Bytes())
	require.NoError(err)
	// attempting to verify the second add delegator tx should fail because the
	// total stake weight would go over the limit.
	err = blk2.Verify(t.Context())
	require.ErrorIs(err, executor.ErrOverDelegated)
}

func TestRemovePermissionedValidatorDuringPendingToCurrentTransitionNotTracked(t *testing.T) {
	require := require.New(t)

	validatorStartTime := latestForkTime.Add(executor.SyncBound).Add(1 * time.Second)
	validatorEndTime := validatorStartTime.Add(360 * 24 * time.Hour)

	vm, _, _ := defaultVM(t, upgradetest.Cortina)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	wallet := newWallet(t, vm, walletConfig{})

	nodeID := ids.GenerateTestNodeID()
	addValidatorTx, err := wallet.IssueAddValidatorTx(
		&txs.Validator{
			NodeID: nodeID,
			Start:  uint64(validatorStartTime.Unix()),
			End:    uint64(validatorEndTime.Unix()),
			Wght:   defaultMaxValidatorStake,
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		},
		reward.PercentDenominator,
	)
	require.NoError(err)

	lastAcceptedID, err := vm.LastAccepted(t.Context())
	require.NoError(err)
	lastAccepted, err := vm.GetBlock(t.Context(), lastAcceptedID)
	require.NoError(err)

	statelessBlk0, err := block.NewBanffStandardBlock(
		lastAccepted.Timestamp().Add(time.Second),
		lastAccepted.ID(),
		lastAccepted.Height()+1,
		[]*txs.Tx{addValidatorTx},
	)
	require.NoError(err)

	blk0, err := vm.ParseBlock(t.Context(), statelessBlk0.Bytes())
	require.NoError(err)
	require.NoError(blk0.Verify(t.Context()))
	require.NoError(blk0.Accept(t.Context()))

	createSubnetTx, err := wallet.IssueCreateSubnetTx(
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{genesistest.DefaultFundedKeys[0].Address()},
		},
	)
	require.NoError(err)

	statelessBlk1, err := block.NewBanffStandardBlock(
		blk0.Timestamp().Add(time.Second),
		blk0.ID(),
		blk0.Height()+1,
		[]*txs.Tx{createSubnetTx},
	)
	require.NoError(err)

	blk1, err := vm.ParseBlock(t.Context(), statelessBlk1.Bytes())
	require.NoError(err)
	require.NoError(blk1.Verify(t.Context()))
	require.NoError(blk1.Accept(t.Context()))

	subnetID := createSubnetTx.ID()
	addSubnetValidatorTx, err := wallet.IssueAddSubnetValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(validatorStartTime.Unix()),
				End:    uint64(validatorEndTime.Unix()),
				Wght:   defaultMaxValidatorStake,
			},
			Subnet: subnetID,
		},
	)
	require.NoError(err)

	statelessBlk2, err := block.NewBanffStandardBlock(
		blk1.Timestamp().Add(time.Second),
		blk1.ID(),
		blk1.Height()+1,
		[]*txs.Tx{addSubnetValidatorTx},
	)
	require.NoError(err)

	blk2, err := vm.ParseBlock(t.Context(), statelessBlk2.Bytes())
	require.NoError(err)
	require.NoError(blk2.Verify(t.Context()))
	require.NoError(blk2.Accept(t.Context()))

	addSubnetValidatorHeight, err := vm.GetCurrentHeight(t.Context())
	require.NoError(err)

	emptyValidatorSet, err := vm.GetValidatorSet(
		t.Context(),
		addSubnetValidatorHeight,
		subnetID,
	)
	require.NoError(err)
	require.Empty(emptyValidatorSet)

	removeSubnetValidatorTx, err := wallet.IssueRemoveSubnetValidatorTx(
		nodeID,
		subnetID,
	)
	require.NoError(err)

	// Set the clock so that the validator will be moved from the pending
	// validator set into the current validator set.
	vm.clock.Set(validatorStartTime)

	statelessBlk3, err := block.NewBanffStandardBlock(
		blk2.Timestamp().Add(time.Second),
		blk2.ID(),
		blk2.Height()+1,
		[]*txs.Tx{removeSubnetValidatorTx},
	)
	require.NoError(err)

	blk3, err := vm.ParseBlock(t.Context(), statelessBlk3.Bytes())
	require.NoError(err)
	require.NoError(blk3.Verify(t.Context()))
	require.NoError(blk3.Accept(t.Context()))

	emptyValidatorSet, err = vm.GetValidatorSet(
		t.Context(),
		addSubnetValidatorHeight,
		subnetID,
	)
	require.NoError(err)
	require.Empty(emptyValidatorSet)
}

func TestRemovePermissionedValidatorDuringPendingToCurrentTransitionTracked(t *testing.T) {
	require := require.New(t)

	validatorStartTime := latestForkTime.Add(executor.SyncBound).Add(1 * time.Second)
	validatorEndTime := validatorStartTime.Add(360 * 24 * time.Hour)

	vm, _, _ := defaultVM(t, upgradetest.Cortina)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	wallet := newWallet(t, vm, walletConfig{})

	nodeID := ids.GenerateTestNodeID()
	addValidatorTx, err := wallet.IssueAddValidatorTx(
		&txs.Validator{
			NodeID: nodeID,
			Start:  uint64(validatorStartTime.Unix()),
			End:    uint64(validatorEndTime.Unix()),
			Wght:   defaultMaxValidatorStake,
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		},
		reward.PercentDenominator,
	)
	require.NoError(err)

	lastAcceptedID, err := vm.LastAccepted(t.Context())
	require.NoError(err)
	lastAccepted, err := vm.GetBlock(t.Context(), lastAcceptedID)
	require.NoError(err)

	statelessBlk0, err := block.NewBanffStandardBlock(
		lastAccepted.Timestamp().Add(time.Second),
		lastAccepted.ID(),
		lastAccepted.Height()+1,
		[]*txs.Tx{addValidatorTx},
	)
	require.NoError(err)

	blk0, err := vm.ParseBlock(t.Context(), statelessBlk0.Bytes())
	require.NoError(err)
	require.NoError(blk0.Verify(t.Context()))
	require.NoError(blk0.Accept(t.Context()))

	createSubnetTx, err := wallet.IssueCreateSubnetTx(
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{genesistest.DefaultFundedKeys[0].Address()},
		},
	)
	require.NoError(err)

	statelessBlk1, err := block.NewBanffStandardBlock(
		blk0.Timestamp().Add(time.Second),
		blk0.ID(),
		blk0.Height()+1,
		[]*txs.Tx{createSubnetTx},
	)
	require.NoError(err)

	blk1, err := vm.ParseBlock(t.Context(), statelessBlk1.Bytes())
	require.NoError(err)
	require.NoError(blk1.Verify(t.Context()))
	require.NoError(blk1.Accept(t.Context()))

	subnetID := createSubnetTx.ID()
	addSubnetValidatorTx, err := wallet.IssueAddSubnetValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(validatorStartTime.Unix()),
				End:    uint64(validatorEndTime.Unix()),
				Wght:   defaultMaxValidatorStake,
			},
			Subnet: subnetID,
		},
	)
	require.NoError(err)

	statelessBlk2, err := block.NewBanffStandardBlock(
		blk1.Timestamp().Add(time.Second),
		blk1.ID(),
		blk1.Height()+1,
		[]*txs.Tx{addSubnetValidatorTx},
	)
	require.NoError(err)

	blk2, err := vm.ParseBlock(t.Context(), statelessBlk2.Bytes())
	require.NoError(err)
	require.NoError(blk2.Verify(t.Context()))
	require.NoError(blk2.Accept(t.Context()))

	removeSubnetValidatorTx, err := wallet.IssueRemoveSubnetValidatorTx(
		nodeID,
		subnetID,
	)
	require.NoError(err)

	// Set the clock so that the validator will be moved from the pending
	// validator set into the current validator set.
	vm.clock.Set(validatorStartTime)

	statelessBlk3, err := block.NewBanffStandardBlock(
		blk2.Timestamp().Add(time.Second),
		blk2.ID(),
		blk2.Height()+1,
		[]*txs.Tx{removeSubnetValidatorTx},
	)
	require.NoError(err)

	blk3, err := vm.ParseBlock(t.Context(), statelessBlk3.Bytes())
	require.NoError(err)
	require.NoError(blk3.Verify(t.Context()))
	require.NoError(blk3.Accept(t.Context()))
}

func TestAddValidatorDuringRemoval(t *testing.T) {
	require := require.New(t)

	vm, _, _ := defaultVM(t, upgradetest.Durango)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	var (
		nodeID   = genesistest.DefaultNodeIDs[0]
		subnetID = testSubnet1.ID()
		wallet   = newWallet(t, vm, walletConfig{
			subnetIDs: []ids.ID{subnetID},
		})

		duration     = defaultMinStakingDuration
		firstEndTime = latestForkTime.Add(duration)
	)

	firstAddSubnetValidatorTx, err := wallet.IssueAddSubnetValidatorTx(&txs.SubnetValidator{
		Validator: txs.Validator{
			NodeID: nodeID,
			End:    uint64(firstEndTime.Unix()),
			Wght:   1,
		},
		Subnet: subnetID,
	})
	require.NoError(err)

	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(firstAddSubnetValidatorTx))
	vm.ctx.Lock.Lock()

	// Accept firstAddSubnetValidatorTx
	require.NoError(buildAndAcceptStandardBlock(vm))

	// Verify that the validator was added
	_, err = vm.state.GetCurrentValidator(subnetID, nodeID)
	require.NoError(err)

	secondEndTime := firstEndTime.Add(duration)
	secondSubnetValidatorTx, err := wallet.IssueAddSubnetValidatorTx(&txs.SubnetValidator{
		Validator: txs.Validator{
			NodeID: nodeID,
			End:    uint64(secondEndTime.Unix()),
			Wght:   1,
		},
		Subnet: subnetID,
	})
	require.NoError(err)

	vm.clock.Set(firstEndTime)
	vm.ctx.Lock.Unlock()
	err = vm.issueTxFromRPC(secondSubnetValidatorTx)
	vm.ctx.Lock.Lock()
	require.ErrorIs(err, state.ErrAddingStakerAfterDeletion)

	// Remove the first subnet validator
	require.NoError(buildAndAcceptStandardBlock(vm))

	// Verify that the validator does not exist
	_, err = vm.state.GetCurrentValidator(subnetID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	// Verify that the invalid transaction was not executed
	_, _, err = vm.state.GetTx(secondSubnetValidatorTx.ID())
	require.ErrorIs(err, database.ErrNotFound)
}

// GetValidatorSet must return the BLS keys for a given validator correctly when
// queried at a previous height, even in case it has currently expired
//
//  1. Add primary network validator
//  2. Advance chain time for the primary network validator to be moved to the
//     current validator set
//  3. Add permissioned subnet validator
//  4. Advance chain time for the subnet validator to be moved to the current
//     validator set
//  5. Advance chain time for the subnet validator to be removed
//  6. Advance chain time for the primary network validator to be removed
//  7. Re-add the primary network validator with a different BLS key
//  8. Advance chain time for the primary network validator to be moved to the
//     current validator set
func TestSubnetValidatorBLSKeyDiffAfterExpiry(t *testing.T) {
	vm, _, _ := defaultVM(t, upgradetest.Cortina)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	subnetID := testSubnet1.TxID
	wallet := newWallet(t, vm, walletConfig{
		subnetIDs: []ids.ID{subnetID},
	})

	var (
		primaryStartTime   = genesistest.DefaultValidatorStartTime.Add(executor.SyncBound)
		subnetStartTime    = primaryStartTime.Add(executor.SyncBound)
		subnetEndTime      = subnetStartTime.Add(defaultMinStakingDuration)
		primaryEndTime     = subnetEndTime.Add(time.Second)
		primaryReStartTime = primaryEndTime.Add(executor.SyncBound)
		primaryReEndTime   = primaryReStartTime.Add(defaultMinStakingDuration)
	)

	// insert primary network validator
	var (
		nodeID       = ids.GenerateTestNodeID()
		rewardsOwner = &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		}
	)
	sk1, err := localsigner.New()
	require.NoError(t, err)
	pk1 := sk1.PublicKey()
	pop1, err := signer.NewProofOfPossession(sk1)
	require.NoError(t, err)

	// build primary network validator with BLS key
	primaryTx, err := wallet.IssueAddPermissionlessValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(primaryStartTime.Unix()),
				End:    uint64(primaryEndTime.Unix()),
				Wght:   vm.MinValidatorStake,
			},
			Subnet: constants.PrimaryNetworkID,
		},
		pop1,
		vm.ctx.AVAXAssetID,
		rewardsOwner,
		rewardsOwner,
		reward.PercentDenominator,
	)
	require.NoError(t, err)

	vm.ctx.Lock.Unlock()
	require.NoError(t, vm.issueTxFromRPC(primaryTx))
	vm.ctx.Lock.Lock()
	require.NoError(t, buildAndAcceptStandardBlock(vm))

	// move time ahead, promoting primary validator to current
	vm.clock.Set(primaryStartTime)
	require.NoError(t, buildAndAcceptStandardBlock(vm))

	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(t, err)

	primaryStartHeight, err := vm.GetCurrentHeight(t.Context())
	require.NoError(t, err)
	t.Logf("primaryStartHeight: %d", primaryStartHeight)

	// insert the subnet validator
	subnetTx, err := wallet.IssueAddSubnetValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(subnetStartTime.Unix()),
				End:    uint64(subnetEndTime.Unix()),
				Wght:   1,
			},
			Subnet: subnetID,
		},
	)
	require.NoError(t, err)

	vm.ctx.Lock.Unlock()
	require.NoError(t, vm.issueTxFromRPC(subnetTx))
	vm.ctx.Lock.Lock()
	require.NoError(t, buildAndAcceptStandardBlock(vm))

	// move time ahead, promoting the subnet validator to current
	vm.clock.Set(subnetStartTime)
	require.NoError(t, buildAndAcceptStandardBlock(vm))

	_, err = vm.state.GetCurrentValidator(subnetID, nodeID)
	require.NoError(t, err)

	subnetStartHeight, err := vm.GetCurrentHeight(t.Context())
	require.NoError(t, err)
	t.Logf("subnetStartHeight: %d", subnetStartHeight)

	// move time ahead, terminating the subnet validator
	vm.clock.Set(subnetEndTime)
	require.NoError(t, buildAndAcceptStandardBlock(vm))

	_, err = vm.state.GetCurrentValidator(subnetID, nodeID)
	require.ErrorIs(t, err, database.ErrNotFound)

	subnetEndHeight, err := vm.GetCurrentHeight(t.Context())
	require.NoError(t, err)
	t.Logf("subnetEndHeight: %d", subnetEndHeight)

	// move time ahead, terminating primary network validator
	vm.clock.Set(primaryEndTime)
	blk, err := vm.Builder.BuildBlock(t.Context()) // must be a proposal block rewarding the primary validator
	require.NoError(t, err)
	require.NoError(t, blk.Verify(t.Context()))

	proposalBlk := blk.(snowman.OracleBlock)
	options, err := proposalBlk.Options(t.Context())
	require.NoError(t, err)

	commit := options[0].(*blockexecutor.Block)
	require.IsType(t, &block.BanffCommitBlock{}, commit.Block)

	require.NoError(t, blk.Accept(t.Context()))
	require.NoError(t, commit.Verify(t.Context()))
	require.NoError(t, commit.Accept(t.Context()))
	require.NoError(t, vm.SetPreference(t.Context(), vm.manager.LastAccepted()))

	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.ErrorIs(t, err, database.ErrNotFound)

	primaryEndHeight, err := vm.GetCurrentHeight(t.Context())
	require.NoError(t, err)
	t.Logf("primaryEndHeight: %d", primaryEndHeight)

	// reinsert primary validator with a different BLS key
	sk2, err := localsigner.New()
	require.NoError(t, err)
	pk2 := sk2.PublicKey()
	pop2, err := signer.NewProofOfPossession(sk2)
	require.NoError(t, err)

	primaryRestartTx, err := wallet.IssueAddPermissionlessValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(primaryReStartTime.Unix()),
				End:    uint64(primaryReEndTime.Unix()),
				Wght:   vm.MinValidatorStake,
			},
			Subnet: constants.PrimaryNetworkID,
		},
		pop2,
		vm.ctx.AVAXAssetID,
		rewardsOwner,
		rewardsOwner,
		reward.PercentDenominator,
	)
	require.NoError(t, err)

	vm.ctx.Lock.Unlock()
	require.NoError(t, vm.issueTxFromRPC(primaryRestartTx))
	vm.ctx.Lock.Lock()
	require.NoError(t, buildAndAcceptStandardBlock(vm))

	// move time ahead, promoting restarted primary validator to current
	vm.clock.Set(primaryReStartTime)
	require.NoError(t, buildAndAcceptStandardBlock(vm))

	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(t, err)

	primaryRestartHeight, err := vm.GetCurrentHeight(t.Context())
	require.NoError(t, err)
	t.Logf("primaryRestartHeight: %d", primaryRestartHeight)

	for height := uint64(0); height <= primaryRestartHeight; height++ {
		t.Run(strconv.Itoa(int(height)), func(t *testing.T) {
			require := require.New(t)

			// The primary network validator doesn't exist for heights
			// [0, primaryStartHeight) and [primaryEndHeight, primaryRestartHeight).
			var expectedPrimaryNetworkErr error
			if height < primaryStartHeight || (height >= primaryEndHeight && height < primaryRestartHeight) {
				expectedPrimaryNetworkErr = database.ErrNotFound
			}

			// The primary network validator's BLS key is pk1 for the first
			// validation period and pk2 for the second validation period.
			var expectedPrimaryNetworkBLSKey *bls.PublicKey
			if height >= primaryStartHeight && height < primaryEndHeight {
				expectedPrimaryNetworkBLSKey = pk1
			} else if height >= primaryRestartHeight {
				expectedPrimaryNetworkBLSKey = pk2
			}

			err := checkValidatorBlsKeyIsSet(
				vm.State,
				nodeID,
				constants.PrimaryNetworkID,
				height,
				expectedPrimaryNetworkBLSKey,
			)
			require.ErrorIs(err, expectedPrimaryNetworkErr)

			// The subnet validator doesn't exist for heights
			// [0, subnetStartHeight) and [subnetEndHeight, primaryRestartHeight).
			var expectedSubnetErr error
			if height < subnetStartHeight || height >= subnetEndHeight {
				expectedSubnetErr = database.ErrNotFound
			}

			err = checkValidatorBlsKeyIsSet(
				vm.State,
				nodeID,
				subnetID,
				height,
				pk1,
			)
			require.ErrorIs(err, expectedSubnetErr)
		})
	}
}

func TestPrimaryNetworkValidatorPopulatedToEmptyBLSKeyDiff(t *testing.T) {
	// A primary network validator has an empty BLS key. Then it restakes adding
	// the BLS key. Querying the validator set back when BLS key was empty must
	// return an empty BLS key.

	// setup
	require := require.New(t)
	vm, _, _ := defaultVM(t, upgradetest.Cortina)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	wallet := newWallet(t, vm, walletConfig{})

	// A primary network validator stake twice
	var (
		primaryStartTime1 = genesistest.DefaultValidatorStartTime.Add(executor.SyncBound)
		primaryEndTime1   = primaryStartTime1.Add(defaultMinStakingDuration)
		primaryStartTime2 = primaryEndTime1.Add(executor.SyncBound)
		primaryEndTime2   = primaryStartTime2.Add(defaultMinStakingDuration)
	)

	// Add a primary network validator with no BLS key
	nodeID := ids.GenerateTestNodeID()
	rewardsOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}

	primaryTx1, err := wallet.IssueAddValidatorTx(
		&txs.Validator{
			NodeID: nodeID,
			Start:  uint64(primaryStartTime1.Unix()),
			End:    uint64(primaryEndTime1.Unix()),
			Wght:   vm.MinValidatorStake,
		},
		rewardsOwner,
		reward.PercentDenominator,
	)
	require.NoError(err)

	lastAcceptedID, err := vm.LastAccepted(t.Context())
	require.NoError(err)
	lastAccepted, err := vm.GetBlock(t.Context(), lastAcceptedID)
	require.NoError(err)

	statelessBlk0, err := block.NewBanffStandardBlock(
		lastAccepted.Timestamp().Add(time.Second),
		lastAccepted.ID(),
		lastAccepted.Height()+1,
		[]*txs.Tx{primaryTx1},
	)
	require.NoError(err)

	blk0, err := vm.ParseBlock(t.Context(), statelessBlk0.Bytes())
	require.NoError(err)
	require.NoError(blk0.Verify(t.Context()))
	require.NoError(blk0.Accept(t.Context()))
	require.NoError(vm.SetPreference(t.Context(), blk0.ID()))

	// move time ahead, promoting primary validator to current
	vm.clock.Set(primaryStartTime1)
	require.NoError(buildAndAcceptStandardBlock(vm))

	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)

	primaryStartHeight, err := vm.GetCurrentHeight(t.Context())
	require.NoError(err)

	// move time ahead, terminating primary network validator
	vm.clock.Set(primaryEndTime1)
	blk1, err := vm.Builder.BuildBlock(t.Context()) // must be a proposal block rewarding the primary validator
	require.NoError(err)
	require.NoError(blk1.Verify(t.Context()))

	proposalBlk := blk1.(snowman.OracleBlock)
	options, err := proposalBlk.Options(t.Context())
	require.NoError(err)

	commit := options[0].(*blockexecutor.Block)
	require.IsType(&block.BanffCommitBlock{}, commit.Block)

	require.NoError(blk1.Accept(t.Context()))
	require.NoError(commit.Verify(t.Context()))
	require.NoError(commit.Accept(t.Context()))
	require.NoError(vm.SetPreference(t.Context(), vm.manager.LastAccepted()))

	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	primaryEndHeight, err := vm.GetCurrentHeight(t.Context())
	require.NoError(err)

	// reinsert primary validator with a different BLS key
	sk, err := localsigner.New()
	require.NoError(err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(err)

	primaryRestartTx, err := wallet.IssueAddPermissionlessValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(primaryStartTime2.Unix()),
				End:    uint64(primaryEndTime2.Unix()),
				Wght:   vm.MinValidatorStake,
			},
			Subnet: constants.PrimaryNetworkID,
		},
		pop,
		vm.ctx.AVAXAssetID,
		rewardsOwner,
		rewardsOwner,
		reward.PercentDenominator,
	)
	require.NoError(err)

	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(primaryRestartTx))
	vm.ctx.Lock.Lock()
	require.NoError(buildAndAcceptStandardBlock(vm))

	// move time ahead, promoting restarted primary validator to current
	vm.clock.Set(primaryStartTime2)
	require.NoError(buildAndAcceptStandardBlock(vm))

	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)

	for height := primaryStartHeight; height < primaryEndHeight; height++ {
		require.NoError(checkValidatorBlsKeyIsSet(
			vm.State,
			nodeID,
			constants.PrimaryNetworkID,
			height,
			nil,
		))
	}
}

func TestSubnetValidatorPopulatedToEmptyBLSKeyDiff(t *testing.T) {
	// A primary network validator has an empty BLS key and a subnet validator.
	// Primary network validator terminates its first staking cycle and it
	// restakes adding the BLS key. Querying the validator set back when BLS key
	// was empty must return an empty BLS key for the subnet validator

	// setup
	require := require.New(t)
	vm, _, _ := defaultVM(t, upgradetest.Cortina)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	subnetID := testSubnet1.TxID
	wallet := newWallet(t, vm, walletConfig{
		subnetIDs: []ids.ID{subnetID},
	})

	// A primary network validator stake twice
	var (
		primaryStartTime1 = genesistest.DefaultValidatorStartTime.Add(executor.SyncBound)
		subnetStartTime   = primaryStartTime1.Add(executor.SyncBound)
		subnetEndTime     = subnetStartTime.Add(defaultMinStakingDuration)
		primaryEndTime1   = subnetEndTime.Add(time.Second)
		primaryStartTime2 = primaryEndTime1.Add(executor.SyncBound)
		primaryEndTime2   = primaryStartTime2.Add(defaultMinStakingDuration)
	)

	// Add a primary network validator with no BLS key
	nodeID := ids.GenerateTestNodeID()
	rewardsOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}

	primaryTx1, err := wallet.IssueAddValidatorTx(
		&txs.Validator{
			NodeID: nodeID,
			Start:  uint64(primaryStartTime1.Unix()),
			End:    uint64(primaryEndTime1.Unix()),
			Wght:   vm.MinValidatorStake,
		},
		rewardsOwner,
		reward.PercentDenominator,
	)
	require.NoError(err)

	lastAcceptedID, err := vm.LastAccepted(t.Context())
	require.NoError(err)
	lastAccepted, err := vm.GetBlock(t.Context(), lastAcceptedID)
	require.NoError(err)

	statelessBlk0, err := block.NewBanffStandardBlock(
		lastAccepted.Timestamp().Add(time.Second),
		lastAccepted.ID(),
		lastAccepted.Height()+1,
		[]*txs.Tx{primaryTx1},
	)
	require.NoError(err)

	blk0, err := vm.ParseBlock(t.Context(), statelessBlk0.Bytes())
	require.NoError(err)
	require.NoError(blk0.Verify(t.Context()))
	require.NoError(blk0.Accept(t.Context()))
	require.NoError(vm.SetPreference(t.Context(), blk0.ID()))

	// move time ahead, promoting primary validator to current
	vm.clock.Set(primaryStartTime1)
	require.NoError(buildAndAcceptStandardBlock(vm))

	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)

	primaryStartHeight, err := vm.GetCurrentHeight(t.Context())
	require.NoError(err)

	// insert the subnet validator
	subnetTx, err := wallet.IssueAddSubnetValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(subnetStartTime.Unix()),
				End:    uint64(subnetEndTime.Unix()),
				Wght:   1,
			},
			Subnet: subnetID,
		},
	)
	require.NoError(err)

	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(subnetTx))
	vm.ctx.Lock.Lock()
	require.NoError(buildAndAcceptStandardBlock(vm))

	// move time ahead, promoting the subnet validator to current
	vm.clock.Set(subnetStartTime)
	require.NoError(buildAndAcceptStandardBlock(vm))

	_, err = vm.state.GetCurrentValidator(subnetID, nodeID)
	require.NoError(err)

	subnetStartHeight, err := vm.GetCurrentHeight(t.Context())
	require.NoError(err)

	// move time ahead, terminating the subnet validator
	vm.clock.Set(subnetEndTime)
	require.NoError(buildAndAcceptStandardBlock(vm))

	_, err = vm.state.GetCurrentValidator(subnetID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	subnetEndHeight, err := vm.GetCurrentHeight(t.Context())
	require.NoError(err)

	// move time ahead, terminating primary network validator
	vm.clock.Set(primaryEndTime1)
	blk1, err := vm.Builder.BuildBlock(t.Context()) // must be a proposal block rewarding the primary validator
	require.NoError(err)
	require.NoError(blk1.Verify(t.Context()))

	proposalBlk := blk1.(snowman.OracleBlock)
	options, err := proposalBlk.Options(t.Context())
	require.NoError(err)

	commit := options[0].(*blockexecutor.Block)
	require.IsType(&block.BanffCommitBlock{}, commit.Block)

	require.NoError(blk1.Accept(t.Context()))
	require.NoError(commit.Verify(t.Context()))
	require.NoError(commit.Accept(t.Context()))
	require.NoError(vm.SetPreference(t.Context(), vm.manager.LastAccepted()))

	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	primaryEndHeight, err := vm.GetCurrentHeight(t.Context())
	require.NoError(err)

	// reinsert primary validator with a different BLS key
	sk2, err := localsigner.New()
	require.NoError(err)
	pop2, err := signer.NewProofOfPossession(sk2)
	require.NoError(err)

	primaryRestartTx, err := wallet.IssueAddPermissionlessValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(primaryStartTime2.Unix()),
				End:    uint64(primaryEndTime2.Unix()),
				Wght:   vm.MinValidatorStake,
			},
			Subnet: constants.PrimaryNetworkID,
		},
		pop2,
		vm.ctx.AVAXAssetID,
		rewardsOwner,
		rewardsOwner,
		reward.PercentDenominator,
	)
	require.NoError(err)

	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(primaryRestartTx))
	vm.ctx.Lock.Lock()
	require.NoError(buildAndAcceptStandardBlock(vm))

	// move time ahead, promoting restarted primary validator to current
	vm.clock.Set(primaryStartTime2)
	require.NoError(buildAndAcceptStandardBlock(vm))

	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)

	for height := primaryStartHeight; height < primaryEndHeight; height++ {
		require.NoError(checkValidatorBlsKeyIsSet(
			vm.State,
			nodeID,
			constants.PrimaryNetworkID,
			height,
			nil,
		))
	}
	for height := subnetStartHeight; height < subnetEndHeight; height++ {
		require.NoError(checkValidatorBlsKeyIsSet(
			vm.State,
			nodeID,
			subnetID,
			height,
			nil,
		))
	}
}

func TestValidatorSetReturnsCopy(t *testing.T) {
	require := require.New(t)

	vm, _, _ := defaultVM(t, upgradetest.Latest)

	validators1, err := vm.GetValidatorSet(t.Context(), 1, constants.PrimaryNetworkID)
	require.NoError(err)

	validators2, err := vm.GetValidatorSet(t.Context(), 1, constants.PrimaryNetworkID)
	require.NoError(err)

	require.NotNil(validators1[genesistest.DefaultNodeIDs[0]])
	delete(validators1, genesistest.DefaultNodeIDs[0])
	require.NotNil(validators2[genesistest.DefaultNodeIDs[0]])
}

func TestSubnetValidatorSetAfterPrimaryNetworkValidatorRemoval(t *testing.T) {
	// A primary network validator and a subnet validator are running.
	// Primary network validator terminates its staking cycle.
	// Querying the validator set when the subnet validator existed should
	// succeed.

	// setup
	require := require.New(t)
	vm, _, _ := defaultVM(t, upgradetest.Cortina)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	subnetID := testSubnet1.TxID
	wallet := newWallet(t, vm, walletConfig{
		subnetIDs: []ids.ID{subnetID},
	})

	// A primary network validator stake twice
	var (
		primaryStartTime1 = genesistest.DefaultValidatorStartTime.Add(executor.SyncBound)
		subnetStartTime   = primaryStartTime1.Add(executor.SyncBound)
		subnetEndTime     = subnetStartTime.Add(defaultMinStakingDuration)
		primaryEndTime1   = subnetEndTime.Add(time.Second)
	)

	// Add a primary network validator with no BLS key
	nodeID := ids.GenerateTestNodeID()

	primaryTx1, err := wallet.IssueAddValidatorTx(
		&txs.Validator{
			NodeID: nodeID,
			Start:  uint64(primaryStartTime1.Unix()),
			End:    uint64(primaryEndTime1.Unix()),
			Wght:   vm.MinValidatorStake,
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		},
		reward.PercentDenominator,
	)
	require.NoError(err)

	lastAcceptedID, err := vm.LastAccepted(t.Context())
	require.NoError(err)
	lastAccepted, err := vm.GetBlock(t.Context(), lastAcceptedID)
	require.NoError(err)

	statelessBlk0, err := block.NewBanffStandardBlock(
		lastAccepted.Timestamp().Add(time.Second),
		lastAccepted.ID(),
		lastAccepted.Height()+1,
		[]*txs.Tx{primaryTx1},
	)
	require.NoError(err)

	blk0, err := vm.ParseBlock(t.Context(), statelessBlk0.Bytes())
	require.NoError(err)
	require.NoError(blk0.Verify(t.Context()))
	require.NoError(blk0.Accept(t.Context()))
	require.NoError(vm.SetPreference(t.Context(), blk0.ID()))

	// move time ahead, promoting primary validator to current
	vm.clock.Set(primaryStartTime1)
	require.NoError(buildAndAcceptStandardBlock(vm))

	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)

	// insert the subnet validator
	subnetTx, err := wallet.IssueAddSubnetValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(subnetStartTime.Unix()),
				End:    uint64(subnetEndTime.Unix()),
				Wght:   1,
			},
			Subnet: subnetID,
		},
	)
	require.NoError(err)

	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(subnetTx))
	vm.ctx.Lock.Lock()
	require.NoError(buildAndAcceptStandardBlock(vm))

	// move time ahead, promoting the subnet validator to current
	vm.clock.Set(subnetStartTime)
	require.NoError(buildAndAcceptStandardBlock(vm))

	_, err = vm.state.GetCurrentValidator(subnetID, nodeID)
	require.NoError(err)

	subnetStartHeight, err := vm.GetCurrentHeight(t.Context())
	require.NoError(err)

	// move time ahead, terminating the subnet validator
	vm.clock.Set(subnetEndTime)
	require.NoError(buildAndAcceptStandardBlock(vm))

	_, err = vm.state.GetCurrentValidator(subnetID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	// move time ahead, terminating primary network validator
	vm.clock.Set(primaryEndTime1)
	blk1, err := vm.Builder.BuildBlock(t.Context()) // must be a proposal block rewarding the primary validator
	require.NoError(err)
	require.NoError(blk1.Verify(t.Context()))

	proposalBlk := blk1.(snowman.OracleBlock)
	options, err := proposalBlk.Options(t.Context())
	require.NoError(err)

	commit := options[0].(*blockexecutor.Block)
	require.IsType(&block.BanffCommitBlock{}, commit.Block)

	require.NoError(blk1.Accept(t.Context()))
	require.NoError(commit.Verify(t.Context()))
	require.NoError(commit.Accept(t.Context()))
	require.NoError(vm.SetPreference(t.Context(), vm.manager.LastAccepted()))

	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	// Generating the validator set should not error when re-introducing a
	// subnet validator whose primary network validator was also removed.
	_, err = vm.State.GetValidatorSet(t.Context(), subnetStartHeight, subnetID)
	require.NoError(err)
}

func TestValidatorSetRaceCondition(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, upgradetest.Cortina)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	nodeID := ids.GenerateTestNodeID()
	require.NoError(vm.Connected(t.Context(), nodeID, version.Current))

	protocolAppRequestBytest, err := gossip.MarshalAppRequest(
		bloom.EmptyFilter.Marshal(),
		ids.Empty[:],
	)
	require.NoError(err)

	appRequestBytes := p2p.PrefixMessage(
		p2p.ProtocolPrefix(p2p.TxGossipHandlerID),
		protocolAppRequestBytest,
	)

	var (
		eg          errgroup.Group
		ctx, cancel = context.WithCancel(t.Context())
	)
	// keep 10 workers running
	for i := 0; i < 10; i++ {
		eg.Go(func() error {
			for ctx.Err() == nil {
				err := vm.AppRequest(
					t.Context(),
					nodeID,
					0,
					time.Now().Add(time.Hour),
					appRequestBytes,
				)
				if err != nil {
					return err
				}
			}
			return nil
		})
	}

	// If the validator set lock isn't held, the race detector should fail here.
	for i := uint64(0); i < 1000; i++ {
		blk, err := block.NewBanffStandardBlock(
			time.Now(),
			vm.state.GetLastAccepted(),
			i,
			nil,
		)
		require.NoError(err)

		vm.state.SetLastAccepted(blk.ID())
		vm.state.SetHeight(blk.Height())
		vm.state.AddStatelessBlock(blk)
	}

	// If the validator set lock is grabbed, we need to make sure to release the
	// lock to avoid a deadlock.
	vm.ctx.Lock.Unlock()
	cancel() // stop and wait for workers
	require.NoError(eg.Wait())
	vm.ctx.Lock.Lock()
}

func TestBanffStandardBlockWithNoChangesRemainsInvalid(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, upgradetest.Etna)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	lastAcceptedID, err := vm.LastAccepted(t.Context())
	require.NoError(err)

	lastAccepted, err := vm.GetBlock(t.Context(), lastAcceptedID)
	require.NoError(err)

	statelessBlk, err := block.NewBanffStandardBlock(
		lastAccepted.Timestamp(),
		lastAcceptedID,
		lastAccepted.Height()+1,
		nil,
	)
	require.NoError(err)

	blk, err := vm.ParseBlock(t.Context(), statelessBlk.Bytes())
	require.NoError(err)

	for range 2 {
		err = blk.Verify(t.Context())
		require.ErrorIs(err, blockexecutor.ErrStandardBlockWithoutChanges)
	}
}

func buildAndAcceptStandardBlock(vm *VM) error {
	blk, err := vm.Builder.BuildBlock(context.Background())
	if err != nil {
		return err
	}

	if err := blk.Verify(context.Background()); err != nil {
		return err
	}

	if err := blk.Accept(context.Background()); err != nil {
		return err
	}

	return vm.SetPreference(context.Background(), vm.manager.LastAccepted())
}

func checkValidatorBlsKeyIsSet(
	valState validators.State,
	nodeID ids.NodeID,
	subnetID ids.ID,
	height uint64,
	expectedBlsKey *bls.PublicKey,
) error {
	vals, err := valState.GetValidatorSet(context.Background(), height, subnetID)
	if err != nil {
		return err
	}

	val, found := vals[nodeID]
	switch {
	case !found:
		return database.ErrNotFound
	case expectedBlsKey == val.PublicKey:
		return nil
	case expectedBlsKey == nil && val.PublicKey != nil:
		return errors.New("unexpected BLS key")
	case expectedBlsKey != nil && val.PublicKey == nil:
		return errors.New("missing BLS key")
	case !bytes.Equal(bls.PublicKeyToUncompressedBytes(expectedBlsKey), bls.PublicKeyToUncompressedBytes(val.PublicKey)):
		return errors.New("incorrect BLS key")
	default:
		return nil
	}
}
