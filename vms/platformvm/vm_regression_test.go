// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	txexecutor "github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

func TestAddDelegatorTxOverDelegatedRegression(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		vm.ctx.Lock.Unlock()
	}()

	validatorStartTime := vm.clock.Time().Add(txexecutor.SyncBound).Add(1 * time.Second)
	validatorEndTime := validatorStartTime.Add(360 * 24 * time.Hour)

	nodeID := ids.GenerateTestNodeID()
	changeAddr := keys[0].PublicKey().Address()

	// create valid tx
	addValidatorTx, err := vm.txBuilder.NewAddValidatorTx(
		vm.MinValidatorStake,
		uint64(validatorStartTime.Unix()),
		uint64(validatorEndTime.Unix()),
		nodeID,
		changeAddr,
		reward.PercentDenominator,
		[]*secp256k1.PrivateKey{keys[0]},
		changeAddr,
	)
	require.NoError(err)

	// trigger block creation
	require.NoError(vm.Builder.AddUnverifiedTx(addValidatorTx))

	addValidatorBlock, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(addValidatorBlock.Verify(context.Background()))
	require.NoError(addValidatorBlock.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	vm.clock.Set(validatorStartTime)

	firstAdvanceTimeBlock, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(firstAdvanceTimeBlock.Verify(context.Background()))
	require.NoError(firstAdvanceTimeBlock.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	firstDelegatorStartTime := validatorStartTime.Add(txexecutor.SyncBound).Add(1 * time.Second)
	firstDelegatorEndTime := firstDelegatorStartTime.Add(vm.MinStakeDuration)

	// create valid tx
	addFirstDelegatorTx, err := vm.txBuilder.NewAddDelegatorTx(
		4*vm.MinValidatorStake, // maximum amount of stake this delegator can provide
		uint64(firstDelegatorStartTime.Unix()),
		uint64(firstDelegatorEndTime.Unix()),
		nodeID,
		changeAddr,
		[]*secp256k1.PrivateKey{keys[0], keys[1]},
		changeAddr,
	)
	require.NoError(err)

	// trigger block creation
	require.NoError(vm.Builder.AddUnverifiedTx(addFirstDelegatorTx))

	addFirstDelegatorBlock, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(addFirstDelegatorBlock.Verify(context.Background()))
	require.NoError(addFirstDelegatorBlock.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	vm.clock.Set(firstDelegatorStartTime)

	secondAdvanceTimeBlock, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(secondAdvanceTimeBlock.Verify(context.Background()))
	require.NoError(secondAdvanceTimeBlock.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	secondDelegatorStartTime := firstDelegatorEndTime.Add(2 * time.Second)
	secondDelegatorEndTime := secondDelegatorStartTime.Add(vm.MinStakeDuration)

	vm.clock.Set(secondDelegatorStartTime.Add(-10 * txexecutor.SyncBound))

	// create valid tx
	addSecondDelegatorTx, err := vm.txBuilder.NewAddDelegatorTx(
		vm.MinDelegatorStake,
		uint64(secondDelegatorStartTime.Unix()),
		uint64(secondDelegatorEndTime.Unix()),
		nodeID,
		changeAddr,
		[]*secp256k1.PrivateKey{keys[0], keys[1], keys[3]},
		changeAddr,
	)
	require.NoError(err)

	// trigger block creation
	require.NoError(vm.Builder.AddUnverifiedTx(addSecondDelegatorTx))

	addSecondDelegatorBlock, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(addSecondDelegatorBlock.Verify(context.Background()))
	require.NoError(addSecondDelegatorBlock.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	thirdDelegatorStartTime := firstDelegatorEndTime.Add(-time.Second)
	thirdDelegatorEndTime := thirdDelegatorStartTime.Add(vm.MinStakeDuration)

	// create valid tx
	addThirdDelegatorTx, err := vm.txBuilder.NewAddDelegatorTx(
		vm.MinDelegatorStake,
		uint64(thirdDelegatorStartTime.Unix()),
		uint64(thirdDelegatorEndTime.Unix()),
		nodeID,
		changeAddr,
		[]*secp256k1.PrivateKey{keys[0], keys[1], keys[4]},
		changeAddr,
	)
	require.NoError(err)

	// trigger block creation
	err = vm.Builder.AddUnverifiedTx(addThirdDelegatorTx)
	require.Error(err, "should have marked the delegator as being over delegated")
}

func TestAddDelegatorTxHeapCorruption(t *testing.T) {
	validatorStartTime := banffForkTime.Add(txexecutor.SyncBound).Add(1 * time.Second)
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
		name       string
		ap3Time    time.Time
		shouldFail bool
	}{
		{
			name:       "pre-upgrade is no longer restrictive",
			ap3Time:    validatorEndTime,
			shouldFail: false,
		},
		{
			name:       "post-upgrade calculate max stake correctly",
			ap3Time:    defaultGenesisTime,
			shouldFail: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			vm, _, _ := defaultVM()
			vm.ApricotPhase3Time = test.ap3Time

			vm.ctx.Lock.Lock()
			defer func() {
				err := vm.Shutdown(context.Background())
				require.NoError(err)

				vm.ctx.Lock.Unlock()
			}()

			key, err := testKeyFactory.NewPrivateKey()
			require.NoError(err)

			id := key.PublicKey().Address()
			changeAddr := keys[0].PublicKey().Address()

			// create valid tx
			addValidatorTx, err := vm.txBuilder.NewAddValidatorTx(
				validatorStake,
				uint64(validatorStartTime.Unix()),
				uint64(validatorEndTime.Unix()),
				ids.NodeID(id),
				id,
				reward.PercentDenominator,
				[]*secp256k1.PrivateKey{keys[0], keys[1]},
				changeAddr,
			)
			require.NoError(err)

			// issue the add validator tx
			err = vm.Builder.AddUnverifiedTx(addValidatorTx)
			require.NoError(err)

			// trigger block creation for the validator tx
			addValidatorBlock, err := vm.Builder.BuildBlock(context.Background())
			require.NoError(err)
			require.NoError(addValidatorBlock.Verify(context.Background()))
			require.NoError(addValidatorBlock.Accept(context.Background()))
			require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

			// create valid tx
			addFirstDelegatorTx, err := vm.txBuilder.NewAddDelegatorTx(
				delegator1Stake,
				uint64(delegator1StartTime.Unix()),
				uint64(delegator1EndTime.Unix()),
				ids.NodeID(id),
				keys[0].PublicKey().Address(),
				[]*secp256k1.PrivateKey{keys[0], keys[1]},
				changeAddr,
			)
			require.NoError(err)

			// issue the first add delegator tx
			err = vm.Builder.AddUnverifiedTx(addFirstDelegatorTx)
			require.NoError(err)

			// trigger block creation for the first add delegator tx
			addFirstDelegatorBlock, err := vm.Builder.BuildBlock(context.Background())
			require.NoError(err)
			require.NoError(addFirstDelegatorBlock.Verify(context.Background()))
			require.NoError(addFirstDelegatorBlock.Accept(context.Background()))
			require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

			// create valid tx
			addSecondDelegatorTx, err := vm.txBuilder.NewAddDelegatorTx(
				delegator2Stake,
				uint64(delegator2StartTime.Unix()),
				uint64(delegator2EndTime.Unix()),
				ids.NodeID(id),
				keys[0].PublicKey().Address(),
				[]*secp256k1.PrivateKey{keys[0], keys[1]},
				changeAddr,
			)
			require.NoError(err)

			// issue the second add delegator tx
			err = vm.Builder.AddUnverifiedTx(addSecondDelegatorTx)
			require.NoError(err)

			// trigger block creation for the second add delegator tx
			addSecondDelegatorBlock, err := vm.Builder.BuildBlock(context.Background())
			require.NoError(err)
			require.NoError(addSecondDelegatorBlock.Verify(context.Background()))
			require.NoError(addSecondDelegatorBlock.Accept(context.Background()))
			require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

			// create valid tx
			addThirdDelegatorTx, err := vm.txBuilder.NewAddDelegatorTx(
				delegator3Stake,
				uint64(delegator3StartTime.Unix()),
				uint64(delegator3EndTime.Unix()),
				ids.NodeID(id),
				keys[0].PublicKey().Address(),
				[]*secp256k1.PrivateKey{keys[0], keys[1]},
				changeAddr,
			)
			require.NoError(err)

			// issue the third add delegator tx
			err = vm.Builder.AddUnverifiedTx(addThirdDelegatorTx)
			require.NoError(err)

			// trigger block creation for the third add delegator tx
			addThirdDelegatorBlock, err := vm.Builder.BuildBlock(context.Background())
			require.NoError(err)
			require.NoError(addThirdDelegatorBlock.Verify(context.Background()))
			require.NoError(addThirdDelegatorBlock.Accept(context.Background()))
			require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

			// create valid tx
			addFourthDelegatorTx, err := vm.txBuilder.NewAddDelegatorTx(
				delegator4Stake,
				uint64(delegator4StartTime.Unix()),
				uint64(delegator4EndTime.Unix()),
				ids.NodeID(id),
				keys[0].PublicKey().Address(),
				[]*secp256k1.PrivateKey{keys[0], keys[1]},
				changeAddr,
			)
			require.NoError(err)

			// issue the fourth add delegator tx
			err = vm.Builder.AddUnverifiedTx(addFourthDelegatorTx)
			require.NoError(err)

			// trigger block creation for the fourth add delegator tx
			addFourthDelegatorBlock, err := vm.Builder.BuildBlock(context.Background())

			if test.shouldFail {
				require.Error(err, "should have failed to allow new delegator")
				return
			}

			require.NoError(err)
			require.NoError(addFourthDelegatorBlock.Verify(context.Background()))
			require.NoError(addFourthDelegatorBlock.Accept(context.Background()))
			require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))
		})
	}
}

