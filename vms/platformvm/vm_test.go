// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"bytes"
	"context"
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
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/bootstrap"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/handler"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/sender"
	"github.com/ava-labs/avalanchego/snow/networking/sender/sendertest"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/math/meter"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/txstest"
	"github.com/ava-labs/avalanchego/vms/platformvm/validators/fee"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/chain/p/wallet"

	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
	smcon "github.com/ava-labs/avalanchego/snow/consensus/snowman"
	smeng "github.com/ava-labs/avalanchego/snow/engine/snowman"
	smblock "github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	snowgetter "github.com/ava-labs/avalanchego/snow/engine/snowman/getter"
	timetracker "github.com/ava-labs/avalanchego/snow/networking/tracker"
	blockbuilder "github.com/ava-labs/avalanchego/vms/platformvm/block/builder"
	blockexecutor "github.com/ava-labs/avalanchego/vms/platformvm/block/executor"
	txexecutor "github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	walletbuilder "github.com/ava-labs/avalanchego/wallet/chain/p/builder"
	walletcommon "github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

const (
	defaultMinDelegatorStake = 1 * units.MilliAvax
	defaultMinValidatorStake = 5 * defaultMinDelegatorStake
	defaultMaxValidatorStake = 100 * defaultMinValidatorStake

	defaultMinStakingDuration = 24 * time.Hour
	defaultMaxStakingDuration = 365 * 24 * time.Hour
)

var (
	defaultRewardConfig = reward.Config{
		MaxConsumptionRate: .12 * reward.PercentDenominator,
		MinConsumptionRate: .10 * reward.PercentDenominator,
		MintingPeriod:      365 * 24 * time.Hour,
		SupplyCap:          720 * units.MegaAvax,
	}

	latestForkTime = genesistest.DefaultValidatorStartTime.Add(time.Second)

	defaultDynamicFeeConfig = gas.Config{
		Weights: gas.Dimensions{
			gas.Bandwidth: 1,
			gas.DBRead:    1,
			gas.DBWrite:   1,
			gas.Compute:   1,
		},
		MaxCapacity:              10_000,
		MaxPerSecond:             1_000,
		TargetPerSecond:          500,
		MinPrice:                 1,
		ExcessConversionConstant: 5_000,
	}
	defaultValidatorFeeConfig = fee.Config{
		Capacity: 100,
		Target:   50,
		// The minimum price is set to 2 so that tests can include cases where
		// L1 validator balances do not evenly divide into a timestamp granular
		// to a second.
		MinPrice:                 2,
		ExcessConversionConstant: 100,
	}

	// subnet that exists at genesis in defaultVM
	testSubnet1 *txs.Tx
)

type mutableSharedMemory struct {
	atomic.SharedMemory
}

func defaultVM(t *testing.T, f upgradetest.Fork) (*VM, database.Database, *mutableSharedMemory) {
	require := require.New(t)

	// always reset latestForkTime (a package level variable)
	// to ensure test independence
	latestForkTime = genesistest.DefaultValidatorStartTime.Add(time.Second)
	vm := &VM{Internal: config.Internal{
		Chains:                 chains.TestManager,
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		SybilProtectionEnabled: true,
		Validators:             validators.NewManager(),
		DynamicFeeConfig:       defaultDynamicFeeConfig,
		ValidatorFeeConfig:     defaultValidatorFeeConfig,
		MinValidatorStake:      defaultMinValidatorStake,
		MaxValidatorStake:      defaultMaxValidatorStake,
		MinDelegatorStake:      defaultMinDelegatorStake,
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
		UpgradeConfig:          upgradetest.GetConfigWithUpgradeTime(f, latestForkTime),
	}}

	db := memdb.New()
	chainDB := prefixdb.New([]byte{0}, db)
	atomicDB := prefixdb.New([]byte{1}, db)

	vm.clock.Set(latestForkTime)
	ctx := snowtest.Context(t, snowtest.PChainID)

	m := atomic.NewMemory(atomicDB)
	msm := &mutableSharedMemory{
		SharedMemory: m.NewSharedMemory(ctx.ChainID),
	}
	ctx.SharedMemory = msm

	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()
	appSender := &enginetest.Sender{}
	appSender.CantSendAppGossip = true
	appSender.SendAppGossipF = func(context.Context, common.SendConfig, []byte) error {
		return nil
	}
	appSender.SendAppErrorF = func(context.Context, ids.NodeID, uint32, int32, string) error {
		return nil
	}

	dynamicConfigBytes := []byte(`{"network":{"max-validator-set-staleness":0}}`)
	require.NoError(vm.Initialize(
		t.Context(),
		ctx,
		chainDB,
		genesistest.NewBytes(t, genesistest.Config{}),
		nil,
		dynamicConfigBytes,
		nil,
		appSender,
	))

	// align chain time and local clock
	vm.state.SetTimestamp(vm.clock.Time())
	vm.state.SetFeeState(gas.State{
		Capacity: defaultDynamicFeeConfig.MaxCapacity,
	})

	require.NoError(vm.SetState(t.Context(), snow.NormalOp))

	wallet := newWallet(t, vm, walletConfig{
		keys: []*secp256k1.PrivateKey{genesistest.DefaultFundedKeys[0]},
	})

	// Create a subnet and store it in testSubnet1
	// Note: following Banff activation, block acceptance will move
	// chain time ahead
	var err error
	testSubnet1, err = wallet.IssueCreateSubnetTx(
		&secp256k1fx.OutputOwners{
			Threshold: 2,
			Addrs: []ids.ShortID{
				genesistest.DefaultFundedKeys[0].Address(),
				genesistest.DefaultFundedKeys[1].Address(),
				genesistest.DefaultFundedKeys[2].Address(),
			},
		},
	)
	require.NoError(err)

	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(testSubnet1))
	vm.ctx.Lock.Lock()
	require.NoError(buildAndAcceptStandardBlock(vm))

	t.Cleanup(func() {
		vm.ctx.Lock.Lock()
		defer vm.ctx.Lock.Unlock()

		require.NoError(vm.Shutdown(t.Context()))
	})

	return vm, db, msm
}

type walletConfig struct {
	keys      []*secp256k1.PrivateKey
	subnetIDs []ids.ID
}

func newWallet(t testing.TB, vm *VM, c walletConfig) wallet.Wallet {
	if len(c.keys) == 0 {
		c.keys = genesistest.DefaultFundedKeys
	}
	return txstest.NewWallet(
		t,
		vm.ctx,
		&vm.Internal,
		vm.state,
		secp256k1fx.NewKeychain(c.keys...),
		c.subnetIDs,
		nil, // validationIDs
		[]ids.ID{vm.ctx.CChainID, vm.ctx.XChainID},
	)
}

