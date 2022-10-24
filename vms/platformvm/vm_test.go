// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
	"github.com/ava-labs/avalanchego/message"
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
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math/meter"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	smcon "github.com/ava-labs/avalanchego/snow/consensus/snowman"
	smeng "github.com/ava-labs/avalanchego/snow/engine/snowman"
	snowgetter "github.com/ava-labs/avalanchego/snow/engine/snowman/getter"
	timetracker "github.com/ava-labs/avalanchego/snow/networking/tracker"
	blockexecutor "github.com/ava-labs/avalanchego/vms/platformvm/blocks/executor"
	txexecutor "github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

const (
	testNetworkID = 10 // To be used in tests
	defaultWeight = 10000
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

	// AVAX asset ID in tests
	avaxAssetID = ids.ID{'y', 'e', 'e', 't'}

	defaultTxFee = uint64(100)

	// chain timestamp at genesis
	defaultGenesisTime = time.Date(1997, 1, 1, 0, 0, 0, 0, time.UTC)

	// time that genesis validators start validating
	defaultValidateStartTime = defaultGenesisTime

	// time that genesis validators stop validating
	defaultValidateEndTime = defaultValidateStartTime.Add(10 * defaultMinStakingDuration)

	banffForkTime = defaultValidateEndTime.Add(-5 * defaultMinStakingDuration)

	// each key controls an address that has [defaultBalance] AVAX at genesis
	keys = crypto.BuildTestKeys()

	defaultMinValidatorStake = 5 * units.MilliAvax
	defaultMaxValidatorStake = 500 * units.MilliAvax
	defaultMinDelegatorStake = 1 * units.MilliAvax

	// amount all genesis validators have in defaultVM
	defaultBalance = 100 * defaultMinValidatorStake

	// subnet that exists at genesis in defaultVM
	// Its controlKeys are keys[0], keys[1], keys[2]
	// Its threshold is 2
	testSubnet1            *txs.Tx
	testSubnet1ControlKeys = keys[0:3]

	xChainID = ids.Empty.Prefix(0)
	cChainID = ids.Empty.Prefix(1)

	// Used to create and use keys.
	testKeyFactory crypto.FactorySECP256K1R
)

type snLookup struct {
	chainsToSubnet map[ids.ID]ids.ID
}

func (sn *snLookup) SubnetID(chainID ids.ID) (ids.ID, error) {
	subnetID, ok := sn.chainsToSubnet[chainID]
	if !ok {
		return ids.ID{}, errors.New("missing subnet associated with requested chainID")
	}
	return subnetID, nil
}

type mutableSharedMemory struct {
	atomic.SharedMemory
}

