// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
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
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	blockexecutor "github.com/ava-labs/avalanchego/vms/platformvm/blocks/executor"
	txexecutor "github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

func TestAddDelegatorTxOverDelegatedRegression(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown())
		vm.ctx.Lock.Unlock()
	}()

	validatorStartTime := defaultGenesisTime.Add(txexecutor.SyncBound).Add(1 * time.Second)
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
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		changeAddr,
	)
	require.NoError(err)

	// trigger block creation
	require.NoError(vm.Builder.AddUnverifiedTx(addValidatorTx))

	addValidatorBlock, err := vm.Builder.BuildBlock()
	require.NoError(err)

	verifyAndAcceptProposalCommitment(require, vm, addValidatorBlock)

	vm.clock.Set(validatorStartTime)

	firstAdvanceTimeBlock, err := vm.Builder.BuildBlock()
	require.NoError(err)

	verifyAndAcceptProposalCommitment(require, vm, firstAdvanceTimeBlock)

	firstDelegatorStartTime := validatorStartTime.Add(txexecutor.SyncBound).Add(1 * time.Second)
	firstDelegatorEndTime := firstDelegatorStartTime.Add(vm.MinStakeDuration)

	// create valid tx
	addFirstDelegatorTx, err := vm.txBuilder.NewAddDelegatorTx(
		4*vm.MinValidatorStake, // maximum amount of stake this delegator can provide
		uint64(firstDelegatorStartTime.Unix()),
		uint64(firstDelegatorEndTime.Unix()),
		nodeID,
		changeAddr,
		[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1]},
		changeAddr,
	)
	require.NoError(err)

	// trigger block creation
	require.NoError(vm.Builder.AddUnverifiedTx(addFirstDelegatorTx))

	addFirstDelegatorBlock, err := vm.Builder.BuildBlock()
	require.NoError(err)

	verifyAndAcceptProposalCommitment(require, vm, addFirstDelegatorBlock)

	vm.clock.Set(firstDelegatorStartTime)

	secondAdvanceTimeBlock, err := vm.Builder.BuildBlock()
	require.NoError(err)

	verifyAndAcceptProposalCommitment(require, vm, secondAdvanceTimeBlock)

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
		[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1], keys[3]},
		changeAddr,
	)
	require.NoError(err)

	// trigger block creation
	require.NoError(vm.Builder.AddUnverifiedTx(addSecondDelegatorTx))

	addSecondDelegatorBlock, err := vm.Builder.BuildBlock()
	require.NoError(err)

	verifyAndAcceptProposalCommitment(require, vm, addSecondDelegatorBlock)

	thirdDelegatorStartTime := firstDelegatorEndTime.Add(-time.Second)
	thirdDelegatorEndTime := thirdDelegatorStartTime.Add(vm.MinStakeDuration)

	// create valid tx
	addThirdDelegatorTx, err := vm.txBuilder.NewAddDelegatorTx(
		vm.MinDelegatorStake,
		uint64(thirdDelegatorStartTime.Unix()),
		uint64(thirdDelegatorEndTime.Unix()),
		nodeID,
		changeAddr,
		[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1], keys[4]},
		changeAddr,
	)
	require.NoError(err)

	// trigger block creation
	err = vm.Builder.AddUnverifiedTx(addThirdDelegatorTx)
	require.Error(err, "should have marked the delegator as being over delegated")
}