// Ensure genesis state is parsed from bytes and stored correctly
func TestGenesis(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, upgradetest.Etna)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	// Ensure the genesis block has been accepted and stored
	genesisBlockID, err := vm.LastAccepted(t.Context()) // lastAccepted should be ID of genesis block
	require.NoError(err)

	// Ensure the genesis block can be retrieved
	genesisBlock, err := vm.manager.GetBlock(genesisBlockID)
	require.NoError(err)
	require.NotNil(genesisBlock)

	genesisState := genesistest.New(t, genesistest.Config{})
	feeCalculator := state.PickFeeCalculator(&vm.Internal, vm.state)
	createSubnetFee, err := feeCalculator.CalculateFee(testSubnet1.Unsigned)
	require.NoError(err)

	// Ensure all the genesis UTXOs are there
	for _, utxo := range genesisState.UTXOs {
		genesisOut := utxo.Out.(*secp256k1fx.TransferOutput)
		utxos, err := avax.GetAllUTXOs(
			vm.state,
			genesisOut.OutputOwners.AddressesSet(),
		)
		require.NoError(err)
		require.Len(utxos, 1)

		out := utxos[0].Out.(*secp256k1fx.TransferOutput)
		if out.Amt != genesisOut.Amt {
			require.Equal(
				[]ids.ShortID{genesistest.DefaultFundedKeys[0].Address()},
				out.OutputOwners.Addrs,
			)
			require.Equal(genesisOut.Amt-createSubnetFee, out.Amt)
		}
	}

	// Ensure current validator set of primary network is correct
	require.Len(genesisState.Validators, vm.Validators.NumValidators(constants.PrimaryNetworkID))

	for _, nodeID := range genesistest.DefaultNodeIDs {
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
	vm, _, _ := defaultVM(t, upgradetest.Latest)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	wallet := newWallet(t, vm, walletConfig{})

	var (
		endTime      = vm.clock.Time().Add(defaultMinStakingDuration)
		nodeID       = ids.GenerateTestNodeID()
		rewardsOwner = &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		}
	)

	sk, err := localsigner.New()
	require.NoError(err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(err)

	// create valid tx
	tx, err := wallet.IssueAddPermissionlessValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				End:    uint64(endTime.Unix()),
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

	// trigger block creation
	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(tx))
	vm.ctx.Lock.Lock()
	require.NoError(buildAndAcceptStandardBlock(vm))

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
	vm, _, _ := defaultVM(t, upgradetest.Cortina)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	wallet := newWallet(t, vm, walletConfig{})

	nodeID := ids.GenerateTestNodeID()
	startTime := genesistest.DefaultValidatorStartTime.Add(-txexecutor.SyncBound).Add(-1 * time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)

	// create invalid tx
	tx, err := wallet.IssueAddValidatorTx(
		&txs.Validator{
			NodeID: nodeID,
			Start:  uint64(startTime.Unix()),
			End:    uint64(endTime.Unix()),
			Wght:   vm.MinValidatorStake,
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		},
		reward.PercentDenominator,
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

	parsedBlock, err := vm.ParseBlock(t.Context(), blkBytes)
	require.NoError(err)

	err = parsedBlock.Verify(t.Context())
	require.ErrorIs(err, txexecutor.ErrTimestampNotBeforeStartTime)

	txID := statelessBlk.Txs()[0].ID()
	reason := vm.Builder.GetDropReason(txID)
	require.ErrorIs(reason, txexecutor.ErrTimestampNotBeforeStartTime)
}

// Reject attempt to add validator to primary network
func TestAddValidatorReject(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, upgradetest.Cortina)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	wallet := newWallet(t, vm, walletConfig{})

	var (
		startTime     = vm.clock.Time().Add(txexecutor.SyncBound).Add(1 * time.Second)
		endTime       = startTime.Add(defaultMinStakingDuration)
		nodeID        = ids.GenerateTestNodeID()
		rewardAddress = ids.GenerateTestShortID()
	)

	// create valid tx
	tx, err := wallet.IssueAddValidatorTx(
		&txs.Validator{
			NodeID: nodeID,
			Start:  uint64(startTime.Unix()),
			End:    uint64(endTime.Unix()),
			Wght:   vm.MinValidatorStake,
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{rewardAddress},
		},
		reward.PercentDenominator,
	)
	require.NoError(err)

	lastAcceptedID, err := vm.LastAccepted(t.Context())
	require.NoError(err)
	lastAccepted, err := vm.GetBlock(t.Context(), lastAcceptedID)
	require.NoError(err)

	statelessBlk, err := block.NewBanffStandardBlock(
		lastAccepted.Timestamp().Add(time.Second),
		lastAccepted.ID(),
		lastAccepted.Height()+1,
		[]*txs.Tx{tx},
	)
	require.NoError(err)

	blk, err := vm.ParseBlock(t.Context(), statelessBlk.Bytes())
	require.NoError(err)

	require.NoError(blk.Verify(t.Context()))
	require.NoError(blk.Reject(t.Context()))

	_, _, err = vm.state.GetTx(tx.ID())
	require.ErrorIs(err, database.ErrNotFound)

	_, err = vm.state.GetPendingValidator(constants.PrimaryNetworkID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)
}

// Reject proposal to add validator to primary network
func TestAddValidatorInvalidNotReissued(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, upgradetest.Latest)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	wallet := newWallet(t, vm, walletConfig{})

	// Use nodeID that is already in the genesis
	repeatNodeID := genesistest.DefaultNodeIDs[0]

	startTime := latestForkTime.Add(txexecutor.SyncBound).Add(1 * time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)

	sk, err := localsigner.New()
	require.NoError(err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(err)

	rewardsOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}

	// create valid tx
	tx, err := wallet.IssueAddPermissionlessValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: repeatNodeID,
				Start:  uint64(startTime.Unix()),
				End:    uint64(endTime.Unix()),
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

	// trigger block creation
	vm.ctx.Lock.Unlock()
	err = vm.issueTxFromRPC(tx)
	vm.ctx.Lock.Lock()
	require.ErrorIs(err, txexecutor.ErrDuplicateValidator)
}

// Accept proposal to add validator to subnet
func TestAddSubnetValidatorAccept(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, upgradetest.Latest)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	subnetID := testSubnet1.ID()
	wallet := newWallet(t, vm, walletConfig{
		subnetIDs: []ids.ID{subnetID},
	})

	var (
		startTime = vm.clock.Time().Add(txexecutor.SyncBound).Add(1 * time.Second)
		endTime   = startTime.Add(defaultMinStakingDuration)
		nodeID    = genesistest.DefaultNodeIDs[0]
	)

	// create valid tx
	// note that [startTime, endTime] is a subset of time that keys[0]
	// validates primary network ([genesistest.DefaultValidatorStartTime, genesistest.DefaultValidatorEndTime])
	tx, err := wallet.IssueAddSubnetValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(startTime.Unix()),
				End:    uint64(endTime.Unix()),
				Wght:   genesistest.DefaultValidatorWeight,
			},
			Subnet: subnetID,
		},
	)
	require.NoError(err)

	// trigger block creation
	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(tx))
	vm.ctx.Lock.Lock()
	require.NoError(buildAndAcceptStandardBlock(vm))

	_, txStatus, err := vm.state.GetTx(tx.ID())
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	// Verify that new validator is in current validator set
	_, err = vm.state.GetCurrentValidator(subnetID, nodeID)
	require.NoError(err)
}