func defaultContext() *snow.Context {
	ctx := snow.DefaultContextTest()
	ctx.NetworkID = testNetworkID
	ctx.XChainID = xChainID
	ctx.AVAXAssetID = avaxAssetID
	aliaser := ids.NewAliaser()

	errs := wrappers.Errs{}
	errs.Add(
		aliaser.Alias(constants.PlatformChainID, "P"),
		aliaser.Alias(constants.PlatformChainID, constants.PlatformChainID.String()),
		aliaser.Alias(xChainID, "X"),
		aliaser.Alias(xChainID, xChainID.String()),
		aliaser.Alias(cChainID, "C"),
		aliaser.Alias(cChainID, cChainID.String()),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
	ctx.BCLookup = aliaser

	ctx.SNLookup = &snLookup{
		chainsToSubnet: map[ids.ID]ids.ID{
			constants.PlatformChainID: constants.PrimaryNetworkID,
			xChainID:                  constants.PrimaryNetworkID,
			cChainID:                  constants.PrimaryNetworkID,
		},
	}
	return ctx
}

// Returns:
// 1) The genesis state
// 2) The byte representation of the default genesis for tests
func defaultGenesis() (*api.BuildGenesisArgs, []byte) {
	genesisUTXOs := make([]api.UTXO, len(keys))
	hrp := constants.NetworkIDToHRP[testNetworkID]
	for i, key := range keys {
		id := key.PublicKey().Address()
		addr, err := address.FormatBech32(hrp, id.Bytes())
		if err != nil {
			panic(err)
		}
		genesisUTXOs[i] = api.UTXO{
			Amount:  json.Uint64(defaultBalance),
			Address: addr,
		}
	}

	genesisValidators := make([]api.PermissionlessValidator, len(keys))
	for i, key := range keys {
		nodeID := ids.NodeID(key.PublicKey().Address())
		addr, err := address.FormatBech32(hrp, nodeID.Bytes())
		if err != nil {
			panic(err)
		}
		genesisValidators[i] = api.PermissionlessValidator{
			Staker: api.Staker{
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
		NetworkID:     json.Uint32(testNetworkID),
		AvaxAssetID:   avaxAssetID,
		UTXOs:         genesisUTXOs,
		Validators:    genesisValidators,
		Chains:        nil,
		Time:          json.Uint64(defaultGenesisTime.Unix()),
		InitialSupply: json.Uint64(360 * units.MegaAvax),
	}

	buildGenesisResponse := api.BuildGenesisReply{}
	platformvmSS := api.StaticService{}
	if err := platformvmSS.BuildGenesis(nil, &buildGenesisArgs, &buildGenesisResponse); err != nil {
		panic(fmt.Errorf("problem while building platform chain's genesis state: %w", err))
	}

	genesisBytes, err := formatting.Decode(buildGenesisResponse.Encoding, buildGenesisResponse.Bytes)
	if err != nil {
		panic(err)
	}

	return &buildGenesisArgs, genesisBytes
}

// Returns:
// 1) The genesis state
// 2) The byte representation of the default genesis for tests
func BuildGenesisTest(t *testing.T) (*api.BuildGenesisArgs, []byte) {
	return BuildGenesisTestWithArgs(t, nil)
}

// Returns:
// 1) The genesis state
// 2) The byte representation of the default genesis for tests
func BuildGenesisTestWithArgs(t *testing.T, args *api.BuildGenesisArgs) (*api.BuildGenesisArgs, []byte) {
	genesisUTXOs := make([]api.UTXO, len(keys))
	hrp := constants.NetworkIDToHRP[testNetworkID]
	for i, key := range keys {
		id := key.PublicKey().Address()
		addr, err := address.FormatBech32(hrp, id.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		genesisUTXOs[i] = api.UTXO{
			Amount:  json.Uint64(defaultBalance),
			Address: addr,
		}
	}

	genesisValidators := make([]api.PermissionlessValidator, len(keys))
	for i, key := range keys {
		nodeID := ids.NodeID(key.PublicKey().Address())
		addr, err := address.FormatBech32(hrp, nodeID.Bytes())
		if err != nil {
			panic(err)
		}
		genesisValidators[i] = api.PermissionlessValidator{
			Staker: api.Staker{
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
		NetworkID:     json.Uint32(testNetworkID),
		AvaxAssetID:   avaxAssetID,
		UTXOs:         genesisUTXOs,
		Validators:    genesisValidators,
		Chains:        nil,
		Time:          json.Uint64(defaultGenesisTime.Unix()),
		InitialSupply: json.Uint64(360 * units.MegaAvax),
		Encoding:      formatting.Hex,
	}

	if args != nil {
		buildGenesisArgs = *args
	}

	buildGenesisResponse := api.BuildGenesisReply{}
	platformvmSS := api.StaticService{}
	if err := platformvmSS.BuildGenesis(nil, &buildGenesisArgs, &buildGenesisResponse); err != nil {
		t.Fatalf("problem while building platform chain's genesis state: %v", err)
	}

	genesisBytes, err := formatting.Decode(buildGenesisResponse.Encoding, buildGenesisResponse.Bytes)
	if err != nil {
		t.Fatal(err)
	}

	return &buildGenesisArgs, genesisBytes
}

func defaultVM() (*VM, database.Database, *mutableSharedMemory) {
	vm := &VM{Factory: Factory{
		Config: config.Config{
			Chains:                 chains.MockManager{},
			UptimeLockedCalculator: uptime.NewLockedCalculator(),
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
			ApricotPhase3Time:      defaultValidateEndTime,
			ApricotPhase5Time:      defaultValidateEndTime,
			BanffTime:              banffForkTime,
		},
	}}

	baseDBManager := manager.NewMemDB(version.Semantic1_0_0)
	chainDBManager := baseDBManager.NewPrefixDBManager([]byte{0})
	atomicDB := prefixdb.New([]byte{1}, baseDBManager.Current().Database)

	vm.clock.Set(banffForkTime.Add(time.Second))
	msgChan := make(chan common.Message, 1)
	ctx := defaultContext()

	m := atomic.NewMemory(atomicDB)
	msm := &mutableSharedMemory{
		SharedMemory: m.NewSharedMemory(ctx.ChainID),
	}
	ctx.SharedMemory = msm

	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()
	_, genesisBytes := defaultGenesis()
	appSender := &common.SenderTest{}
	appSender.CantSendAppGossip = true
	appSender.SendAppGossipF = func(context.Context, []byte) error { return nil }

	if err := vm.Initialize(ctx, chainDBManager, genesisBytes, nil, nil, msgChan, nil, appSender); err != nil {
		panic(err)
	}
	if err := vm.SetState(snow.NormalOp); err != nil {
		panic(err)
	}

	// Create a subnet and store it in testSubnet1
	// Note: following Banff activation, block acceptance will move
	// chain time ahead
	var err error
	testSubnet1, err = vm.txBuilder.NewCreateSubnetTx(
		2, // threshold; 2 sigs from keys[0], keys[1], keys[2] needed to add validator to this subnet
		// control keys are keys[0], keys[1], keys[2]
		[]ids.ShortID{keys[0].PublicKey().Address(), keys[1].PublicKey().Address(), keys[2].PublicKey().Address()},
		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // pays tx fee
		keys[0].PublicKey().Address(),           // change addr
	)
	if err != nil {
		panic(err)
	} else if err := vm.Builder.AddUnverifiedTx(testSubnet1); err != nil {
		panic(err)
	} else if blk, err := vm.Builder.BuildBlock(); err != nil {
		panic(err)
	} else if err := blk.Verify(); err != nil {
		panic(err)
	} else if err := blk.Accept(); err != nil {
		panic(err)
	} else if err := vm.SetPreference(vm.manager.LastAccepted()); err != nil {
		panic(err)
	}

	return vm, baseDBManager.Current().Database, msm
}

func GenesisVMWithArgs(t *testing.T, args *api.BuildGenesisArgs) ([]byte, chan common.Message, *VM, *atomic.Memory) {
	var genesisBytes []byte

	if args != nil {
		_, genesisBytes = BuildGenesisTestWithArgs(t, args)
	} else {
		_, genesisBytes = BuildGenesisTest(t)
	}

	vm := &VM{Factory: Factory{
		Config: config.Config{
			Chains:                 chains.MockManager{},
			Validators:             validators.NewManager(),
			UptimeLockedCalculator: uptime.NewLockedCalculator(),
			TxFee:                  defaultTxFee,
			MinValidatorStake:      defaultMinValidatorStake,
			MaxValidatorStake:      defaultMaxValidatorStake,
			MinDelegatorStake:      defaultMinDelegatorStake,
			MinStakeDuration:       defaultMinStakingDuration,
			MaxStakeDuration:       defaultMaxStakingDuration,
			RewardConfig:           defaultRewardConfig,
			BanffTime:              banffForkTime,
		},
	}}

	baseDBManager := manager.NewMemDB(version.Semantic1_0_0)
	chainDBManager := baseDBManager.NewPrefixDBManager([]byte{0})
	atomicDB := prefixdb.New([]byte{1}, baseDBManager.Current().Database)

	vm.clock.Set(defaultGenesisTime)
	msgChan := make(chan common.Message, 1)
	ctx := defaultContext()

	m := atomic.NewMemory(atomicDB)

	ctx.SharedMemory = m.NewSharedMemory(ctx.ChainID)

	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()
	appSender := &common.SenderTest{T: t}
	appSender.CantSendAppGossip = true
	appSender.SendAppGossipF = func(context.Context, []byte) error { return nil }
	if err := vm.Initialize(ctx, chainDBManager, genesisBytes, nil, nil, msgChan, nil, appSender); err != nil {
		t.Fatal(err)
	}
	if err := vm.SetState(snow.NormalOp); err != nil {
		panic(err)
	}

	// Create a subnet and store it in testSubnet1
	var err error
	testSubnet1, err = vm.txBuilder.NewCreateSubnetTx(
		2, // threshold; 2 sigs from keys[0], keys[1], keys[2] needed to add validator to this subnet
		// control keys are keys[0], keys[1], keys[2]
		[]ids.ShortID{keys[0].PublicKey().Address(), keys[1].PublicKey().Address(), keys[2].PublicKey().Address()},
		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // pays tx fee
		keys[0].PublicKey().Address(),           // change addr
	)
	if err != nil {
		panic(err)
	} else if err := vm.Builder.AddUnverifiedTx(testSubnet1); err != nil {
		panic(err)
	} else if blk, err := vm.Builder.BuildBlock(); err != nil {
		panic(err)
	} else if err := blk.Verify(); err != nil {
		panic(err)
	} else if err := blk.Accept(); err != nil {
		panic(err)
	}

	return genesisBytes, msgChan, vm, m
}

// Ensure genesis state is parsed from bytes and stored correctly
func TestGenesis(t *testing.T) {
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	// Ensure the genesis block has been accepted and stored
	genesisBlockID, err := vm.LastAccepted() // lastAccepted should be ID of genesis block
	if err != nil {
		t.Fatal(err)
	}
	if genesisBlock, err := vm.manager.GetBlock(genesisBlockID); err != nil {
		t.Fatalf("couldn't get genesis block: %v", err)
	} else if genesisBlock.Status() != choices.Accepted {
		t.Fatal("genesis block should be accepted")
	}

	genesisState, _ := defaultGenesis()
	// Ensure all the genesis UTXOs are there
	for _, utxo := range genesisState.UTXOs {
		_, addrBytes, err := address.ParseBech32(utxo.Address)
		if err != nil {
			t.Fatal(err)
		}
		addr, err := ids.ToShortID(addrBytes)
		if err != nil {
			t.Fatal(err)
		}
		addrs := ids.ShortSet{}
		addrs.Add(addr)
		utxos, err := avax.GetAllUTXOs(vm.state, addrs)
		if err != nil {
			t.Fatal("couldn't find UTXO")
		} else if len(utxos) != 1 {
			t.Fatal("expected each address to have one UTXO")
		} else if out, ok := utxos[0].Out.(*secp256k1fx.TransferOutput); !ok {
			t.Fatal("expected utxo output to be type *secp256k1fx.TransferOutput")
		} else if out.Amount() != uint64(utxo.Amount) {
			id := keys[0].PublicKey().Address()
			hrp := constants.NetworkIDToHRP[testNetworkID]
			addr, err := address.FormatBech32(hrp, id.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			if utxo.Address == addr { // Address that paid tx fee to create testSubnet1 has less tokens
				if out.Amount() != uint64(utxo.Amount)-vm.TxFee {
					t.Fatalf("expected UTXO to have value %d but has value %d", uint64(utxo.Amount)-vm.TxFee, out.Amount())
				}
			} else {
				t.Fatalf("expected UTXO to have value %d but has value %d", uint64(utxo.Amount), out.Amount())
			}
		}
	}

	// Ensure current validator set of primary network is correct
	vdrSet, ok := vm.Validators.GetValidators(constants.PrimaryNetworkID)
	if !ok {
		t.Fatalf("Missing the primary network validator set")
	}
	currentValidators := vdrSet.List()
	if len(currentValidators) != len(genesisState.Validators) {
		t.Fatal("vm's current validator set is wrong")
	}
	for _, key := range keys {
		if addr := key.PublicKey().Address(); !vdrSet.Contains(ids.NodeID(addr)) {
			t.Fatalf("should have had validator with NodeID %s", addr)
		}
	}

	// Ensure the new subnet we created exists
	if _, _, err := vm.state.GetTx(testSubnet1.ID()); err != nil {
		t.Fatalf("expected subnet %s to exist", testSubnet1.ID())
	}
}

// accept proposal to add validator to primary network
func TestAddValidatorCommit(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown())
		vm.ctx.Lock.Unlock()
	}()

	startTime := vm.clock.Time().Add(txexecutor.SyncBound).Add(1 * time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)
	nodeID := ids.GenerateTestNodeID()
	rewardAddress := ids.GenerateTestShortID()

	// create valid tx
	tx, err := vm.txBuilder.NewAddValidatorTx(
		vm.MinValidatorStake,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		nodeID,
		rewardAddress,
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty, // change addr
	)
	require.NoError(err)

	// trigger block creation
	require.NoError(vm.Builder.AddUnverifiedTx(tx))

	blk, err := vm.Builder.BuildBlock()
	require.NoError(err)

	require.NoError(blk.Verify())
	require.NoError(blk.Accept())

	_, txStatus, err := vm.state.GetTx(tx.ID())
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	// Verify that new validator now in pending validator set
	_, err = vm.state.GetPendingValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)
}

// verify invalid attempt to add validator to primary network
func TestInvalidAddValidatorCommit(t *testing.T) {
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	startTime := defaultGenesisTime.Add(-txexecutor.SyncBound).Add(-1 * time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)
	key, _ := testKeyFactory.NewPrivateKey()
	nodeID := ids.NodeID(key.PublicKey().Address())

	// create invalid tx
	tx, err := vm.txBuilder.NewAddValidatorTx(
		vm.MinValidatorStake,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		nodeID,
		ids.ShortID(nodeID),
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty, // change addr
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
	statelessBlk, err := blocks.NewBanffStandardBlock(
		preferred.Timestamp(),
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{tx},
	)
	if err != nil {
		t.Fatal(err)
	}
	blk := vm.manager.NewBlock(statelessBlk)
	if err != nil {
		t.Fatal(err)
	}
	blkBytes := blk.Bytes()

	parsedBlock, err := vm.ParseBlock(blkBytes)
	if err != nil {
		t.Fatal(err)
	}

	if err := parsedBlock.Verify(); err == nil {
		t.Fatalf("Should have errored during verification")
	}
	txID := statelessBlk.Txs()[0].ID()
	if _, dropped := vm.Builder.GetDropReason(txID); !dropped {
		t.Fatal("tx should be in dropped tx cache")
	}
}

// Reject attempt to add validator to primary network
func TestAddValidatorReject(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown())
		vm.ctx.Lock.Unlock()
	}()

	startTime := vm.clock.Time().Add(txexecutor.SyncBound).Add(1 * time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)
	nodeID := ids.GenerateTestNodeID()
	rewardAddress := ids.GenerateTestShortID()

	// create valid tx
	tx, err := vm.txBuilder.NewAddValidatorTx(
		vm.MinValidatorStake,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		nodeID,
		rewardAddress,
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty, // change addr
	)
	require.NoError(err)

	// trigger block creation
	require.NoError(vm.Builder.AddUnverifiedTx(tx))

	blk, err := vm.Builder.BuildBlock()
	require.NoError(err)

	require.NoError(blk.Verify())
	require.NoError(blk.Reject())

	_, _, err = vm.state.GetTx(tx.ID())
	require.Error(err, database.ErrNotFound)

	_, err = vm.state.GetPendingValidator(constants.PrimaryNetworkID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)
}

// Reject proposal to add validator to primary network
func TestAddValidatorInvalidNotReissued(t *testing.T) {
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	// Use nodeID that is already in the genesis
	repeatNodeID := ids.NodeID(keys[0].PublicKey().Address())

	startTime := defaultGenesisTime.Add(txexecutor.SyncBound).Add(1 * time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)

	// create valid tx
	tx, err := vm.txBuilder.NewAddValidatorTx(
		vm.MinValidatorStake,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		repeatNodeID,
		ids.ShortID(repeatNodeID),
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		t.Fatal(err)
	}

	// trigger block creation
	if err := vm.Builder.AddUnverifiedTx(tx); err == nil {
		t.Fatal("Expected BuildBlock to error due to adding a validator with a nodeID that is already in the validator set.")
	}
}

// Accept proposal to add validator to subnet
func TestAddSubnetValidatorAccept(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown())
		vm.ctx.Lock.Unlock()
	}()

	startTime := vm.clock.Time().Add(txexecutor.SyncBound).Add(1 * time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)
	nodeID := ids.NodeID(keys[0].PublicKey().Address())

	// create valid tx
	// note that [startTime, endTime] is a subset of time that keys[0]
	// validates primary network ([defaultValidateStartTime, defaultValidateEndTime])
	tx, err := vm.txBuilder.NewAddSubnetValidatorTx(
		defaultWeight,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		nodeID,
		testSubnet1.ID(),
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		ids.ShortEmpty, // change addr
	)
	require.NoError(err)

	// trigger block creation
	require.NoError(vm.Builder.AddUnverifiedTx(tx))

	blk, err := vm.Builder.BuildBlock()
	require.NoError(err)

	require.NoError(blk.Verify())
	require.NoError(blk.Accept())

	_, txStatus, err := vm.state.GetTx(tx.ID())
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	// Verify that new validator is in pending validator set
	_, err = vm.state.GetPendingValidator(testSubnet1.ID(), nodeID)
	require.NoError(err)
}

// Reject proposal to add validator to subnet
func TestAddSubnetValidatorReject(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown())
		vm.ctx.Lock.Unlock()
	}()

	startTime := vm.clock.Time().Add(txexecutor.SyncBound).Add(1 * time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)
	nodeID := ids.NodeID(keys[0].PublicKey().Address())

	// create valid tx
	// note that [startTime, endTime] is a subset of time that keys[0]
	// validates primary network ([defaultValidateStartTime, defaultValidateEndTime])
	tx, err := vm.txBuilder.NewAddSubnetValidatorTx(
		defaultWeight,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		nodeID,
		testSubnet1.ID(),
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[1], testSubnet1ControlKeys[2]},
		ids.ShortEmpty, // change addr
	)
	require.NoError(err)

	// trigger block creation
	require.NoError(vm.Builder.AddUnverifiedTx(tx))

	blk, err := vm.Builder.BuildBlock()
	require.NoError(err)

	require.NoError(blk.Verify())
	require.NoError(blk.Reject())

	_, _, err = vm.state.GetTx(tx.ID())
	require.Error(err, database.ErrNotFound)

	// Verify that new validator NOT in pending validator set
	_, err = vm.state.GetPendingValidator(testSubnet1.ID(), nodeID)
	require.ErrorIs(err, database.ErrNotFound)
}