func TestAddDelegatorTxHeapCorruption(t *testing.T) {
	validatorStartTime := defaultGenesisTime.Add(txexecutor.SyncBound).Add(1 * time.Second)
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
				err := vm.Shutdown()
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
				[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1]},
				changeAddr,
			)
			require.NoError(err)

			// issue the add validator tx
			err = vm.Builder.AddUnverifiedTx(addValidatorTx)
			require.NoError(err)

			// trigger block creation for the validator tx
			addValidatorBlock, err := vm.Builder.BuildBlock()
			require.NoError(err)

			verifyAndAcceptProposalCommitment(require, vm, addValidatorBlock)

			// create valid tx
			addFirstDelegatorTx, err := vm.txBuilder.NewAddDelegatorTx(
				delegator1Stake,
				uint64(delegator1StartTime.Unix()),
				uint64(delegator1EndTime.Unix()),
				ids.NodeID(id),
				keys[0].PublicKey().Address(),
				[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1]},
				changeAddr,
			)
			require.NoError(err)

			// issue the first add delegator tx
			err = vm.Builder.AddUnverifiedTx(addFirstDelegatorTx)
			require.NoError(err)

			// trigger block creation for the first add delegator tx
			addFirstDelegatorBlock, err := vm.Builder.BuildBlock()
			require.NoError(err)

			verifyAndAcceptProposalCommitment(require, vm, addFirstDelegatorBlock)

			// create valid tx
			addSecondDelegatorTx, err := vm.txBuilder.NewAddDelegatorTx(
				delegator2Stake,
				uint64(delegator2StartTime.Unix()),
				uint64(delegator2EndTime.Unix()),
				ids.NodeID(id),
				keys[0].PublicKey().Address(),
				[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1]},
				changeAddr,
			)
			require.NoError(err)

			// issue the second add delegator tx
			err = vm.Builder.AddUnverifiedTx(addSecondDelegatorTx)
			require.NoError(err)

			// trigger block creation for the second add delegator tx
			addSecondDelegatorBlock, err := vm.Builder.BuildBlock()
			require.NoError(err)

			verifyAndAcceptProposalCommitment(require, vm, addSecondDelegatorBlock)

			// create valid tx
			addThirdDelegatorTx, err := vm.txBuilder.NewAddDelegatorTx(
				delegator3Stake,
				uint64(delegator3StartTime.Unix()),
				uint64(delegator3EndTime.Unix()),
				ids.NodeID(id),
				keys[0].PublicKey().Address(),
				[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1]},
				changeAddr,
			)
			require.NoError(err)

			// issue the third add delegator tx
			err = vm.Builder.AddUnverifiedTx(addThirdDelegatorTx)
			require.NoError(err)

			// trigger block creation for the third add delegator tx
			addThirdDelegatorBlock, err := vm.Builder.BuildBlock()
			require.NoError(err)

			verifyAndAcceptProposalCommitment(require, vm, addThirdDelegatorBlock)

			// create valid tx
			addFourthDelegatorTx, err := vm.txBuilder.NewAddDelegatorTx(
				delegator4Stake,
				uint64(delegator4StartTime.Unix()),
				uint64(delegator4EndTime.Unix()),
				ids.NodeID(id),
				keys[0].PublicKey().Address(),
				[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1]},
				changeAddr,
			)
			require.NoError(err)

			// issue the fourth add delegator tx
			err = vm.Builder.AddUnverifiedTx(addFourthDelegatorTx)
			require.NoError(err)

			// trigger block creation for the fourth add delegator tx
			addFourthDelegatorBlock, err := vm.Builder.BuildBlock()

			if test.shouldFail {
				require.Error(err, "should have failed to allow new delegator")
				return
			}

			require.NoError(err)

			verifyAndAcceptProposalCommitment(require, vm, addFourthDelegatorBlock)
		})
	}
}

