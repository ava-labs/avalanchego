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

	"github.com/golang/mock/gomock"

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
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
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
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
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

type mutableSharedMemory struct {
	atomic.SharedMemory
}

func defaultContext() *snow.Context {
	ctx := snow.DefaultContextTest()
	ctx.NetworkID = testNetworkID
	ctx.XChainID = xChainID
	ctx.CChainID = cChainID
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

	ctx.ValidatorState = &validators.TestState{
		GetSubnetIDF: func(_ context.Context, chainID ids.ID) (ids.ID, error) {
			subnetID, ok := map[ids.ID]ids.ID{
				constants.PlatformChainID: constants.PrimaryNetworkID,
				xChainID:                  constants.PrimaryNetworkID,
				cChainID:                  constants.PrimaryNetworkID,
			}[chainID]
			if !ok {
				return ids.Empty, errors.New("missing")
			}
			return subnetID, nil
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
	vdrs := validators.NewManager()
	primaryVdrs := validators.NewSet()
	_ = vdrs.Add(constants.PrimaryNetworkID, primaryVdrs)
	vm := &VM{Factory: Factory{
		Config: config.Config{
			Chains:                 chains.MockManager{},
			UptimeLockedCalculator: uptime.NewLockedCalculator(),
			Validators:             vdrs,
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
	appSender.SendAppGossipF = func(context.Context, []byte) error {
		return nil
	}

	err := vm.Initialize(
		context.Background(),
		ctx,
		chainDBManager,
		genesisBytes,
		nil,
		nil,
		msgChan,
		nil,
		appSender,
	)
	if err != nil {
		panic(err)
	}

	err = vm.SetState(context.Background(), snow.NormalOp)
	if err != nil {
		panic(err)
	}

	// Create a subnet and store it in testSubnet1
	// Note: following Banff activation, block acceptance will move
	// chain time ahead
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
	} else if blk, err := vm.Builder.BuildBlock(context.Background()); err != nil {
		panic(err)
	} else if err := blk.Verify(context.Background()); err != nil {
		panic(err)
	} else if err := blk.Accept(context.Background()); err != nil {
		panic(err)
	} else if err := vm.SetPreference(context.Background(), vm.manager.LastAccepted()); err != nil {
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

	vdrs := validators.NewManager()
	primaryVdrs := validators.NewSet()
	_ = vdrs.Add(constants.PrimaryNetworkID, primaryVdrs)
	vm := &VM{Factory: Factory{
		Config: config.Config{
			Chains:                 chains.MockManager{},
			Validators:             vdrs,
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
	appSender.SendAppGossipF = func(context.Context, []byte) error {
		return nil
	}
	err := vm.Initialize(
		context.Background(),
		ctx,
		chainDBManager,
		genesisBytes,
		nil,
		nil,
		msgChan,
		nil,
		appSender,
	)
	if err != nil {
		t.Fatal(err)
	}

	err = vm.SetState(context.Background(), snow.NormalOp)
	if err != nil {
		panic(err)
	}

	// Create a subnet and store it in testSubnet1
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
	} else if blk, err := vm.Builder.BuildBlock(context.Background()); err != nil {
		panic(err)
	} else if err := blk.Verify(context.Background()); err != nil {
		panic(err)
	} else if err := blk.Accept(context.Background()); err != nil {
		panic(err)
	}

	return genesisBytes, msgChan, vm, m
}

// Ensure genesis state is parsed from bytes and stored correctly
func TestGenesis(t *testing.T) {
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	// Ensure the genesis block has been accepted and stored
	genesisBlockID, err := vm.LastAccepted(context.Background()) // lastAccepted should be ID of genesis block
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
		addrs := set.Set[ids.ShortID]{}
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
	vdrSet, ok := vm.Validators.Get(constants.PrimaryNetworkID)
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
		require.NoError(vm.Shutdown(context.Background()))
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

	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))
	require.NoError(blk.Accept(context.Background()))

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
		if err := vm.Shutdown(context.Background()); err != nil {
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

	parsedBlock, err := vm.ParseBlock(context.Background(), blkBytes)
	if err != nil {
		t.Fatal(err)
	}

	if err := parsedBlock.Verify(context.Background()); err == nil {
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
		require.NoError(vm.Shutdown(context.Background()))
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

	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))
	require.NoError(blk.Reject(context.Background()))

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
		if err := vm.Shutdown(context.Background()); err != nil {
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
		require.NoError(vm.Shutdown(context.Background()))
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

	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))
	require.NoError(blk.Accept(context.Background()))

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
		require.NoError(vm.Shutdown(context.Background()))
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

	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))
	require.NoError(blk.Reject(context.Background()))

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
		require.NoError(vm.Shutdown(context.Background()))
		vm.ctx.Lock.Unlock()
	}()

	// Fast forward clock to time for genesis validators to leave
	vm.clock.Set(defaultValidateEndTime)

	blk, err := vm.Builder.BuildBlock(context.Background()) // should advance time
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))

	// Assert preferences are correct
	block := blk.(smcon.OracleBlock)
	options, err := block.Options(context.Background())
	require.NoError(err)

	commit := options[0].(*blockexecutor.Block)
	_, ok := commit.Block.(*blocks.BanffCommitBlock)
	require.True(ok)
	abort := options[1].(*blockexecutor.Block)
	_, ok = abort.Block.(*blocks.BanffAbortBlock)
	require.True(ok)

	require.NoError(block.Accept(context.Background()))
	require.NoError(commit.Verify(context.Background()))
	require.NoError(abort.Verify(context.Background()))

	txID := blk.(blocks.Block).Txs()[0].ID()
	{
		onAccept, ok := vm.manager.GetState(abort.ID())
		require.True(ok)

		_, txStatus, err := onAccept.GetTx(txID)
		require.NoError(err)
		require.Equal(status.Aborted, txStatus)
	}

	require.NoError(commit.Accept(context.Background())) // advance the timestamp
	lastAcceptedID, err := vm.LastAccepted(context.Background())
	require.NoError(err)
	require.NoError(vm.SetPreference(context.Background(), lastAcceptedID))

	_, txStatus, err := vm.state.GetTx(txID)
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	// Verify that chain's timestamp has advanced
	timestamp := vm.state.GetTimestamp()
	require.Equal(defaultValidateEndTime.Unix(), timestamp.Unix())

	blk, err = vm.Builder.BuildBlock(context.Background()) // should contain proposal to reward genesis validator
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))

	// Assert preferences are correct
	block = blk.(smcon.OracleBlock)
	options, err = block.Options(context.Background())
	require.NoError(err)

	commit = options[0].(*blockexecutor.Block)
	_, ok = commit.Block.(*blocks.BanffCommitBlock)
	require.True(ok)

	abort = options[1].(*blockexecutor.Block)
	_, ok = abort.Block.(*blocks.BanffAbortBlock)
	require.True(ok)

	require.NoError(block.Accept(context.Background()))
	require.NoError(commit.Verify(context.Background()))
	require.NoError(abort.Verify(context.Background()))

	txID = blk.(blocks.Block).Txs()[0].ID()
	{
		onAccept, ok := vm.manager.GetState(abort.ID())
		require.True(ok)

		_, txStatus, err := onAccept.GetTx(txID)
		require.NoError(err)
		require.Equal(status.Aborted, txStatus)
	}

	require.NoError(commit.Accept(context.Background())) // reward the genesis validator

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
		require.NoError(vm.Shutdown(context.Background()))
		vm.ctx.Lock.Unlock()
	}()

	// Fast forward clock to time for genesis validators to leave
	vm.clock.Set(defaultValidateEndTime)

	blk, err := vm.Builder.BuildBlock(context.Background()) // should advance time
	require.NoError(err)
	require.NoError(blk.Verify(context.Background()))

	// Assert preferences are correct
	block := blk.(smcon.OracleBlock)
	options, err := block.Options(context.Background())
	require.NoError(err)

	commit := options[0].(*blockexecutor.Block)
	_, ok := commit.Block.(*blocks.BanffCommitBlock)
	require.True(ok)

	abort := options[1].(*blockexecutor.Block)
	_, ok = abort.Block.(*blocks.BanffAbortBlock)
	require.True(ok)

	require.NoError(block.Accept(context.Background()))
	require.NoError(commit.Verify(context.Background()))
	require.NoError(abort.Verify(context.Background()))

	txID := blk.(blocks.Block).Txs()[0].ID()
	{
		onAccept, ok := vm.manager.GetState(abort.ID())
		require.True(ok)

		_, txStatus, err := onAccept.GetTx(txID)
		require.NoError(err)
		require.Equal(status.Aborted, txStatus)
	}

	require.NoError(commit.Accept(context.Background())) // advance the timestamp
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	_, txStatus, err := vm.state.GetTx(txID)
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	timestamp := vm.state.GetTimestamp()
	require.Equal(defaultValidateEndTime.Unix(), timestamp.Unix())

	blk, err = vm.Builder.BuildBlock(context.Background()) // should contain proposal to reward genesis validator
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))

	block = blk.(smcon.OracleBlock)
	options, err = block.Options(context.Background())
	require.NoError(err)

	commit = options[0].(*blockexecutor.Block)
	_, ok = commit.Block.(*blocks.BanffCommitBlock)
	require.True(ok)

	abort = options[1].(*blockexecutor.Block)
	_, ok = abort.Block.(*blocks.BanffAbortBlock)
	require.True(ok)

	require.NoError(blk.Accept(context.Background()))
	require.NoError(commit.Verify(context.Background()))

	txID = blk.(blocks.Block).Txs()[0].ID()
	{
		onAccept, ok := vm.manager.GetState(commit.ID())
		require.True(ok)

		_, txStatus, err := onAccept.GetTx(txID)
		require.NoError(err)
		require.Equal(status.Committed, txStatus)
	}

	require.NoError(abort.Verify(context.Background()))
	require.NoError(abort.Accept(context.Background())) // do not reward the genesis validator

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
		require.NoError(vm.Shutdown(context.Background()))
		vm.ctx.Lock.Unlock()
	}()

	// Fast forward clock to time for genesis validators to leave
	vm.clock.Set(defaultValidateEndTime)

	blk, err := vm.Builder.BuildBlock(context.Background()) // should advance time
	require.NoError(err)
	require.NoError(blk.Verify(context.Background()))

	// Assert preferences are correct
	block := blk.(smcon.OracleBlock)
	options, err := block.Options(context.Background())
	require.NoError(err)

	commit := options[0].(*blockexecutor.Block)
	_, ok := commit.Block.(*blocks.BanffCommitBlock)
	require.True(ok)

	abort := options[1].(*blockexecutor.Block)
	_, ok = abort.Block.(*blocks.BanffAbortBlock)
	require.True(ok)

	require.NoError(block.Accept(context.Background()))
	require.NoError(commit.Verify(context.Background()))
	require.NoError(abort.Verify(context.Background()))

	txID := blk.(blocks.Block).Txs()[0].ID()
	{
		onAccept, ok := vm.manager.GetState(abort.ID())
		require.True(ok)

		_, txStatus, err := onAccept.GetTx(txID)
		require.NoError(err)
		require.Equal(status.Aborted, txStatus)
	}

	require.NoError(commit.Accept(context.Background())) // advance the timestamp
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	_, txStatus, err := vm.state.GetTx(txID)
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	timestamp := vm.state.GetTimestamp()
	require.Equal(defaultValidateEndTime.Unix(), timestamp.Unix())

	// should contain proposal to reward genesis validator
	blk, err = vm.Builder.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))

	block = blk.(smcon.OracleBlock)
	options, err = block.Options(context.Background())
	require.NoError(err)

	commit = options[0].(*blockexecutor.Block)
	_, ok = commit.Block.(*blocks.BanffCommitBlock)
	require.True(ok)

	abort = options[1].(*blockexecutor.Block)
	_, ok = abort.Block.(*blocks.BanffAbortBlock)
	require.True(ok)

	require.NoError(blk.Accept(context.Background()))
	require.NoError(commit.Verify(context.Background()))

	txID = blk.(blocks.Block).Txs()[0].ID()
	{
		onAccept, ok := vm.manager.GetState(commit.ID())
		require.True(ok)

		_, txStatus, err := onAccept.GetTx(txID)
		require.NoError(err)
		require.Equal(status.Committed, txStatus)
	}

	require.NoError(abort.Verify(context.Background()))
	require.NoError(abort.Accept(context.Background())) // do not reward the genesis validator

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
		if err := vm.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()
	if _, err := vm.Builder.BuildBlock(context.Background()); err == nil {
		t.Fatalf("Should have errored on BuildBlock")
	}
}