// Test case where primary network validator rewarded
func TestRewardValidatorAccept(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown())
		vm.ctx.Lock.Unlock()
	}()

	// Fast forward clock to time for genesis validators to leave
	vm.clock.Set(defaultValidateEndTime)

	blk, err := vm.Builder.BuildBlock() // should advance time
	require.NoError(err)

	require.NoError(blk.Verify())

	// Assert preferences are correct
	block := blk.(smcon.OracleBlock)
	options, err := block.Options()
	require.NoError(err)

	commit := options[0].(*blockexecutor.Block)
	_, ok := commit.Block.(*blocks.BanffCommitBlock)
	require.True(ok)
	abort := options[1].(*blockexecutor.Block)
	_, ok = abort.Block.(*blocks.BanffAbortBlock)
	require.True(ok)

	require.NoError(block.Accept())
	require.NoError(commit.Verify())
	require.NoError(abort.Verify())

	txID := blk.(blocks.Block).Txs()[0].ID()
	{
		onAccept, ok := vm.manager.GetState(abort.ID())
		require.True(ok)

		_, txStatus, err := onAccept.GetTx(txID)
		require.NoError(err)
		require.Equal(status.Aborted, txStatus)
	}

	require.NoError(commit.Accept()) // advance the timestamp
	lastAcceptedID, err := vm.LastAccepted()
	require.NoError(err)
	require.NoError(vm.SetPreference(lastAcceptedID))

	_, txStatus, err := vm.state.GetTx(txID)
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	// Verify that chain's timestamp has advanced
	timestamp := vm.state.GetTimestamp()
	require.Equal(defaultValidateEndTime.Unix(), timestamp.Unix())

	blk, err = vm.Builder.BuildBlock() // should contain proposal to reward genesis validator
	require.NoError(err)

	require.NoError(blk.Verify())

	// Assert preferences are correct
	block = blk.(smcon.OracleBlock)
	options, err = block.Options()
	require.NoError(err)

	commit = options[0].(*blockexecutor.Block)
	_, ok = commit.Block.(*blocks.BanffCommitBlock)
	require.True(ok)

	abort = options[1].(*blockexecutor.Block)
	_, ok = abort.Block.(*blocks.BanffAbortBlock)
	require.True(ok)

	require.NoError(block.Accept())
	require.NoError(commit.Verify())
	require.NoError(abort.Verify())

	txID = blk.(blocks.Block).Txs()[0].ID()
	{
		onAccept, ok := vm.manager.GetState(abort.ID())
		require.True(ok)

		_, txStatus, err := onAccept.GetTx(txID)
		require.NoError(err)
		require.Equal(status.Aborted, txStatus)
	}

	require.NoError(commit.Accept()) // reward the genesis validator

	_, txStatus, err = vm.state.GetTx(txID)
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, ids.NodeID(keys[1].PublicKey().Address()))
	require.ErrorIs(err, database.ErrNotFound)
}

