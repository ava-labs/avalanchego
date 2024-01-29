// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/queue"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/bootstrap"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/handler"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/sender"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math/meter"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	smcon "github.com/ava-labs/avalanchego/snow/consensus/snowman"
	smeng "github.com/ava-labs/avalanchego/snow/engine/snowman"
	snowgetter "github.com/ava-labs/avalanchego/snow/engine/snowman/getter"
	timetracker "github.com/ava-labs/avalanchego/snow/networking/tracker"
	blockbuilder "github.com/ava-labs/avalanchego/vms/platformvm/block/builder"
	blockexecutor "github.com/ava-labs/avalanchego/vms/platformvm/block/executor"
	txbuilder "github.com/ava-labs/avalanchego/vms/platformvm/txs/builder"
	txexecutor "github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

type activeFork uint8

const (
	apricotPhase3 activeFork = iota
	apricotPhase5
	banffFork
	cortinaFork
	durangoFork

	latestFork activeFork = durangoFork

	defaultWeight uint64 = 10000
)

var (
	defaultMinStakingDuration = 24 * time.Hour
	defaultMaxStakingDuration = 365 * 24 * time.Hour

	defaultRewardConfig = reward.Config{
		MaxConsumptionRate: .12 * reward.PercentDenominator,
		MinConsumptionRate: .10 * reward.PercentDenominator,
		MintingPeriod:      365 * 24 * time.Hour,
		SupplyCap:          720 * units.MegaAvax,
	}

	defaultTxFee = uint64(100)

	// chain timestamp at genesis
	defaultGenesisTime = time.Date(1997, 1, 1, 0, 0, 0, 0, time.UTC)

	// time that genesis validators start validating
	defaultValidateStartTime = defaultGenesisTime

	// time that genesis validators stop validating
	defaultValidateEndTime = defaultValidateStartTime.Add(10 * defaultMinStakingDuration)

	latestForkTime = defaultGenesisTime.Add(time.Second)

	// each key controls an address that has [defaultBalance] AVAX at genesis
	keys = secp256k1.TestKeys()

	// Node IDs of genesis validators. Initialized in init function
	genesisNodeIDs           []ids.NodeID
	defaultMinDelegatorStake = 1 * units.MilliAvax
	defaultMinValidatorStake = 5 * defaultMinDelegatorStake
	defaultMaxValidatorStake = 100 * defaultMinValidatorStake
	defaultBalance           = 2 * defaultMaxValidatorStake // amount all genesis validators have in defaultVM

	// subnet that exists at genesis in defaultVM
	// Its controlKeys are keys[0], keys[1], keys[2]
	// Its threshold is 2
	testSubnet1            *txs.Tx
	testSubnet1ControlKeys = keys[0:3]
)

func init() {
	for _, key := range keys {
		// TODO: use ids.GenerateTestNodeID() instead of ids.BuildTestNodeID
		// Can be done when TestGetState is refactored
		nodeBytes := key.PublicKey().Address()
		nodeID := ids.BuildTestNodeID(nodeBytes[:])

		genesisNodeIDs = append(genesisNodeIDs, nodeID)
	}
}

type mutableSharedMemory struct {
	atomic.SharedMemory
}

// Returns:
// 1) The genesis state
// 2) The byte representation of the default genesis for tests
func defaultGenesis(t *testing.T, avaxAssetID ids.ID) (*api.BuildGenesisArgs, []byte) {
	require := require.New(t)

	genesisUTXOs := make([]api.UTXO, len(keys))
	for i, key := range keys {
		id := key.PublicKey().Address()
		addr, err := address.FormatBech32(constants.UnitTestHRP, id.Bytes())
		require.NoError(err)
		genesisUTXOs[i] = api.UTXO{
			Amount:  json.Uint64(defaultBalance),
			Address: addr,
		}
	}

	genesisValidators := make([]api.GenesisPermissionlessValidator, len(genesisNodeIDs))
	for i, nodeID := range genesisNodeIDs {
		addr, err := address.FormatBech32(constants.UnitTestHRP, nodeID.Bytes())
		require.NoError(err)
		genesisValidators[i] = api.GenesisPermissionlessValidator{
			GenesisValidator: api.GenesisValidator{
				StartTime: json.Uint64(defaultValidateStartTime.Unix()),
				EndTime:   json.Uint64(defaultValidateEndTime.Unix()),
				NodeID:    nodeID,
			},
			RewardOwner: &api.Owner{
				Threshold: 1,
				Addresses: []string{addr},
			},
			Staked: []api.UTXO{{
				Amount:  json.Uint64(defaultWeight),
				Address: addr,
			}},
			DelegationFee: reward.PercentDenominator,
		}
	}

	buildGenesisArgs := api.BuildGenesisArgs{
		Encoding:      formatting.Hex,
		NetworkID:     json.Uint32(constants.UnitTestID),
		AvaxAssetID:   avaxAssetID,
		UTXOs:         genesisUTXOs,
		Validators:    genesisValidators,
		Chains:        nil,
		Time:          json.Uint64(defaultGenesisTime.Unix()),
		InitialSupply: json.Uint64(360 * units.MegaAvax),
	}

	buildGenesisResponse := api.BuildGenesisReply{}
	platformvmSS := api.StaticService{}
	require.NoError(platformvmSS.BuildGenesis(nil, &buildGenesisArgs, &buildGenesisResponse))

	genesisBytes, err := formatting.Decode(buildGenesisResponse.Encoding, buildGenesisResponse.Bytes)
	require.NoError(err)

	return &buildGenesisArgs, genesisBytes
}

func defaultVM(t *testing.T, fork activeFork) (*VM, database.Database, *mutableSharedMemory) {
	require := require.New(t)
	var (
		apricotPhase3Time = mockable.MaxTime
		apricotPhase5Time = mockable.MaxTime
		banffTime         = mockable.MaxTime
		cortinaTime       = mockable.MaxTime
		durangoTime       = mockable.MaxTime
	)

	// always reset latestForkTime (a package level variable)
	// to ensure test independence
	latestForkTime = defaultGenesisTime.Add(time.Second)
	switch fork {
	case durangoFork:
		durangoTime = latestForkTime
		fallthrough
	case cortinaFork:
		cortinaTime = latestForkTime
		fallthrough
	case banffFork:
		banffTime = latestForkTime
		fallthrough
	case apricotPhase5:
		apricotPhase5Time = latestForkTime
		fallthrough
	case apricotPhase3:
		apricotPhase3Time = latestForkTime
	default:
		require.NoError(fmt.Errorf("unhandled fork %d", fork))
	}

	vm := &VM{Config: config.Config{
		Chains:                 chains.TestManager,
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		SybilProtectionEnabled: true,
		Validators:             validators.NewManager(),
		TxFee:                  defaultTxFee,
		CreateSubnetTxFee:      100 * defaultTxFee,
		TransformSubnetTxFee:   100 * defaultTxFee,
		CreateBlockchainTxFee:  100 * defaultTxFee,
		MinValidatorStake:      defaultMinValidatorStake,
		MaxValidatorStake:      defaultMaxValidatorStake,
		MinDelegatorStake:      defaultMinDelegatorStake,
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
		ApricotPhase3Time:      apricotPhase3Time,
		ApricotPhase5Time:      apricotPhase5Time,
		BanffTime:              banffTime,
		CortinaTime:            cortinaTime,
		DurangoTime:            durangoTime,
	}}

	db := memdb.New()
	chainDB := prefixdb.New([]byte{0}, db)
	atomicDB := prefixdb.New([]byte{1}, db)

	vm.clock.Set(latestForkTime)
	msgChan := make(chan common.Message, 1)
	ctx := snowtest.Context(t, snowtest.PChainID)

	m := atomic.NewMemory(atomicDB)
	msm := &mutableSharedMemory{
		SharedMemory: m.NewSharedMemory(ctx.ChainID),
	}
	ctx.SharedMemory = msm

	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()
	_, genesisBytes := defaultGenesis(t, ctx.AVAXAssetID)
	appSender := &common.SenderTest{}
	appSender.CantSendAppGossip = true
	appSender.SendAppGossipF = func(context.Context, []byte) error {
		return nil
	}

	dynamicConfigBytes := []byte(`{"network":{"max-validator-set-staleness":0}}`)
	require.NoError(vm.Initialize(
		context.Background(),
		ctx,
		chainDB,
		genesisBytes,
		nil,
		dynamicConfigBytes,
		msgChan,
		nil,
		appSender,
	))

	// align chain time and local clock
	vm.state.SetTimestamp(vm.clock.Time())

	require.NoError(vm.SetState(context.Background(), snow.NormalOp))

	// Create a subnet and store it in testSubnet1
	// Note: following Banff activation, block acceptance will move
	// chain time ahead
	var err error
	testSubnet1, err = vm.txBuilder.NewCreateSubnetTx(
		2, // threshold; 2 sigs from keys[0], keys[1], keys[2] needed to add validator to this subnet
		// control keys are keys[0], keys[1], keys[2]
		[]ids.ShortID{keys[0].PublicKey().Address(), keys[1].PublicKey().Address(), keys[2].PublicKey().Address()},
		[]*secp256k1.PrivateKey{keys[0]}, // pays tx fee
		keys[0].PublicKey().Address(),    // change addr
	)
	require.NoError(err)
	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTx(context.Background(), testSubnet1))
	vm.ctx.Lock.Lock()
	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(blk.Verify(context.Background()))
	require.NoError(blk.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	t.Cleanup(func() {
		vm.ctx.Lock.Lock()
		defer vm.ctx.Lock.Unlock()

		require.NoError(vm.Shutdown(context.Background()))
	})

	return vm, db, msm
}