// Test that calling Verify on a block with an unverified parent doesn't cause a
// panic.
func TestUnverifiedParentPanicRegression(t *testing.T) {
	require := require.New(t)
	_, genesisBytes := defaultGenesis()

	baseDBManager := manager.NewMemDB(version.Semantic1_0_0)
	atomicDB := prefixdb.New([]byte{1}, baseDBManager.Current().Database)

	vdrs := validators.NewManager()
	primaryVdrs := validators.NewSet()
	_ = vdrs.Add(constants.PrimaryNetworkID, primaryVdrs)
	vm := &VM{Factory: Factory{
		Config: config.Config{
			Chains:                 chains.TestManager,
			Validators:             vdrs,
			UptimeLockedCalculator: uptime.NewLockedCalculator(),
			MinStakeDuration:       defaultMinStakingDuration,
			MaxStakeDuration:       defaultMaxStakingDuration,
			RewardConfig:           defaultRewardConfig,
			BanffTime:              banffForkTime,
		},
	}}

	ctx := defaultContext()
	ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()

	msgChan := make(chan common.Message, 1)
	err := vm.Initialize(
		context.Background(),
		ctx,
		baseDBManager,
		genesisBytes,
		nil,
		nil,
		msgChan,
		nil,
		nil,
	)
	require.NoError(err)

	m := atomic.NewMemory(atomicDB)
	vm.ctx.SharedMemory = m.NewSharedMemory(ctx.ChainID)

	// set time to post Banff fork
	vm.clock.Set(banffForkTime.Add(time.Second))
	vm.state.SetTimestamp(banffForkTime.Add(time.Second))

	key0 := keys[0]
	key1 := keys[1]
	addr0 := key0.PublicKey().Address()
	addr1 := key1.PublicKey().Address()

	addSubnetTx0, err := vm.txBuilder.NewCreateSubnetTx(
		1,
		[]ids.ShortID{addr0},
		[]*secp256k1.PrivateKey{key0},
		addr0,
	)
	require.NoError(err)

	addSubnetTx1, err := vm.txBuilder.NewCreateSubnetTx(
		1,
		[]ids.ShortID{addr1},
		[]*secp256k1.PrivateKey{key1},
		addr1,
	)
	require.NoError(err)

	addSubnetTx2, err := vm.txBuilder.NewCreateSubnetTx(
		1,
		[]ids.ShortID{addr1},
		[]*secp256k1.PrivateKey{key1},
		addr0,
	)
	require.NoError(err)

	preferred, err := vm.Builder.Preferred()
	require.NoError(err)

	preferredChainTime := preferred.Timestamp()
	preferredID := preferred.ID()
	preferredHeight := preferred.Height()

	statelessStandardBlk, err := blocks.NewBanffStandardBlock(
		preferredChainTime,
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{addSubnetTx0},
	)
	require.NoError(err)
	addSubnetBlk0 := vm.manager.NewBlock(statelessStandardBlk)

	statelessStandardBlk, err = blocks.NewBanffStandardBlock(
		preferredChainTime,
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{addSubnetTx1},
	)
	require.NoError(err)
	addSubnetBlk1 := vm.manager.NewBlock(statelessStandardBlk)

	statelessStandardBlk, err = blocks.NewBanffStandardBlock(
		preferredChainTime,
		addSubnetBlk1.ID(),
		preferredHeight+2,
		[]*txs.Tx{addSubnetTx2},
	)
	require.NoError(err)
	addSubnetBlk2 := vm.manager.NewBlock(statelessStandardBlk)

	_, err = vm.ParseBlock(context.Background(), addSubnetBlk0.Bytes())
	require.NoError(err)

	_, err = vm.ParseBlock(context.Background(), addSubnetBlk1.Bytes())
	require.NoError(err)

	_, err = vm.ParseBlock(context.Background(), addSubnetBlk2.Bytes())
	require.NoError(err)

	require.NoError(addSubnetBlk0.Verify(context.Background()))
	require.NoError(addSubnetBlk0.Accept(context.Background()))

	// Doesn't matter what verify returns as long as it's not panicking.
	_ = addSubnetBlk2.Verify(context.Background())
}