// Test case where primary network validator not rewarded
func TestRewardValidatorReject(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown())
		vm.ctx.Lock.Unlock()
	}()

	// Fast forward clock to time for genesis validators to leave
	vm.clock.Set(defaultValidateEndTime)

	blk, err := vm.Builder.BuildBlock() // should advance time
	require.NoError(err)
	require.NoError(blk.Verify())

	// Assert preferences are correct
	block := blk.(smcon.OracleBlock)
	options, err := block.Options()
	require.NoError(err)

	commit := options[0].(*blockexecutor.Block)
	_, ok := commit.Block.(*blocks.BanffCommitBlock)
	require.True(ok)

	abort := options[1].(*blockexecutor.Block)
	_, ok = abort.Block.(*blocks.BanffAbortBlock)
	require.True(ok)

	require.NoError(block.Accept())
	require.NoError(commit.Verify())
	require.NoError(abort.Verify())

	txID := blk.(blocks.Block).Txs()[0].ID()
	{
		onAccept, ok := vm.manager.GetState(abort.ID())
		require.True(ok)

		_, txStatus, err := onAccept.GetTx(txID)
		require.NoError(err)
		require.Equal(status.Aborted, txStatus)
	}

	require.NoError(commit.Accept()) // advance the timestamp
	require.NoError(vm.SetPreference(vm.manager.LastAccepted()))

	_, txStatus, err := vm.state.GetTx(txID)
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	timestamp := vm.state.GetTimestamp()
	require.Equal(defaultValidateEndTime.Unix(), timestamp.Unix())

	blk, err = vm.Builder.BuildBlock() // should contain proposal to reward genesis validator
	require.NoError(err)

	require.NoError(blk.Verify())

	block = blk.(smcon.OracleBlock)
	options, err = block.Options()
	require.NoError(err)

	commit = options[0].(*blockexecutor.Block)
	_, ok = commit.Block.(*blocks.BanffCommitBlock)
	require.True(ok)

	abort = options[1].(*blockexecutor.Block)
	_, ok = abort.Block.(*blocks.BanffAbortBlock)
	require.True(ok)

	require.NoError(blk.Accept())
	require.NoError(commit.Verify())

	txID = blk.(blocks.Block).Txs()[0].ID()
	{
		onAccept, ok := vm.manager.GetState(commit.ID())
		require.True(ok)

		_, txStatus, err := onAccept.GetTx(txID)
		require.NoError(err)
		require.Equal(status.Committed, txStatus)
	}

	require.NoError(abort.Verify())
	require.NoError(abort.Accept()) // do not reward the genesis validator

	_, txStatus, err = vm.state.GetTx(txID)
	require.NoError(err)
	require.Equal(status.Aborted, txStatus)

	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, ids.NodeID(keys[1].PublicKey().Address()))
	require.ErrorIs(err, database.ErrNotFound)
}

// Test case where primary network validator is preferred to be rewarded
func TestRewardValidatorPreferred(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown())
		vm.ctx.Lock.Unlock()
	}()

	// Fast forward clock to time for genesis validators to leave
	vm.clock.Set(defaultValidateEndTime)

	blk, err := vm.Builder.BuildBlock() // should advance time
	require.NoError(err)
	require.NoError(blk.Verify())

	// Assert preferences are correct
	block := blk.(smcon.OracleBlock)
	options, err := block.Options()
	require.NoError(err)

	commit := options[0].(*blockexecutor.Block)
	_, ok := commit.Block.(*blocks.BanffCommitBlock)
	require.True(ok)

	abort := options[1].(*blockexecutor.Block)
	_, ok = abort.Block.(*blocks.BanffAbortBlock)
	require.True(ok)

	require.NoError(block.Accept())
	require.NoError(commit.Verify())
	require.NoError(abort.Verify())

	txID := blk.(blocks.Block).Txs()[0].ID()
	{
		onAccept, ok := vm.manager.GetState(abort.ID())
		require.True(ok)

		_, txStatus, err := onAccept.GetTx(txID)
		require.NoError(err)
		require.Equal(status.Aborted, txStatus)
	}

	require.NoError(commit.Accept()) // advance the timestamp
	require.NoError(vm.SetPreference(vm.manager.LastAccepted()))

	_, txStatus, err := vm.state.GetTx(txID)
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	timestamp := vm.state.GetTimestamp()
	require.Equal(defaultValidateEndTime.Unix(), timestamp.Unix())

	// should contain proposal to reward genesis validator
	blk, err = vm.Builder.BuildBlock()
	require.NoError(err)

	require.NoError(blk.Verify())

	block = blk.(smcon.OracleBlock)
	options, err = block.Options()
	require.NoError(err)

	commit = options[0].(*blockexecutor.Block)
	_, ok = commit.Block.(*blocks.BanffCommitBlock)
	require.True(ok)

	abort = options[1].(*blockexecutor.Block)
	_, ok = abort.Block.(*blocks.BanffAbortBlock)
	require.True(ok)

	require.NoError(blk.Accept())
	require.NoError(commit.Verify())

	txID = blk.(blocks.Block).Txs()[0].ID()
	{
		onAccept, ok := vm.manager.GetState(commit.ID())
		require.True(ok)

		_, txStatus, err := onAccept.GetTx(txID)
		require.NoError(err)
		require.Equal(status.Committed, txStatus)
	}

	require.NoError(abort.Verify())
	require.NoError(abort.Accept()) // do not reward the genesis validator

	_, txStatus, err = vm.state.GetTx(txID)
	require.NoError(err)
	require.Equal(status.Aborted, txStatus)

	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, ids.NodeID(keys[1].PublicKey().Address()))
	require.ErrorIs(err, database.ErrNotFound)
}

// Ensure BuildBlock errors when there is no block to build
func TestUnneededBuildBlock(t *testing.T) {
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()
	if _, err := vm.Builder.BuildBlock(); err == nil {
		t.Fatalf("Should have errored on BuildBlock")
	}
}