// Ensure genesis state is parsed from bytes and stored correctly
func TestGenesis(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, latestFork)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	// Ensure the genesis block has been accepted and stored
	genesisBlockID, err := vm.LastAccepted(context.Background()) // lastAccepted should be ID of genesis block
	require.NoError(err)

	genesisBlock, err := vm.manager.GetBlock(genesisBlockID)
	require.NoError(err)
	require.Equal(choices.Accepted, genesisBlock.Status())

	genesisState, _ := defaultGenesis(t, vm.ctx.AVAXAssetID)
	// Ensure all the genesis UTXOs are there
	for _, utxo := range genesisState.UTXOs {
		_, addrBytes, err := address.ParseBech32(utxo.Address)
		require.NoError(err)

		addr, err := ids.ToShortID(addrBytes)
		require.NoError(err)

		addrs := set.Of(addr)
		utxos, err := avax.GetAllUTXOs(vm.state, addrs)
		require.NoError(err)
		require.Len(utxos, 1)

		out := utxos[0].Out.(*secp256k1fx.TransferOutput)
		if out.Amount() != uint64(utxo.Amount) {
			id := keys[0].PublicKey().Address()
			addr, err := address.FormatBech32(constants.UnitTestHRP, id.Bytes())
			require.NoError(err)

			require.Equal(utxo.Address, addr)
			require.Equal(uint64(utxo.Amount)-vm.CreateSubnetTxFee, out.Amount())
		}
	}

	// Ensure current validator set of primary network is correct
	require.Len(genesisState.Validators, vm.Validators.Count(constants.PrimaryNetworkID))

	for _, nodeID := range genesisNodeIDs {
		_, ok := vm.Validators.GetValidator(constants.PrimaryNetworkID, nodeID)
		require.True(ok)
	}

	// Ensure the new subnet we created exists
	_, _, err = vm.state.GetTx(testSubnet1.ID())
	require.NoError(err)
}

// accept proposal to add validator to primary network
func TestAddValidatorCommit(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, latestFork)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	var (
		startTime     = vm.clock.Time().Add(txexecutor.SyncBound).Add(1 * time.Second)
		endTime       = startTime.Add(defaultMinStakingDuration)
		nodeID        = ids.GenerateTestNodeID()
		rewardAddress = ids.GenerateTestShortID()
	)

	// create valid tx
	tx, err := vm.txBuilder.NewAddValidatorTx(
		vm.MinValidatorStake,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		nodeID,
		rewardAddress,
		reward.PercentDenominator,
		[]*secp256k1.PrivateKey{keys[0]},
		ids.ShortEmpty, // change addr
	)
	require.NoError(err)

	// trigger block creation
	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTx(context.Background(), tx))
	vm.ctx.Lock.Lock()

	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))
	require.NoError(blk.Accept(context.Background()))

	_, txStatus, err := vm.state.GetTx(tx.ID())
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	// Verify that new validator now in current validator set
	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)
}

// verify invalid attempt to add validator to primary network
func TestInvalidAddValidatorCommit(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, cortinaFork)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	nodeID := ids.GenerateTestNodeID()
	startTime := defaultGenesisTime.Add(-txexecutor.SyncBound).Add(-1 * time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)

	// create invalid tx
	tx, err := vm.txBuilder.NewAddValidatorTx(
		vm.MinValidatorStake,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		nodeID,
		ids.GenerateTestShortID(),
		reward.PercentDenominator,
		[]*secp256k1.PrivateKey{keys[0]},
		ids.ShortEmpty, // change addr
	)
	require.NoError(err)

	preferredID := vm.manager.Preferred()
	preferred, err := vm.manager.GetBlock(preferredID)
	require.NoError(err)
	preferredHeight := preferred.Height()

	statelessBlk, err := block.NewBanffStandardBlock(
		preferred.Timestamp(),
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{tx},
	)
	require.NoError(err)

	blkBytes := statelessBlk.Bytes()

	parsedBlock, err := vm.ParseBlock(context.Background(), blkBytes)
	require.NoError(err)

	err = parsedBlock.Verify(context.Background())
	require.ErrorIs(err, txexecutor.ErrTimestampNotBeforeStartTime)

	txID := statelessBlk.Txs()[0].ID()
	reason := vm.Builder.GetDropReason(txID)
	require.ErrorIs(reason, txexecutor.ErrTimestampNotBeforeStartTime)
}

// Reject attempt to add validator to primary network
func TestAddValidatorReject(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, cortinaFork)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	var (
		startTime     = vm.clock.Time().Add(txexecutor.SyncBound).Add(1 * time.Second)
		endTime       = startTime.Add(defaultMinStakingDuration)
		nodeID        = ids.GenerateTestNodeID()
		rewardAddress = ids.GenerateTestShortID()
	)

	// create valid tx
	tx, err := vm.txBuilder.NewAddValidatorTx(
		vm.MinValidatorStake,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		nodeID,
		rewardAddress,
		reward.PercentDenominator,
		[]*secp256k1.PrivateKey{keys[0]},
		ids.ShortEmpty, // change addr
	)
	require.NoError(err)

	// trigger block creation
	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTx(context.Background(), tx))
	vm.ctx.Lock.Lock()

	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))
	require.NoError(blk.Reject(context.Background()))

	_, _, err = vm.state.GetTx(tx.ID())
	require.ErrorIs(err, database.ErrNotFound)

	_, err = vm.state.GetPendingValidator(constants.PrimaryNetworkID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)
}

// Reject proposal to add validator to primary network
func TestAddValidatorInvalidNotReissued(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, latestFork)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	// Use nodeID that is already in the genesis
	repeatNodeID := genesisNodeIDs[0]

	startTime := latestForkTime.Add(txexecutor.SyncBound).Add(1 * time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)

	// create valid tx
	tx, err := vm.txBuilder.NewAddValidatorTx(
		vm.MinValidatorStake,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		repeatNodeID,
		ids.GenerateTestShortID(),
		reward.PercentDenominator,
		[]*secp256k1.PrivateKey{keys[0]},
		ids.ShortEmpty, // change addr
	)
	require.NoError(err)

	// trigger block creation
	vm.ctx.Lock.Unlock()
	err = vm.issueTx(context.Background(), tx)
	require.ErrorIs(err, txexecutor.ErrAlreadyValidator)
	vm.ctx.Lock.Lock()
}

// Accept proposal to add validator to subnet
func TestAddSubnetValidatorAccept(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, latestFork)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	var (
		startTime = vm.clock.Time().Add(txexecutor.SyncBound).Add(1 * time.Second)
		endTime   = startTime.Add(defaultMinStakingDuration)
		nodeID    = genesisNodeIDs[0]
	)

	// create valid tx
	// note that [startTime, endTime] is a subset of time that keys[0]
	// validates primary network ([defaultValidateStartTime, defaultValidateEndTime])
	tx, err := vm.txBuilder.NewAddSubnetValidatorTx(
		defaultWeight,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		nodeID,
		testSubnet1.ID(),
		[]*secp256k1.PrivateKey{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		ids.ShortEmpty, // change addr
	)
	require.NoError(err)

	// trigger block creation
	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTx(context.Background(), tx))
	vm.ctx.Lock.Lock()

	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))
	require.NoError(blk.Accept(context.Background()))

	_, txStatus, err := vm.state.GetTx(tx.ID())
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	// Verify that new validator is in current validator set
	_, err = vm.state.GetCurrentValidator(testSubnet1.ID(), nodeID)
	require.NoError(err)
}