func TestRejectedStateRegressionInvalidValidatorTimestamp(t *testing.T) {
	require := require.New(t)

	vm, baseDB, mutableSharedMemory := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		err := vm.Shutdown(context.Background())
		require.NoError(err)

		vm.ctx.Lock.Unlock()
	}()

	newValidatorStartTime := vm.clock.Time().Add(txexecutor.SyncBound).Add(1 * time.Second)
	newValidatorEndTime := newValidatorStartTime.Add(defaultMinStakingDuration)

	key, err := testKeyFactory.NewPrivateKey()
	require.NoError(err)

	nodeID := ids.NodeID(key.PublicKey().Address())

	// Create the tx to add a new validator
	addValidatorTx, err := vm.txBuilder.NewAddValidatorTx(
		vm.MinValidatorStake,
		uint64(newValidatorStartTime.Unix()),
		uint64(newValidatorEndTime.Unix()),
		nodeID,
		ids.ShortID(nodeID),
		reward.PercentDenominator,
		[]*secp256k1.PrivateKey{keys[0]},
		ids.ShortEmpty,
	)
	require.NoError(err)

	// Create the standard block to add the new validator
	preferred, err := vm.Builder.Preferred()
	require.NoError(err)

	preferredChainTime := preferred.Timestamp()
	preferredID := preferred.ID()
	preferredHeight := preferred.Height()

	statelessBlk, err := blocks.NewBanffStandardBlock(
		preferredChainTime,
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{addValidatorTx},
	)
	require.NoError(err)

	addValidatorStandardBlk := vm.manager.NewBlock(statelessBlk)
	err = addValidatorStandardBlk.Verify(context.Background())
	require.NoError(err)

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
			Amt:          vm.TxFee,
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
					Amt: vm.TxFee,
				},
			},
		},
	}
	signedImportTx := &txs.Tx{Unsigned: unsignedImportTx}
	err = signedImportTx.Sign(txs.Codec, [][]*secp256k1.PrivateKey{
		{}, // There is one input, with no required signers
	})
	require.NoError(err)

	// Create the standard block that will fail verification, and then be
	// re-verified.
	preferredChainTime = addValidatorStandardBlk.Timestamp()
	preferredID = addValidatorStandardBlk.ID()
	preferredHeight = addValidatorStandardBlk.Height()

	statelessImportBlk, err := blocks.NewBanffStandardBlock(
		preferredChainTime,
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{signedImportTx},
	)
	require.NoError(err)

	importBlk := vm.manager.NewBlock(statelessImportBlk)

	// Because the shared memory UTXO hasn't been populated, this block is
	// currently invalid.
	err = importBlk.Verify(context.Background())
	require.Error(err)

	// Because we no longer ever reject a block in verification, the status
	// should remain as processing.
	importBlkStatus := importBlk.Status()
	require.Equal(choices.Processing, importBlkStatus)

	// Populate the shared memory UTXO.
	m := atomic.NewMemory(prefixdb.New([]byte{5}, baseDB))

	mutableSharedMemory.SharedMemory = m.NewSharedMemory(vm.ctx.ChainID)
	peerSharedMemory := m.NewSharedMemory(vm.ctx.XChainID)

	utxoBytes, err := txs.Codec.Marshal(txs.Version, utxo)
	require.NoError(err)

	inputID := utxo.InputID()
	err = peerSharedMemory.Apply(
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
	)
	require.NoError(err)

	// Because the shared memory UTXO has now been populated, the block should
	// pass verification.
	err = importBlk.Verify(context.Background())
	require.NoError(err)

	// The status shouldn't have been changed during a successful verification.
	importBlkStatus = importBlk.Status()
	require.Equal(choices.Processing, importBlkStatus)

	// Move chain time ahead to bring the new validator from the pending
	// validator set into the current validator set.
	vm.clock.Set(newValidatorStartTime)

	// Create the proposal block that should have moved the new validator from
	// the pending validator set into the current validator set.
	preferredID = importBlk.ID()
	preferredHeight = importBlk.Height()

	statelessAdvanceTimeStandardBlk, err := blocks.NewBanffStandardBlock(
		newValidatorStartTime,
		preferredID,
		preferredHeight+1,
		nil,
	)
	require.NoError(err)

	advanceTimeStandardBlk := vm.manager.NewBlock(statelessAdvanceTimeStandardBlk)
	err = advanceTimeStandardBlk.Verify(context.Background())
	require.NoError(err)

	// Accept all the blocks
	allBlocks := []snowman.Block{
		addValidatorStandardBlk,
		importBlk,
		advanceTimeStandardBlk,
	}
	for _, blk := range allBlocks {
		err = blk.Accept(context.Background())
		require.NoError(err)

		status := blk.Status()
		require.Equal(choices.Accepted, status)
	}

	// Force a reload of the state from the database.
	vm.Config.Validators = validators.NewManager()
	vm.Config.Validators.Add(constants.PrimaryNetworkID, validators.NewSet())
	is, err := state.New(
		vm.dbManager.Current().Database,
		nil,
		prometheus.NewRegistry(),
		&vm.Config,
		vm.ctx,
		metrics.Noop,
		reward.NewCalculator(vm.Config.RewardConfig),
	)
	require.NoError(err)
	vm.state = is

	// Verify that new validator is now in the current validator set.
	{
		_, err := vm.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
		require.NoError(err)

		_, err = vm.state.GetPendingValidator(constants.PrimaryNetworkID, nodeID)
		require.ErrorIs(err, database.ErrNotFound)

		currentTimestamp := vm.state.GetTimestamp()
		require.Equal(newValidatorStartTime.Unix(), currentTimestamp.Unix())
	}
}