// test acceptance of proposal to create a new chain
func TestCreateChain(t *testing.T) {
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	tx, err := vm.txBuilder.NewCreateChainTx(
		testSubnet1.ID(),
		nil,
		ids.ID{'t', 'e', 's', 't', 'v', 'm'},
		nil,
		"name",
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		t.Fatal(err)
	} else if err := vm.Builder.AddUnverifiedTx(tx); err != nil {
		t.Fatal(err)
	} else if blk, err := vm.Builder.BuildBlock(); err != nil { // should contain proposal to create chain
		t.Fatal(err)
	} else if err := blk.Verify(); err != nil {
		t.Fatal(err)
	} else if err := blk.Accept(); err != nil {
		t.Fatal(err)
	} else if _, txStatus, err := vm.state.GetTx(tx.ID()); err != nil {
		t.Fatal(err)
	} else if txStatus != status.Committed {
		t.Fatalf("status should be Committed but is %s", txStatus)
	}

	// Verify chain was created
	chains, err := vm.state.GetChains(testSubnet1.ID())
	if err != nil {
		t.Fatal(err)
	}
	foundNewChain := false
	for _, chain := range chains {
		if bytes.Equal(chain.Bytes(), tx.Bytes()) {
			foundNewChain = true
		}
	}
	if !foundNewChain {
		t.Fatal("should've created new chain but didn't")
	}
}

// test where we:
// 1) Create a subnet
// 2) Add a validator to the subnet's pending validator set
// 3) Advance timestamp to validator's start time (moving the validator from pending to current)
// 4) Advance timestamp to validator's end time (removing validator from current)
func TestCreateSubnet(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown())
		vm.ctx.Lock.Unlock()
	}()

	nodeID := ids.NodeID(keys[0].PublicKey().Address())

	createSubnetTx, err := vm.txBuilder.NewCreateSubnetTx(
		1, // threshold
		[]ids.ShortID{ // control keys
			keys[0].PublicKey().Address(),
			keys[1].PublicKey().Address(),
		},
		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // payer
		keys[0].PublicKey().Address(),           // change addr
	)
	require.NoError(err)

	require.NoError(vm.Builder.AddUnverifiedTx(createSubnetTx))

	// should contain proposal to create subnet
	blk, err := vm.Builder.BuildBlock()
	require.NoError(err)

	require.NoError(blk.Verify())
	require.NoError(blk.Accept())
	require.NoError(vm.SetPreference(vm.manager.LastAccepted()))

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
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty, // change addr
	)
	require.NoError(err)

	require.NoError(vm.Builder.AddUnverifiedTx(addValidatorTx))

	blk, err = vm.Builder.BuildBlock() // should add validator to the new subnet
	require.NoError(err)

	require.NoError(blk.Verify())
	require.NoError(blk.Accept()) // add the validator to pending validator set
	require.NoError(vm.SetPreference(vm.manager.LastAccepted()))

	txID := blk.(blocks.Block).Txs()[0].ID()
	_, txStatus, err = vm.state.GetTx(txID)
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	_, err = vm.state.GetPendingValidator(createSubnetTx.ID(), nodeID)
	require.NoError(err)

	// Advance time to when new validator should start validating
	// Create a block with an advance time tx that moves validator
	// from pending to current validator set
	vm.clock.Set(startTime)
	blk, err = vm.Builder.BuildBlock() // should be advance time tx
	require.NoError(err)
	require.NoError(blk.Verify())
	require.NoError(blk.Accept()) // move validator addValidatorTx from pending to current
	require.NoError(vm.SetPreference(vm.manager.LastAccepted()))

	_, err = vm.state.GetPendingValidator(createSubnetTx.ID(), nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	_, err = vm.state.GetCurrentValidator(createSubnetTx.ID(), nodeID)
	require.NoError(err)

	// fast forward clock to time validator should stop validating
	vm.clock.Set(endTime)
	blk, err = vm.Builder.BuildBlock()
	require.NoError(err)
	require.NoError(blk.Verify())
	require.NoError(blk.Accept()) // remove validator from current validator set

	_, err = vm.state.GetPendingValidator(createSubnetTx.ID(), nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	_, err = vm.state.GetCurrentValidator(createSubnetTx.ID(), nodeID)
	require.ErrorIs(err, database.ErrNotFound)
}

// test asset import
func TestAtomicImport(t *testing.T) {
	vm, baseDB, mutableSharedMemory := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	utxoID := avax.UTXOID{
		TxID:        ids.Empty.Prefix(1),
		OutputIndex: 1,
	}
	amount := uint64(50000)
	recipientKey := keys[1]

	m := atomic.NewMemory(prefixdb.New([]byte{5}, baseDB))

	mutableSharedMemory.SharedMemory = m.NewSharedMemory(vm.ctx.ChainID)
	peerSharedMemory := m.NewSharedMemory(vm.ctx.XChainID)

	if _, err := vm.txBuilder.NewImportTx(
		vm.ctx.XChainID,
		recipientKey.PublicKey().Address(),
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty, // change addr
	); err == nil {
		t.Fatalf("should have errored due to missing utxos")
	}

	// Provide the avm UTXO

	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  avax.Asset{ID: avaxAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: amount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{recipientKey.PublicKey().Address()},
			},
		},
	}
	utxoBytes, err := txs.Codec.Marshal(txs.Version, utxo)
	if err != nil {
		t.Fatal(err)
	}
	inputID := utxo.InputID()
	if err := peerSharedMemory.Apply(map[ids.ID]*atomic.Requests{vm.ctx.ChainID: {PutRequests: []*atomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			recipientKey.PublicKey().Address().Bytes(),
		},
	}}}}); err != nil {
		t.Fatal(err)
	}

	tx, err := vm.txBuilder.NewImportTx(
		vm.ctx.XChainID,
		recipientKey.PublicKey().Address(),
		[]*crypto.PrivateKeySECP256K1R{recipientKey},
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.Builder.AddUnverifiedTx(tx); err != nil {
		t.Fatal(err)
	} else if blk, err := vm.Builder.BuildBlock(); err != nil {
		t.Fatal(err)
	} else if err := blk.Verify(); err != nil {
		t.Fatal(err)
	} else if err := blk.Accept(); err != nil {
		t.Fatal(err)
	} else if _, txStatus, err := vm.state.GetTx(tx.ID()); err != nil {
		t.Fatal(err)
	} else if txStatus != status.Committed {
		t.Fatalf("status should be Committed but is %s", txStatus)
	}
	inputID = utxoID.InputID()
	if _, err := vm.ctx.SharedMemory.Get(vm.ctx.XChainID, [][]byte{inputID[:]}); err == nil {
		t.Fatalf("shouldn't have been able to read the utxo")
	}
}

// test optimistic asset import
func TestOptimisticAtomicImport(t *testing.T) {
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

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
	if err := tx.Sign(txs.Codec, [][]*crypto.PrivateKeySECP256K1R{{}}); err != nil {
		t.Fatal(err)
	}

	preferred, err := vm.Builder.Preferred()
	if err != nil {
		t.Fatal(err)
	}
	preferredID := preferred.ID()
	preferredHeight := preferred.Height()

	statelessBlk, err := blocks.NewApricotAtomicBlock(
		preferredID,
		preferredHeight+1,
		tx,
	)
	if err != nil {
		t.Fatal(err)
	}
	blk := vm.manager.NewBlock(statelessBlk)

	if err := blk.Verify(); err == nil {
		t.Fatalf("Block should have failed verification due to missing UTXOs")
	}

	if err := vm.SetState(snow.Bootstrapping); err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(); err != nil {
		t.Fatal(err)
	}

	if err := vm.SetState(snow.NormalOp); err != nil {
		t.Fatal(err)
	}

	_, txStatus, err := vm.state.GetTx(tx.ID())
	if err != nil {
		t.Fatal(err)
	}

	if txStatus != status.Committed {
		t.Fatalf("Wrong status returned. Expected %s; Got %s", status.Committed, txStatus)
	}
}