// Reject proposal to add validator to subnet
func TestAddSubnetValidatorReject(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, latestFork)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	var (
		startTime = vm.clock.Time().Add(txexecutor.SyncBound).Add(1 * time.Second)
		endTime   = startTime.Add(defaultMinStakingDuration)
		nodeID    = genesisNodeIDs[0]
	)

	// create valid tx
	// note that [startTime, endTime] is a subset of time that keys[0]
	// validates primary network ([defaultValidateStartTime, defaultValidateEndTime])
	tx, err := vm.txBuilder.NewAddSubnetValidatorTx(
		defaultWeight,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		nodeID,
		testSubnet1.ID(),
		[]*secp256k1.PrivateKey{testSubnet1ControlKeys[1], testSubnet1ControlKeys[2]},
		ids.ShortEmpty, // change addr
	)
	require.NoError(err)

	// trigger block creation
	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTx(context.Background(), tx))
	vm.ctx.Lock.Lock()

	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))
	require.NoError(blk.Reject(context.Background()))

	_, _, err = vm.state.GetTx(tx.ID())
	require.ErrorIs(err, database.ErrNotFound)

	// Verify that new validator NOT in validator set
	_, err = vm.state.GetCurrentValidator(testSubnet1.ID(), nodeID)
	require.ErrorIs(err, database.ErrNotFound)
}

// Test case where primary network validator rewarded
func TestRewardValidatorAccept(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, latestFork)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	// Fast forward clock to time for genesis validators to leave
	vm.clock.Set(defaultValidateEndTime)

	// Advance time and create proposal to reward a genesis validator
	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(blk.Verify(context.Background()))

	// Assert preferences are correct
	options, err := blk.(smcon.OracleBlock).Options(context.Background())
	require.NoError(err)

	commit := options[0].(*blockexecutor.Block)
	require.IsType(&block.BanffCommitBlock{}, commit.Block)
	abort := options[1].(*blockexecutor.Block)
	require.IsType(&block.BanffAbortBlock{}, abort.Block)

	// Assert block tries to reward a genesis validator
	rewardTx := blk.(block.Block).Txs()[0].Unsigned
	require.IsType(&txs.RewardValidatorTx{}, rewardTx)

	// Verify options and accept commmit block
	require.NoError(commit.Verify(context.Background()))
	require.NoError(abort.Verify(context.Background()))
	txID := blk.(block.Block).Txs()[0].ID()
	{
		onAbort, ok := vm.manager.GetState(abort.ID())
		require.True(ok)

		_, txStatus, err := onAbort.GetTx(txID)
		require.NoError(err)
		require.Equal(status.Aborted, txStatus)
	}

	require.NoError(blk.Accept(context.Background()))
	require.NoError(commit.Accept(context.Background()))

	// Verify that chain's timestamp has advanced
	timestamp := vm.state.GetTimestamp()
	require.Equal(defaultValidateEndTime.Unix(), timestamp.Unix())

	// Verify that rewarded validator has been removed.
	// Note that test genesis has multiple validators
	// terminating at the same time. The rewarded validator
	// will the first by txID. To make the test more stable
	// (txID changes every time we change any parameter
	// of the tx creating the validator), we explicitly
	//  check that rewarded validator is removed from staker set.
	_, txStatus, err := vm.state.GetTx(txID)
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	tx, _, err := vm.state.GetTx(rewardTx.(*txs.RewardValidatorTx).TxID)
	require.NoError(err)
	require.IsType(&txs.AddValidatorTx{}, tx.Unsigned)

	valTx, _ := tx.Unsigned.(*txs.AddValidatorTx)
	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, valTx.NodeID())
	require.ErrorIs(err, database.ErrNotFound)
}

// Test case where primary network validator not rewarded
func TestRewardValidatorReject(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, latestFork)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	// Fast forward clock to time for genesis validators to leave
	vm.clock.Set(defaultValidateEndTime)

	// Advance time and create proposal to reward a genesis validator
	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(blk.Verify(context.Background()))

	// Assert preferences are correct
	oracleBlk := blk.(smcon.OracleBlock)
	options, err := oracleBlk.Options(context.Background())
	require.NoError(err)

	commit := options[0].(*blockexecutor.Block)
	require.IsType(&block.BanffCommitBlock{}, commit.Block)

	abort := options[1].(*blockexecutor.Block)
	require.IsType(&block.BanffAbortBlock{}, abort.Block)

	// Assert block tries to reward a genesis validator
	rewardTx := oracleBlk.(block.Block).Txs()[0].Unsigned
	require.IsType(&txs.RewardValidatorTx{}, rewardTx)

	// Verify options and accept abort block
	require.NoError(commit.Verify(context.Background()))
	require.NoError(abort.Verify(context.Background()))
	txID := blk.(block.Block).Txs()[0].ID()
	{
		onAccept, ok := vm.manager.GetState(commit.ID())
		require.True(ok)

		_, txStatus, err := onAccept.GetTx(txID)
		require.NoError(err)
		require.Equal(status.Committed, txStatus)
	}

	require.NoError(blk.Accept(context.Background()))
	require.NoError(abort.Accept(context.Background()))

	// Verify that chain's timestamp has advanced
	timestamp := vm.state.GetTimestamp()
	require.Equal(defaultValidateEndTime.Unix(), timestamp.Unix())

	// Verify that rewarded validator has been removed.
	// Note that test genesis has multiple validators
	// terminating at the same time. The rewarded validator
	// will the first by txID. To make the test more stable
	// (txID changes every time we change any parameter
	// of the tx creating the validator), we explicitly
	//  check that rewarded validator is removed from staker set.
	_, txStatus, err := vm.state.GetTx(txID)
	require.NoError(err)
	require.Equal(status.Aborted, txStatus)

	tx, _, err := vm.state.GetTx(rewardTx.(*txs.RewardValidatorTx).TxID)
	require.NoError(err)
	require.IsType(&txs.AddValidatorTx{}, tx.Unsigned)

	valTx, _ := tx.Unsigned.(*txs.AddValidatorTx)
	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, valTx.NodeID())
	require.ErrorIs(err, database.ErrNotFound)
}

// Ensure BuildBlock errors when there is no block to build
func TestUnneededBuildBlock(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, latestFork)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	_, err := vm.Builder.BuildBlock(context.Background())
	require.ErrorIs(err, blockbuilder.ErrNoPendingBlocks)
}

// test acceptance of proposal to create a new chain
func TestCreateChain(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, latestFork)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	tx, err := vm.txBuilder.NewCreateChainTx(
		testSubnet1.ID(),
		nil,
		ids.ID{'t', 'e', 's', 't', 'v', 'm'},
		nil,
		"name",
		[]*secp256k1.PrivateKey{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		ids.ShortEmpty, // change addr
	)
	require.NoError(err)

	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTx(context.Background(), tx))
	vm.ctx.Lock.Lock()

	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err) // should contain proposal to create chain

	require.NoError(blk.Verify(context.Background()))

	require.NoError(blk.Accept(context.Background()))

	_, txStatus, err := vm.state.GetTx(tx.ID())
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	// Verify chain was created
	chains, err := vm.state.GetChains(testSubnet1.ID())
	require.NoError(err)

	foundNewChain := false
	for _, chain := range chains {
		if bytes.Equal(chain.Bytes(), tx.Bytes()) {
			foundNewChain = true
		}
	}
	require.True(foundNewChain)
}