// Reject proposal to add validator to subnet
func TestAddSubnetValidatorReject(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, upgradetest.Latest)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	subnetID := testSubnet1.ID()
	wallet := newWallet(t, vm, walletConfig{
		subnetIDs: []ids.ID{subnetID},
	})

	var (
		startTime = vm.clock.Time().Add(txexecutor.SyncBound).Add(1 * time.Second)
		endTime   = startTime.Add(defaultMinStakingDuration)
		nodeID    = genesistest.DefaultNodeIDs[0]
	)

	// create valid tx
	// note that [startTime, endTime] is a subset of time that keys[0]
	// validates primary network ([genesistest.DefaultValidatorStartTime, genesistest.DefaultValidatorEndTime])
	tx, err := wallet.IssueAddSubnetValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(startTime.Unix()),
				End:    uint64(endTime.Unix()),
				Wght:   genesistest.DefaultValidatorWeight,
			},
			Subnet: testSubnet1.ID(),
		},
	)
	require.NoError(err)

	// trigger block creation
	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(tx))
	vm.ctx.Lock.Lock()

	blk, err := vm.Builder.BuildBlock(t.Context())
	require.NoError(err)

	require.NoError(blk.Verify(t.Context()))
	require.NoError(blk.Reject(t.Context()))

	_, _, err = vm.state.GetTx(tx.ID())
	require.ErrorIs(err, database.ErrNotFound)

	// Verify that new validator NOT in validator set
	_, err = vm.state.GetCurrentValidator(testSubnet1.ID(), nodeID)
	require.ErrorIs(err, database.ErrNotFound)
}

// Test case where primary network validator rewarded
func TestRewardValidatorAccept(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, upgradetest.Latest)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	// Fast forward clock to time for genesis validators to leave
	vm.clock.Set(genesistest.DefaultValidatorEndTime)

	// Advance time and create proposal to reward a genesis validator
	blk, err := vm.Builder.BuildBlock(t.Context())
	require.NoError(err)
	require.NoError(blk.Verify(t.Context()))

	// Assert preferences are correct
	options, err := blk.(smcon.OracleBlock).Options(t.Context())
	require.NoError(err)

	commit := options[0].(*blockexecutor.Block)
	require.IsType(&block.BanffCommitBlock{}, commit.Block)
	abort := options[1].(*blockexecutor.Block)
	require.IsType(&block.BanffAbortBlock{}, abort.Block)

	// Assert block tries to reward a genesis validator
	rewardTx := blk.(block.Block).Txs()[0].Unsigned
	require.IsType(&txs.RewardValidatorTx{}, rewardTx)

	// Verify options and accept commit block
	require.NoError(commit.Verify(t.Context()))
	require.NoError(abort.Verify(t.Context()))
	txID := blk.(block.Block).Txs()[0].ID()
	{
		onAbort, ok := vm.manager.GetState(abort.ID())
		require.True(ok)

		_, txStatus, err := onAbort.GetTx(txID)
		require.NoError(err)
		require.Equal(status.Aborted, txStatus)
	}

	require.NoError(blk.Accept(t.Context()))
	require.NoError(commit.Accept(t.Context()))

	// Verify that chain's timestamp has advanced
	timestamp := vm.state.GetTimestamp()
	require.Equal(genesistest.DefaultValidatorEndTimeUnix, uint64(timestamp.Unix()))

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
	vm, _, _ := defaultVM(t, upgradetest.Latest)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	// Fast forward clock to time for genesis validators to leave
	vm.clock.Set(genesistest.DefaultValidatorEndTime)

	// Advance time and create proposal to reward a genesis validator
	blk, err := vm.Builder.BuildBlock(t.Context())
	require.NoError(err)
	require.NoError(blk.Verify(t.Context()))

	// Assert preferences are correct
	oracleBlk := blk.(smcon.OracleBlock)
	options, err := oracleBlk.Options(t.Context())
	require.NoError(err)

	commit := options[0].(*blockexecutor.Block)
	require.IsType(&block.BanffCommitBlock{}, commit.Block)

	abort := options[1].(*blockexecutor.Block)
	require.IsType(&block.BanffAbortBlock{}, abort.Block)

	// Assert block tries to reward a genesis validator
	rewardTx := oracleBlk.(block.Block).Txs()[0].Unsigned
	require.IsType(&txs.RewardValidatorTx{}, rewardTx)

	// Verify options and accept abort block
	require.NoError(commit.Verify(t.Context()))
	require.NoError(abort.Verify(t.Context()))
	txID := blk.(block.Block).Txs()[0].ID()
	{
		onAccept, ok := vm.manager.GetState(commit.ID())
		require.True(ok)

		_, txStatus, err := onAccept.GetTx(txID)
		require.NoError(err)
		require.Equal(status.Committed, txStatus)
	}

	require.NoError(blk.Accept(t.Context()))
	require.NoError(abort.Accept(t.Context()))

	// Verify that chain's timestamp has advanced
	timestamp := vm.state.GetTimestamp()
	require.Equal(genesistest.DefaultValidatorEndTimeUnix, uint64(timestamp.Unix()))

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
	vm, _, _ := defaultVM(t, upgradetest.Latest)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	_, err := vm.Builder.BuildBlock(t.Context())
	require.ErrorIs(err, blockbuilder.ErrNoPendingBlocks)
}