// test restarting the node
func TestRestartFullyAccepted(t *testing.T) {
	require := require.New(t)
	_, genesisBytes := defaultGenesis()
	db := manager.NewMemDB(version.Semantic1_0_0)

	firstDB := db.NewPrefixDBManager([]byte{})
	firstVM := &VM{Factory: Factory{
		Config: config.Config{
			Chains:                 chains.MockManager{},
			Validators:             validators.NewManager(),
			UptimeLockedCalculator: uptime.NewLockedCalculator(),
			MinStakeDuration:       defaultMinStakingDuration,
			MaxStakeDuration:       defaultMaxStakingDuration,
			RewardConfig:           defaultRewardConfig,
			BanffTime:              banffForkTime,
		},
	}}

	firstCtx := defaultContext()

	baseDBManager := manager.NewMemDB(version.Semantic1_0_0)
	atomicDB := prefixdb.New([]byte{1}, baseDBManager.Current().Database)
	m := atomic.NewMemory(atomicDB)
	msm := &mutableSharedMemory{
		SharedMemory: m.NewSharedMemory(firstCtx.ChainID),
	}
	firstCtx.SharedMemory = msm

	initialClkTime := banffForkTime.Add(time.Second)
	firstVM.clock.Set(initialClkTime)
	firstCtx.Lock.Lock()

	firstMsgChan := make(chan common.Message, 1)
	err := firstVM.Initialize(firstCtx, firstDB, genesisBytes, nil, nil, firstMsgChan, nil, nil)
	require.NoError(err)

	genesisID, err := firstVM.LastAccepted()
	require.NoError(err)

	nextChainTime := initialClkTime.Add(time.Second)
	firstVM.clock.Set(initialClkTime)
	preferred, err := firstVM.Builder.Preferred()
	require.NoError(err)
	preferredID := preferred.ID()
	preferredHeight := preferred.Height()

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
	require.NoError(tx.Sign(txs.Codec, [][]*crypto.PrivateKeySECP256K1R{{}}))

	statelessBlk, err := blocks.NewBanffStandardBlock(
		nextChainTime,
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{tx},
	)
	require.NoError(err)

	firstAdvanceTimeBlk := firstVM.manager.NewBlock(statelessBlk)

	nextChainTime = nextChainTime.Add(2 * time.Second)
	firstVM.clock.Set(nextChainTime)
	require.NoError(firstAdvanceTimeBlk.Verify())
	require.NoError(firstAdvanceTimeBlk.Accept())

	require.NoError(firstVM.Shutdown())
	firstCtx.Lock.Unlock()

	secondVM := &VM{Factory: Factory{
		Config: config.Config{
			Chains:                 chains.MockManager{},
			Validators:             validators.NewManager(),
			UptimeLockedCalculator: uptime.NewLockedCalculator(),
			MinStakeDuration:       defaultMinStakingDuration,
			MaxStakeDuration:       defaultMaxStakingDuration,
			RewardConfig:           defaultRewardConfig,
			BanffTime:              banffForkTime,
		},
	}}

	secondCtx := defaultContext()
	secondCtx.SharedMemory = msm
	secondVM.clock.Set(initialClkTime)
	secondCtx.Lock.Lock()
	defer func() {
		if err := secondVM.Shutdown(); err != nil {
			t.Fatal(err)
		}
		secondCtx.Lock.Unlock()
	}()

	secondDB := db.NewPrefixDBManager([]byte{})
	secondMsgChan := make(chan common.Message, 1)
	err = secondVM.Initialize(secondCtx, secondDB, genesisBytes, nil, nil, secondMsgChan, nil, nil)
	require.NoError(err)

	lastAccepted, err := secondVM.LastAccepted()
	require.NoError(err)
	require.Equal(genesisID, lastAccepted)
}