// test where we:
// 1) Create a subnet
// 2) Add a validator to the subnet's current validator set
// 3) Advance timestamp to validator's end time (removing validator from current)
func TestCreateSubnet(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, latestFork)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	nodeID := genesisNodeIDs[0]
	createSubnetTx, err := vm.txBuilder.NewCreateSubnetTx(
		1, // threshold
		[]ids.ShortID{ // control keys
			keys[0].PublicKey().Address(),
			keys[1].PublicKey().Address(),
		},
		[]*secp256k1.PrivateKey{keys[0]}, // payer
		keys[0].PublicKey().Address(),    // change addr
	)
	require.NoError(err)

	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTx(context.Background(), createSubnetTx))
	vm.ctx.Lock.Lock()

	// should contain the CreateSubnetTx
	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))
	require.NoError(blk.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	_, txStatus, err := vm.state.GetTx(createSubnetTx.ID())
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	subnets, err := vm.state.GetSubnets()
	require.NoError(err)

	found := false
	for _, subnet := range subnets {
		if subnet.ID() == createSubnetTx.ID() {
			found = true
			break
		}
	}
	require.True(found)

	// Now that we've created a new subnet, add a validator to that subnet
	startTime := vm.clock.Time().Add(txexecutor.SyncBound).Add(1 * time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)
	// [startTime, endTime] is subset of time keys[0] validates default subnet so tx is valid
	addValidatorTx, err := vm.txBuilder.NewAddSubnetValidatorTx(
		defaultWeight,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		nodeID,
		createSubnetTx.ID(),
		[]*secp256k1.PrivateKey{keys[0]},
		ids.ShortEmpty, // change addr
	)
	require.NoError(err)

	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTx(context.Background(), addValidatorTx))
	vm.ctx.Lock.Lock()

	blk, err = vm.Builder.BuildBlock(context.Background()) // should add validator to the new subnet
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))
	require.NoError(blk.Accept(context.Background())) // add the validator to current validator set
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	txID := blk.(block.Block).Txs()[0].ID()
	_, txStatus, err = vm.state.GetTx(txID)
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	_, err = vm.state.GetPendingValidator(createSubnetTx.ID(), nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	_, err = vm.state.GetCurrentValidator(createSubnetTx.ID(), nodeID)
	require.NoError(err)

	// fast forward clock to time validator should stop validating
	vm.clock.Set(endTime)
	blk, err = vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(blk.Verify(context.Background()))
	require.NoError(blk.Accept(context.Background())) // remove validator from current validator set

	_, err = vm.state.GetPendingValidator(createSubnetTx.ID(), nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	_, err = vm.state.GetCurrentValidator(createSubnetTx.ID(), nodeID)
	require.ErrorIs(err, database.ErrNotFound)
}

// test asset import
func TestAtomicImport(t *testing.T) {
	require := require.New(t)
	vm, baseDB, mutableSharedMemory := defaultVM(t, latestFork)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	utxoID := avax.UTXOID{
		TxID:        ids.Empty.Prefix(1),
		OutputIndex: 1,
	}
	amount := uint64(50000)
	recipientKey := keys[1]

	m := atomic.NewMemory(prefixdb.New([]byte{5}, baseDB))

	mutableSharedMemory.SharedMemory = m.NewSharedMemory(vm.ctx.ChainID)
	peerSharedMemory := m.NewSharedMemory(vm.ctx.XChainID)

	_, err := vm.txBuilder.NewImportTx(
		vm.ctx.XChainID,
		recipientKey.PublicKey().Address(),
		[]*secp256k1.PrivateKey{keys[0]},
		ids.ShortEmpty, // change addr
	)
	require.ErrorIs(err, txbuilder.ErrNoFunds)

	// Provide the avm UTXO

	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  avax.Asset{ID: vm.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: amount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{recipientKey.PublicKey().Address()},
			},
		},
	}
	utxoBytes, err := txs.Codec.Marshal(txs.CodecVersion, utxo)
	require.NoError(err)

	inputID := utxo.InputID()
	require.NoError(peerSharedMemory.Apply(map[ids.ID]*atomic.Requests{
		vm.ctx.ChainID: {
			PutRequests: []*atomic.Element{
				{
					Key:   inputID[:],
					Value: utxoBytes,
					Traits: [][]byte{
						recipientKey.PublicKey().Address().Bytes(),
					},
				},
			},
		},
	}))

	tx, err := vm.txBuilder.NewImportTx(
		vm.ctx.XChainID,
		recipientKey.PublicKey().Address(),
		[]*secp256k1.PrivateKey{recipientKey},
		ids.ShortEmpty, // change addr
	)
	require.NoError(err)

	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTx(context.Background(), tx))
	vm.ctx.Lock.Lock()

	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))

	require.NoError(blk.Accept(context.Background()))

	_, txStatus, err := vm.state.GetTx(tx.ID())
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	inputID = utxoID.InputID()
	_, err = vm.ctx.SharedMemory.Get(vm.ctx.XChainID, [][]byte{inputID[:]})
	require.ErrorIs(err, database.ErrNotFound)
}

// test optimistic asset import
func TestOptimisticAtomicImport(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, apricotPhase3)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	tx := &txs.Tx{Unsigned: &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    vm.ctx.NetworkID,
			BlockchainID: vm.ctx.ChainID,
		}},
		SourceChain: vm.ctx.XChainID,
		ImportedInputs: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID:        ids.Empty.Prefix(1),
				OutputIndex: 1,
			},
			Asset: avax.Asset{ID: vm.ctx.AVAXAssetID},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
			},
		}},
	}}
	require.NoError(tx.Initialize(txs.Codec))

	preferredID := vm.manager.Preferred()
	preferred, err := vm.manager.GetBlock(preferredID)
	require.NoError(err)
	preferredHeight := preferred.Height()

	statelessBlk, err := block.NewApricotAtomicBlock(
		preferredID,
		preferredHeight+1,
		tx,
	)
	require.NoError(err)

	blk := vm.manager.NewBlock(statelessBlk)

	err = blk.Verify(context.Background())
	require.ErrorIs(err, database.ErrNotFound) // erred due to missing shared memory UTXOs

	require.NoError(vm.SetState(context.Background(), snow.Bootstrapping))

	require.NoError(blk.Verify(context.Background())) // skips shared memory UTXO verification during bootstrapping

	require.NoError(blk.Accept(context.Background()))

	require.NoError(vm.SetState(context.Background(), snow.NormalOp))

	_, txStatus, err := vm.state.GetTx(tx.ID())
	require.NoError(err)

	require.Equal(status.Committed, txStatus)
}

// test restarting the node
func TestRestartFullyAccepted(t *testing.T) {
	require := require.New(t)
	db := memdb.New()

	firstDB := prefixdb.New([]byte{}, db)
	firstVM := &VM{Config: config.Config{
		Chains:                 chains.TestManager,
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
		BanffTime:              latestForkTime,
		CortinaTime:            latestForkTime,
		DurangoTime:            latestForkTime,
	}}

	firstCtx := snowtest.Context(t, snowtest.PChainID)

	_, genesisBytes := defaultGenesis(t, firstCtx.AVAXAssetID)

	baseDB := memdb.New()
	atomicDB := prefixdb.New([]byte{1}, baseDB)
	m := atomic.NewMemory(atomicDB)
	firstCtx.SharedMemory = m.NewSharedMemory(firstCtx.ChainID)

	initialClkTime := latestForkTime.Add(time.Second)
	firstVM.clock.Set(initialClkTime)
	firstCtx.Lock.Lock()

	firstMsgChan := make(chan common.Message, 1)
	require.NoError(firstVM.Initialize(
		context.Background(),
		firstCtx,
		firstDB,
		genesisBytes,
		nil,
		nil,
		firstMsgChan,
		nil,
		nil,
	))

	genesisID, err := firstVM.LastAccepted(context.Background())
	require.NoError(err)

	// include a tx to make the block be accepted
	tx := &txs.Tx{Unsigned: &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    firstVM.ctx.NetworkID,
			BlockchainID: firstVM.ctx.ChainID,
		}},
		SourceChain: firstVM.ctx.XChainID,
		ImportedInputs: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID:        ids.Empty.Prefix(1),
				OutputIndex: 1,
			},
			Asset: avax.Asset{ID: firstVM.ctx.AVAXAssetID},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
			},
		}},
	}}
	require.NoError(tx.Initialize(txs.Codec))

	nextChainTime := initialClkTime.Add(time.Second)
	firstVM.clock.Set(initialClkTime)

	preferredID := firstVM.manager.Preferred()
	preferred, err := firstVM.manager.GetBlock(preferredID)
	require.NoError(err)
	preferredHeight := preferred.Height()

	statelessBlk, err := block.NewBanffStandardBlock(
		nextChainTime,
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{tx},
	)
	require.NoError(err)

	firstAdvanceTimeBlk := firstVM.manager.NewBlock(statelessBlk)

	nextChainTime = nextChainTime.Add(2 * time.Second)
	firstVM.clock.Set(nextChainTime)
	require.NoError(firstAdvanceTimeBlk.Verify(context.Background()))
	require.NoError(firstAdvanceTimeBlk.Accept(context.Background()))

	require.NoError(firstVM.Shutdown(context.Background()))
	firstCtx.Lock.Unlock()

	secondVM := &VM{Config: config.Config{
		Chains:                 chains.TestManager,
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
		BanffTime:              latestForkTime,
		CortinaTime:            latestForkTime,
		DurangoTime:            latestForkTime,
	}}

	secondCtx := snowtest.Context(t, snowtest.PChainID)
	secondCtx.SharedMemory = firstCtx.SharedMemory
	secondVM.clock.Set(initialClkTime)
	secondCtx.Lock.Lock()
	defer func() {
		require.NoError(secondVM.Shutdown(context.Background()))
		secondCtx.Lock.Unlock()
	}()

	secondDB := prefixdb.New([]byte{}, db)
	secondMsgChan := make(chan common.Message, 1)
	require.NoError(secondVM.Initialize(
		context.Background(),
		secondCtx,
		secondDB,
		genesisBytes,
		nil,
		nil,
		secondMsgChan,
		nil,
		nil,
	))

	lastAccepted, err := secondVM.LastAccepted(context.Background())
	require.NoError(err)
	require.Equal(genesisID, lastAccepted)
}