// test acceptance of proposal to create a new chain
func TestCreateChain(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, upgradetest.Latest)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	subnetID := testSubnet1.ID()
	wallet := newWallet(t, vm, walletConfig{
		subnetIDs: []ids.ID{subnetID},
	})

	tx, err := wallet.IssueCreateChainTx(
		subnetID,
		nil,
		ids.ID{'t', 'e', 's', 't', 'v', 'm'},
		nil,
		"name",
	)
	require.NoError(err)

	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(tx))
	vm.ctx.Lock.Lock()
	require.NoError(buildAndAcceptStandardBlock(vm))

	_, txStatus, err := vm.state.GetTx(tx.ID())
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	// Verify chain was created
	chains, err := vm.state.GetChains(subnetID)
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
	vm, _, _ := defaultVM(t, upgradetest.Latest)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	wallet := newWallet(t, vm, walletConfig{})
	createSubnetTx, err := wallet.IssueCreateSubnetTx(
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				genesistest.DefaultFundedKeys[0].Address(),
				genesistest.DefaultFundedKeys[1].Address(),
			},
		},
	)
	require.NoError(err)

	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(createSubnetTx))
	vm.ctx.Lock.Lock()
	require.NoError(buildAndAcceptStandardBlock(vm))

	subnetID := createSubnetTx.ID()
	_, txStatus, err := vm.state.GetTx(subnetID)
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	subnetIDs, err := vm.state.GetSubnetIDs()
	require.NoError(err)
	require.Contains(subnetIDs, subnetID)

	// Now that we've created a new subnet, add a validator to that subnet
	nodeID := genesistest.DefaultNodeIDs[0]
	startTime := vm.clock.Time().Add(txexecutor.SyncBound).Add(1 * time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)
	// [startTime, endTime] is subset of time keys[0] validates default subnet so tx is valid
	addValidatorTx, err := wallet.IssueAddSubnetValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(startTime.Unix()),
				End:    uint64(endTime.Unix()),
				Wght:   genesistest.DefaultValidatorWeight,
			},
			Subnet: subnetID,
		},
	)
	require.NoError(err)

	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(addValidatorTx))
	vm.ctx.Lock.Lock()
	require.NoError(buildAndAcceptStandardBlock(vm))

	txID := addValidatorTx.ID()
	_, txStatus, err = vm.state.GetTx(txID)
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	_, err = vm.state.GetPendingValidator(subnetID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	_, err = vm.state.GetCurrentValidator(subnetID, nodeID)
	require.NoError(err)

	// remove validator from current validator set
	vm.clock.Set(endTime)
	require.NoError(buildAndAcceptStandardBlock(vm))

	_, err = vm.state.GetPendingValidator(subnetID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	_, err = vm.state.GetCurrentValidator(subnetID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)
}

// test asset import
func TestAtomicImport(t *testing.T) {
	require := require.New(t)
	vm, baseDB, mutableSharedMemory := defaultVM(t, upgradetest.Latest)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	recipientKey := genesistest.DefaultFundedKeys[1]
	importOwners := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{recipientKey.Address()},
	}

	m := atomic.NewMemory(prefixdb.New([]byte{5}, baseDB))
	mutableSharedMemory.SharedMemory = m.NewSharedMemory(vm.ctx.ChainID)

	wallet := newWallet(t, vm, walletConfig{})
	_, err := wallet.IssueImportTx(
		vm.ctx.XChainID,
		importOwners,
	)
	require.ErrorIs(err, walletbuilder.ErrInsufficientFunds)

	// Provide the avm UTXO
	peerSharedMemory := m.NewSharedMemory(vm.ctx.XChainID)
	utxoID := avax.UTXOID{
		TxID:        ids.GenerateTestID(),
		OutputIndex: 1,
	}
	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  avax.Asset{ID: vm.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt:          50 * units.MicroAvax,
			OutputOwners: *importOwners,
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
						recipientKey.Address().Bytes(),
					},
				},
			},
		},
	}))

	// The wallet must be re-loaded because the shared memory has changed
	wallet = newWallet(t, vm, walletConfig{})
	tx, err := wallet.IssueImportTx(
		vm.ctx.XChainID,
		importOwners,
	)
	require.NoError(err)

	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(tx))
	vm.ctx.Lock.Lock()
	require.NoError(buildAndAcceptStandardBlock(vm))

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
	vm, _, _ := defaultVM(t, upgradetest.ApricotPhase3)
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

	err = blk.Verify(t.Context())
	require.ErrorIs(err, database.ErrNotFound) // erred due to missing shared memory UTXOs

	require.NoError(vm.SetState(t.Context(), snow.Bootstrapping))

	require.NoError(blk.Verify(t.Context())) // skips shared memory UTXO verification during bootstrapping

	require.NoError(blk.Accept(t.Context()))

	require.NoError(vm.SetState(t.Context(), snow.NormalOp))

	_, txStatus, err := vm.state.GetTx(tx.ID())
	require.NoError(err)

	require.Equal(status.Committed, txStatus)
}

// test restarting the node
func TestRestartFullyAccepted(t *testing.T) {
	require := require.New(t)
	db := memdb.New()

	firstDB := prefixdb.New([]byte{}, db)
	firstVM := &VM{Internal: config.Internal{
		Chains:                 chains.TestManager,
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
		UpgradeConfig:          upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, latestForkTime),
	}}

	firstCtx := snowtest.Context(t, snowtest.PChainID)

	genesisBytes := genesistest.NewBytes(t, genesistest.Config{})

	baseDB := memdb.New()
	atomicDB := prefixdb.New([]byte{1}, baseDB)
	m := atomic.NewMemory(atomicDB)
	firstCtx.SharedMemory = m.NewSharedMemory(firstCtx.ChainID)

	initialClkTime := latestForkTime.Add(time.Second)
	firstVM.clock.Set(initialClkTime)
	firstCtx.Lock.Lock()

	require.NoError(firstVM.Initialize(
		t.Context(),
		firstCtx,
		firstDB,
		genesisBytes,
		nil,
		nil,
		nil,
		nil,
	))

	genesisID, err := firstVM.LastAccepted(t.Context())
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
	require.NoError(firstAdvanceTimeBlk.Verify(t.Context()))
	require.NoError(firstAdvanceTimeBlk.Accept(t.Context()))

	require.NoError(firstVM.Shutdown(t.Context()))
	firstCtx.Lock.Unlock()

	secondVM := &VM{Internal: config.Internal{
		Chains:                 chains.TestManager,
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
		UpgradeConfig:          upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, latestForkTime),
	}}

	secondCtx := snowtest.Context(t, snowtest.PChainID)
	secondCtx.SharedMemory = firstCtx.SharedMemory
	secondVM.clock.Set(initialClkTime)
	secondCtx.Lock.Lock()
	defer func() {
		require.NoError(secondVM.Shutdown(t.Context()))
		secondCtx.Lock.Unlock()
	}()

	secondDB := prefixdb.New([]byte{}, db)
	require.NoError(secondVM.Initialize(
		t.Context(),
		secondCtx,
		secondDB,
		genesisBytes,
		nil,
		nil,
		nil,
		nil,
	))

	lastAccepted, err := secondVM.LastAccepted(t.Context())
	require.NoError(err)
	require.Equal(genesisID, lastAccepted)
}