// test bootstrapping the node
func TestBootstrapPartiallyAccepted(t *testing.T) {
	require := require.New(t)

	_, genesisBytes := defaultGenesis()

	baseDBManager := manager.NewMemDB(version.Semantic1_0_0)
	vmDBManager := baseDBManager.NewPrefixDBManager([]byte("vm"))
	bootstrappingDB := prefixdb.New([]byte("bootstrapping"), baseDBManager.Current().Database)

	blocked, err := queue.NewWithMissing(bootstrappingDB, "", prometheus.NewRegistry())
	require.NoError(err)

	vm := &VM{Factory: Factory{
		Config: config.Config{
			Chains:                 chains.MockManager{},
			Validators:             validators.NewManager(),
			UptimeLockedCalculator: uptime.NewLockedCalculator(),
			MinStakeDuration:       defaultMinStakingDuration,
			MaxStakeDuration:       defaultMaxStakingDuration,
			RewardConfig:           defaultRewardConfig,
			BanffTime:              banffForkTime,
		},
	}}

	initialClkTime := banffForkTime.Add(time.Second)
	vm.clock.Set(initialClkTime)
	ctx := defaultContext()

	atomicDB := prefixdb.New([]byte{1}, baseDBManager.Current().Database)
	m := atomic.NewMemory(atomicDB)
	msm := &mutableSharedMemory{
		SharedMemory: m.NewSharedMemory(ctx.ChainID),
	}
	ctx.SharedMemory = msm

	consensusCtx := snow.DefaultConsensusContextTest()
	consensusCtx.Context = ctx
	consensusCtx.SetState(snow.Initializing)
	ctx.Lock.Lock()

	msgChan := make(chan common.Message, 1)
	require.NoError(vm.Initialize(ctx, vmDBManager, genesisBytes, nil, nil, msgChan, nil, nil))

	preferred, err := vm.Builder.Preferred()
	require.NoError(err)

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
	require.NoError(tx.Sign(txs.Codec, [][]*crypto.PrivateKeySECP256K1R{{}}))

	nextChainTime := initialClkTime.Add(time.Second)
	preferredID := preferred.ID()
	preferredHeight := preferred.Height()
	statelessBlk, err := blocks.NewBanffStandardBlock(
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

	peerID := ids.NodeID{1, 2, 3, 4, 5, 4, 3, 2, 1}
	vdrs := validators.NewSet()
	require.NoError(vdrs.AddWeight(peerID, 1))
	beacons := vdrs

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

	chainRouter := &router.ChainRouter{}

	metrics := prometheus.NewRegistry()
	mc, err := message.NewCreator(metrics, "dummyNamespace", true, 10*time.Second)
	require.NoError(err)

	err = chainRouter.Initialize(ids.EmptyNodeID, logging.NoLog{}, mc, timeoutManager, time.Second, ids.Set{}, ids.Set{}, nil, router.HealthConfig{}, "", prometheus.NewRegistry())
	require.NoError(err)

	externalSender := &sender.ExternalSenderTest{TB: t}
	externalSender.Default(true)

	// Passes messages from the consensus engine to the network
	sender, err := sender.New(
		consensusCtx,
		mc,
		externalSender,
		chainRouter,
		timeoutManager,
		sender.GossipConfig{
			AcceptedFrontierPeerSize:  1,
			OnAcceptPeerSize:          1,
			AppGossipValidatorSize:    1,
			AppGossipNonValidatorSize: 1,
		},
	)
	require.NoError(err)

	var reqID uint32
	externalSender.SendF = func(msg message.OutboundMessage, nodeIDs ids.NodeIDSet, _ ids.ID, _ bool) ids.NodeIDSet {
		inMsg, err := mc.Parse(msg.Bytes(), ctx.NodeID, func() {})
		require.NoError(err)
		require.Equal(message.GetAcceptedFrontier, inMsg.Op())

		requestIDIntf, err := inMsg.Get(message.RequestID)
		require.NoError(err)
		requestID, ok := requestIDIntf.(uint32)
		require.True(ok)

		reqID = requestID
		return nodeIDs
	}

	isBootstrapped := false
	subnet := &common.SubnetTest{
		T:               t,
		IsBootstrappedF: func() bool { return isBootstrapped },
		BootstrappedF:   func(ids.ID) { isBootstrapped = true },
	}

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, (beacons.Weight()+1)/2)
	beacons.RegisterCallbackListener(startup)

	// The engine handles consensus
	consensus := &smcon.Topological{}
	commonCfg := common.Config{
		Ctx:                            consensusCtx,
		Validators:                     vdrs,
		Beacons:                        beacons,
		SampleK:                        beacons.Len(),
		StartupTracker:                 startup,
		Alpha:                          (beacons.Weight() + 1) / 2,
		Sender:                         sender,
		Subnet:                         subnet,
		AncestorsMaxContainersSent:     2000,
		AncestorsMaxContainersReceived: 2000,
		SharedCfg:                      &common.SharedConfig{},
	}

	snowGetHandler, err := snowgetter.New(vm, commonCfg)
	require.NoError(err)

	bootstrapConfig := bootstrap.Config{
		Config:        commonCfg,
		AllGetsServer: snowGetHandler,
		Blocked:       blocked,
		VM:            vm,
	}

	// Asynchronously passes messages from the network to the consensus engine
	cpuTracker, err := timetracker.NewResourceTracker(prometheus.NewRegistry(), resource.NoUsage, meter.ContinuousFactory{}, time.Second)
	require.NoError(err)

	handler, err := handler.New(
		mc,
		bootstrapConfig.Ctx,
		vdrs,
		msgChan,
		nil,
		time.Hour,
		cpuTracker,
	)
	require.NoError(err)

	engineConfig := smeng.Config{
		Ctx:           bootstrapConfig.Ctx,
		AllGetsServer: snowGetHandler,
		VM:            bootstrapConfig.VM,
		Sender:        bootstrapConfig.Sender,
		Validators:    vdrs,
		Params: snowball.Parameters{
			K:                     1,
			Alpha:                 1,
			BetaVirtuous:          20,
			BetaRogue:             20,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Consensus: consensus,
	}
	engine, err := smeng.New(engineConfig)
	require.NoError(err)

	handler.SetConsensus(engine)

	bootstrapper, err := bootstrap.New(
		bootstrapConfig,
		engine.Start,
	)
	require.NoError(err)

	handler.SetBootstrapper(bootstrapper)

	// Allow incoming messages to be routed to the new chain
	chainRouter.AddChain(handler)
	ctx.Lock.Unlock()

	handler.Start(false)

	ctx.Lock.Lock()
	if err := bootstrapper.Connected(peerID, version.CurrentApp); err != nil {
		t.Fatal(err)
	}

	externalSender.SendF = func(msg message.OutboundMessage, nodeIDs ids.NodeIDSet, _ ids.ID, _ bool) ids.NodeIDSet {
		inMsg, err := mc.Parse(msg.Bytes(), ctx.NodeID, func() {})
		require.NoError(err)
		require.Equal(message.GetAccepted, inMsg.Op())

		requestIDIntf, err := inMsg.Get(message.RequestID)
		require.NoError(err)

		requestID, ok := requestIDIntf.(uint32)
		require.True(ok)

		reqID = requestID
		return nodeIDs
	}

	frontier := []ids.ID{advanceTimeBlkID}
	if err := bootstrapper.AcceptedFrontier(context.Background(), peerID, reqID, frontier); err != nil {
		t.Fatal(err)
	}

	externalSender.SendF = func(msg message.OutboundMessage, nodeIDs ids.NodeIDSet, _ ids.ID, _ bool) ids.NodeIDSet {
		inMsg, err := mc.Parse(msg.Bytes(), ctx.NodeID, func() {})
		require.NoError(err)
		require.Equal(message.GetAncestors, inMsg.Op())

		requestIDIntf, err := inMsg.Get(message.RequestID)
		require.NoError(err)
		requestID, ok := requestIDIntf.(uint32)
		require.True(ok)

		reqID = requestID

		containerIDIntf, err := inMsg.Get(message.ContainerID)
		require.NoError(err)
		containerIDBytes, ok := containerIDIntf.([]byte)
		require.True(ok)
		containerID, err := ids.ToID(containerIDBytes)
		require.NoError(err)
		if containerID != advanceTimeBlkID {
			t.Fatalf("wrong block requested")
		}

		return nodeIDs
	}

	require.NoError(bootstrapper.Accepted(context.Background(), peerID, reqID, frontier))

	externalSender.SendF = nil
	externalSender.CantSend = false

	require.NoError(bootstrapper.Ancestors(context.Background(), peerID, reqID, [][]byte{advanceTimeBlkBytes}))

	preferred, err = vm.Builder.Preferred()
	require.NoError(err)

	if preferred.ID() != advanceTimeBlk.ID() {
		t.Fatalf("wrong preference reported after bootstrapping to proposal block\nPreferred: %s\nExpected: %s\nGenesis: %s",
			preferred.ID(),
			advanceTimeBlk.ID(),
			preferredID)
	}
	ctx.Lock.Unlock()

	chainRouter.Shutdown()
}

func TestUnverifiedParent(t *testing.T) {
	require := require.New(t)
	_, genesisBytes := defaultGenesis()
	dbManager := manager.NewMemDB(version.Semantic1_0_0)

	vm := &VM{Factory: Factory{
		Config: config.Config{
			Chains:                 chains.MockManager{},
			Validators:             validators.NewManager(),
			UptimeLockedCalculator: uptime.NewLockedCalculator(),
			MinStakeDuration:       defaultMinStakingDuration,
			MaxStakeDuration:       defaultMaxStakingDuration,
			RewardConfig:           defaultRewardConfig,
			BanffTime:              banffForkTime,
		},
	}}

	initialClkTime := banffForkTime.Add(time.Second)
	vm.clock.Set(initialClkTime)
	ctx := defaultContext()
	ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown())
		ctx.Lock.Unlock()
	}()

	msgChan := make(chan common.Message, 1)
	err := vm.Initialize(ctx, dbManager, genesisBytes, nil, nil, msgChan, nil, nil)
	require.NoError(err)

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
	require.NoError(tx1.Sign(txs.Codec, [][]*crypto.PrivateKeySECP256K1R{{}}))

	preferred, err := vm.Builder.Preferred()
	require.NoError(err)
	nextChainTime := initialClkTime.Add(time.Second)
	preferredID := preferred.ID()
	preferredHeight := preferred.Height()

	statelessBlk, err := blocks.NewBanffStandardBlock(
		nextChainTime,
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{tx1},
	)
	require.NoError(err)
	firstAdvanceTimeBlk := vm.manager.NewBlock(statelessBlk)
	err = firstAdvanceTimeBlk.Verify()
	require.NoError(err)

	// include a tx1 to make the block be accepted
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
	require.NoError(tx1.Sign(txs.Codec, [][]*crypto.PrivateKeySECP256K1R{{}}))
	nextChainTime = nextChainTime.Add(time.Second)
	vm.clock.Set(nextChainTime)
	statelessSecondAdvanceTimeBlk, err := blocks.NewBanffStandardBlock(
		nextChainTime,
		firstAdvanceTimeBlk.ID(),
		firstAdvanceTimeBlk.Height()+1,
		[]*txs.Tx{tx2},
	)
	require.NoError(err)
	secondAdvanceTimeBlk := vm.manager.NewBlock(statelessSecondAdvanceTimeBlk)

	require.Equal(secondAdvanceTimeBlk.Parent(), firstAdvanceTimeBlk.ID())
	require.NoError(secondAdvanceTimeBlk.Verify())
}

func TestMaxStakeAmount(t *testing.T) {
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	nodeID := ids.NodeID(keys[0].PublicKey().Address())

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
			require.EqualValues(defaultWeight, amount)
		})
	}
}