// Test that calling Verify on a block with an unverified parent doesn't cause a
// panic.
func TestUnverifiedParentPanicRegression(t *testing.T) {
	_, genesisBytes := defaultGenesis()

	baseDBManager := manager.NewMemDB(version.Semantic1_0_0)
	atomicDB := prefixdb.New([]byte{1}, baseDBManager.Current().Database)

	vm := &VM{Factory: Factory{
		Config: config.Config{
			Chains:                 chains.MockManager{},
			Validators:             validators.NewManager(),
			UptimeLockedCalculator: uptime.NewLockedCalculator(),
			MinStakeDuration:       defaultMinStakingDuration,
			MaxStakeDuration:       defaultMaxStakingDuration,
			RewardConfig:           defaultRewardConfig,
			BanffTime:              mockable.MaxTime, // Banff not yet active
		},
	}}

	vm.clock.Set(defaultGenesisTime)
	ctx := defaultContext()
	ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		ctx.Lock.Unlock()
	}()

	msgChan := make(chan common.Message, 1)
	if err := vm.Initialize(ctx, baseDBManager, genesisBytes, nil, nil, msgChan, nil, nil); err != nil {
		t.Fatal(err)
	}

	m := atomic.NewMemory(atomicDB)
	vm.ctx.SharedMemory = m.NewSharedMemory(ctx.ChainID)

	key0 := keys[0]
	key1 := keys[1]
	addr0 := key0.PublicKey().Address()
	addr1 := key1.PublicKey().Address()

	addSubnetTx0, err := vm.txBuilder.NewCreateSubnetTx(
		1,
		[]ids.ShortID{addr0},
		[]*crypto.PrivateKeySECP256K1R{key0},
		addr0,
	)
	if err != nil {
		t.Fatal(err)
	}

	addSubnetTx1, err := vm.txBuilder.NewCreateSubnetTx(
		1,
		[]ids.ShortID{addr1},
		[]*crypto.PrivateKeySECP256K1R{key1},
		addr1,
	)
	if err != nil {
		t.Fatal(err)
	}

	addSubnetTx2, err := vm.txBuilder.NewCreateSubnetTx(
		1,
		[]ids.ShortID{addr1},
		[]*crypto.PrivateKeySECP256K1R{key1},
		addr0,
	)
	if err != nil {
		t.Fatal(err)
	}

	preferred, err := vm.Builder.Preferred()
	if err != nil {
		t.Fatal(err)
	}
	preferredID := preferred.ID()
	preferredHeight := preferred.Height()

	statelessStandardBlk, err := blocks.NewApricotStandardBlock(
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{addSubnetTx0},
	)
	if err != nil {
		t.Fatal(err)
	}
	addSubnetBlk0 := vm.manager.NewBlock(statelessStandardBlk)

	statelessStandardBlk, err = blocks.NewApricotStandardBlock(
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{addSubnetTx1},
	)
	if err != nil {
		t.Fatal(err)
	}
	addSubnetBlk1 := vm.manager.NewBlock(statelessStandardBlk)

	statelessStandardBlk, err = blocks.NewApricotStandardBlock(
		addSubnetBlk1.ID(),
		preferredHeight+2,
		[]*txs.Tx{addSubnetTx2},
	)
	if err != nil {
		t.Fatal(err)
	}
	addSubnetBlk2 := vm.manager.NewBlock(statelessStandardBlk)

	if _, err := vm.ParseBlock(addSubnetBlk0.Bytes()); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.ParseBlock(addSubnetBlk1.Bytes()); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.ParseBlock(addSubnetBlk2.Bytes()); err != nil {
		t.Fatal(err)
	}

	if err := addSubnetBlk0.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := addSubnetBlk0.Accept(); err != nil {
		t.Fatal(err)
	}
	// Doesn't matter what verify returns as long as it's not panicking.
	_ = addSubnetBlk2.Verify()
}