// test acceptance of proposal to create a new chain
func TestCreateChain(t *testing.T) {
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
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
	} else if blk, err := vm.Builder.BuildBlock(context.Background()); err != nil { // should contain proposal to create chain
		t.Fatal(err)
	} else if err := blk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	} else if err := blk.Accept(context.Background()); err != nil {
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
		require.NoError(vm.Shutdown(context.Background()))
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
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty, // change addr
	)
	require.NoError(err)

	require.NoError(vm.Builder.AddUnverifiedTx(addValidatorTx))

	blk, err = vm.Builder.BuildBlock(context.Background()) // should add validator to the new subnet
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))
	require.NoError(blk.Accept(context.Background())) // add the validator to pending validator set
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

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
	blk, err = vm.Builder.BuildBlock(context.Background()) // should be advance time tx
	require.NoError(err)
	require.NoError(blk.Verify(context.Background()))
	require.NoError(blk.Accept(context.Background())) // move validator addValidatorTx from pending to current
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

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
	vm, baseDB, mutableSharedMemory := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
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
	} else if blk, err := vm.Builder.BuildBlock(context.Background()); err != nil {
		t.Fatal(err)
	} else if err := blk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	} else if err := blk.Accept(context.Background()); err != nil {
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
		if err := vm.Shutdown(context.Background()); err != nil {
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
	if err := tx.Initialize(txs.Codec); err != nil {
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

	if err := blk.Verify(context.Background()); err == nil {
		t.Fatalf("Block should have failed verification due to missing UTXOs")
	}

	if err := vm.SetState(context.Background(), snow.Bootstrapping); err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := vm.SetState(context.Background(), snow.NormalOp); err != nil {
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
	firstVdrs := validators.NewManager()
	firstPrimaryVdrs := validators.NewSet()
	_ = firstVdrs.Add(constants.PrimaryNetworkID, firstPrimaryVdrs)
	firstVM := &VM{Factory: Factory{
		Config: config.Config{
			Chains:                 chains.MockManager{},
			Validators:             firstVdrs,
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
	err := firstVM.Initialize(
		context.Background(),
		firstCtx,
		firstDB,
		genesisBytes,
		nil,
		nil,
		firstMsgChan,
		nil,
		nil,
	)
	require.NoError(err)

	genesisID, err := firstVM.LastAccepted(context.Background())
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
	require.NoError(tx.Initialize(txs.Codec))

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
	require.NoError(firstAdvanceTimeBlk.Verify(context.Background()))
	require.NoError(firstAdvanceTimeBlk.Accept(context.Background()))

	require.NoError(firstVM.Shutdown(context.Background()))
	firstCtx.Lock.Unlock()

	secondVdrs := validators.NewManager()
	secondPrimaryVdrs := validators.NewSet()
	_ = secondVdrs.Add(constants.PrimaryNetworkID, secondPrimaryVdrs)
	secondVM := &VM{Factory: Factory{
		Config: config.Config{
			Chains:                 chains.MockManager{},
			Validators:             secondVdrs,
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
		if err := secondVM.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
		secondCtx.Lock.Unlock()
	}()

	secondDB := db.NewPrefixDBManager([]byte{})
	secondMsgChan := make(chan common.Message, 1)
	err = secondVM.Initialize(
		context.Background(),
		secondCtx,
		secondDB,
		genesisBytes,
		nil,
		nil,
		secondMsgChan,
		nil,
		nil,
	)
	require.NoError(err)

	lastAccepted, err := secondVM.LastAccepted(context.Background())
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

	vdrs := validators.NewManager()
	primaryVdrs := validators.NewSet()
	_ = vdrs.Add(constants.PrimaryNetworkID, primaryVdrs)
	vm := &VM{Factory: Factory{
		Config: config.Config{
			Chains:                 chains.MockManager{},
			Validators:             vdrs,
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
	err = vm.Initialize(
		context.Background(),
		ctx,
		vmDBManager,
		genesisBytes,
		nil,
		nil,
		msgChan,
		nil,
		nil,
	)
	require.NoError(err)

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
	require.NoError(tx.Initialize(txs.Codec))

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
	beacons := validators.NewSet()
	require.NoError(beacons.Add(peerID, nil, ids.Empty, 1))

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

	err = chainRouter.Initialize(
		ids.EmptyNodeID,
		logging.NoLog{},
		timeoutManager,
		time.Second,
		set.Set[ids.ID]{},
		set.Set[ids.ID]{},
		nil,
		router.HealthConfig{},
		"",
		prometheus.NewRegistry(),
	)
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
	externalSender.SendF = func(msg message.OutboundMessage, nodeIDs set.Set[ids.NodeID], _ ids.ID, _ bool) set.Set[ids.NodeID] {
		inMsg, err := mc.Parse(msg.Bytes(), ctx.NodeID, func() {})
		require.NoError(err)
		require.Equal(message.GetAcceptedFrontierOp, inMsg.Op())

		requestID, ok := message.GetRequestID(inMsg.Message())
		require.True(ok)

		reqID = requestID
		return nodeIDs
	}

	isBootstrapped := false
	subnet := &common.SubnetTest{
		T: t,
		IsBootstrappedF: func() bool {
			return isBootstrapped
		},
		BootstrappedF: func(ids.ID) {
			isBootstrapped = true
		},
	}

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, (beacons.Weight()+1)/2)
	beacons.RegisterCallbackListener(startup)

	// The engine handles consensus
	consensus := &smcon.Topological{}
	commonCfg := common.Config{
		Ctx:                            consensusCtx,
		Validators:                     beacons,
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
	cpuTracker, err := timetracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		time.Second,
	)
	require.NoError(err)

	handler, err := handler.New(
		bootstrapConfig.Ctx,
		beacons,
		msgChan,
		nil,
		time.Hour,
		cpuTracker,
		vm,
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
		context.Background(),
		bootstrapConfig,
		engine.Start,
	)
	require.NoError(err)

	handler.SetBootstrapper(bootstrapper)

	// Allow incoming messages to be routed to the new chain
	chainRouter.AddChain(context.Background(), handler)
	ctx.Lock.Unlock()

	handler.Start(context.Background(), false)

	ctx.Lock.Lock()
	if err := bootstrapper.Connected(context.Background(), peerID, version.CurrentApp); err != nil {
		t.Fatal(err)
	}

	externalSender.SendF = func(msg message.OutboundMessage, nodeIDs set.Set[ids.NodeID], _ ids.ID, _ bool) set.Set[ids.NodeID] {
		inMsgIntf, err := mc.Parse(msg.Bytes(), ctx.NodeID, func() {})
		require.NoError(err)
		require.Equal(message.GetAcceptedOp, inMsgIntf.Op())
		inMsg := inMsgIntf.Message().(*p2ppb.GetAccepted)

		reqID = inMsg.RequestId
		return nodeIDs
	}

	frontier := []ids.ID{advanceTimeBlkID}
	if err := bootstrapper.AcceptedFrontier(context.Background(), peerID, reqID, frontier); err != nil {
		t.Fatal(err)
	}

	externalSender.SendF = func(msg message.OutboundMessage, nodeIDs set.Set[ids.NodeID], _ ids.ID, _ bool) set.Set[ids.NodeID] {
		inMsgIntf, err := mc.Parse(msg.Bytes(), ctx.NodeID, func() {})
		require.NoError(err)
		require.Equal(message.GetAncestorsOp, inMsgIntf.Op())
		inMsg := inMsgIntf.Message().(*p2ppb.GetAncestors)

		reqID = inMsg.RequestId

		containerID, err := ids.ToID(inMsg.ContainerId)
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

	chainRouter.Shutdown(context.Background())
}

func TestUnverifiedParent(t *testing.T) {
	require := require.New(t)
	_, genesisBytes := defaultGenesis()
	dbManager := manager.NewMemDB(version.Semantic1_0_0)

	vdrs := validators.NewManager()
	primaryVdrs := validators.NewSet()
	_ = vdrs.Add(constants.PrimaryNetworkID, primaryVdrs)
	vm := &VM{Factory: Factory{
		Config: config.Config{
			Chains:                 chains.MockManager{},
			Validators:             vdrs,
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
		require.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()

	msgChan := make(chan common.Message, 1)
	err := vm.Initialize(
		context.Background(),
		ctx,
		dbManager,
		genesisBytes,
		nil,
		nil,
		msgChan,
		nil,
		nil,
	)
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
	require.NoError(tx1.Initialize(txs.Codec))

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
	err = firstAdvanceTimeBlk.Verify(context.Background())
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
	require.NoError(tx1.Initialize(txs.Codec))
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
	require.NoError(secondAdvanceTimeBlk.Verify(context.Background()))
}

func TestMaxStakeAmount(t *testing.T) {
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
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
	firstVdrs := validators.NewManager()
	firstPrimaryVdrs := validators.NewSet()
	_ = firstVdrs.Add(constants.PrimaryNetworkID, firstPrimaryVdrs)
	firstVM := &VM{Factory: Factory{
		Config: config.Config{
			Chains:                 chains.MockManager{},
			UptimePercentage:       .2,
			RewardConfig:           defaultRewardConfig,
			Validators:             firstVdrs,
			UptimeLockedCalculator: uptime.NewLockedCalculator(),
			BanffTime:              banffForkTime,
		},
	}}

	firstCtx := defaultContext()
	firstCtx.Lock.Lock()

	firstMsgChan := make(chan common.Message, 1)
	err := firstVM.Initialize(
		context.Background(),
		firstCtx,
		firstDB,
		genesisBytes,
		nil,
		nil,
		firstMsgChan,
		nil,
		nil,
	)
	require.NoError(err)

	initialClkTime := banffForkTime.Add(time.Second)
	firstVM.clock.Set(initialClkTime)
	firstVM.uptimeManager.(uptime.TestManager).SetTime(initialClkTime)

	require.NoError(firstVM.SetState(context.Background(), snow.Bootstrapping))
	require.NoError(firstVM.SetState(context.Background(), snow.NormalOp))

	// Fast forward clock to time for genesis validators to leave
	firstVM.uptimeManager.(uptime.TestManager).SetTime(defaultValidateEndTime)

	require.NoError(firstVM.Shutdown(context.Background()))
	firstCtx.Lock.Unlock()

	secondDB := db.NewPrefixDBManager([]byte{})
	secondVdrs := validators.NewManager()
	secondPrimaryVdrs := validators.NewSet()
	_ = secondVdrs.Add(constants.PrimaryNetworkID, secondPrimaryVdrs)
	secondVM := &VM{Factory: Factory{
		Config: config.Config{
			Chains:                 chains.MockManager{},
			UptimePercentage:       .21,
			Validators:             secondVdrs,
			UptimeLockedCalculator: uptime.NewLockedCalculator(),
			BanffTime:              banffForkTime,
		},
	}}

	secondCtx := defaultContext()
	secondCtx.Lock.Lock()
	defer func() {
		require.NoError(secondVM.Shutdown(context.Background()))
		secondCtx.Lock.Unlock()
	}()

	secondMsgChan := make(chan common.Message, 1)
	err = secondVM.Initialize(
		context.Background(),
		secondCtx,
		secondDB,
		genesisBytes,
		nil,
		nil,
		secondMsgChan,
		nil,
		nil,
	)
	require.NoError(err)

	secondVM.clock.Set(defaultValidateStartTime.Add(2 * defaultMinStakingDuration))
	secondVM.uptimeManager.(uptime.TestManager).SetTime(defaultValidateStartTime.Add(2 * defaultMinStakingDuration))

	require.NoError(secondVM.SetState(context.Background(), snow.Bootstrapping))
	require.NoError(secondVM.SetState(context.Background(), snow.NormalOp))

	secondVM.clock.Set(defaultValidateEndTime)
	secondVM.uptimeManager.(uptime.TestManager).SetTime(defaultValidateEndTime)

	blk, err := secondVM.Builder.BuildBlock(context.Background()) // should advance time
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))

	// Assert preferences are correct
	block := blk.(smcon.OracleBlock)
	options, err := block.Options(context.Background())
	require.NoError(err)

	commit := options[0].(*blockexecutor.Block)
	_, ok := commit.Block.(*blocks.BanffCommitBlock)
	require.True(ok)

	abort := options[1].(*blockexecutor.Block)
	_, ok = abort.Block.(*blocks.BanffAbortBlock)
	require.True(ok)

	require.NoError(block.Accept(context.Background()))
	require.NoError(commit.Verify(context.Background()))
	require.NoError(abort.Verify(context.Background()))
	require.NoError(secondVM.SetPreference(context.Background(), secondVM.manager.LastAccepted()))

	proposalTx := blk.(blocks.Block).Txs()[0]
	{
		onAccept, ok := secondVM.manager.GetState(abort.ID())
		require.True(ok)

		_, txStatus, err := onAccept.GetTx(proposalTx.ID())
		require.NoError(err)
		require.Equal(status.Aborted, txStatus)
	}

	require.NoError(commit.Accept(context.Background())) // advance the timestamp
	require.NoError(secondVM.SetPreference(context.Background(), secondVM.manager.LastAccepted()))

	_, txStatus, err := secondVM.state.GetTx(proposalTx.ID())
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	// Verify that chain's timestamp has advanced
	timestamp := secondVM.state.GetTimestamp()
	require.Equal(defaultValidateEndTime.Unix(), timestamp.Unix())

	blk, err = secondVM.Builder.BuildBlock(context.Background()) // should contain proposal to reward genesis validator
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))

	block = blk.(smcon.OracleBlock)
	options, err = block.Options(context.Background())
	require.NoError(err)

	commit = options[0].(*blockexecutor.Block)
	_, ok = commit.Block.(*blocks.BanffCommitBlock)
	require.True(ok)

	abort = options[1].(*blockexecutor.Block)
	_, ok = abort.Block.(*blocks.BanffAbortBlock)
	require.True(ok)

	require.NoError(blk.Accept(context.Background()))
	require.NoError(commit.Verify(context.Background()))
	require.NoError(secondVM.SetPreference(context.Background(), secondVM.manager.LastAccepted()))

	proposalTx = blk.(blocks.Block).Txs()[0]
	{
		onAccept, ok := secondVM.manager.GetState(commit.ID())
		require.True(ok)

		_, txStatus, err := onAccept.GetTx(proposalTx.ID())
		require.NoError(err)
		require.Equal(status.Committed, txStatus)
	}

	require.NoError(abort.Verify(context.Background()))
	require.NoError(abort.Accept(context.Background())) // do not reward the genesis validator
	require.NoError(secondVM.SetPreference(context.Background(), secondVM.manager.LastAccepted()))

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

	vdrs := validators.NewManager()
	primaryVdrs := validators.NewSet()
	_ = vdrs.Add(constants.PrimaryNetworkID, primaryVdrs)
	vm := &VM{Factory: Factory{
		Config: config.Config{
			Chains:                 chains.MockManager{},
			UptimePercentage:       .2,
			RewardConfig:           defaultRewardConfig,
			Validators:             vdrs,
			UptimeLockedCalculator: uptime.NewLockedCalculator(),
			BanffTime:              banffForkTime,
		},
	}}

	ctx := defaultContext()
	ctx.Lock.Lock()

	msgChan := make(chan common.Message, 1)
	appSender := &common.SenderTest{T: t}
	err := vm.Initialize(
		context.Background(),
		ctx,
		db,
		genesisBytes,
		nil,
		nil,
		msgChan,
		nil,
		appSender,
	)
	require.NoError(err)

	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()

	initialClkTime := banffForkTime.Add(time.Second)
	vm.clock.Set(initialClkTime)
	vm.uptimeManager.(uptime.TestManager).SetTime(initialClkTime)

	require.NoError(vm.SetState(context.Background(), snow.Bootstrapping))
	require.NoError(vm.SetState(context.Background(), snow.NormalOp))

	// Fast forward clock to time for genesis validators to leave
	vm.clock.Set(defaultValidateEndTime)
	vm.uptimeManager.(uptime.TestManager).SetTime(defaultValidateEndTime)

	blk, err := vm.Builder.BuildBlock(context.Background()) // should advance time
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))

	// first the time will be advanced.
	block := blk.(smcon.OracleBlock)
	options, err := block.Options(context.Background())
	require.NoError(err)

	commit := options[0].(*blockexecutor.Block)
	_, ok := commit.Block.(*blocks.BanffCommitBlock)
	require.True(ok)

	abort := options[1].(*blockexecutor.Block)
	_, ok = abort.Block.(*blocks.BanffAbortBlock)
	require.True(ok)

	require.NoError(block.Accept(context.Background()))
	require.NoError(commit.Verify(context.Background()))
	require.NoError(abort.Verify(context.Background()))
	require.NoError(commit.Accept(context.Background())) // advance the timestamp
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	// Verify that chain's timestamp has advanced
	timestamp := vm.state.GetTimestamp()
	require.Equal(defaultValidateEndTime.Unix(), timestamp.Unix())

	// should contain proposal to reward genesis validator
	blk, err = vm.Builder.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))

	block = blk.(smcon.OracleBlock)
	options, err = block.Options(context.Background())
	require.NoError(err)

	commit = options[0].(*blockexecutor.Block)
	_, ok = commit.Block.(*blocks.BanffCommitBlock)
	require.True(ok)

	abort = options[1].(*blockexecutor.Block)
	_, ok = abort.Block.(*blocks.BanffAbortBlock)
	require.True(ok)

	require.NoError(blk.Accept(context.Background()))
	require.NoError(commit.Verify(context.Background()))
	require.NoError(abort.Verify(context.Background()))
	require.NoError(abort.Accept(context.Background())) // do not reward the genesis validator
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	_, err = vm.state.GetCurrentValidator(
		constants.PrimaryNetworkID,
		ids.NodeID(keys[1].PublicKey().Address()),
	)
	require.ErrorIs(err, database.ErrNotFound)
}

func TestVM_GetValidatorSet(t *testing.T) {
	r := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Setup VM
	_, genesisBytes := defaultGenesis()
	db := manager.NewMemDB(version.Semantic1_0_0)

	vdrManager := validators.NewManager()
	primaryVdrs := validators.NewSet()
	_ = vdrManager.Add(constants.PrimaryNetworkID, primaryVdrs)

	vm := &VM{Factory: Factory{
		Config: config.Config{
			Chains:                 chains.MockManager{},
			UptimePercentage:       .2,
			RewardConfig:           defaultRewardConfig,
			Validators:             vdrManager,
			UptimeLockedCalculator: uptime.NewLockedCalculator(),
			BanffTime:              mockable.MaxTime,
		},
	}}

	ctx := defaultContext()
	ctx.Lock.Lock()

	msgChan := make(chan common.Message, 1)
	appSender := &common.SenderTest{T: t}
	r.NoError(vm.Initialize(context.Background(), ctx, db, genesisBytes, nil, nil, msgChan, nil, appSender))
	defer func() {
		r.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()

	vm.clock.Set(defaultGenesisTime)
	vm.uptimeManager.(uptime.TestManager).SetTime(defaultGenesisTime)

	r.NoError(vm.SetState(context.Background(), snow.Bootstrapping))
	r.NoError(vm.SetState(context.Background(), snow.NormalOp))

	var (
		oldVdrs       = vm.Validators
		oldState      = vm.state
		numVdrs       = 4
		vdrBaseWeight = uint64(1_000)
		vdrs          []*validators.Validator
	)
	// Populate the validator set to use below
	for i := 0; i < numVdrs; i++ {
		sk, err := bls.NewSecretKey()
		r.NoError(err)

		vdrs = append(vdrs, &validators.Validator{
			NodeID:    ids.GenerateTestNodeID(),
			PublicKey: bls.PublicFromSecretKey(sk),
			Weight:    vdrBaseWeight + uint64(i),
		})
	}

	type test struct {
		name string
		// Height we're getting the diff at
		height             uint64
		lastAcceptedHeight uint64
		subnetID           ids.ID
		// Validator sets at tip
		currentPrimaryNetworkValidators []*validators.Validator
		currentSubnetValidators         []*validators.Validator
		// Diff at tip, block before tip, etc.
		// This must have [height] - [lastAcceptedHeight] elements
		weightDiffs []map[ids.NodeID]*state.ValidatorWeightDiff
		// Diff at tip, block before tip, etc.
		// This must have [height] - [lastAcceptedHeight] elements
		pkDiffs        []map[ids.NodeID]*bls.PublicKey
		expectedVdrSet map[ids.NodeID]*validators.GetValidatorOutput
		expectedErr    error
	}

	tests := []test{
		{
			name:               "after tip",
			height:             1,
			lastAcceptedHeight: 0,
			expectedVdrSet:     map[ids.NodeID]*validators.GetValidatorOutput{},
			expectedErr:        database.ErrNotFound,
		},
		{
			name:               "at tip",
			height:             1,
			lastAcceptedHeight: 1,
			currentPrimaryNetworkValidators: []*validators.Validator{
				copyPrimaryValidator(vdrs[0]),
			},
			currentSubnetValidators: []*validators.Validator{
				copySubnetValidator(vdrs[0]),
			},
			expectedVdrSet: map[ids.NodeID]*validators.GetValidatorOutput{
				vdrs[0].NodeID: {
					NodeID:    vdrs[0].NodeID,
					PublicKey: vdrs[0].PublicKey,
					Weight:    vdrs[0].Weight,
				},
			},
			expectedErr: nil,
		},
		{
			name:               "1 before tip",
			height:             2,
			lastAcceptedHeight: 3,
			currentPrimaryNetworkValidators: []*validators.Validator{
				copyPrimaryValidator(vdrs[0]),
				copyPrimaryValidator(vdrs[1]),
			},
			currentSubnetValidators: []*validators.Validator{
				// At tip we have these 2 validators
				copySubnetValidator(vdrs[0]),
				copySubnetValidator(vdrs[1]),
			},
			weightDiffs: []map[ids.NodeID]*state.ValidatorWeightDiff{
				{
					// At the tip block vdrs[0] lost weight, vdrs[1] gained weight,
					// and vdrs[2] left
					vdrs[0].NodeID: {
						Decrease: true,
						Amount:   1,
					},
					vdrs[1].NodeID: {
						Decrease: false,
						Amount:   1,
					},
					vdrs[2].NodeID: {
						Decrease: true,
						Amount:   vdrs[2].Weight,
					},
				},
			},
			pkDiffs: []map[ids.NodeID]*bls.PublicKey{
				{
					vdrs[2].NodeID: vdrs[2].PublicKey,
				},
			},
			expectedVdrSet: map[ids.NodeID]*validators.GetValidatorOutput{
				vdrs[0].NodeID: {
					NodeID:    vdrs[0].NodeID,
					PublicKey: vdrs[0].PublicKey,
					Weight:    vdrs[0].Weight + 1,
				},
				vdrs[1].NodeID: {
					NodeID:    vdrs[1].NodeID,
					PublicKey: vdrs[1].PublicKey,
					Weight:    vdrs[1].Weight - 1,
				},
				vdrs[2].NodeID: {
					NodeID:    vdrs[2].NodeID,
					PublicKey: vdrs[2].PublicKey,
					Weight:    vdrs[2].Weight,
				},
			},
			expectedErr: nil,
		},
		{
			name:               "2 before tip",
			height:             3,
			lastAcceptedHeight: 5,
			currentPrimaryNetworkValidators: []*validators.Validator{
				copyPrimaryValidator(vdrs[0]),
				copyPrimaryValidator(vdrs[1]),
			},
			currentSubnetValidators: []*validators.Validator{
				// At tip we have these 2 validators
				copySubnetValidator(vdrs[0]),
				copySubnetValidator(vdrs[1]),
			},
			weightDiffs: []map[ids.NodeID]*state.ValidatorWeightDiff{
				{
					// At the tip block vdrs[0] lost weight, vdrs[1] gained weight,
					// and vdrs[2] left
					vdrs[0].NodeID: {
						Decrease: true,
						Amount:   1,
					},
					vdrs[1].NodeID: {
						Decrease: false,
						Amount:   1,
					},
					vdrs[2].NodeID: {
						Decrease: true,
						Amount:   vdrs[2].Weight,
					},
				},
				{
					// At the block before tip vdrs[0] lost weight, vdrs[1] gained weight,
					// vdrs[2] joined
					vdrs[0].NodeID: {
						Decrease: true,
						Amount:   1,
					},
					vdrs[1].NodeID: {
						Decrease: false,
						Amount:   1,
					},
					vdrs[2].NodeID: {
						Decrease: false,
						Amount:   vdrs[2].Weight,
					},
				},
			},
			pkDiffs: []map[ids.NodeID]*bls.PublicKey{
				{
					vdrs[2].NodeID: vdrs[2].PublicKey,
				},
				{},
			},
			expectedVdrSet: map[ids.NodeID]*validators.GetValidatorOutput{
				vdrs[0].NodeID: {
					NodeID:    vdrs[0].NodeID,
					PublicKey: vdrs[0].PublicKey,
					Weight:    vdrs[0].Weight + 2,
				},
				vdrs[1].NodeID: {
					NodeID:    vdrs[1].NodeID,
					PublicKey: vdrs[1].PublicKey,
					Weight:    vdrs[1].Weight - 2,
				},
			},
			expectedErr: nil,
		},
		{
			name:               "1 before tip; nil public key",
			height:             4,
			lastAcceptedHeight: 5,
			currentPrimaryNetworkValidators: []*validators.Validator{
				copyPrimaryValidator(vdrs[0]),
				copyPrimaryValidator(vdrs[1]),
			},
			currentSubnetValidators: []*validators.Validator{
				// At tip we have these 2 validators
				copySubnetValidator(vdrs[0]),
				copySubnetValidator(vdrs[1]),
			},
			weightDiffs: []map[ids.NodeID]*state.ValidatorWeightDiff{
				{
					// At the tip block vdrs[0] lost weight, vdrs[1] gained weight,
					// and vdrs[2] left
					vdrs[0].NodeID: {
						Decrease: true,
						Amount:   1,
					},
					vdrs[1].NodeID: {
						Decrease: false,
						Amount:   1,
					},
					vdrs[2].NodeID: {
						Decrease: true,
						Amount:   vdrs[2].Weight,
					},
				},
			},
			pkDiffs: []map[ids.NodeID]*bls.PublicKey{
				{},
			},
			expectedVdrSet: map[ids.NodeID]*validators.GetValidatorOutput{
				vdrs[0].NodeID: {
					NodeID:    vdrs[0].NodeID,
					PublicKey: vdrs[0].PublicKey,
					Weight:    vdrs[0].Weight + 1,
				},
				vdrs[1].NodeID: {
					NodeID:    vdrs[1].NodeID,
					PublicKey: vdrs[1].PublicKey,
					Weight:    vdrs[1].Weight - 1,
				},
				vdrs[2].NodeID: {
					NodeID: vdrs[2].NodeID,
					Weight: vdrs[2].Weight,
				},
			},
			expectedErr: nil,
		},
		{
			name:               "1 before tip; subnet",
			height:             5,
			lastAcceptedHeight: 6,
			subnetID:           ids.GenerateTestID(),
			currentPrimaryNetworkValidators: []*validators.Validator{
				copyPrimaryValidator(vdrs[0]),
				copyPrimaryValidator(vdrs[1]),
				copyPrimaryValidator(vdrs[3]),
			},
			currentSubnetValidators: []*validators.Validator{
				// At tip we have these 2 validators
				copySubnetValidator(vdrs[0]),
				copySubnetValidator(vdrs[1]),
			},
			weightDiffs: []map[ids.NodeID]*state.ValidatorWeightDiff{
				{
					// At the tip block vdrs[0] lost weight, vdrs[1] gained weight,
					// and vdrs[2] left
					vdrs[0].NodeID: {
						Decrease: true,
						Amount:   1,
					},
					vdrs[1].NodeID: {
						Decrease: false,
						Amount:   1,
					},
					vdrs[2].NodeID: {
						Decrease: true,
						Amount:   vdrs[2].Weight,
					},
				},
			},
			pkDiffs: []map[ids.NodeID]*bls.PublicKey{
				{},
			},
			expectedVdrSet: map[ids.NodeID]*validators.GetValidatorOutput{
				vdrs[0].NodeID: {
					NodeID:    vdrs[0].NodeID,
					PublicKey: vdrs[0].PublicKey,
					Weight:    vdrs[0].Weight + 1,
				},
				vdrs[1].NodeID: {
					NodeID:    vdrs[1].NodeID,
					PublicKey: vdrs[1].PublicKey,
					Weight:    vdrs[1].Weight - 1,
				},
				vdrs[2].NodeID: {
					NodeID: vdrs[2].NodeID,
					Weight: vdrs[2].Weight,
				},
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			// Mock the VM's validators
			vdrs := validators.NewMockManager(ctrl)
			vm.Validators = vdrs
			mockSubnetVdrSet := validators.NewMockSet(ctrl)
			mockSubnetVdrSet.EXPECT().List().Return(tt.currentSubnetValidators).AnyTimes()
			vdrs.EXPECT().Get(tt.subnetID).Return(mockSubnetVdrSet, true).AnyTimes()

			mockPrimaryVdrSet := mockSubnetVdrSet
			if tt.subnetID != constants.PrimaryNetworkID {
				mockPrimaryVdrSet = validators.NewMockSet(ctrl)
				vdrs.EXPECT().Get(constants.PrimaryNetworkID).Return(mockPrimaryVdrSet, true).AnyTimes()
			}
			for _, vdr := range tt.currentPrimaryNetworkValidators {
				mockPrimaryVdrSet.EXPECT().Get(vdr.NodeID).Return(vdr, true).AnyTimes()
			}

			// Mock the block manager
			mockManager := blockexecutor.NewMockManager(ctrl)
			vm.manager = mockManager

			// Mock the VM's state
			mockState := state.NewMockState(ctrl)
			vm.state = mockState

			// Tell state what diffs to report
			for _, weightDiff := range tt.weightDiffs {
				mockState.EXPECT().GetValidatorWeightDiffs(gomock.Any(), gomock.Any()).Return(weightDiff, nil)
			}

			for _, pkDiff := range tt.pkDiffs {
				mockState.EXPECT().GetValidatorPublicKeyDiffs(gomock.Any()).Return(pkDiff, nil)
			}

			// Tell state last accepted block to report
			mockTip := smcon.NewMockBlock(ctrl)
			mockTip.EXPECT().Height().Return(tt.lastAcceptedHeight)
			mockTipID := ids.GenerateTestID()
			mockState.EXPECT().GetLastAccepted().Return(mockTipID)
			mockManager.EXPECT().GetBlock(mockTipID).Return(mockTip, nil)

			// Compute validator set at previous height
			gotVdrSet, err := vm.GetValidatorSet(context.Background(), tt.height, tt.subnetID)
			require.ErrorIs(err, tt.expectedErr)
			if tt.expectedErr != nil {
				return
			}
			require.Equal(len(tt.expectedVdrSet), len(gotVdrSet))
			for nodeID, vdr := range tt.expectedVdrSet {
				otherVdr, ok := gotVdrSet[nodeID]
				require.True(ok)
				require.Equal(vdr, otherVdr)
			}
		})
	}

	// Put these back so we don't need to mock calls made on Shutdown
	vm.Validators = oldVdrs
	vm.state = oldState
}

func copyPrimaryValidator(vdr *validators.Validator) *validators.Validator {
	newVdr := *vdr
	return &newVdr
}

func copySubnetValidator(vdr *validators.Validator) *validators.Validator {
	newVdr := *vdr
	newVdr.PublicKey = nil
	return &newVdr
}