func TestUptimeDisallowedWithRestart(t *testing.T) {
	require := require.New(t)
	_, genesisBytes := defaultGenesis()
	db := manager.NewMemDB(version.Semantic1_0_0)

	firstDB := db.NewPrefixDBManager([]byte{})
	firstVM := &VM{Factory: Factory{
		Config: config.Config{
			Chains:                 chains.MockManager{},
			UptimePercentage:       .2,
			RewardConfig:           defaultRewardConfig,
			Validators:             validators.NewManager(),
			UptimeLockedCalculator: uptime.NewLockedCalculator(),
			BanffTime:              banffForkTime,
		},
	}}

	firstCtx := defaultContext()
	firstCtx.Lock.Lock()

	firstMsgChan := make(chan common.Message, 1)
	require.NoError(firstVM.Initialize(firstCtx, firstDB, genesisBytes, nil, nil, firstMsgChan, nil, nil))

	initialClkTime := banffForkTime.Add(time.Second)
	firstVM.clock.Set(initialClkTime)
	firstVM.uptimeManager.(uptime.TestManager).SetTime(initialClkTime)

	require.NoError(firstVM.SetState(snow.Bootstrapping))
	require.NoError(firstVM.SetState(snow.NormalOp))

	// Fast forward clock to time for genesis validators to leave
	firstVM.uptimeManager.(uptime.TestManager).SetTime(defaultValidateEndTime)

	require.NoError(firstVM.Shutdown())
	firstCtx.Lock.Unlock()

	secondDB := db.NewPrefixDBManager([]byte{})
	secondVM := &VM{Factory: Factory{
		Config: config.Config{
			Chains:                 chains.MockManager{},
			UptimePercentage:       .21,
			Validators:             validators.NewManager(),
			UptimeLockedCalculator: uptime.NewLockedCalculator(),
			BanffTime:              banffForkTime,
		},
	}}

	secondCtx := defaultContext()
	secondCtx.Lock.Lock()
	defer func() {
		require.NoError(secondVM.Shutdown())
		secondCtx.Lock.Unlock()
	}()

	secondMsgChan := make(chan common.Message, 1)
	require.NoError(secondVM.Initialize(secondCtx, secondDB, genesisBytes, nil, nil, secondMsgChan, nil, nil))

	secondVM.clock.Set(defaultValidateStartTime.Add(2 * defaultMinStakingDuration))
	secondVM.uptimeManager.(uptime.TestManager).SetTime(defaultValidateStartTime.Add(2 * defaultMinStakingDuration))

	require.NoError(secondVM.SetState(snow.Bootstrapping))
	require.NoError(secondVM.SetState(snow.NormalOp))

	secondVM.clock.Set(defaultValidateEndTime)
	secondVM.uptimeManager.(uptime.TestManager).SetTime(defaultValidateEndTime)

	blk, err := secondVM.Builder.BuildBlock() // should advance time
	require.NoError(err)

	require.NoError(blk.Verify())

	// Assert preferences are correct
	block := blk.(smcon.OracleBlock)
	options, err := block.Options()
	require.NoError(err)

	commit := options[0].(*blockexecutor.Block)
	_, ok := commit.Block.(*blocks.BanffCommitBlock)
	require.True(ok)

	abort := options[1].(*blockexecutor.Block)
	_, ok = abort.Block.(*blocks.BanffAbortBlock)
	require.True(ok)

	require.NoError(block.Accept())
	require.NoError(commit.Verify())
	require.NoError(abort.Verify())
	require.NoError(secondVM.SetPreference(secondVM.manager.LastAccepted()))

	proposalTx := blk.(blocks.Block).Txs()[0]
	{
		onAccept, ok := secondVM.manager.GetState(abort.ID())
		require.True(ok)

		_, txStatus, err := onAccept.GetTx(proposalTx.ID())
		require.NoError(err)
		require.Equal(status.Aborted, txStatus)
	}

	require.NoError(commit.Accept()) // advance the timestamp
	require.NoError(secondVM.SetPreference(secondVM.manager.LastAccepted()))

	_, txStatus, err := secondVM.state.GetTx(proposalTx.ID())
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	// Verify that chain's timestamp has advanced
	timestamp := secondVM.state.GetTimestamp()
	require.Equal(defaultValidateEndTime.Unix(), timestamp.Unix())

	blk, err = secondVM.Builder.BuildBlock() // should contain proposal to reward genesis validator
	require.NoError(err)

	require.NoError(blk.Verify())

	block = blk.(smcon.OracleBlock)
	options, err = block.Options()
	require.NoError(err)

	commit = options[0].(*blockexecutor.Block)
	_, ok = commit.Block.(*blocks.BanffCommitBlock)
	require.True(ok)

	abort = options[1].(*blockexecutor.Block)
	_, ok = abort.Block.(*blocks.BanffAbortBlock)
	require.True(ok)

	require.NoError(blk.Accept())
	require.NoError(commit.Verify())
	require.NoError(secondVM.SetPreference(secondVM.manager.LastAccepted()))

	proposalTx = blk.(blocks.Block).Txs()[0]
	{
		onAccept, ok := secondVM.manager.GetState(commit.ID())
		require.True(ok)

		_, txStatus, err := onAccept.GetTx(proposalTx.ID())
		require.NoError(err)
		require.Equal(status.Committed, txStatus)
	}

	require.NoError(abort.Verify())
	require.NoError(abort.Accept()) // do not reward the genesis validator
	require.NoError(secondVM.SetPreference(secondVM.manager.LastAccepted()))

	_, txStatus, err = secondVM.state.GetTx(proposalTx.ID())
	require.NoError(err)
	require.Equal(status.Aborted, txStatus)

	_, err = secondVM.state.GetCurrentValidator(
		constants.PrimaryNetworkID,
		ids.NodeID(keys[1].PublicKey().Address()),
	)
	require.ErrorIs(err, database.ErrNotFound)
}

func TestUptimeDisallowedAfterNeverConnecting(t *testing.T) {
	require := require.New(t)
	_, genesisBytes := defaultGenesis()
	db := manager.NewMemDB(version.Semantic1_0_0)

	vm := &VM{Factory: Factory{
		Config: config.Config{
			Chains:                 chains.MockManager{},
			UptimePercentage:       .2,
			RewardConfig:           defaultRewardConfig,
			Validators:             validators.NewManager(),
			UptimeLockedCalculator: uptime.NewLockedCalculator(),
			BanffTime:              banffForkTime,
		},
	}}

	ctx := defaultContext()
	ctx.Lock.Lock()

	msgChan := make(chan common.Message, 1)
	appSender := &common.SenderTest{T: t}
	require.NoError(vm.Initialize(ctx, db, genesisBytes, nil, nil, msgChan, nil, appSender))
	defer func() {
		require.NoError(vm.Shutdown())
		ctx.Lock.Unlock()
	}()

	initialClkTime := banffForkTime.Add(time.Second)
	vm.clock.Set(initialClkTime)
	vm.uptimeManager.(uptime.TestManager).SetTime(initialClkTime)

	require.NoError(vm.SetState(snow.Bootstrapping))
	require.NoError(vm.SetState(snow.NormalOp))

	// Fast forward clock to time for genesis validators to leave
	vm.clock.Set(defaultValidateEndTime)
	vm.uptimeManager.(uptime.TestManager).SetTime(defaultValidateEndTime)

	blk, err := vm.Builder.BuildBlock() // should advance time
	require.NoError(err)

	require.NoError(blk.Verify())

	// first the time will be advanced.
	block := blk.(smcon.OracleBlock)
	options, err := block.Options()
	require.NoError(err)

	commit := options[0].(*blockexecutor.Block)
	_, ok := commit.Block.(*blocks.BanffCommitBlock)
	require.True(ok)

	abort := options[1].(*blockexecutor.Block)
	_, ok = abort.Block.(*blocks.BanffAbortBlock)
	require.True(ok)

	require.NoError(block.Accept())
	require.NoError(commit.Verify())
	require.NoError(abort.Verify())
	require.NoError(commit.Accept()) // advance the timestamp
	require.NoError(vm.SetPreference(vm.manager.LastAccepted()))

	// Verify that chain's timestamp has advanced
	timestamp := vm.state.GetTimestamp()
	require.Equal(defaultValidateEndTime.Unix(), timestamp.Unix())

	// should contain proposal to reward genesis validator
	blk, err = vm.Builder.BuildBlock()
	require.NoError(err)

	require.NoError(blk.Verify())

	block = blk.(smcon.OracleBlock)
	options, err = block.Options()
	require.NoError(err)

	commit = options[0].(*blockexecutor.Block)
	_, ok = commit.Block.(*blocks.BanffCommitBlock)
	require.True(ok)

	abort = options[1].(*blockexecutor.Block)
	_, ok = abort.Block.(*blocks.BanffAbortBlock)
	require.True(ok)

	require.NoError(blk.Accept())
	require.NoError(commit.Verify())
	require.NoError(abort.Verify())
	require.NoError(abort.Accept()) // do not reward the genesis validator
	require.NoError(vm.SetPreference(vm.manager.LastAccepted()))

	_, err = vm.state.GetCurrentValidator(
		constants.PrimaryNetworkID,
		ids.NodeID(keys[1].PublicKey().Address()),
	)
	require.ErrorIs(err, database.ErrNotFound)
}