// Test that after bootstrapping a node to an oracle block, the preference of
// the child block is correctly initialized by the engine.
func TestBootstrapPartiallyAccepted(t *testing.T) {
	require := require.New(t)

	// Initialize the VM so that we can pass it into the bootstrapping engine.
	baseDB := memdb.New()
	vmDB := prefixdb.New(chains.VMDBPrefix, baseDB)
	bootstrappingDB := prefixdb.New(chains.ChainBootstrappingDBPrefix, baseDB)

	vm := &VM{Internal: config.Internal{
		Chains:                 chains.TestManager,
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
		UpgradeConfig:          upgradetest.GetConfig(upgradetest.Latest),
	}}

	// Advance the time so that the VM will want to remove the first validator.
	vm.clock.Set(genesistest.DefaultValidatorEndTime)

	ctx := snowtest.Context(t, snowtest.PChainID)
	ctx.Lock.Lock()

	require.NoError(vm.Initialize(
		t.Context(),
		ctx,
		vmDB,
		genesistest.NewBytes(t, genesistest.Config{}),
		nil,
		nil,
		nil,
		nil,
	))

	timeoutManager, err := timeout.NewManager(
		&timer.AdaptiveTimeoutConfig{
			InitialTimeout:     time.Millisecond,
			MinimumTimeout:     time.Millisecond,
			MaximumTimeout:     10 * time.Second,
			TimeoutHalflife:    5 * time.Minute,
			TimeoutCoefficient: 1.25,
		},
		benchlist.NewNoBenchlist(),
		prometheus.NewRegistry(),
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	go timeoutManager.Dispatch()
	defer timeoutManager.Stop()

	chainRouter := &router.ChainRouter{}
	require.NoError(chainRouter.Initialize(
		ctx.NodeID,
		ctx.Log,
		timeoutManager,
		time.Second,
		nil,
		true,
		nil,
		nil,
		router.HealthConfig{},
		prometheus.NewRegistry(),
	))

	mc, err := message.NewCreator(
		prometheus.NewRegistry(),
		constants.DefaultNetworkCompressionType,
		10*time.Second,
	)
	require.NoError(err)

	consensusCtx := snowtest.ConsensusContext(ctx)
	externalSender := &sendertest.External{TB: t}
	externalSender.Default(true)
	subnet := subnets.New(ctx.NodeID, subnets.Config{})
	// Passes messages from the consensus engine to the network
	sender, err := sender.New(
		consensusCtx,
		mc,
		externalSender,
		chainRouter,
		timeoutManager,
		p2ppb.EngineType_ENGINE_TYPE_CHAIN,
		subnet,
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	isBootstrapped := false
	bootstrapTracker := &enginetest.BootstrapTracker{
		T: t,
		IsBootstrappedF: func() bool {
			return isBootstrapped
		},
		BootstrappedF: func(ids.ID) {
			isBootstrapped = true
		},
	}

	// Create a validator manager for the bootstrapping engine.
	vdrID := ids.GenerateTestNodeID()
	beacons := validators.NewManager()
	require.NoError(beacons.AddStaker(ctx.SubnetID, vdrID, nil, ids.Empty, 1))

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, 1)
	beacons.RegisterSetCallbackListener(ctx.SubnetID, startup)

	// The engine handles consensus
	snowGetHandler, err := snowgetter.New(
		vm,
		sender,
		ctx.Log,
		time.Second,
		2000,
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	peerTracker, err := p2p.NewPeerTracker(
		ctx.Log,
		"peer_tracker",
		prometheus.NewRegistry(),
		set.Of(ctx.NodeID),
		nil,
	)
	require.NoError(err)

	bootstrapConfig := bootstrap.Config{
		Haltable:                       &common.Halter{},
		NonVerifyingParse:              vm.ParseBlock,
		AllGetsServer:                  snowGetHandler,
		Ctx:                            consensusCtx,
		Beacons:                        beacons,
		SampleK:                        1,
		StartupTracker:                 startup,
		PeerTracker:                    peerTracker,
		Sender:                         sender,
		BootstrapTracker:               bootstrapTracker,
		AncestorsMaxContainersReceived: 2000,
		DB:                             bootstrappingDB,
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

	noopSubscription := func(ctx context.Context) (common.Message, error) {
		<-ctx.Done()
		return common.Message(0), ctx.Err()
	}

	h, err := handler.New(
		bootstrapConfig.Ctx,
		&smblock.ChangeNotifier{},
		noopSubscription,
		beacons,
		time.Hour,
		2,
		cpuTracker,
		subnet,
		peers,
		peerTracker,
		prometheus.NewRegistry(),
		func() {},
	)
	require.NoError(err)

	engineConfig := smeng.Config{
		Ctx:                 bootstrapConfig.Ctx,
		AllGetsServer:       snowGetHandler,
		VM:                  bootstrapConfig.VM,
		Sender:              bootstrapConfig.Sender,
		Validators:          beacons,
		ConnectedValidators: peers,
		Params: snowball.Parameters{
			K:                     1,
			AlphaPreference:       1,
			AlphaConfidence:       1,
			Beta:                  20,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Consensus: &smcon.Topological{Factory: snowball.SnowflakeFactory},
	}
	engine, err := smeng.New(engineConfig)
	require.NoError(err)

	bootstrapper, err := bootstrap.New(
		bootstrapConfig,
		engine.Start,
	)
	require.NoError(err)
	bootstrapper.TimeoutRegistrar = &enginetest.Timer{}

	h.SetEngineManager(&handler.EngineManager{
		DAG: nil,
		Chain: &handler.Engine{
			StateSyncer:  nil,
			Bootstrapper: bootstrapper,
			Consensus:    engine,
		},
	})

	consensusCtx.State.Set(snow.EngineState{
		Type:  p2ppb.EngineType_ENGINE_TYPE_CHAIN,
		State: snow.Bootstrapping,
	})

	// Allow incoming messages to be routed to the new chain
	chainRouter.AddChain(t.Context(), h)
	ctx.Lock.Unlock()

	h.Start(t.Context(), false)

	ctx.Lock.Lock()

	// Mark the validator as connected. We should request the accepted frontier.
	var reqID uint32
	externalSender.SendF = func(msg *message.OutboundMessage, config common.SendConfig, _ ids.ID, _ subnets.Allower) set.Set[ids.NodeID] {
		inMsg, err := mc.Parse(msg.Bytes, ctx.NodeID, func() {})
		require.NoError(err)
		require.Equal(message.GetAcceptedFrontierOp, inMsg.Op)

		requestID, ok := message.GetRequestID(inMsg.Message)
		require.True(ok)

		reqID = requestID
		return config.NodeIDs
	}

	peerTracker.Connected(vdrID, version.Current)
	require.NoError(bootstrapper.Connected(t.Context(), vdrID, version.Current))

	// Create a valid block to remove the first genesis validator that is not
	// related to the VM.
	lastAcceptedID := vm.state.GetLastAccepted()
	lastAccepted, err := vm.state.GetStatelessBlock(lastAcceptedID)
	require.NoError(err)

	currentStakerIterator, err := vm.state.GetCurrentStakerIterator()
	require.NoError(err)
	require.True(currentStakerIterator.Next())

	stakerToRemove := currentStakerIterator.Value()
	rewardValidatorTx, err := blockbuilder.NewRewardValidatorTx(ctx, stakerToRemove.TxID)
	require.NoError(err)
	currentStakerIterator.Release()

	rewardValidatorBlk, err := block.NewBanffProposalBlock(
		stakerToRemove.EndTime,
		lastAcceptedID,
		lastAccepted.Height()+1,
		rewardValidatorTx,
		nil,
	)
	require.NoError(err)

	// Report the validator removal as the last accepted block with the accepted
	// frontier. We should request a confirmation that this block is accepted.
	externalSender.SendF = func(msg *message.OutboundMessage, config common.SendConfig, _ ids.ID, _ subnets.Allower) set.Set[ids.NodeID] {
		inMsgIntf, err := mc.Parse(msg.Bytes, ctx.NodeID, func() {})
		require.NoError(err)
		require.Equal(message.GetAcceptedOp, inMsgIntf.Op)
		inMsg := inMsgIntf.Message.(*p2ppb.GetAccepted)

		reqID = inMsg.RequestId
		return config.NodeIDs
	}

	require.NoError(bootstrapper.AcceptedFrontier(t.Context(), vdrID, reqID, rewardValidatorBlk.ID()))

	// Report the validator removal as accepted. We should request the validator
	// removal block and any ancestors of it.
	externalSender.SendF = func(msg *message.OutboundMessage, config common.SendConfig, _ ids.ID, _ subnets.Allower) set.Set[ids.NodeID] {
		inMsgIntf, err := mc.Parse(msg.Bytes, ctx.NodeID, func() {})
		require.NoError(err)
		require.Equal(message.GetAncestorsOp, inMsgIntf.Op)
		inMsg := inMsgIntf.Message.(*p2ppb.GetAncestors)

		reqID = inMsg.RequestId

		containerID, err := ids.ToID(inMsg.ContainerId)
		require.NoError(err)
		require.Equal(rewardValidatorBlk.ID(), containerID)
		return config.NodeIDs
	}

	frontier := set.Of(rewardValidatorBlk.ID())
	require.NoError(bootstrapper.Accepted(t.Context(), vdrID, reqID, frontier))

	// Provide the validator removal block. We should process this block and
	// then do another round of bootstrapping.
	externalSender.SendF = func(msg *message.OutboundMessage, config common.SendConfig, _ ids.ID, _ subnets.Allower) set.Set[ids.NodeID] {
		inMsg, err := mc.Parse(msg.Bytes, ctx.NodeID, func() {})
		require.NoError(err)
		require.Equal(message.GetAcceptedFrontierOp, inMsg.Op)

		requestID, ok := message.GetRequestID(inMsg.Message)
		require.True(ok)

		reqID = requestID
		return config.NodeIDs
	}

	require.NoError(bootstrapper.Ancestors(t.Context(), vdrID, reqID, [][]byte{rewardValidatorBlk.Bytes()}))

	// We should again report the validator removal block as the last accepted
	// block.
	externalSender.SendF = func(msg *message.OutboundMessage, config common.SendConfig, _ ids.ID, _ subnets.Allower) set.Set[ids.NodeID] {
		inMsgIntf, err := mc.Parse(msg.Bytes, ctx.NodeID, func() {})
		require.NoError(err)
		require.Equal(message.GetAcceptedOp, inMsgIntf.Op)
		inMsg := inMsgIntf.Message.(*p2ppb.GetAccepted)

		reqID = inMsg.RequestId
		return config.NodeIDs
	}

	require.NoError(bootstrapper.AcceptedFrontier(t.Context(), vdrID, reqID, rewardValidatorBlk.ID()))

	// Again confirming the validator removal block as accepted should
	// transition us out of bootstrapping. At this point we should check for
	// option blocks and register them in the correct order.
	externalSender.SendF = nil
	externalSender.CantSend = false

	require.NoError(bootstrapper.Accepted(t.Context(), vdrID, reqID, frontier))

	// Verify the locally preferred option on the validator removal aligns with
	// our consensus preference.
	//
	// We should prefer commit because our VM thinks we have been offline for
	// the staking duration.
	commitBlock, err := block.NewBanffCommitBlock(
		rewardValidatorBlk.Timestamp(),
		rewardValidatorBlk.ID(),
		rewardValidatorBlk.Height()+1,
	)
	require.NoError(err)
	require.Equal(commitBlock.ID(), vm.manager.Preferred())

	ctx.Lock.Unlock()
	chainRouter.Shutdown(t.Context())
}

func TestUnverifiedParent(t *testing.T) {
	require := require.New(t)

	vm := &VM{Internal: config.Internal{
		Chains:                 chains.TestManager,
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
		UpgradeConfig:          upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, latestForkTime),
	}}

	initialClkTime := latestForkTime.Add(time.Second)
	vm.clock.Set(initialClkTime)
	ctx := snowtest.Context(t, snowtest.PChainID)
	ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
		ctx.Lock.Unlock()
	}()

	require.NoError(vm.Initialize(
		t.Context(),
		ctx,
		memdb.New(),
		genesistest.NewBytes(t, genesistest.Config{}),
		nil,
		nil,
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
	require.NoError(firstAdvanceTimeBlk.Verify(t.Context()))

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
	require.NoError(secondAdvanceTimeBlk.Verify(t.Context()))
}

func TestMaxStakeAmount(t *testing.T) {
	vm, _, _ := defaultVM(t, upgradetest.Latest)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	nodeID := genesistest.DefaultNodeIDs[0]

	tests := []struct {
		description string
		startTime   time.Time
		endTime     time.Time
	}{
		{
			description: "[validator.StartTime] == [startTime] < [endTime] == [validator.EndTime]",
			startTime:   genesistest.DefaultValidatorStartTime,
			endTime:     genesistest.DefaultValidatorEndTime,
		},
		{
			description: "[validator.StartTime] < [startTime] < [endTime] == [validator.EndTime]",
			startTime:   genesistest.DefaultValidatorStartTime.Add(time.Minute),
			endTime:     genesistest.DefaultValidatorEndTime,
		},
		{
			description: "[validator.StartTime] == [startTime] < [endTime] < [validator.EndTime]",
			startTime:   genesistest.DefaultValidatorStartTime,
			endTime:     genesistest.DefaultValidatorEndTime.Add(-time.Minute),
		},
		{
			description: "[validator.StartTime] < [startTime] < [endTime] < [validator.EndTime]",
			startTime:   genesistest.DefaultValidatorStartTime.Add(time.Minute),
			endTime:     genesistest.DefaultValidatorEndTime.Add(-time.Minute),
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			require := require.New(t)
			staker, err := txexecutor.GetValidator(vm.state, constants.PrimaryNetworkID, nodeID)
			require.NoError(err)

			amount, err := txexecutor.GetMaxWeight(vm.state, staker, test.startTime, test.endTime)
			require.NoError(err)
			require.Equal(genesistest.DefaultValidatorWeight, amount)
		})
	}
}

func TestUptimeDisallowedWithRestart(t *testing.T) {
	require := require.New(t)
	latestForkTime = genesistest.DefaultValidatorStartTime.Add(defaultMinStakingDuration)
	db := memdb.New()

	firstDB := prefixdb.New([]byte{}, db)
	const firstUptimePercentage = 20 // 20%
	firstVM := &VM{Internal: config.Internal{
		Chains:                 chains.TestManager,
		UptimePercentage:       firstUptimePercentage / 100.,
		RewardConfig:           defaultRewardConfig,
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		UpgradeConfig:          upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, latestForkTime),
	}}

	firstCtx := snowtest.Context(t, snowtest.PChainID)
	firstCtx.Lock.Lock()

	genesisBytes := genesistest.NewBytes(t, genesistest.Config{})

	require.NoError(firstVM.Initialize(
		t.Context(),
		firstCtx,
		firstDB,
		genesisBytes,
		nil,
		nil,
		nil,
		nil,
	))

	initialClkTime := latestForkTime.Add(time.Second)
	firstVM.clock.Set(initialClkTime)

	// Set VM state to NormalOp, to start tracking validators' uptime
	require.NoError(firstVM.SetState(t.Context(), snow.Bootstrapping))
	require.NoError(firstVM.SetState(t.Context(), snow.NormalOp))

	// Fast forward clock so that validators meet 20% uptime required for reward
	durationForReward := genesistest.DefaultValidatorEndTime.Sub(genesistest.DefaultValidatorStartTime) * firstUptimePercentage / 100
	vmStopTime := genesistest.DefaultValidatorStartTime.Add(durationForReward)
	firstVM.clock.Set(vmStopTime)

	// Shutdown VM to stop all genesis validator uptime.
	// At this point they have been validating for the 20% uptime needed to be rewarded
	require.NoError(firstVM.Shutdown(t.Context()))
	firstCtx.Lock.Unlock()

	// Restart the VM with a larger uptime requirement
	secondDB := prefixdb.New([]byte{}, db)
	const secondUptimePercentage = 21 // 21% > firstUptimePercentage, so uptime for reward is not met now
	secondVM := &VM{Internal: config.Internal{
		Chains:                 chains.TestManager,
		UptimePercentage:       secondUptimePercentage / 100.,
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		UpgradeConfig:          upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, latestForkTime),
	}}

	secondCtx := snowtest.Context(t, snowtest.PChainID)
	secondCtx.Lock.Lock()
	defer func() {
		require.NoError(secondVM.Shutdown(t.Context()))
		secondCtx.Lock.Unlock()
	}()

	atomicDB := prefixdb.New([]byte{1}, db)
	m := atomic.NewMemory(atomicDB)
	secondCtx.SharedMemory = m.NewSharedMemory(secondCtx.ChainID)

	require.NoError(secondVM.Initialize(
		t.Context(),
		secondCtx,
		secondDB,
		genesisBytes,
		nil,
		nil,
		nil,
		nil,
	))

	secondVM.clock.Set(vmStopTime)

	// Set VM state to NormalOp, to start tracking validators' uptime
	require.NoError(secondVM.SetState(t.Context(), snow.Bootstrapping))
	require.NoError(secondVM.SetState(t.Context(), snow.NormalOp))

	// after restart and change of uptime required for reward, push validators to their end of life
	secondVM.clock.Set(genesistest.DefaultValidatorEndTime)

	// evaluate a genesis validator for reward
	blk, err := secondVM.Builder.BuildBlock(t.Context())
	require.NoError(err)
	require.NoError(blk.Verify(t.Context()))

	// Assert preferences are correct.
	// secondVM should prefer abort since uptime requirements are not met anymore
	oracleBlk := blk.(smcon.OracleBlock)
	options, err := oracleBlk.Options(t.Context())
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
	require.NoError(commit.Verify(t.Context()))
	require.NoError(abort.Verify(t.Context()))
	require.NoError(blk.Accept(t.Context()))
	require.NoError(abort.Accept(t.Context()))
	require.NoError(secondVM.SetPreference(t.Context(), secondVM.manager.LastAccepted()))

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
	latestForkTime = genesistest.DefaultValidatorStartTime.Add(defaultMinStakingDuration)

	db := memdb.New()

	vm := &VM{Internal: config.Internal{
		Chains:                 chains.TestManager,
		UptimePercentage:       .2,
		RewardConfig:           defaultRewardConfig,
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		UpgradeConfig:          upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, latestForkTime),
	}}

	ctx := snowtest.Context(t, snowtest.PChainID)
	ctx.Lock.Lock()

	atomicDB := prefixdb.New([]byte{1}, db)
	m := atomic.NewMemory(atomicDB)
	ctx.SharedMemory = m.NewSharedMemory(ctx.ChainID)

	appSender := &enginetest.Sender{T: t}
	require.NoError(vm.Initialize(
		t.Context(),
		ctx,
		db,
		genesistest.NewBytes(t, genesistest.Config{}),
		nil,
		nil,
		nil,
		appSender,
	))

	defer func() {
		require.NoError(vm.Shutdown(t.Context()))
		ctx.Lock.Unlock()
	}()

	initialClkTime := latestForkTime.Add(time.Second)
	vm.clock.Set(initialClkTime)

	// Set VM state to NormalOp, to start tracking validators' uptime
	require.NoError(vm.SetState(t.Context(), snow.Bootstrapping))
	require.NoError(vm.SetState(t.Context(), snow.NormalOp))

	// Fast forward clock to time for genesis validators to leave
	vm.clock.Set(genesistest.DefaultValidatorEndTime)

	// evaluate a genesis validator for reward
	blk, err := vm.Builder.BuildBlock(t.Context())
	require.NoError(err)
	require.NoError(blk.Verify(t.Context()))

	// Assert preferences are correct.
	// vm should prefer abort since uptime requirements are not met.
	oracleBlk := blk.(smcon.OracleBlock)
	options, err := oracleBlk.Options(t.Context())
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
	require.NoError(commit.Verify(t.Context()))
	require.NoError(abort.Verify(t.Context()))
	require.NoError(blk.Accept(t.Context()))
	require.NoError(abort.Accept(t.Context()))
	require.NoError(vm.SetPreference(t.Context(), vm.manager.LastAccepted()))

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

	vm, _, _ := defaultVM(t, upgradetest.Latest)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	wallet := newWallet(t, vm, walletConfig{})

	nodeID := ids.GenerateTestNodeID()
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
				Wght:   defaultMaxValidatorStake,
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
	require.NoError(vm.issueTxFromRPC(addValidatorTx))
	vm.ctx.Lock.Lock()
	require.NoError(buildAndAcceptStandardBlock(vm))

	createSubnetTx, err := wallet.IssueCreateSubnetTx(
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{genesistest.DefaultFundedKeys[0].Address()},
		},
	)
	require.NoError(err)

	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(createSubnetTx))
	vm.ctx.Lock.Lock()
	require.NoError(buildAndAcceptStandardBlock(vm))

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

	removeSubnetValidatorTx, err := wallet.IssueRemoveSubnetValidatorTx(
		nodeID,
		subnetID,
	)
	require.NoError(err)

	lastAcceptedID := vm.state.GetLastAccepted()
	lastAcceptedHeight, err := vm.GetCurrentHeight(t.Context())
	require.NoError(err)
	statelessBlock, err := block.NewBanffStandardBlock(
		vm.state.GetTimestamp(),
		lastAcceptedID,
		lastAcceptedHeight+1,
		[]*txs.Tx{
			addSubnetValidatorTx,
			removeSubnetValidatorTx,
		},
	)
	require.NoError(err)

	blockBytes := statelessBlock.Bytes()
	block, err := vm.ParseBlock(t.Context(), blockBytes)
	require.NoError(err)
	require.NoError(block.Verify(t.Context()))
	require.NoError(block.Accept(t.Context()))
	require.NoError(vm.SetPreference(t.Context(), vm.manager.LastAccepted()))

	_, err = vm.state.GetPendingValidator(subnetID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)
}