// test bootstrapping the node
func TestBootstrapPartiallyAccepted(t *testing.T) {
	require := require.New(t)

	baseDB := memdb.New()
	vmDB := prefixdb.New(chains.VMDBPrefix, baseDB)
	bootstrappingDB := prefixdb.New(chains.ChainBootstrappingDBPrefix, baseDB)
	blocked, err := queue.NewWithMissing(bootstrappingDB, "", prometheus.NewRegistry())
	require.NoError(err)

	vm := &VM{Config: config.Config{
		Chains:                 chains.TestManager,
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
		BanffTime:              latestForkTime,
		CortinaTime:            latestForkTime,
		DurangoTime:            latestForkTime,
	}}

	initialClkTime := latestForkTime.Add(time.Second)
	vm.clock.Set(initialClkTime)
	ctx := snowtest.Context(t, snowtest.PChainID)

	_, genesisBytes := defaultGenesis(t, ctx.AVAXAssetID)

	atomicDB := prefixdb.New([]byte{1}, baseDB)
	m := atomic.NewMemory(atomicDB)
	ctx.SharedMemory = m.NewSharedMemory(ctx.ChainID)

	consensusCtx := snowtest.ConsensusContext(ctx)
	ctx.Lock.Lock()

	msgChan := make(chan common.Message, 1)
	require.NoError(vm.Initialize(
		context.Background(),
		ctx,
		vmDB,
		genesisBytes,
		nil,
		nil,
		msgChan,
		nil,
		nil,
	))

	// include a tx to make the block be accepted
	tx := &txs.Tx{Unsigned: &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    vm.ctx.NetworkID,
			BlockchainID: vm.ctx.ChainID,
		}},
		SourceChain: vm.ctx.XChainID,
		ImportedInputs: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID:        ids.Empty.Prefix(1),
				OutputIndex: 1,
			},
			Asset: avax.Asset{ID: vm.ctx.AVAXAssetID},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
			},
		}},
	}}
	require.NoError(tx.Initialize(txs.Codec))

	nextChainTime := initialClkTime.Add(time.Second)

	preferredID := vm.manager.Preferred()
	preferred, err := vm.manager.GetBlock(preferredID)
	require.NoError(err)
	preferredHeight := preferred.Height()

	statelessBlk, err := block.NewBanffStandardBlock(
		nextChainTime,
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{tx},
	)
	require.NoError(err)

	advanceTimeBlk := vm.manager.NewBlock(statelessBlk)
	require.NoError(err)

	advanceTimeBlkID := advanceTimeBlk.ID()
	advanceTimeBlkBytes := advanceTimeBlk.Bytes()

	peerID := ids.BuildTestNodeID([]byte{1, 2, 3, 4, 5, 4, 3, 2, 1})
	beacons := validators.NewManager()
	require.NoError(beacons.AddStaker(ctx.SubnetID, peerID, nil, ids.Empty, 1))

	benchlist := benchlist.NewNoBenchlist()
	timeoutManager, err := timeout.NewManager(
		&timer.AdaptiveTimeoutConfig{
			InitialTimeout:     time.Millisecond,
			MinimumTimeout:     time.Millisecond,
			MaximumTimeout:     10 * time.Second,
			TimeoutHalflife:    5 * time.Minute,
			TimeoutCoefficient: 1.25,
		},
		benchlist,
		"",
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	go timeoutManager.Dispatch()
	defer timeoutManager.Stop()

	chainRouter := &router.ChainRouter{}

	metrics := prometheus.NewRegistry()
	mc, err := message.NewCreator(logging.NoLog{}, metrics, "dummyNamespace", constants.DefaultNetworkCompressionType, 10*time.Second)
	require.NoError(err)

	require.NoError(chainRouter.Initialize(
		ids.EmptyNodeID,
		logging.NoLog{},
		timeoutManager,
		time.Second,
		set.Set[ids.ID]{},
		true,
		set.Set[ids.ID]{},
		nil,
		router.HealthConfig{},
		"",
		prometheus.NewRegistry(),
	))

	externalSender := &sender.ExternalSenderTest{TB: t}
	externalSender.Default(true)

	// Passes messages from the consensus engine to the network
	gossipConfig := subnets.GossipConfig{
		AcceptedFrontierPeerSize:  1,
		OnAcceptPeerSize:          1,
		AppGossipValidatorSize:    1,
		AppGossipNonValidatorSize: 1,
	}
	sender, err := sender.New(
		consensusCtx,
		mc,
		externalSender,
		chainRouter,
		timeoutManager,
		p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		subnets.New(consensusCtx.NodeID, subnets.Config{GossipConfig: gossipConfig}),
	)
	require.NoError(err)

	isBootstrapped := false
	bootstrapTracker := &common.BootstrapTrackerTest{
		T: t,
		IsBootstrappedF: func() bool {
			return isBootstrapped
		},
		BootstrappedF: func(ids.ID) {
			isBootstrapped = true
		},
	}

	peers := tracker.NewPeers()
	totalWeight, err := beacons.TotalWeight(ctx.SubnetID)
	require.NoError(err)
	startup := tracker.NewStartup(peers, (totalWeight+1)/2)
	beacons.RegisterCallbackListener(ctx.SubnetID, startup)

	// The engine handles consensus
	snowGetHandler, err := snowgetter.New(
		vm,
		sender,
		consensusCtx.Log,
		time.Second,
		2000,
		consensusCtx.Registerer,
	)
	require.NoError(err)

	bootstrapConfig := bootstrap.Config{
		AllGetsServer:                  snowGetHandler,
		Ctx:                            consensusCtx,
		Beacons:                        beacons,
		SampleK:                        beacons.Count(ctx.SubnetID),
		StartupTracker:                 startup,
		Sender:                         sender,
		BootstrapTracker:               bootstrapTracker,
		AncestorsMaxContainersReceived: 2000,
		Blocked:                        blocked,
		VM:                             vm,
	}

	// Asynchronously passes messages from the network to the consensus engine
	cpuTracker, err := timetracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(err)

	h, err := handler.New(
		bootstrapConfig.Ctx,
		beacons,
		msgChan,
		time.Hour,
		2,
		cpuTracker,
		vm,
		subnets.New(ctx.NodeID, subnets.Config{}),
		tracker.NewPeers(),
	)
	require.NoError(err)

	engineConfig := smeng.Config{
		Ctx:           bootstrapConfig.Ctx,
		AllGetsServer: snowGetHandler,
		VM:            bootstrapConfig.VM,
		Sender:        bootstrapConfig.Sender,
		Validators:    beacons,
		Params: snowball.Parameters{
			K:                     1,
			AlphaPreference:       1,
			AlphaConfidence:       1,
			BetaVirtuous:          20,
			BetaRogue:             20,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Consensus: &smcon.Topological{},
	}
	engine, err := smeng.New(engineConfig)
	require.NoError(err)

	bootstrapper, err := bootstrap.New(
		bootstrapConfig,
		engine.Start,
	)
	require.NoError(err)

	h.SetEngineManager(&handler.EngineManager{
		Avalanche: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    engine,
		},
		Snowman: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    engine,
		},
	})

	consensusCtx.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.NormalOp,
	})

	// Allow incoming messages to be routed to the new chain
	chainRouter.AddChain(context.Background(), h)
	ctx.Lock.Unlock()

	h.Start(context.Background(), false)

	ctx.Lock.Lock()
	var reqID uint32
	externalSender.SendF = func(msg message.OutboundMessage, nodeIDs set.Set[ids.NodeID], _ ids.ID, _ subnets.Allower) set.Set[ids.NodeID] {
		inMsg, err := mc.Parse(msg.Bytes(), ctx.NodeID, func() {})
		require.NoError(err)
		require.Equal(message.GetAcceptedFrontierOp, inMsg.Op())

		requestID, ok := message.GetRequestID(inMsg.Message())
		require.True(ok)

		reqID = requestID
		return nodeIDs
	}

	require.NoError(bootstrapper.Connected(context.Background(), peerID, version.CurrentApp))

	externalSender.SendF = func(msg message.OutboundMessage, nodeIDs set.Set[ids.NodeID], _ ids.ID, _ subnets.Allower) set.Set[ids.NodeID] {
		inMsgIntf, err := mc.Parse(msg.Bytes(), ctx.NodeID, func() {})
		require.NoError(err)
		require.Equal(message.GetAcceptedOp, inMsgIntf.Op())
		inMsg := inMsgIntf.Message().(*p2p.GetAccepted)

		reqID = inMsg.RequestId
		return nodeIDs
	}

	require.NoError(bootstrapper.AcceptedFrontier(context.Background(), peerID, reqID, advanceTimeBlkID))

	externalSender.SendF = func(msg message.OutboundMessage, nodeIDs set.Set[ids.NodeID], _ ids.ID, _ subnets.Allower) set.Set[ids.NodeID] {
		inMsgIntf, err := mc.Parse(msg.Bytes(), ctx.NodeID, func() {})
		require.NoError(err)
		require.Equal(message.GetAncestorsOp, inMsgIntf.Op())
		inMsg := inMsgIntf.Message().(*p2p.GetAncestors)

		reqID = inMsg.RequestId

		containerID, err := ids.ToID(inMsg.ContainerId)
		require.NoError(err)
		require.Equal(advanceTimeBlkID, containerID)
		return nodeIDs
	}

	frontier := set.Of(advanceTimeBlkID)
	require.NoError(bootstrapper.Accepted(context.Background(), peerID, reqID, frontier))

	externalSender.SendF = func(msg message.OutboundMessage, nodeIDs set.Set[ids.NodeID], _ ids.ID, _ subnets.Allower) set.Set[ids.NodeID] {
		inMsg, err := mc.Parse(msg.Bytes(), ctx.NodeID, func() {})
		require.NoError(err)
		require.Equal(message.GetAcceptedFrontierOp, inMsg.Op())

		requestID, ok := message.GetRequestID(inMsg.Message())
		require.True(ok)

		reqID = requestID
		return nodeIDs
	}

	require.NoError(bootstrapper.Ancestors(context.Background(), peerID, reqID, [][]byte{advanceTimeBlkBytes}))

	externalSender.SendF = func(msg message.OutboundMessage, nodeIDs set.Set[ids.NodeID], _ ids.ID, _ subnets.Allower) set.Set[ids.NodeID] {
		inMsgIntf, err := mc.Parse(msg.Bytes(), ctx.NodeID, func() {})
		require.NoError(err)
		require.Equal(message.GetAcceptedOp, inMsgIntf.Op())
		inMsg := inMsgIntf.Message().(*p2p.GetAccepted)

		reqID = inMsg.RequestId
		return nodeIDs
	}

	require.NoError(bootstrapper.AcceptedFrontier(context.Background(), peerID, reqID, advanceTimeBlkID))

	externalSender.SendF = nil
	externalSender.CantSend = false

	require.NoError(bootstrapper.Accepted(context.Background(), peerID, reqID, frontier))
	require.Equal(advanceTimeBlk.ID(), vm.manager.Preferred())

	ctx.Lock.Unlock()
	chainRouter.Shutdown(context.Background())
}