func TestRejectedStateRegressionInvalidValidatorTimestamp(t *testing.T) {
	require := require.New(t)

	vm, baseDB, mutableSharedMemory := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		err := vm.Shutdown()
		require.NoError(err)

		vm.ctx.Lock.Unlock()
	}()

	newValidatorStartTime := defaultGenesisTime.Add(txexecutor.SyncBound).Add(1 * time.Second)
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
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty,
	)
	require.NoError(err)

	// Create the proposal block to add the new validator
	preferred, err := vm.Builder.Preferred()
	require.NoError(err)

	preferredID := preferred.ID()
	preferredHeight := preferred.Height()

	statelessBlk, err := blocks.NewApricotProposalBlock(
		preferredID,
		preferredHeight+1,
		addValidatorTx,
	)
	require.NoError(err)

	addValidatorProposalBlk := vm.manager.NewBlock(statelessBlk)

	err = addValidatorProposalBlk.Verify()
	require.NoError(err)

	// Get the commit block to add the new validator
	addValidatorProposalOptions, err := addValidatorProposalBlk.(snowman.OracleBlock).Options()
	require.NoError(err)

	addValidatorProposalCommitIntf := addValidatorProposalOptions[0]
	addValidatorProposalCommit, ok := addValidatorProposalCommitIntf.(*blockexecutor.Block)
	require.True(ok)

	err = addValidatorProposalCommit.Verify()
	require.NoError(err)

	// Verify that the new validator now in pending validator set
	{
		onAccept, found := vm.manager.GetState(addValidatorProposalCommit.ID())
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
	err = signedImportTx.Sign(txs.Codec, [][]*crypto.PrivateKeySECP256K1R{
		{}, // There is one input, with no required signers
	})
	require.NoError(err)

	// Create the standard block that will fail verification, and then be
	// re-verified.
	preferredID = addValidatorProposalCommit.ID()
	preferredHeight = addValidatorProposalCommit.Height()

	statelessImportBlk, err := blocks.NewApricotStandardBlock(
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{signedImportTx},
	)
	require.NoError(err)

	importBlk := vm.manager.NewBlock(statelessImportBlk)

	// Because the shared memory UTXO hasn't been populated, this block is
	// currently invalid.
	err = importBlk.Verify()
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
	err = importBlk.Verify()
	require.NoError(err)

	// The status shouldn't have been changed during a successful verification.
	importBlkStatus = importBlk.Status()
	require.Equal(choices.Processing, importBlkStatus)

	// Create the tx that would have moved the new validator from the pending
	// validator set into the current validator set.
	vm.clock.Set(newValidatorStartTime)
	advanceTimeTx, err := vm.txBuilder.NewAdvanceTimeTx(newValidatorStartTime)
	require.NoError(err)

	// Create the proposal block that should have moved the new validator from
	// the pending validator set into the current validator set.
	preferredID = importBlk.ID()
	preferredHeight = importBlk.Height()

	statelessAdvanceTimeProposalBlk, err := blocks.NewApricotProposalBlock(
		preferredID,
		preferredHeight+1,
		advanceTimeTx,
	)
	require.NoError(err)

	advanceTimeProposalBlk := vm.manager.NewBlock(statelessAdvanceTimeProposalBlk)
	err = advanceTimeProposalBlk.Verify()
	require.NoError(err)

	// Get the commit block that advances the timestamp to the point that the
	// validator should be moved from the pending validator set into the current
	// validator set.
	advanceTimeProposalOptions, err := advanceTimeProposalBlk.(snowman.OracleBlock).Options()
	require.NoError(err)

	advanceTimeProposalCommitIntf := advanceTimeProposalOptions[0]
	advanceTimeProposalCommit, ok := advanceTimeProposalCommitIntf.(*blockexecutor.Block)
	require.True(ok)
	_, ok = advanceTimeProposalCommit.Block.(*blocks.ApricotCommitBlock)
	require.True(ok)

	err = advanceTimeProposalCommit.Verify()
	require.NoError(err)

	// Accept all the blocks
	allBlocks := []snowman.Block{
		addValidatorProposalBlk,
		addValidatorProposalCommit,
		importBlk,
		advanceTimeProposalBlk,
		advanceTimeProposalCommit,
	}
	for _, blk := range allBlocks {
		err = blk.Accept()
		require.NoError(err)

		status := blk.Status()
		require.Equal(choices.Accepted, status)
	}

	// Force a reload of the state from the database.
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
		err := vm.Shutdown()
		require.NoError(err)

		vm.ctx.Lock.Unlock()
	}()

	vm.state.SetCurrentSupply(constants.PrimaryNetworkID, defaultRewardConfig.SupplyCap/2)

	newValidatorStartTime0 := defaultGenesisTime.Add(txexecutor.SyncBound).Add(1 * time.Second)
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
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty,
	)
	require.NoError(err)

	// Create the proposal block to add the first new validator
	preferred, err := vm.Builder.Preferred()
	require.NoError(err)

	preferredID := preferred.ID()
	preferredHeight := preferred.Height()

	statelessAddValidatorProposalBlk0, err := blocks.NewApricotProposalBlock(
		preferredID,
		preferredHeight+1,
		addValidatorTx0,
	)
	require.NoError(err)

	addValidatorProposalBlk0 := vm.manager.NewBlock(statelessAddValidatorProposalBlk0)
	err = addValidatorProposalBlk0.Verify()
	require.NoError(err)

	// Get the commit block to add the first new validator
	addValidatorProposalOptions0, err := addValidatorProposalBlk0.(snowman.OracleBlock).Options()
	require.NoError(err)

	addValidatorProposalCommitIntf0 := addValidatorProposalOptions0[0]
	addValidatorProposalCommit0, ok := addValidatorProposalCommitIntf0.(*blockexecutor.Block)
	require.True(ok)
	_, ok = addValidatorProposalCommit0.Block.(*blocks.ApricotCommitBlock)
	require.True(ok)

	err = addValidatorProposalCommit0.Verify()
	require.NoError(err)

	// Verify that first new validator now in pending validator set
	{
		onAccept, ok := vm.manager.GetState(addValidatorProposalCommit0.ID())
		require.True(ok)

		_, err := onAccept.GetPendingValidator(constants.PrimaryNetworkID, nodeID0)
		require.NoError(err)
	}

	// Create the tx that moves the first new validator from the pending
	// validator set into the current validator set.
	vm.clock.Set(newValidatorStartTime0)
	advanceTimeTx0, err := vm.txBuilder.NewAdvanceTimeTx(newValidatorStartTime0)
	require.NoError(err)

	// Create the proposal block that moves the first new validator from the
	// pending validator set into the current validator set.
	preferredID = addValidatorProposalCommit0.ID()
	preferredHeight = addValidatorProposalCommit0.Height()

	statelessAdvanceTimeProposalBlk0, err := blocks.NewApricotProposalBlock(
		preferredID,
		preferredHeight+1,
		advanceTimeTx0,
	)
	require.NoError(err)

	advanceTimeProposalBlk0 := vm.manager.NewBlock(statelessAdvanceTimeProposalBlk0)

	err = advanceTimeProposalBlk0.Verify()
	require.NoError(err)

	// Get the commit block that advances the timestamp to the point that the
	// first new validator should be moved from the pending validator set into
	// the current validator set.
	advanceTimeProposalOptions0, err := advanceTimeProposalBlk0.(snowman.OracleBlock).Options()
	require.NoError(err)

	advanceTimeProposalCommitIntf0 := advanceTimeProposalOptions0[0]
	advanceTimeProposalCommit0, ok := advanceTimeProposalCommitIntf0.(*blockexecutor.Block)
	require.True(ok)
	_, ok = advanceTimeProposalCommit0.Block.(*blocks.ApricotCommitBlock)
	require.True(ok)

	err = advanceTimeProposalCommit0.Verify()
	require.NoError(err)

	// Verify that the first new validator is now in the current validator set.
	{
		onAccept, ok := vm.manager.GetState(advanceTimeProposalCommit0.ID())
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
	err = signedImportTx.Sign(txs.Codec, [][]*crypto.PrivateKeySECP256K1R{
		{}, // There is one input, with no required signers
	})
	require.NoError(err)

	// Create the standard block that will fail verification, and then be
	// re-verified.
	preferredID = advanceTimeProposalCommit0.ID()
	preferredHeight = advanceTimeProposalCommit0.Height()

	statelessImportBlk, err := blocks.NewApricotStandardBlock(
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{signedImportTx},
	)
	require.NoError(err)

	importBlk := vm.manager.NewBlock(statelessImportBlk)
	// Because the shared memory UTXO hasn't been populated, this block is
	// currently invalid.
	err = importBlk.Verify()
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
	err = importBlk.Verify()
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
		[]*crypto.PrivateKeySECP256K1R{keys[1]},
		ids.ShortEmpty,
	)
	require.NoError(err)

	// Create the proposal block to add the second new validator
	preferredID = importBlk.ID()
	preferredHeight = importBlk.Height()

	statelessAddValidatorProposalBlk1, err := blocks.NewApricotProposalBlock(
		preferredID,
		preferredHeight+1,
		addValidatorTx1,
	)
	require.NoError(err)

	addValidatorProposalBlk1 := vm.manager.NewBlock(statelessAddValidatorProposalBlk1)

	err = addValidatorProposalBlk1.Verify()
	require.NoError(err)

	// Get the commit block to add the second new validator
	addValidatorProposalOptions1, err := addValidatorProposalBlk1.(snowman.OracleBlock).Options()
	require.NoError(err)

	addValidatorProposalCommitIntf1 := addValidatorProposalOptions1[0]
	addValidatorProposalCommit1, ok := addValidatorProposalCommitIntf1.(*blockexecutor.Block)
	require.True(ok)
	_, ok = addValidatorProposalCommit1.Block.(*blocks.ApricotCommitBlock)
	require.True(ok)

	err = addValidatorProposalCommit1.Verify()
	require.NoError(err)

	// Verify that the second new validator now in pending validator set
	{
		onAccept, ok := vm.manager.GetState(addValidatorProposalCommit1.ID())
		require.True(ok)

		_, err := onAccept.GetPendingValidator(constants.PrimaryNetworkID, nodeID1)
		require.NoError(err)
	}

	// Create the tx that moves the second new validator from the pending
	// validator set into the current validator set.
	vm.clock.Set(newValidatorStartTime1)
	advanceTimeTx1, err := vm.txBuilder.NewAdvanceTimeTx(newValidatorStartTime1)
	require.NoError(err)

	// Create the proposal block that moves the second new validator from the
	// pending validator set into the current validator set.
	preferredID = addValidatorProposalCommit1.ID()
	preferredHeight = addValidatorProposalCommit1.Height()

	statelessAdvanceTimeProposalBlk1, err := blocks.NewApricotProposalBlock(
		preferredID,
		preferredHeight+1,
		advanceTimeTx1,
	)
	require.NoError(err)

	advanceTimeProposalBlk1 := vm.manager.NewBlock(statelessAdvanceTimeProposalBlk1)

	err = advanceTimeProposalBlk1.Verify()
	require.NoError(err)

	// Get the commit block that advances the timestamp to the point that the
	// second new validator should be moved from the pending validator set into
	// the current validator set.
	advanceTimeProposalOptions1, err := advanceTimeProposalBlk1.(snowman.OracleBlock).Options()
	require.NoError(err)

	advanceTimeProposalCommitIntf1 := advanceTimeProposalOptions1[0]
	advanceTimeProposalCommit1, ok := advanceTimeProposalCommitIntf1.(*blockexecutor.Block)
	require.True(ok)
	_, ok = advanceTimeProposalCommit1.Block.(*blocks.ApricotCommitBlock)
	require.True(ok)

	err = advanceTimeProposalCommit1.Verify()
	require.NoError(err)

	// Verify that the second new validator is now in the current validator set.
	{
		onAccept, ok := vm.manager.GetState(advanceTimeProposalCommit1.ID())
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
		addValidatorProposalBlk0,
		addValidatorProposalCommit0,
		advanceTimeProposalBlk0,
		advanceTimeProposalCommit0,
		importBlk,
		addValidatorProposalBlk1,
		addValidatorProposalCommit1,
		advanceTimeProposalBlk1,
		advanceTimeProposalCommit1,
	}
	for _, blk := range allBlocks {
		err = blk.Accept()
		require.NoError(err)

		status := blk.Status()
		require.Equal(choices.Accepted, status)
	}

	// Force a reload of the state from the database.
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
		err := vm.Shutdown()
		require.NoError(err)

		vm.ctx.Lock.Unlock()
	}()

	nodeID0 := ids.NodeID(keys[0].PublicKey().Address())
	nodeID1 := ids.NodeID(keys[1].PublicKey().Address())
	nodeID2 := ids.NodeID(keys[2].PublicKey().Address())
	nodeID3 := ids.NodeID(keys[3].PublicKey().Address())
	nodeID4 := ids.NodeID(keys[4].PublicKey().Address())

	currentHeight, err := vm.GetCurrentHeight()
	require.NoError(err)
	require.EqualValues(1, currentHeight)

	expectedValidators1 := map[ids.NodeID]uint64{
		nodeID0: defaultWeight,
		nodeID1: defaultWeight,
		nodeID2: defaultWeight,
		nodeID3: defaultWeight,
		nodeID4: defaultWeight,
	}
	validators, err := vm.GetValidatorSet(1, constants.PrimaryNetworkID)
	require.NoError(err)
	require.Equal(expectedValidators1, validators)

	newValidatorStartTime0 := defaultGenesisTime.Add(txexecutor.SyncBound).Add(1 * time.Second)
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
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.GenerateTestShortID(),
	)
	require.NoError(err)

	// Create the proposal block to add the first new validator
	preferred, err := vm.Builder.Preferred()
	require.NoError(err)

	preferredID := preferred.ID()
	preferredHeight := preferred.Height()

	statelessProposalBlk, err := blocks.NewApricotProposalBlock(
		preferredID,
		preferredHeight+1,
		addValidatorTx0,
	)
	require.NoError(err)
	addValidatorProposalBlk0 := vm.manager.NewBlock(statelessProposalBlk)

	verifyAndAcceptProposalCommitment(require, vm, addValidatorProposalBlk0)

	currentHeight, err = vm.GetCurrentHeight()
	require.NoError(err)
	require.EqualValues(3, currentHeight)

	for i := uint64(1); i <= 3; i++ {
		validators, err = vm.GetValidatorSet(i, constants.PrimaryNetworkID)
		require.NoError(err)
		require.Equal(expectedValidators1, validators)
	}

	// Create the tx that moves the first new validator from the pending
	// validator set into the current validator set.
	vm.clock.Set(newValidatorStartTime0)
	advanceTimeTx0, err := vm.txBuilder.NewAdvanceTimeTx(newValidatorStartTime0)
	require.NoError(err)

	// Create the proposal block that moves the first new validator from the
	// pending validator set into the current validator set.
	preferred, err = vm.Builder.Preferred()
	require.NoError(err)

	preferredID = preferred.ID()
	preferredHeight = preferred.Height()

	statelessProposalBlk, err = blocks.NewApricotProposalBlock(
		preferredID,
		preferredHeight+1,
		advanceTimeTx0,
	)
	require.NoError(err)
	advanceTimeProposalBlk0 := vm.manager.NewBlock(statelessProposalBlk)

	verifyAndAcceptProposalCommitment(require, vm, advanceTimeProposalBlk0)

	currentHeight, err = vm.GetCurrentHeight()
	require.NoError(err)
	require.EqualValues(5, currentHeight)

	for i := uint64(1); i <= 4; i++ {
		validators, err = vm.GetValidatorSet(i, constants.PrimaryNetworkID)
		require.NoError(err)
		require.Equal(expectedValidators1, validators)
	}

	expectedValidators2 := map[ids.NodeID]uint64{
		nodeID0: defaultWeight,
		nodeID1: defaultWeight,
		nodeID2: defaultWeight,
		nodeID3: defaultWeight,
		nodeID4: defaultWeight,
		nodeID5: vm.MaxValidatorStake,
	}
	validators, err = vm.GetValidatorSet(5, constants.PrimaryNetworkID)
	require.NoError(err)
	require.Equal(expectedValidators2, validators)
}