func TestRejectedStateRegressionInvalidValidatorReward(t *testing.T) {
	require := require.New(t)

	vm, baseDB, mutableSharedMemory := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		err := vm.Shutdown(context.Background())
		require.NoError(err)

		vm.ctx.Lock.Unlock()
	}()

	vm.state.SetCurrentSupply(constants.PrimaryNetworkID, defaultRewardConfig.SupplyCap/2)

	newValidatorStartTime0 := vm.clock.Time().Add(txexecutor.SyncBound).Add(1 * time.Second)
	newValidatorEndTime0 := newValidatorStartTime0.Add(defaultMaxStakingDuration)

	nodeID0 := ids.NodeID(ids.GenerateTestShortID())

	// Create the tx to add the first new validator
	addValidatorTx0, err := vm.txBuilder.NewAddValidatorTx(
		vm.MaxValidatorStake,
		uint64(newValidatorStartTime0.Unix()),
		uint64(newValidatorEndTime0.Unix()),
		nodeID0,
		ids.ShortID(nodeID0),
		reward.PercentDenominator,
		[]*secp256k1.PrivateKey{keys[0]},
		ids.ShortEmpty,
	)
	require.NoError(err)

	// Create the standard block to add the first new validator
	preferred, err := vm.Builder.Preferred()
	require.NoError(err)

	preferredChainTime := preferred.Timestamp()
	preferredID := preferred.ID()
	preferredHeight := preferred.Height()

	statelessAddValidatorStandardBlk0, err := blocks.NewBanffStandardBlock(
		preferredChainTime,
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{addValidatorTx0},
	)
	require.NoError(err)

	addValidatorStandardBlk0 := vm.manager.NewBlock(statelessAddValidatorStandardBlk0)
	err = addValidatorStandardBlk0.Verify(context.Background())
	require.NoError(err)

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

	statelessAdvanceTimeStandardBlk0, err := blocks.NewBanffStandardBlock(
		newValidatorStartTime0,
		preferredID,
		preferredHeight+1,
		nil,
	)
	require.NoError(err)

	advanceTimeStandardBlk0 := vm.manager.NewBlock(statelessAdvanceTimeStandardBlk0)
	err = advanceTimeStandardBlk0.Verify(context.Background())
	require.NoError(err)

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
			Amt:          vm.TxFee,
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
					Amt: vm.TxFee,
				},
			},
		},
	}
	signedImportTx := &txs.Tx{Unsigned: unsignedImportTx}
	err = signedImportTx.Sign(txs.Codec, [][]*secp256k1.PrivateKey{
		{}, // There is one input, with no required signers
	})
	require.NoError(err)

	// Create the standard block that will fail verification, and then be
	// re-verified.
	preferredChainTime = advanceTimeStandardBlk0.Timestamp()
	preferredID = advanceTimeStandardBlk0.ID()
	preferredHeight = advanceTimeStandardBlk0.Height()

	statelessImportBlk, err := blocks.NewBanffStandardBlock(
		preferredChainTime,
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{signedImportTx},
	)
	require.NoError(err)

	importBlk := vm.manager.NewBlock(statelessImportBlk)
	// Because the shared memory UTXO hasn't been populated, this block is
	// currently invalid.
	err = importBlk.Verify(context.Background())
	require.Error(err)

	// Because we no longer ever reject a block in verification, the status
	// should remain as processing.
	importBlkStatus := importBlk.Status()
	require.Equal(choices.Processing, importBlkStatus)

	// Populate the shared memory UTXO.
	m := atomic.NewMemory(prefixdb.New([]byte{5}, baseDB))

	mutableSharedMemory.SharedMemory = m.NewSharedMemory(vm.ctx.ChainID)
	peerSharedMemory := m.NewSharedMemory(vm.ctx.XChainID)

	utxoBytes, err := txs.Codec.Marshal(txs.Version, utxo)
	require.NoError(err)

	inputID := utxo.InputID()
	err = peerSharedMemory.Apply(
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
	)
	require.NoError(err)

	// Because the shared memory UTXO has now been populated, the block should
	// pass verification.
	err = importBlk.Verify(context.Background())
	require.NoError(err)

	// The status shouldn't have been changed during a successful verification.
	importBlkStatus = importBlk.Status()
	require.Equal(choices.Processing, importBlkStatus)

	newValidatorStartTime1 := newValidatorStartTime0.Add(txexecutor.SyncBound).Add(1 * time.Second)
	newValidatorEndTime1 := newValidatorStartTime1.Add(defaultMaxStakingDuration)

	nodeID1 := ids.NodeID(ids.GenerateTestShortID())

	// Create the tx to add the second new validator
	addValidatorTx1, err := vm.txBuilder.NewAddValidatorTx(
		vm.MaxValidatorStake,
		uint64(newValidatorStartTime1.Unix()),
		uint64(newValidatorEndTime1.Unix()),
		nodeID1,
		ids.ShortID(nodeID1),
		reward.PercentDenominator,
		[]*secp256k1.PrivateKey{keys[1]},
		ids.ShortEmpty,
	)
	require.NoError(err)

	// Create the standard block to add the second new validator
	preferredChainTime = importBlk.Timestamp()
	preferredID = importBlk.ID()
	preferredHeight = importBlk.Height()

	statelessAddValidatorStandardBlk1, err := blocks.NewBanffStandardBlock(
		preferredChainTime,
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{addValidatorTx1},
	)
	require.NoError(err)

	addValidatorStandardBlk1 := vm.manager.NewBlock(statelessAddValidatorStandardBlk1)

	err = addValidatorStandardBlk1.Verify(context.Background())
	require.NoError(err)

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

	statelessAdvanceTimeStandardBlk1, err := blocks.NewBanffStandardBlock(
		newValidatorStartTime1,
		preferredID,
		preferredHeight+1,
		nil,
	)
	require.NoError(err)

	advanceTimeStandardBlk1 := vm.manager.NewBlock(statelessAdvanceTimeStandardBlk1)
	err = advanceTimeStandardBlk1.Verify(context.Background())
	require.NoError(err)

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
		err = blk.Accept(context.Background())
		require.NoError(err)

		status := blk.Status()
		require.Equal(choices.Accepted, status)
	}

	// Force a reload of the state from the database.
	vm.Config.Validators = validators.NewManager()
	vm.Config.Validators.Add(constants.PrimaryNetworkID, validators.NewSet())
	is, err := state.New(
		vm.dbManager.Current().Database,
		nil,
		prometheus.NewRegistry(),
		&vm.Config,
		vm.ctx,
		metrics.Noop,
		reward.NewCalculator(vm.Config.RewardConfig),
	)
	require.NoError(err)
	vm.state = is

	// Verify that validators are in the current validator set with the correct
	// reward calculated.
	{
		staker0, err := vm.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID0)
		require.NoError(err)
		require.EqualValues(60000000, staker0.PotentialReward)

		staker1, err := vm.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID1)
		require.NoError(err)
		require.EqualValues(59999999, staker1.PotentialReward)

		_, err = vm.state.GetPendingValidator(constants.PrimaryNetworkID, nodeID0)
		require.ErrorIs(err, database.ErrNotFound)

		_, err = vm.state.GetPendingValidator(constants.PrimaryNetworkID, nodeID1)
		require.ErrorIs(err, database.ErrNotFound)

		currentTimestamp := vm.state.GetTimestamp()
		require.Equal(newValidatorStartTime1.Unix(), currentTimestamp.Unix())
	}
}