func TestUnverifiedParent(t *testing.T) {
	require := require.New(t)

	vm := &VM{Config: config.Config{
		Chains:                 chains.TestManager,
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
		BanffTime:              latestForkTime,
		CortinaTime:            latestForkTime,
		DurangoTime:            latestForkTime,
	}}

	initialClkTime := latestForkTime.Add(time.Second)
	vm.clock.Set(initialClkTime)
	ctx := snowtest.Context(t, snowtest.PChainID)
	ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()

	_, genesisBytes := defaultGenesis(t, ctx.AVAXAssetID)

	msgChan := make(chan common.Message, 1)
	require.NoError(vm.Initialize(
		context.Background(),
		ctx,
		memdb.New(),
		genesisBytes,
		nil,
		nil,
		msgChan,
		nil,
		nil,
	))

	// include a tx1 to make the block be accepted
	tx1 := &txs.Tx{Unsigned: &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    vm.ctx.NetworkID,
			BlockchainID: vm.ctx.ChainID,
		}},
		SourceChain: vm.ctx.XChainID,
		ImportedInputs: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID:        ids.Empty.Prefix(1),
				OutputIndex: 1,
			},
			Asset: avax.Asset{ID: vm.ctx.AVAXAssetID},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
			},
		}},
	}}
	require.NoError(tx1.Initialize(txs.Codec))

	nextChainTime := initialClkTime.Add(time.Second)

	preferredID := vm.manager.Preferred()
	preferred, err := vm.manager.GetBlock(preferredID)
	require.NoError(err)
	preferredHeight := preferred.Height()

	statelessBlk, err := block.NewBanffStandardBlock(
		nextChainTime,
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{tx1},
	)
	require.NoError(err)
	firstAdvanceTimeBlk := vm.manager.NewBlock(statelessBlk)
	require.NoError(firstAdvanceTimeBlk.Verify(context.Background()))

	// include a tx2 to make the block be accepted
	tx2 := &txs.Tx{Unsigned: &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    vm.ctx.NetworkID,
			BlockchainID: vm.ctx.ChainID,
		}},
		SourceChain: vm.ctx.XChainID,
		ImportedInputs: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID:        ids.Empty.Prefix(2),
				OutputIndex: 2,
			},
			Asset: avax.Asset{ID: vm.ctx.AVAXAssetID},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
			},
		}},
	}}
	require.NoError(tx2.Initialize(txs.Codec))
	nextChainTime = nextChainTime.Add(time.Second)
	vm.clock.Set(nextChainTime)
	statelessSecondAdvanceTimeBlk, err := block.NewBanffStandardBlock(
		nextChainTime,
		firstAdvanceTimeBlk.ID(),
		firstAdvanceTimeBlk.Height()+1,
		[]*txs.Tx{tx2},
	)
	require.NoError(err)
	secondAdvanceTimeBlk := vm.manager.NewBlock(statelessSecondAdvanceTimeBlk)

	require.Equal(secondAdvanceTimeBlk.Parent(), firstAdvanceTimeBlk.ID())
	require.NoError(secondAdvanceTimeBlk.Verify(context.Background()))
}

func TestMaxStakeAmount(t *testing.T) {
	vm, _, _ := defaultVM(t, latestFork)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	nodeID := genesisNodeIDs[0]

	tests := []struct {
		description string
		startTime   time.Time
		endTime     time.Time
	}{
		{
			description: "[validator.StartTime] == [startTime] < [endTime] == [validator.EndTime]",
			startTime:   defaultValidateStartTime,
			endTime:     defaultValidateEndTime,
		},
		{
			description: "[validator.StartTime] < [startTime] < [endTime] == [validator.EndTime]",
			startTime:   defaultValidateStartTime.Add(time.Minute),
			endTime:     defaultValidateEndTime,
		},
		{
			description: "[validator.StartTime] == [startTime] < [endTime] < [validator.EndTime]",
			startTime:   defaultValidateStartTime,
			endTime:     defaultValidateEndTime.Add(-time.Minute),
		},
		{
			description: "[validator.StartTime] < [startTime] < [endTime] < [validator.EndTime]",
			startTime:   defaultValidateStartTime.Add(time.Minute),
			endTime:     defaultValidateEndTime.Add(-time.Minute),
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			require := require.New(t)
			staker, err := txexecutor.GetValidator(vm.state, constants.PrimaryNetworkID, nodeID)
			require.NoError(err)

			amount, err := txexecutor.GetMaxWeight(vm.state, staker, test.startTime, test.endTime)
			require.NoError(err)
			require.Equal(defaultWeight, amount)
		})
	}
}