func TestTransferSubnetOwnershipTx(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, upgradetest.Latest)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	wallet := newWallet(t, vm, walletConfig{})

	expectedSubnetOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{genesistest.DefaultFundedKeys[0].Address()},
	}
	createSubnetTx, err := wallet.IssueCreateSubnetTx(
		expectedSubnetOwner,
	)
	require.NoError(err)

	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(createSubnetTx))
	vm.ctx.Lock.Lock()
	require.NoError(buildAndAcceptStandardBlock(vm))

	subnetID := createSubnetTx.ID()
	subnetOwner, err := vm.state.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(expectedSubnetOwner, subnetOwner)

	expectedSubnetOwner = &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}
	transferSubnetOwnershipTx, err := wallet.IssueTransferSubnetOwnershipTx(
		subnetID,
		expectedSubnetOwner,
	)
	require.NoError(err)

	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(transferSubnetOwnershipTx))
	vm.ctx.Lock.Lock()
	require.NoError(buildAndAcceptStandardBlock(vm))

	subnetOwner, err = vm.state.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(expectedSubnetOwner, subnetOwner)
}

func TestBaseTx(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, upgradetest.Durango)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	wallet := newWallet(t, vm, walletConfig{})

	baseTx, err := wallet.IssueBaseTx(
		[]*avax.TransferableOutput{
			{
				Asset: avax.Asset{ID: vm.ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 100 * units.MicroAvax,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs: []ids.ShortID{
							ids.GenerateTestShortID(),
						},
					},
				},
			},
		},
	)
	require.NoError(err)

	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(baseTx))
	vm.ctx.Lock.Lock()
	require.NoError(buildAndAcceptStandardBlock(vm))

	_, txStatus, err := vm.state.GetTx(baseTx.ID())
	require.NoError(err)
	require.Equal(status.Committed, txStatus)
}