func TestValidatorSetAtCacheOverwriteRegression(t *testing.T) {
	require := require.New(t)

	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		err := vm.Shutdown(context.Background())
		require.NoError(err)

		vm.ctx.Lock.Unlock()
	}()

	nodeID0 := ids.NodeID(keys[0].PublicKey().Address())
	nodeID1 := ids.NodeID(keys[1].PublicKey().Address())
	nodeID2 := ids.NodeID(keys[2].PublicKey().Address())
	nodeID3 := ids.NodeID(keys[3].PublicKey().Address())
	nodeID4 := ids.NodeID(keys[4].PublicKey().Address())

	currentHeight, err := vm.GetCurrentHeight(context.Background())
	require.NoError(err)
	require.EqualValues(1, currentHeight)

	expectedValidators1 := map[ids.NodeID]uint64{
		nodeID0: defaultWeight,
		nodeID1: defaultWeight,
		nodeID2: defaultWeight,
		nodeID3: defaultWeight,
		nodeID4: defaultWeight,
	}
	validators, err := vm.GetValidatorSet(context.Background(), 1, constants.PrimaryNetworkID)
	require.NoError(err)
	for nodeID, weight := range expectedValidators1 {
		require.Equal(weight, validators[nodeID].Weight)
	}

	newValidatorStartTime0 := vm.clock.Time().Add(txexecutor.SyncBound).Add(1 * time.Second)
	newValidatorEndTime0 := newValidatorStartTime0.Add(defaultMaxStakingDuration)

	nodeID5 := ids.GenerateTestNodeID()

	// Create the tx to add the first new validator
	addValidatorTx0, err := vm.txBuilder.NewAddValidatorTx(
		vm.MaxValidatorStake,
		uint64(newValidatorStartTime0.Unix()),
		uint64(newValidatorEndTime0.Unix()),
		nodeID5,
		ids.GenerateTestShortID(),
		reward.PercentDenominator,
		[]*secp256k1.PrivateKey{keys[0]},
		ids.GenerateTestShortID(),
	)
	require.NoError(err)

	// Create the standard block to add the first new validator
	preferred, err := vm.Builder.Preferred()
	require.NoError(err)

	preferredChainTime := preferred.Timestamp()
	preferredID := preferred.ID()
	preferredHeight := preferred.Height()

	statelessStandardBlk, err := blocks.NewBanffStandardBlock(
		preferredChainTime,
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{addValidatorTx0},
	)
	require.NoError(err)
	addValidatorProposalBlk0 := vm.manager.NewBlock(statelessStandardBlk)
	require.NoError(addValidatorProposalBlk0.Verify(context.Background()))
	require.NoError(addValidatorProposalBlk0.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	currentHeight, err = vm.GetCurrentHeight(context.Background())
	require.NoError(err)
	require.EqualValues(2, currentHeight)

	for i := uint64(1); i <= 2; i++ {
		validators, err = vm.GetValidatorSet(context.Background(), i, constants.PrimaryNetworkID)
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
	preferred, err = vm.Builder.Preferred()
	require.NoError(err)
	preferredID = preferred.ID()
	preferredHeight = preferred.Height()

	statelessStandardBlk, err = blocks.NewBanffStandardBlock(
		newValidatorStartTime0,
		preferredID,
		preferredHeight+1,
		nil,
	)
	require.NoError(err)
	advanceTimeProposalBlk0 := vm.manager.NewBlock(statelessStandardBlk)
	require.NoError(advanceTimeProposalBlk0.Verify(context.Background()))
	require.NoError(advanceTimeProposalBlk0.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	currentHeight, err = vm.GetCurrentHeight(context.Background())
	require.NoError(err)
	require.EqualValues(3, currentHeight)

	for i := uint64(1); i <= 2; i++ {
		validators, err = vm.GetValidatorSet(context.Background(), i, constants.PrimaryNetworkID)
		require.NoError(err)
		for nodeID, weight := range expectedValidators1 {
			require.Equal(weight, validators[nodeID].Weight)
		}
	}

	expectedValidators2 := map[ids.NodeID]uint64{
		nodeID0: defaultWeight,
		nodeID1: defaultWeight,
		nodeID2: defaultWeight,
		nodeID3: defaultWeight,
		nodeID4: defaultWeight,
		nodeID5: vm.MaxValidatorStake,
	}
	validators, err = vm.GetValidatorSet(context.Background(), 3, constants.PrimaryNetworkID)
	require.NoError(err)
	for nodeID, weight := range expectedValidators2 {
		require.Equal(weight, validators[nodeID].Weight)
	}
}

func TestAddDelegatorTxAddBeforeRemove(t *testing.T) {
	require := require.New(t)

	validatorStartTime := banffForkTime.Add(txexecutor.SyncBound).Add(1 * time.Second)
	validatorEndTime := validatorStartTime.Add(360 * 24 * time.Hour)
	validatorStake := defaultMaxValidatorStake / 5

	delegator1StartTime := validatorStartTime
	delegator1EndTime := delegator1StartTime.Add(3 * defaultMinStakingDuration)
	delegator1Stake := defaultMaxValidatorStake - validatorStake

	delegator2StartTime := delegator1EndTime
	delegator2EndTime := delegator2StartTime.Add(3 * defaultMinStakingDuration)
	delegator2Stake := defaultMaxValidatorStake - validatorStake

	vm, _, _ := defaultVM()

	vm.ctx.Lock.Lock()
	defer func() {
		err := vm.Shutdown(context.Background())
		require.NoError(err)

		vm.ctx.Lock.Unlock()
	}()

	key, err := testKeyFactory.NewPrivateKey()
	require.NoError(err)

	id := key.PublicKey().Address()
	changeAddr := keys[0].PublicKey().Address()

	// create valid tx
	addValidatorTx, err := vm.txBuilder.NewAddValidatorTx(
		validatorStake,
		uint64(validatorStartTime.Unix()),
		uint64(validatorEndTime.Unix()),
		ids.NodeID(id),
		id,
		reward.PercentDenominator,
		[]*secp256k1.PrivateKey{keys[0], keys[1]},
		changeAddr,
	)
	require.NoError(err)

	// issue the add validator tx
	err = vm.Builder.AddUnverifiedTx(addValidatorTx)
	require.NoError(err)

	// trigger block creation for the validator tx
	addValidatorBlock, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(addValidatorBlock.Verify(context.Background()))
	require.NoError(addValidatorBlock.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	// create valid tx
	addFirstDelegatorTx, err := vm.txBuilder.NewAddDelegatorTx(
		delegator1Stake,
		uint64(delegator1StartTime.Unix()),
		uint64(delegator1EndTime.Unix()),
		ids.NodeID(id),
		keys[0].PublicKey().Address(),
		[]*secp256k1.PrivateKey{keys[0], keys[1]},
		changeAddr,
	)
	require.NoError(err)

	// issue the first add delegator tx
	err = vm.Builder.AddUnverifiedTx(addFirstDelegatorTx)
	require.NoError(err)

	// trigger block creation for the first add delegator tx
	addFirstDelegatorBlock, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(addFirstDelegatorBlock.Verify(context.Background()))
	require.NoError(addFirstDelegatorBlock.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	// create valid tx
	addSecondDelegatorTx, err := vm.txBuilder.NewAddDelegatorTx(
		delegator2Stake,
		uint64(delegator2StartTime.Unix()),
		uint64(delegator2EndTime.Unix()),
		ids.NodeID(id),
		keys[0].PublicKey().Address(),
		[]*secp256k1.PrivateKey{keys[0], keys[1]},
		changeAddr,
	)
	require.NoError(err)

	// attempting to issue the second add delegator tx should fail because the
	// total stake weight would go over the limit.
	require.Error(vm.Builder.AddUnverifiedTx(addSecondDelegatorTx))
}

func TestRemovePermissionedValidatorDuringPendingToCurrentTransitionNotTracked(t *testing.T) {
	require := require.New(t)

	validatorStartTime := banffForkTime.Add(txexecutor.SyncBound).Add(1 * time.Second)
	validatorEndTime := validatorStartTime.Add(360 * 24 * time.Hour)

	vm, _, _ := defaultVM()

	vm.ctx.Lock.Lock()
	defer func() {
		err := vm.Shutdown(context.Background())
		require.NoError(err)

		vm.ctx.Lock.Unlock()
	}()

	key, err := testKeyFactory.NewPrivateKey()
	require.NoError(err)

	id := key.PublicKey().Address()
	changeAddr := keys[0].PublicKey().Address()

	addValidatorTx, err := vm.txBuilder.NewAddValidatorTx(
		defaultMaxValidatorStake,
		uint64(validatorStartTime.Unix()),
		uint64(validatorEndTime.Unix()),
		ids.NodeID(id),
		id,
		reward.PercentDenominator,
		[]*secp256k1.PrivateKey{keys[0], keys[1]},
		changeAddr,
	)
	require.NoError(err)

	err = vm.Builder.AddUnverifiedTx(addValidatorTx)
	require.NoError(err)

	// trigger block creation for the validator tx
	addValidatorBlock, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(addValidatorBlock.Verify(context.Background()))
	require.NoError(addValidatorBlock.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	createSubnetTx, err := vm.txBuilder.NewCreateSubnetTx(
		1,
		[]ids.ShortID{changeAddr},
		[]*secp256k1.PrivateKey{keys[0], keys[1]},
		changeAddr,
	)
	require.NoError(err)

	err = vm.Builder.AddUnverifiedTx(createSubnetTx)
	require.NoError(err)

	// trigger block creation for the subnet tx
	createSubnetBlock, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(createSubnetBlock.Verify(context.Background()))
	require.NoError(createSubnetBlock.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	addSubnetValidatorTx, err := vm.txBuilder.NewAddSubnetValidatorTx(
		defaultMaxValidatorStake,
		uint64(validatorStartTime.Unix()),
		uint64(validatorEndTime.Unix()),
		ids.NodeID(id),
		createSubnetTx.ID(),
		[]*secp256k1.PrivateKey{keys[0], keys[1]},
		changeAddr,
	)
	require.NoError(err)

	err = vm.Builder.AddUnverifiedTx(addSubnetValidatorTx)
	require.NoError(err)

	// trigger block creation for the validator tx
	addSubnetValidatorBlock, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(addSubnetValidatorBlock.Verify(context.Background()))
	require.NoError(addSubnetValidatorBlock.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	emptyValidatorSet, err := vm.GetValidatorSet(
		context.Background(),
		addSubnetValidatorBlock.Height(),
		createSubnetTx.ID(),
	)
	require.NoError(err)
	require.Empty(emptyValidatorSet)

	removeSubnetValidatorTx, err := vm.txBuilder.NewRemoveSubnetValidatorTx(
		ids.NodeID(id),
		createSubnetTx.ID(),
		[]*secp256k1.PrivateKey{keys[0], keys[1]},
		changeAddr,
	)
	require.NoError(err)

	// Set the clock so that the validator will be moved from the pending
	// validator set into the current validator set.
	vm.clock.Set(validatorStartTime)

	err = vm.Builder.AddUnverifiedTx(removeSubnetValidatorTx)
	require.NoError(err)

	// trigger block creation for the validator tx
	removeSubnetValidatorBlock, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(removeSubnetValidatorBlock.Verify(context.Background()))
	require.NoError(removeSubnetValidatorBlock.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	emptyValidatorSet, err = vm.GetValidatorSet(
		context.Background(),
		addSubnetValidatorBlock.Height(),
		createSubnetTx.ID(),
	)
	require.NoError(err)
	require.Empty(emptyValidatorSet)
}

func TestRemovePermissionedValidatorDuringPendingToCurrentTransitionTracked(t *testing.T) {
	require := require.New(t)

	validatorStartTime := banffForkTime.Add(txexecutor.SyncBound).Add(1 * time.Second)
	validatorEndTime := validatorStartTime.Add(360 * 24 * time.Hour)

	vm, _, _ := defaultVM()

	vm.ctx.Lock.Lock()
	defer func() {
		err := vm.Shutdown(context.Background())
		require.NoError(err)

		vm.ctx.Lock.Unlock()
	}()

	key, err := testKeyFactory.NewPrivateKey()
	require.NoError(err)

	id := key.PublicKey().Address()
	changeAddr := keys[0].PublicKey().Address()

	addValidatorTx, err := vm.txBuilder.NewAddValidatorTx(
		defaultMaxValidatorStake,
		uint64(validatorStartTime.Unix()),
		uint64(validatorEndTime.Unix()),
		ids.NodeID(id),
		id,
		reward.PercentDenominator,
		[]*secp256k1.PrivateKey{keys[0], keys[1]},
		changeAddr,
	)
	require.NoError(err)

	err = vm.Builder.AddUnverifiedTx(addValidatorTx)
	require.NoError(err)

	// trigger block creation for the validator tx
	addValidatorBlock, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(addValidatorBlock.Verify(context.Background()))
	require.NoError(addValidatorBlock.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	createSubnetTx, err := vm.txBuilder.NewCreateSubnetTx(
		1,
		[]ids.ShortID{changeAddr},
		[]*secp256k1.PrivateKey{keys[0], keys[1]},
		changeAddr,
	)
	require.NoError(err)

	err = vm.Builder.AddUnverifiedTx(createSubnetTx)
	require.NoError(err)

	// trigger block creation for the subnet tx
	createSubnetBlock, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(createSubnetBlock.Verify(context.Background()))
	require.NoError(createSubnetBlock.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	vm.TrackedSubnets.Add(createSubnetTx.ID())
	subnetValidators := validators.NewSet()
	err = vm.state.ValidatorSet(createSubnetTx.ID(), subnetValidators)
	require.NoError(err)

	added := vm.Validators.Add(createSubnetTx.ID(), subnetValidators)
	require.True(added)

	addSubnetValidatorTx, err := vm.txBuilder.NewAddSubnetValidatorTx(
		defaultMaxValidatorStake,
		uint64(validatorStartTime.Unix()),
		uint64(validatorEndTime.Unix()),
		ids.NodeID(id),
		createSubnetTx.ID(),
		[]*secp256k1.PrivateKey{keys[0], keys[1]},
		changeAddr,
	)
	require.NoError(err)

	err = vm.Builder.AddUnverifiedTx(addSubnetValidatorTx)
	require.NoError(err)

	// trigger block creation for the validator tx
	addSubnetValidatorBlock, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(addSubnetValidatorBlock.Verify(context.Background()))
	require.NoError(addSubnetValidatorBlock.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	removeSubnetValidatorTx, err := vm.txBuilder.NewRemoveSubnetValidatorTx(
		ids.NodeID(id),
		createSubnetTx.ID(),
		[]*secp256k1.PrivateKey{keys[0], keys[1]},
		changeAddr,
	)
	require.NoError(err)

	// Set the clock so that the validator will be moved from the pending
	// validator set into the current validator set.
	vm.clock.Set(validatorStartTime)

	err = vm.Builder.AddUnverifiedTx(removeSubnetValidatorTx)
	require.NoError(err)

	// trigger block creation for the validator tx
	removeSubnetValidatorBlock, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(removeSubnetValidatorBlock.Verify(context.Background()))
	require.NoError(removeSubnetValidatorBlock.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))
}