func TestUptimeDisallowedWithRestart(t *testing.T) {
	require := require.New(t)
	latestForkTime = defaultValidateStartTime.Add(defaultMinStakingDuration)
	db := memdb.New()

	firstDB := prefixdb.New([]byte{}, db)
	const firstUptimePercentage = 20 // 20%
	firstVM := &VM{Config: config.Config{
		Chains:                 chains.TestManager,
		UptimePercentage:       firstUptimePercentage / 100.,
		RewardConfig:           defaultRewardConfig,
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		BanffTime:              latestForkTime,
		CortinaTime:            latestForkTime,
		DurangoTime:            latestForkTime,
	}}

	firstCtx := snowtest.Context(t, snowtest.PChainID)
	firstCtx.Lock.Lock()

	_, genesisBytes := defaultGenesis(t, firstCtx.AVAXAssetID)

	firstMsgChan := make(chan common.Message, 1)
	require.NoError(firstVM.Initialize(
		context.Background(),
		firstCtx,
		firstDB,
		genesisBytes,
		nil,
		nil,
		firstMsgChan,
		nil,
		nil,
	))

	initialClkTime := latestForkTime.Add(time.Second)
	firstVM.clock.Set(initialClkTime)

	// Set VM state to NormalOp, to start tracking validators' uptime
	require.NoError(firstVM.SetState(context.Background(), snow.Bootstrapping))
	require.NoError(firstVM.SetState(context.Background(), snow.NormalOp))

	// Fast forward clock so that validators meet 20% uptime required for reward
	durationForReward := defaultValidateEndTime.Sub(defaultValidateStartTime) * firstUptimePercentage / 100
	vmStopTime := defaultValidateStartTime.Add(durationForReward)
	firstVM.clock.Set(vmStopTime)

	// Shutdown VM to stop all genesis validator uptime.
	// At this point they have been validating for the 20% uptime needed to be rewarded
	require.NoError(firstVM.Shutdown(context.Background()))
	firstCtx.Lock.Unlock()

	// Restart the VM with a larger uptime requirement
	secondDB := prefixdb.New([]byte{}, db)
	const secondUptimePercentage = 21 // 21% > firstUptimePercentage, so uptime for reward is not met now
	secondVM := &VM{Config: config.Config{
		Chains:                 chains.TestManager,
		UptimePercentage:       secondUptimePercentage / 100.,
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		BanffTime:              latestForkTime,
		CortinaTime:            latestForkTime,
		DurangoTime:            latestForkTime,
	}}

	secondCtx := snowtest.Context(t, snowtest.PChainID)
	secondCtx.Lock.Lock()
	defer func() {
		require.NoError(secondVM.Shutdown(context.Background()))
		secondCtx.Lock.Unlock()
	}()

	atomicDB := prefixdb.New([]byte{1}, db)
	m := atomic.NewMemory(atomicDB)
	secondCtx.SharedMemory = m.NewSharedMemory(secondCtx.ChainID)

	secondMsgChan := make(chan common.Message, 1)
	require.NoError(secondVM.Initialize(
		context.Background(),
		secondCtx,
		secondDB,
		genesisBytes,
		nil,
		nil,
		secondMsgChan,
		nil,
		nil,
	))

	secondVM.clock.Set(vmStopTime)

	// Set VM state to NormalOp, to start tracking validators' uptime
	require.NoError(secondVM.SetState(context.Background(), snow.Bootstrapping))
	require.NoError(secondVM.SetState(context.Background(), snow.NormalOp))

	// after restart and change of uptime required for reward, push validators to their end of life
	secondVM.clock.Set(defaultValidateEndTime)

	// evaluate a genesis validator for reward
	blk, err := secondVM.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(blk.Verify(context.Background()))

	// Assert preferences are correct.
	// secondVM should prefer abort since uptime requirements are not met anymore
	oracleBlk := blk.(smcon.OracleBlock)
	options, err := oracleBlk.Options(context.Background())
	require.NoError(err)

	abort := options[0].(*blockexecutor.Block)
	require.IsType(&block.BanffAbortBlock{}, abort.Block)

	commit := options[1].(*blockexecutor.Block)
	require.IsType(&block.BanffCommitBlock{}, commit.Block)

	// Assert block tries to reward a genesis validator
	rewardTx := oracleBlk.(block.Block).Txs()[0].Unsigned
	require.IsType(&txs.RewardValidatorTx{}, rewardTx)
	txID := blk.(block.Block).Txs()[0].ID()

	// Verify options and accept abort block
	require.NoError(commit.Verify(context.Background()))
	require.NoError(abort.Verify(context.Background()))
	require.NoError(blk.Accept(context.Background()))
	require.NoError(abort.Accept(context.Background()))
	require.NoError(secondVM.SetPreference(context.Background(), secondVM.manager.LastAccepted()))

	// Verify that rewarded validator has been removed.
	// Note that test genesis has multiple validators
	// terminating at the same time. The rewarded validator
	// will the first by txID. To make the test more stable
	// (txID changes every time we change any parameter
	// of the tx creating the validator), we explicitly
	//  check that rewarded validator is removed from staker set.
	_, txStatus, err := secondVM.state.GetTx(txID)
	require.NoError(err)
	require.Equal(status.Aborted, txStatus)

	tx, _, err := secondVM.state.GetTx(rewardTx.(*txs.RewardValidatorTx).TxID)
	require.NoError(err)
	require.IsType(&txs.AddValidatorTx{}, tx.Unsigned)

	valTx, _ := tx.Unsigned.(*txs.AddValidatorTx)
	_, err = secondVM.state.GetCurrentValidator(constants.PrimaryNetworkID, valTx.NodeID())
	require.ErrorIs(err, database.ErrNotFound)
}

func TestUptimeDisallowedAfterNeverConnecting(t *testing.T) {
	require := require.New(t)
	latestForkTime = defaultValidateStartTime.Add(defaultMinStakingDuration)

	db := memdb.New()

	vm := &VM{Config: config.Config{
		Chains:                 chains.TestManager,
		UptimePercentage:       .2,
		RewardConfig:           defaultRewardConfig,
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		BanffTime:              latestForkTime,
		CortinaTime:            latestForkTime,
		DurangoTime:            latestForkTime,
	}}

	ctx := snowtest.Context(t, snowtest.PChainID)
	ctx.Lock.Lock()

	_, genesisBytes := defaultGenesis(t, ctx.AVAXAssetID)

	atomicDB := prefixdb.New([]byte{1}, db)
	m := atomic.NewMemory(atomicDB)
	ctx.SharedMemory = m.NewSharedMemory(ctx.ChainID)

	msgChan := make(chan common.Message, 1)
	appSender := &common.SenderTest{T: t}
	require.NoError(vm.Initialize(
		context.Background(),
		ctx,
		db,
		genesisBytes,
		nil,
		nil,
		msgChan,
		nil,
		appSender,
	))

	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()

	initialClkTime := latestForkTime.Add(time.Second)
	vm.clock.Set(initialClkTime)

	// Set VM state to NormalOp, to start tracking validators' uptime
	require.NoError(vm.SetState(context.Background(), snow.Bootstrapping))
	require.NoError(vm.SetState(context.Background(), snow.NormalOp))

	// Fast forward clock to time for genesis validators to leave
	vm.clock.Set(defaultValidateEndTime)

	// evaluate a genesis validator for reward
	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(blk.Verify(context.Background()))

	// Assert preferences are correct.
	// vm should prefer abort since uptime requirements are not met.
	oracleBlk := blk.(smcon.OracleBlock)
	options, err := oracleBlk.Options(context.Background())
	require.NoError(err)

	abort := options[0].(*blockexecutor.Block)
	require.IsType(&block.BanffAbortBlock{}, abort.Block)

	commit := options[1].(*blockexecutor.Block)
	require.IsType(&block.BanffCommitBlock{}, commit.Block)

	// Assert block tries to reward a genesis validator
	rewardTx := oracleBlk.(block.Block).Txs()[0].Unsigned
	require.IsType(&txs.RewardValidatorTx{}, rewardTx)
	txID := blk.(block.Block).Txs()[0].ID()

	// Verify options and accept abort block
	require.NoError(commit.Verify(context.Background()))
	require.NoError(abort.Verify(context.Background()))
	require.NoError(blk.Accept(context.Background()))
	require.NoError(abort.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	// Verify that rewarded validator has been removed.
	// Note that test genesis has multiple validators
	// terminating at the same time. The rewarded validator
	// will the first by txID. To make the test more stable
	// (txID changes every time we change any parameter
	// of the tx creating the validator), we explicitly
	//  check that rewarded validator is removed from staker set.
	_, txStatus, err := vm.state.GetTx(txID)
	require.NoError(err)
	require.Equal(status.Aborted, txStatus)

	tx, _, err := vm.state.GetTx(rewardTx.(*txs.RewardValidatorTx).TxID)
	require.NoError(err)
	require.IsType(&txs.AddValidatorTx{}, tx.Unsigned)

	valTx, _ := tx.Unsigned.(*txs.AddValidatorTx)
	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, valTx.NodeID())
	require.ErrorIs(err, database.ErrNotFound)
}