func TestAddDelegatorTxAddBeforeRemove(t *testing.T) {
	require := require.New(t)

	validatorStartTime := defaultGenesisTime.Add(txexecutor.SyncBound).Add(1 * time.Second)
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
		err := vm.Shutdown()
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
		[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1]},
		changeAddr,
	)
	require.NoError(err)

	// issue the add validator tx
	err = vm.Builder.AddUnverifiedTx(addValidatorTx)
	require.NoError(err)

	// trigger block creation for the validator tx
	addValidatorBlock, err := vm.Builder.BuildBlock()
	require.NoError(err)

	verifyAndAcceptProposalCommitment(require, vm, addValidatorBlock)

	// create valid tx
	addFirstDelegatorTx, err := vm.txBuilder.NewAddDelegatorTx(
		delegator1Stake,
		uint64(delegator1StartTime.Unix()),
		uint64(delegator1EndTime.Unix()),
		ids.NodeID(id),
		keys[0].PublicKey().Address(),
		[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1]},
		changeAddr,
	)
	require.NoError(err)

	// issue the first add delegator tx
	err = vm.Builder.AddUnverifiedTx(addFirstDelegatorTx)
	require.NoError(err)

	// trigger block creation for the first add delegator tx
	addFirstDelegatorBlock, err := vm.Builder.BuildBlock()
	require.NoError(err)

	verifyAndAcceptProposalCommitment(require, vm, addFirstDelegatorBlock)

	// create valid tx
	addSecondDelegatorTx, err := vm.txBuilder.NewAddDelegatorTx(
		delegator2Stake,
		uint64(delegator2StartTime.Unix()),
		uint64(delegator2EndTime.Unix()),
		ids.NodeID(id),
		keys[0].PublicKey().Address(),
		[]*crypto.PrivateKeySECP256K1R{keys[0], keys[1]},
		changeAddr,
	)
	require.NoError(err)

	// attempting to issue the second add delegator tx should fail because the
	// total stake weight would go over the limit.
	require.Error(vm.Builder.AddUnverifiedTx(addSecondDelegatorTx))
}

func verifyAndAcceptProposalCommitment(require *require.Assertions, vm *VM, blk snowman.Block) {
	// Verify the proposed block
	require.NoError(blk.Verify())

	// Assert preferences are correct
	proposalBlk := blk.(snowman.OracleBlock)
	options, err := proposalBlk.Options()
	require.NoError(err)

	// verify the preferences
	commit := options[0].(*blockexecutor.Block)
	_, ok := commit.Block.(*blocks.ApricotCommitBlock)
	require.True(ok, "expected commit block to be preferred")

	abort := options[1].(*blockexecutor.Block)
	_, ok = abort.Block.(*blocks.ApricotAbortBlock)
	require.True(ok, "expected abort block to be issued")

	// Verify the options
	require.NoError(commit.Verify())
	require.NoError(abort.Verify())

	// Accept the proposal block and the commit block
	require.NoError(proposalBlk.Accept())
	require.NoError(commit.Accept())
	require.NoError(abort.Reject())
	require.NoError(vm.SetPreference(vm.manager.LastAccepted()))
}