func TestPruneMempool(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, upgradetest.Latest)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	wallet := newWallet(t, vm, walletConfig{})

	// Create a tx that will be valid regardless of timestamp.
	baseTx, err := wallet.IssueBaseTx(
		[]*avax.TransferableOutput{
			{
				Asset: avax.Asset{ID: vm.ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 100 * units.MicroAvax,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs: []ids.ShortID{
							genesistest.DefaultFundedKeys[0].Address(),
						},
					},
				},
			},
		},
		walletcommon.WithCustomAddresses(set.Of(
			genesistest.DefaultFundedKeys[0].Address(),
		)),
	)
	require.NoError(err)

	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(baseTx))
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
				NodeID: ids.GenerateTestNodeID(),
				Start:  uint64(startTime.Unix()),
				End:    uint64(endTime.Unix()),
				Wght:   defaultMinValidatorStake,
			},
			Subnet: constants.PrimaryNetworkID,
		},
		pop,
		vm.ctx.AVAXAssetID,
		rewardsOwner,
		rewardsOwner,
		20000,
		walletcommon.WithCustomAddresses(set.Of(
			genesistest.DefaultFundedKeys[1].Address(),
		)),
	)
	require.NoError(err)

	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(addValidatorTx))
	vm.ctx.Lock.Lock()

	// [addValidatorTx] and [baseTx] should be in the mempool.
	addValidatorTxID := addValidatorTx.ID()
	_, ok = vm.Builder.Get(addValidatorTxID)
	require.True(ok)
	_, ok = vm.Builder.Get(baseTxID)
	require.True(ok)

	// Advance clock to [endTime], making [addValidatorTx] invalid.
	vm.clock.Set(endTime)

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