func TestRemovePermissionedValidatorDuringAddPending(t *testing.T) {
	require := require.New(t)

	validatorStartTime := latestForkTime.Add(txexecutor.SyncBound).Add(1 * time.Second)
	validatorEndTime := validatorStartTime.Add(360 * 24 * time.Hour)

	vm, _, _ := defaultVM(t, latestFork)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)

	id := key.PublicKey().Address()
	nodeID := ids.GenerateTestNodeID()

	addValidatorTx, err := vm.txBuilder.NewAddValidatorTx(
		defaultMaxValidatorStake,
		uint64(validatorStartTime.Unix()),
		uint64(validatorEndTime.Unix()),
		nodeID,
		id,
		reward.PercentDenominator,
		[]*secp256k1.PrivateKey{keys[0]},
		keys[0].Address(),
	)
	require.NoError(err)

	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTx(context.Background(), addValidatorTx))
	vm.ctx.Lock.Lock()

	// trigger block creation for the validator tx
	addValidatorBlock, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(addValidatorBlock.Verify(context.Background()))
	require.NoError(addValidatorBlock.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	createSubnetTx, err := vm.txBuilder.NewCreateSubnetTx(
		1,
		[]ids.ShortID{id},
		[]*secp256k1.PrivateKey{keys[0]},
		keys[0].Address(),
	)
	require.NoError(err)

	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTx(context.Background(), createSubnetTx))
	vm.ctx.Lock.Lock()

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
		nodeID,
		createSubnetTx.ID(),
		[]*secp256k1.PrivateKey{key, keys[1]},
		keys[1].Address(),
	)
	require.NoError(err)

	removeSubnetValidatorTx, err := vm.txBuilder.NewRemoveSubnetValidatorTx(
		nodeID,
		createSubnetTx.ID(),
		[]*secp256k1.PrivateKey{key, keys[2]},
		keys[2].Address(),
	)
	require.NoError(err)

	statelessBlock, err := block.NewBanffStandardBlock(
		vm.state.GetTimestamp(),
		createSubnetBlock.ID(),
		createSubnetBlock.Height()+1,
		[]*txs.Tx{
			addSubnetValidatorTx,
			removeSubnetValidatorTx,
		},
	)
	require.NoError(err)

	blockBytes := statelessBlock.Bytes()
	block, err := vm.ParseBlock(context.Background(), blockBytes)
	require.NoError(err)
	require.NoError(block.Verify(context.Background()))
	require.NoError(block.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	_, err = vm.state.GetPendingValidator(createSubnetTx.ID(), nodeID)
	require.ErrorIs(err, database.ErrNotFound)
}

func TestTransferSubnetOwnershipTx(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, latestFork)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	// Create a subnet
	createSubnetTx, err := vm.txBuilder.NewCreateSubnetTx(
		1,
		[]ids.ShortID{keys[0].PublicKey().Address()},
		[]*secp256k1.PrivateKey{keys[0]},
		keys[0].Address(),
	)
	require.NoError(err)
	subnetID := createSubnetTx.ID()

	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTx(context.Background(), createSubnetTx))
	vm.ctx.Lock.Lock()
	createSubnetBlock, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)

	createSubnetRawBlock := createSubnetBlock.(*blockexecutor.Block).Block
	require.IsType(&block.BanffStandardBlock{}, createSubnetRawBlock)
	require.Contains(createSubnetRawBlock.Txs(), createSubnetTx)

	require.NoError(createSubnetBlock.Verify(context.Background()))
	require.NoError(createSubnetBlock.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	subnetOwner, err := vm.state.GetSubnetOwner(subnetID)
	require.NoError(err)
	expectedOwner := &secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs: []ids.ShortID{
			keys[0].PublicKey().Address(),
		},
	}
	require.Equal(expectedOwner, subnetOwner)

	transferSubnetOwnershipTx, err := vm.txBuilder.NewTransferSubnetOwnershipTx(
		subnetID,
		1,
		[]ids.ShortID{keys[1].PublicKey().Address()},
		[]*secp256k1.PrivateKey{keys[0]},
		ids.ShortEmpty, // change addr
	)
	require.NoError(err)

	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTx(context.Background(), transferSubnetOwnershipTx))
	vm.ctx.Lock.Lock()
	transferSubnetOwnershipBlock, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)

	transferSubnetOwnershipRawBlock := transferSubnetOwnershipBlock.(*blockexecutor.Block).Block
	require.IsType(&block.BanffStandardBlock{}, transferSubnetOwnershipRawBlock)
	require.Contains(transferSubnetOwnershipRawBlock.Txs(), transferSubnetOwnershipTx)

	require.NoError(transferSubnetOwnershipBlock.Verify(context.Background()))
	require.NoError(transferSubnetOwnershipBlock.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	subnetOwner, err = vm.state.GetSubnetOwner(subnetID)
	require.NoError(err)
	expectedOwner = &secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs: []ids.ShortID{
			keys[1].PublicKey().Address(),
		},
	}
	require.Equal(expectedOwner, subnetOwner)
}

func TestBaseTx(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, latestFork)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	sendAmt := uint64(100000)
	changeAddr := ids.ShortEmpty

	baseTx, err := vm.txBuilder.NewBaseTx(
		sendAmt,
		secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				keys[1].Address(),
			},
		},
		[]*secp256k1.PrivateKey{keys[0]},
		changeAddr,
	)
	require.NoError(err)

	totalInputAmt := uint64(0)
	key0InputAmt := uint64(0)
	for inputID := range baseTx.Unsigned.InputIDs() {
		utxo, err := vm.state.GetUTXO(inputID)
		require.NoError(err)
		require.IsType(&secp256k1fx.TransferOutput{}, utxo.Out)
		castOut := utxo.Out.(*secp256k1fx.TransferOutput)
		if castOut.AddressesSet().Equals(set.Of(keys[0].Address())) {
			key0InputAmt += castOut.Amt
		}
		totalInputAmt += castOut.Amt
	}
	require.Equal(totalInputAmt, key0InputAmt)

	totalOutputAmt := uint64(0)
	key0OutputAmt := uint64(0)
	key1OutputAmt := uint64(0)
	changeAddrOutputAmt := uint64(0)
	for _, output := range baseTx.Unsigned.Outputs() {
		require.IsType(&secp256k1fx.TransferOutput{}, output.Out)
		castOut := output.Out.(*secp256k1fx.TransferOutput)
		if castOut.AddressesSet().Equals(set.Of(keys[0].Address())) {
			key0OutputAmt += castOut.Amt
		}
		if castOut.AddressesSet().Equals(set.Of(keys[1].Address())) {
			key1OutputAmt += castOut.Amt
		}
		if castOut.AddressesSet().Equals(set.Of(changeAddr)) {
			changeAddrOutputAmt += castOut.Amt
		}
		totalOutputAmt += castOut.Amt
	}
	require.Equal(totalOutputAmt, key0OutputAmt+key1OutputAmt+changeAddrOutputAmt)

	require.Equal(vm.TxFee, totalInputAmt-totalOutputAmt)
	require.Equal(sendAmt, key1OutputAmt)

	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTx(context.Background(), baseTx))
	vm.ctx.Lock.Lock()
	baseTxBlock, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)

	baseTxRawBlock := baseTxBlock.(*blockexecutor.Block).Block
	require.IsType(&block.BanffStandardBlock{}, baseTxRawBlock)
	require.Contains(baseTxRawBlock.Txs(), baseTx)

	require.NoError(baseTxBlock.Verify(context.Background()))
	require.NoError(baseTxBlock.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))
}

func TestPruneMempool(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, latestFork)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	// Create a tx that will be valid regardless of timestamp.
	sendAmt := uint64(100000)
	changeAddr := ids.ShortEmpty

	baseTx, err := vm.txBuilder.NewBaseTx(
		sendAmt,
		secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				keys[1].Address(),
			},
		},
		[]*secp256k1.PrivateKey{keys[0]},
		changeAddr,
	)
	require.NoError(err)

	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTx(context.Background(), baseTx))
	vm.ctx.Lock.Lock()

	// [baseTx] should be in the mempool.
	baseTxID := baseTx.ID()
	_, ok := vm.Builder.Get(baseTxID)
	require.True(ok)

	// Create a tx that will be invalid after time advancement.
	var (
		startTime = vm.clock.Time()
		endTime   = startTime.Add(vm.MinStakeDuration)
	)

	addValidatorTx, err := vm.txBuilder.NewAddValidatorTx(
		defaultMinValidatorStake,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		ids.GenerateTestNodeID(),
		keys[2].Address(),
		20000,
		[]*secp256k1.PrivateKey{keys[1]},
		ids.ShortEmpty,
	)
	require.NoError(err)

	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTx(context.Background(), addValidatorTx))
	vm.ctx.Lock.Lock()

	// Advance clock to [endTime], making [addValidatorTx] invalid.
	vm.clock.Set(endTime)

	// [addValidatorTx] and [baseTx] should still be in the mempool.
	addValidatorTxID := addValidatorTx.ID()
	_, ok = vm.Builder.Get(addValidatorTxID)
	require.True(ok)
	_, ok = vm.Builder.Get(baseTxID)
	require.True(ok)

	vm.ctx.Lock.Unlock()
	require.NoError(vm.pruneMempool())
	vm.ctx.Lock.Lock()

	// [addValidatorTx] should be ejected from the mempool.
	// [baseTx] should still be in the mempool.
	_, ok = vm.Builder.Get(addValidatorTxID)
	require.False(ok)
	_, ok = vm.Builder.Get(baseTxID)
	require.True(ok)
}