func TestThrottleBlockBuildingUntilNormalOperationsStart(t *testing.T) {
	require := require.New(t)

	latestForkTime = genesistest.DefaultValidatorStartTime.Add(time.Second)
	vm := &VM{Internal: config.Internal{
		Chains:                 chains.TestManager,
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		SybilProtectionEnabled: true,
		Validators:             validators.NewManager(),
		DynamicFeeConfig:       defaultDynamicFeeConfig,
		ValidatorFeeConfig:     defaultValidatorFeeConfig,
		MinValidatorStake:      defaultMinValidatorStake,
		MaxValidatorStake:      defaultMaxValidatorStake,
		MinDelegatorStake:      defaultMinDelegatorStake,
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
		UpgradeConfig:          upgradetest.GetConfigWithUpgradeTime(upgradetest.Latest, latestForkTime),
	}}

	vm.clock.Set(latestForkTime)
	ctx := snowtest.Context(t, snowtest.PChainID)

	ctx.Lock.Lock()

	require.NoError(vm.Initialize(
		t.Context(),
		ctx,
		memdb.New(),
		genesistest.NewBytes(t, genesistest.Config{}),
		nil,
		nil,
		nil,
		&enginetest.Sender{
			SendAppGossipF: func(context.Context, common.SendConfig, []byte) error {
				return nil
			},
			SendAppErrorF: func(context.Context, ids.NodeID, uint32, int32, string) error {
				return nil
			},
		},
	))
	defer func() {
		vm.ctx.Lock.Lock()
		defer vm.ctx.Lock.Unlock()

		require.NoError(vm.Shutdown(t.Context()))
	}()

	require.NoError(vm.SetState(t.Context(), snow.Bootstrapping))

	// Advance the time so that the block builder would be willing to remove the
	// genesis validators.
	newTime := latestForkTime.Add(genesistest.DefaultValidatorDuration)
	vm.clock.Set(newTime)

	ctx.Lock.Unlock()

	impatientContext, cancel := context.WithTimeout(t.Context(), time.Millisecond*100)
	defer cancel()

	msg, err := vm.WaitForEvent(impatientContext)
	require.ErrorIs(err, context.DeadlineExceeded)
	require.Zero(msg)

	ctx.Lock.Lock()
	require.NoError(vm.SetState(t.Context(), snow.NormalOp))
	ctx.Lock.Unlock()

	impatientContext, cancel = context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	msg, err = vm.WaitForEvent(impatientContext)
	require.NoError(err)
	require.Equal(common.PendingTxs, msg)
}
