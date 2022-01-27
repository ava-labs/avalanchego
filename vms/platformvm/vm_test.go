// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/prometheus/client_golang/prometheus"

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
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	smcon "github.com/ava-labs/avalanchego/snow/consensus/snowman"
	smeng "github.com/ava-labs/avalanchego/snow/engine/snowman"
	snowgetter "github.com/ava-labs/avalanchego/snow/engine/snowman/getter"
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

	// each key controls an address that has [defaultBalance] AVAX at genesis
	keys []*crypto.PrivateKeySECP256K1R

	defaultMinValidatorStake = 5 * units.MilliAvax
	defaultMaxValidatorStake = 500 * units.MilliAvax
	defaultMinDelegatorStake = 1 * units.MilliAvax

	// amount all genesis validators have in defaultVM
	defaultBalance = 100 * defaultMinValidatorStake

	// subnet that exists at genesis in defaultVM
	// Its controlKeys are keys[0], keys[1], keys[2]
	// Its threshold is 2
	testSubnet1            *UnsignedCreateSubnetTx
	testSubnet1ControlKeys []*crypto.PrivateKeySECP256K1R

	xChainID = ids.Empty.Prefix(0)
	cChainID = ids.Empty.Prefix(1)
)

var (
	errShouldPrefCommit = errors.New("should prefer to commit proposal")
	errShouldPrefAbort  = errors.New("should prefer to abort proposal")
)

const (
	testNetworkID = 10 // To be used in tests
	defaultWeight = 10000
)

func init() {
	ctx := defaultContext()
	factory := crypto.FactorySECP256K1R{}
	for _, key := range []string{
		"24jUJ9vZexUM6expyMcT48LBx27k1m7xpraoV62oSQAHdziao5",
		"2MMvUMsxx6zsHSNXJdFD8yc5XkancvwyKPwpw4xUK3TCGDuNBY",
		"cxb7KpGWhDMALTjNNSJ7UQkkomPesyWAPUaWRGdyeBNzR6f35",
		"ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN",
		"2RWLv6YVEXDiWLpaCbXhhqxtLbnFaKQsWPSSMSPhpWo47uJAeV",
	} {
		privKeyBytes, err := formatting.Decode(formatting.CB58, key)
		ctx.Log.AssertNoError(err)
		pk, err := factory.ToPrivateKey(privKeyBytes)
		ctx.Log.AssertNoError(err)
		keys = append(keys, pk.(*crypto.PrivateKeySECP256K1R))
	}
	testSubnet1ControlKeys = keys[0:3]
}

type snLookup struct {
	chainsToSubnet map[ids.ID]ids.ID
}

func (sn *snLookup) SubnetID(chainID ids.ID) (ids.ID, error) {
	subnetID, ok := sn.chainsToSubnet[chainID]
	if !ok {
		return ids.ID{}, errors.New("")
	}
	return subnetID, nil
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
func defaultGenesis() (*BuildGenesisArgs, []byte) {
	genesisUTXOs := make([]APIUTXO, len(keys))
	hrp := constants.NetworkIDToHRP[testNetworkID]
	for i, key := range keys {
		id := key.PublicKey().Address()
		addr, err := formatting.FormatBech32(hrp, id.Bytes())
		if err != nil {
			panic(err)
		}
		genesisUTXOs[i] = APIUTXO{
			Amount:  json.Uint64(defaultBalance),
			Address: addr,
		}
	}

	genesisValidators := make([]APIPrimaryValidator, len(keys))
	for i, key := range keys {
		id := key.PublicKey().Address()
		addr, err := formatting.FormatBech32(hrp, id.Bytes())
		if err != nil {
			panic(err)
		}
		genesisValidators[i] = APIPrimaryValidator{
			APIStaker: APIStaker{
				StartTime: json.Uint64(defaultValidateStartTime.Unix()),
				EndTime:   json.Uint64(defaultValidateEndTime.Unix()),
				NodeID:    id.PrefixedString(constants.NodeIDPrefix),
			},
			RewardOwner: &APIOwner{
				Threshold: 1,
				Addresses: []string{addr},
			},
			Staked: []APIUTXO{{
				Amount:  json.Uint64(defaultWeight),
				Address: addr,
			}},
			DelegationFee: reward.PercentDenominator,
		}
	}

	buildGenesisArgs := BuildGenesisArgs{
		Encoding:      formatting.Hex,
		NetworkID:     json.Uint32(testNetworkID),
		AvaxAssetID:   avaxAssetID,
		UTXOs:         genesisUTXOs,
		Validators:    genesisValidators,
		Chains:        nil,
		Time:          json.Uint64(defaultGenesisTime.Unix()),
		InitialSupply: json.Uint64(360 * units.MegaAvax),
	}

	buildGenesisResponse := BuildGenesisReply{}
	platformvmSS := StaticService{}
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
func BuildGenesisTest(t *testing.T) (*BuildGenesisArgs, []byte) {
	return BuildGenesisTestWithArgs(t, nil)
}

// Returns:
// 1) The genesis state
// 2) The byte representation of the default genesis for tests
func BuildGenesisTestWithArgs(t *testing.T, args *BuildGenesisArgs) (*BuildGenesisArgs, []byte) {
	genesisUTXOs := make([]APIUTXO, len(keys))
	hrp := constants.NetworkIDToHRP[testNetworkID]
	for i, key := range keys {
		id := key.PublicKey().Address()
		addr, err := formatting.FormatBech32(hrp, id.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		genesisUTXOs[i] = APIUTXO{
			Amount:  json.Uint64(defaultBalance),
			Address: addr,
		}
	}

	genesisValidators := make([]APIPrimaryValidator, len(keys))
	for i, key := range keys {
		id := key.PublicKey().Address()
		addr, err := formatting.FormatBech32(hrp, id.Bytes())
		if err != nil {
			panic(err)
		}
		genesisValidators[i] = APIPrimaryValidator{
			APIStaker: APIStaker{
				StartTime: json.Uint64(defaultValidateStartTime.Unix()),
				EndTime:   json.Uint64(defaultValidateEndTime.Unix()),
				NodeID:    id.PrefixedString(constants.NodeIDPrefix),
			},
			RewardOwner: &APIOwner{
				Threshold: 1,
				Addresses: []string{addr},
			},
			Staked: []APIUTXO{{
				Amount:  json.Uint64(defaultWeight),
				Address: addr,
			}},
			DelegationFee: reward.PercentDenominator,
		}
	}

	buildGenesisArgs := BuildGenesisArgs{
		NetworkID:     json.Uint32(testNetworkID),
		AvaxAssetID:   avaxAssetID,
		UTXOs:         genesisUTXOs,
		Validators:    genesisValidators,
		Chains:        nil,
		Time:          json.Uint64(defaultGenesisTime.Unix()),
		InitialSupply: json.Uint64(360 * units.MegaAvax),
		Encoding:      formatting.CB58,
	}

	if args != nil {
		buildGenesisArgs = *args
	}

	buildGenesisResponse := BuildGenesisReply{}
	platformvmSS := StaticService{}
	if err := platformvmSS.BuildGenesis(nil, &buildGenesisArgs, &buildGenesisResponse); err != nil {
		t.Fatalf("problem while building platform chain's genesis state: %v", err)
	}

	genesisBytes, err := formatting.Decode(buildGenesisResponse.Encoding, buildGenesisResponse.Bytes)
	if err != nil {
		t.Fatal(err)
	}

	return &buildGenesisArgs, genesisBytes
}

func defaultVM() (*VM, database.Database, *common.SenderTest) {
	vm := &VM{Factory: Factory{
		Chains:                 chains.MockManager{},
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		Validators:             validators.NewManager(),
		TxFee:                  defaultTxFee,
		CreateSubnetTxFee:      100 * defaultTxFee,
		CreateBlockchainTxFee:  100 * defaultTxFee,
		MinValidatorStake:      defaultMinValidatorStake,
		MaxValidatorStake:      defaultMaxValidatorStake,
		MinDelegatorStake:      defaultMinDelegatorStake,
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
		ApricotPhase3Time:      defaultValidateEndTime,
		ApricotPhase4Time:      defaultValidateEndTime,
		ApricotPhase5Time:      defaultValidateEndTime,
	}}

	baseDBManager := manager.NewMemDB(version.DefaultVersion1_0_0)
	chainDBManager := baseDBManager.NewPrefixDBManager([]byte{0})
	atomicDB := prefixdb.New([]byte{1}, baseDBManager.Current().Database)

	vm.clock.Set(defaultGenesisTime)
	msgChan := make(chan common.Message, 1)
	ctx := defaultContext()

	m := &atomic.Memory{}
	err := m.Initialize(logging.NoLog{}, atomicDB)
	if err != nil {
		panic(err)
	}
	ctx.SharedMemory = m.NewSharedMemory(ctx.ChainID)

	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()
	_, genesisBytes := defaultGenesis()
	appSender := &common.SenderTest{}
	appSender.CantSendAppGossip = true
	appSender.SendAppGossipF = func([]byte) error { return nil }

	if err := vm.Initialize(ctx, chainDBManager, genesisBytes, nil, nil, msgChan, nil, appSender); err != nil {
		panic(err)
	}
	if err := vm.SetState(snow.NormalOp); err != nil {
		panic(err)
	}

	// Create a subnet and store it in testSubnet1
	if tx, err := vm.newCreateSubnetTx(
		2, // threshold; 2 sigs from keys[0], keys[1], keys[2] needed to add validator to this subnet
		// control keys are keys[0], keys[1], keys[2]
		[]ids.ShortID{keys[0].PublicKey().Address(), keys[1].PublicKey().Address(), keys[2].PublicKey().Address()},
		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // pays tx fee
		keys[0].PublicKey().Address(),           // change addr
	); err != nil {
		panic(err)
	} else if err := vm.blockBuilder.AddUnverifiedTx(tx); err != nil {
		panic(err)
	} else if blk, err := vm.BuildBlock(); err != nil {
		panic(err)
	} else if err := blk.Verify(); err != nil {
		panic(err)
	} else if err := blk.Accept(); err != nil {
		panic(err)
	} else {
		testSubnet1 = tx.UnsignedTx.(*UnsignedCreateSubnetTx)
	}

	return vm, baseDBManager.Current().Database, appSender
}

func GenesisVMWithArgs(t *testing.T, args *BuildGenesisArgs) ([]byte, chan common.Message, *VM, *atomic.Memory) {
	var genesisBytes []byte

	if args != nil {
		_, genesisBytes = BuildGenesisTestWithArgs(t, args)
	} else {
		_, genesisBytes = BuildGenesisTest(t)
	}

	vm := &VM{Factory: Factory{
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
	}}

	baseDBManager := manager.NewMemDB(version.DefaultVersion1_0_0)
	chainDBManager := baseDBManager.NewPrefixDBManager([]byte{0})
	atomicDB := prefixdb.New([]byte{1}, baseDBManager.Current().Database)

	vm.clock.Set(defaultGenesisTime)
	msgChan := make(chan common.Message, 1)
	ctx := defaultContext()

	m := &atomic.Memory{}
	err := m.Initialize(logging.NoLog{}, atomicDB)
	if err != nil {
		panic(err)
	}

	ctx.SharedMemory = m.NewSharedMemory(ctx.ChainID)

	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()
	appSender := &common.SenderTest{}
	appSender.CantSendAppGossip = true
	appSender.SendAppGossipF = func([]byte) error { return nil }
	if err := vm.Initialize(ctx, chainDBManager, genesisBytes, nil, nil, msgChan, nil, appSender); err != nil {
		t.Fatal(err)
	}
	if err := vm.SetState(snow.NormalOp); err != nil {
		panic(err)
	}

	// Create a subnet and store it in testSubnet1
	if tx, err := vm.newCreateSubnetTx(
		2, // threshold; 2 sigs from keys[0], keys[1], keys[2] needed to add validator to this subnet
		// control keys are keys[0], keys[1], keys[2]
		[]ids.ShortID{keys[0].PublicKey().Address(), keys[1].PublicKey().Address(), keys[2].PublicKey().Address()},
		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // pays tx fee
		keys[0].PublicKey().Address(),           // change addr
	); err != nil {
		panic(err)
	} else if err := vm.blockBuilder.AddUnverifiedTx(tx); err != nil {
		panic(err)
	} else if blk, err := vm.BuildBlock(); err != nil {
		panic(err)
	} else if err := blk.Verify(); err != nil {
		panic(err)
	} else if err := blk.Accept(); err != nil {
		panic(err)
	} else {
		testSubnet1 = tx.UnsignedTx.(*UnsignedCreateSubnetTx)
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
	if genesisBlock, err := vm.getBlock(genesisBlockID); err != nil {
		t.Fatalf("couldn't get genesis block: %v", err)
	} else if genesisBlock.Status() != choices.Accepted {
		t.Fatal("genesis block should be accepted")
	}

	genesisState, _ := defaultGenesis()
	// Ensure all the genesis UTXOs are there
	for _, utxo := range genesisState.UTXOs {
		_, addrBytes, err := formatting.ParseBech32(utxo.Address)
		if err != nil {
			t.Fatal(err)
		}
		addr, err := ids.ToShortID(addrBytes)
		if err != nil {
			t.Fatal(err)
		}
		addrs := ids.ShortSet{}
		addrs.Add(addr)
		utxos, err := avax.GetAllUTXOs(vm.internalState, addrs)
		if err != nil {
			t.Fatal("couldn't find UTXO")
		} else if len(utxos) != 1 {
			t.Fatal("expected each address to have one UTXO")
		} else if out, ok := utxos[0].Out.(*secp256k1fx.TransferOutput); !ok {
			t.Fatal("expected utxo output to be type *secp256k1fx.TransferOutput")
		} else if out.Amount() != uint64(utxo.Amount) {
			id := keys[0].PublicKey().Address()
			hrp := constants.NetworkIDToHRP[testNetworkID]
			addr, err := formatting.FormatBech32(hrp, id.Bytes())
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
		if addr := key.PublicKey().Address(); !vdrSet.Contains(addr) {
			t.Fatalf("should have had validator with NodeID %s", addr)
		}
	}

	// Ensure genesis timestamp is correct
	if timestamp := vm.internalState.GetTimestamp(); timestamp.Unix() != int64(genesisState.Time) {
		t.Fatalf("vm's time is incorrect. Expected %v got %v", genesisState.Time, timestamp)
	}

	// Ensure the new subnet we created exists
	if _, _, err := vm.internalState.GetTx(testSubnet1.ID()); err != nil {
		t.Fatalf("expected subnet %s to exist", testSubnet1.ID())
	}
}

// accept proposal to add validator to primary network
func TestAddValidatorCommit(t *testing.T) {
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	startTime := defaultGenesisTime.Add(syncBound).Add(1 * time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)
	key, err := vm.factory.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	nodeID := key.PublicKey().Address()

	// create valid tx
	tx, err := vm.newAddValidatorTx(
		vm.MinValidatorStake,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		nodeID,
		nodeID,
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		t.Fatal(err)
	}

	// trigger block creation
	if err := vm.blockBuilder.AddUnverifiedTx(tx); err != nil {
		t.Fatal(err)
	}
	blk, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	// Assert preferences are correct
	block := blk.(*ProposalBlock)
	options, err := block.Options()
	if err != nil {
		t.Fatal(err)
	}
	commit, ok := options[0].(*CommitBlock)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	}
	_, ok = options[1].(*AbortBlock)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	} else if err := block.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if err := commit.Accept(); err != nil { // commit the proposal
		t.Fatal(err)
	} else if _, txStatus, err := vm.internalState.GetTx(tx.ID()); err != nil {
		t.Fatal(err)
	} else if txStatus != status.Committed {
		t.Fatalf("status of tx should be Committed but is %s", txStatus)
	}

	pendingStakers := vm.internalState.PendingStakerChainState()

	// Verify that new validator now in pending validator set
	if _, err := pendingStakers.GetValidatorTx(nodeID); err != nil {
		t.Fatalf("Should have added validator to the pending queue")
	}
}

// verify invalid proposal to add validator to primary network
func TestInvalidAddValidatorCommit(t *testing.T) {
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	startTime := defaultGenesisTime.Add(-syncBound).Add(-1 * time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)
	key, _ := vm.factory.NewPrivateKey()
	nodeID := key.PublicKey().Address()

	// create invalid tx
	tx, err := vm.newAddValidatorTx(
		vm.MinValidatorStake,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		nodeID,
		nodeID,
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		t.Fatal(err)
	}

	preferred, err := vm.Preferred()
	if err != nil {
		t.Fatal(err)
	}
	preferredID := preferred.ID()
	preferredHeight := preferred.Height()
	blk, err := vm.newProposalBlock(preferredID, preferredHeight+1, *tx)
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
	if _, ok := vm.droppedTxCache.Get(blk.Tx.ID()); !ok {
		t.Fatal("tx should be in dropped tx cache")
	}
}

// Reject proposal to add validator to primary network
func TestAddValidatorReject(t *testing.T) {
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	startTime := defaultGenesisTime.Add(syncBound).Add(1 * time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)
	key, _ := vm.factory.NewPrivateKey()
	nodeID := key.PublicKey().Address()

	// create valid tx
	tx, err := vm.newAddValidatorTx(
		vm.MinValidatorStake,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		nodeID,
		nodeID,
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		t.Fatal(err)
	}

	// trigger block creation
	if err := vm.blockBuilder.AddUnverifiedTx(tx); err != nil {
		t.Fatal(err)
	}
	blk, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	// Assert preferences are correct
	block := blk.(*ProposalBlock)
	options, err := block.Options()
	if err != nil {
		t.Fatal(err)
	} else if commit, ok := options[0].(*CommitBlock); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*AbortBlock); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if err := block.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil { // should pass verification
		t.Fatal(err)
	} else if err := abort.Verify(); err != nil { // should pass verification
		t.Fatal(err)
	} else if err := abort.Accept(); err != nil { // reject the proposal
		t.Fatal(err)
	} else if _, txStatus, err := vm.internalState.GetTx(tx.ID()); err != nil {
		t.Fatal(err)
	} else if txStatus != status.Aborted {
		t.Fatalf("status should be Aborted but is %s", txStatus)
	}

	pendingStakers := vm.internalState.PendingStakerChainState()

	// Verify that new validator NOT in pending validator set
	if _, err := pendingStakers.GetValidatorTx(nodeID); err == nil {
		t.Fatalf("Shouldn't have added validator to the pending queue")
	}
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
	repeatNodeID := keys[0].PublicKey().Address()

	startTime := defaultGenesisTime.Add(syncBound).Add(1 * time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)

	// create valid tx
	tx, err := vm.newAddValidatorTx(
		vm.MinValidatorStake,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		repeatNodeID,
		repeatNodeID,
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		t.Fatal(err)
	}

	// trigger block creation
	if err := vm.blockBuilder.AddUnverifiedTx(tx); err == nil {
		t.Fatal("Expected BuildBlock to error due to adding a validator with a nodeID that is already in the validator set.")
	}
}

// Accept proposal to add validator to subnet
func TestAddSubnetValidatorAccept(t *testing.T) {
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	startTime := defaultValidateStartTime.Add(syncBound).Add(1 * time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)
	nodeID := keys[0].PublicKey().Address()

	// create valid tx
	// note that [startTime, endTime] is a subset of time that keys[0]
	// validates primary network ([defaultValidateStartTime, defaultValidateEndTime])
	tx, err := vm.newAddSubnetValidatorTx(
		defaultWeight,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		nodeID,
		testSubnet1.ID(),
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		t.Fatal(err)
	}

	// trigger block creation
	if err := vm.blockBuilder.AddUnverifiedTx(tx); err != nil {
		t.Fatal(err)
	}
	blk, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	// Assert preferences are correct
	block := blk.(*ProposalBlock)
	options, err := block.Options()
	if err != nil {
		t.Fatal(err)
	}
	commit, ok := options[0].(*CommitBlock)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*AbortBlock); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if err := block.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Verify(); err != nil {
		t.Fatal(err)
	} else if _, txStatus, err := abort.onAccept().GetTx(tx.ID()); err != nil {
		t.Fatal(err)
	} else if txStatus != status.Aborted {
		t.Fatalf("status should be Aborted but is %s", txStatus)
	} else if err := commit.Accept(); err != nil { // accept the proposal
		t.Fatal(err)
	} else if _, txStatus, err := vm.internalState.GetTx(tx.ID()); err != nil {
		t.Fatal(err)
	} else if txStatus != status.Committed {
		t.Fatalf("status should be Committed but is %s", txStatus)
	}

	pendingStakers := vm.internalState.PendingStakerChainState()
	vdr := pendingStakers.GetValidator(nodeID)
	_, exists := vdr.SubnetValidators()[testSubnet1.ID()]

	// Verify that new validator is in pending validator set
	if !exists {
		t.Fatalf("Should have added validator to the pending queue")
	}
}

// Reject proposal to add validator to subnet
func TestAddSubnetValidatorReject(t *testing.T) {
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	startTime := defaultValidateStartTime.Add(syncBound).Add(1 * time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)
	nodeID := keys[0].PublicKey().Address()

	// create valid tx
	// note that [startTime, endTime] is a subset of time that keys[0]
	// validates primary network ([defaultValidateStartTime, defaultValidateEndTime])
	tx, err := vm.newAddSubnetValidatorTx(
		defaultWeight,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		nodeID,
		testSubnet1.ID(),
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[1], testSubnet1ControlKeys[2]},
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		t.Fatal(err)
	}

	// trigger block creation
	if err := vm.blockBuilder.AddUnverifiedTx(tx); err != nil {
		t.Fatal(err)
	}
	blk, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	// Assert preferences are correct
	block := blk.(*ProposalBlock)
	options, err := block.Options()
	if err != nil {
		t.Fatal(err)
	}
	commit, ok := options[0].(*CommitBlock)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*AbortBlock); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if err := block.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if _, txStatus, err := commit.onAccept().GetTx(tx.ID()); err != nil {
		t.Fatal(err)
	} else if txStatus != status.Committed {
		t.Fatalf("status should be Committed but is %s", txStatus)
	} else if err := abort.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Accept(); err != nil { // reject the proposal
		t.Fatal(err)
	} else if _, txStatus, err := vm.internalState.GetTx(tx.ID()); err != nil {
		t.Fatal(err)
	} else if txStatus != status.Aborted {
		t.Fatalf("status should be Aborted but is %s", txStatus)
	}

	pendingStakers := vm.internalState.PendingStakerChainState()
	vdr := pendingStakers.GetValidator(nodeID)
	_, exists := vdr.SubnetValidators()[testSubnet1.ID()]

	// Verify that new validator NOT in pending validator set
	if exists {
		t.Fatalf("Shouldn't have added validator to the pending queue")
	}
}

// Test case where primary network validator rewarded
func TestRewardValidatorAccept(t *testing.T) {
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	// Fast forward clock to time for genesis validators to leave
	vm.clock.Set(defaultValidateEndTime)

	blk, err := vm.BuildBlock() // should contain proposal to advance time
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	// Assert preferences are correct
	block := blk.(*ProposalBlock)
	options, err := block.Options()
	if err != nil {
		t.Fatal(err)
	}
	commit, ok := options[0].(*CommitBlock)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*AbortBlock); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if err := block.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Verify(); err != nil {
		t.Fatal(err)
	} else if _, txStatus, err := abort.onAccept().GetTx(block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if txStatus != status.Aborted {
		t.Fatalf("status should be Aborted but is %s", txStatus)
	} else if err := commit.Accept(); err != nil { // advance the timestamp
		t.Fatal(err)
	} else if _, txStatus, err := vm.internalState.GetTx(block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if txStatus != status.Committed {
		t.Fatalf("status should be Committed but is %s", txStatus)
	}

	// Verify that chain's timestamp has advanced
	if timestamp := vm.internalState.GetTimestamp(); !timestamp.Equal(defaultValidateEndTime) {
		t.Fatal("expected timestamp to have advanced")
	}

	blk, err = vm.BuildBlock() // should contain proposal to reward genesis validator
	if err != nil {
		t.Fatal(err)
	} else if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	// Assert preferences are correct
	block = blk.(*ProposalBlock)
	options, err = block.Options()
	if err != nil {
		t.Fatal(err)
	}
	commit, ok = options[0].(*CommitBlock)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*AbortBlock); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if err := block.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Verify(); err != nil {
		t.Fatal(err)
	} else if _, txStatus, err := abort.onAccept().GetTx(block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if txStatus != status.Aborted {
		t.Fatalf("status should be Aborted but is %s", txStatus)
	} else if err := commit.Accept(); err != nil { // reward the genesis validator
		t.Fatal(err)
	} else if _, txStatus, err := vm.internalState.GetTx(block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if txStatus != status.Committed {
		t.Fatalf("status should be Committed but is %s", txStatus)
	}

	currentStakers := vm.internalState.CurrentStakerChainState()
	if _, err := currentStakers.GetValidator(keys[1].PublicKey().Address()); err == nil {
		t.Fatal("should have removed a genesis validator")
	}
}

// Test case where primary network validator not rewarded
func TestRewardValidatorReject(t *testing.T) {
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	// Fast forward clock to time for genesis validators to leave
	vm.clock.Set(defaultValidateEndTime)

	blk, err := vm.BuildBlock() // should contain proposal to advance time
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	// Assert preferences are correct
	block := blk.(*ProposalBlock)
	if options, err := block.Options(); err != nil {
		t.Fatal(err)
	} else if commit, ok := options[0].(*CommitBlock); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*AbortBlock); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if err := block.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Verify(); err != nil {
		t.Fatal(err)
	} else if _, txStatus, err := abort.onAccept().GetTx(block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if txStatus != status.Aborted {
		t.Fatalf("status should be Aborted but is %s", txStatus)
	} else if err := commit.Accept(); err != nil { // advance the timestamp
		t.Fatal(err)
	} else if _, txStatus, err := vm.internalState.GetTx(block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if txStatus != status.Committed {
		t.Fatalf("status should be Committed but is %s", txStatus)
	} else if timestamp := vm.internalState.GetTimestamp(); !timestamp.Equal(defaultValidateEndTime) {
		t.Fatal("expected timestamp to have advanced")
	}

	if blk, err = vm.BuildBlock(); err != nil { // should contain proposal to reward genesis validator
		t.Fatal(err)
	} else if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	block = blk.(*ProposalBlock)
	if options, err := block.Options(); err != nil { // Assert preferences are correct
		t.Fatal(err)
	} else if commit, ok := options[0].(*CommitBlock); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*AbortBlock); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if err := blk.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if _, txStatus, err := commit.onAccept().GetTx(block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if txStatus != status.Committed {
		t.Fatalf("status should be Committed but is %s", txStatus)
	} else if err := abort.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Accept(); err != nil { // do not reward the genesis validator
		t.Fatal(err)
	} else if _, txStatus, err := vm.internalState.GetTx(block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if txStatus != status.Aborted {
		t.Fatalf("status should be Aborted but is %s", txStatus)
	}

	currentStakers := vm.internalState.CurrentStakerChainState()
	if _, err := currentStakers.GetValidator(keys[1].PublicKey().Address()); err == nil {
		t.Fatal("should have removed a genesis validator")
	}
}

// Test case where primary network validator is preferred to be rewarded
func TestRewardValidatorPreferred(t *testing.T) {
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	// Fast forward clock to time for genesis validators to leave
	vm.clock.Set(defaultValidateEndTime)

	blk, err := vm.BuildBlock() // should contain proposal to advance time
	if err != nil {
		t.Fatal(err)
	} else if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	// Assert preferences are correct
	block := blk.(*ProposalBlock)
	if options, err := block.Options(); err != nil {
		t.Fatal(err)
	} else if commit, ok := options[0].(*CommitBlock); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*AbortBlock); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if err := block.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Verify(); err != nil {
		t.Fatal(err)
	} else if _, txStatus, err := abort.onAccept().GetTx(block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if txStatus != status.Aborted {
		t.Fatalf("status should be Aborted but is %s", txStatus)
	} else if err := commit.Accept(); err != nil { // advance the timestamp
		t.Fatal(err)
	} else if _, txStatus, err := vm.internalState.GetTx(block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if txStatus != status.Committed {
		t.Fatalf("status should be Committed but is %s", txStatus)
	} else if timestamp := vm.internalState.GetTimestamp(); !timestamp.Equal(defaultValidateEndTime) {
		t.Fatal("expected timestamp to have advanced")
	}

	if blk, err = vm.BuildBlock(); err != nil { // should contain proposal to reward genesis validator
		t.Fatal(err)
	} else if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	block = blk.(*ProposalBlock)
	if options, err := blk.(*ProposalBlock).Options(); err != nil { // Assert preferences are correct
		t.Fatal(err)
	} else if commit, ok := options[0].(*CommitBlock); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*AbortBlock); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if err := blk.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if _, txStatus, err := commit.onAccept().GetTx(block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if txStatus != status.Committed {
		t.Fatalf("status should be Committed but is %s", txStatus)
	} else if err := abort.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Accept(); err != nil { // do not reward the genesis validator
		t.Fatal(err)
	} else if _, txStatus, err := vm.internalState.GetTx(block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if txStatus != status.Aborted {
		t.Fatalf("status should be Aborted but is %s", txStatus)
	}

	currentStakers := vm.internalState.CurrentStakerChainState()
	if _, err := currentStakers.GetValidator(keys[1].PublicKey().Address()); err == nil {
		t.Fatal("should have removed a genesis validator")
	}
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
	if _, err := vm.BuildBlock(); err == nil {
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

	tx, err := vm.newCreateChainTx(
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
	} else if err := vm.blockBuilder.AddUnverifiedTx(tx); err != nil {
		t.Fatal(err)
	} else if blk, err := vm.BuildBlock(); err != nil { // should contain proposal to create chain
		t.Fatal(err)
	} else if err := blk.Verify(); err != nil {
		t.Fatal(err)
	} else if err := blk.Accept(); err != nil {
		t.Fatal(err)
	} else if _, txStatus, err := vm.internalState.GetTx(tx.ID()); err != nil {
		t.Fatal(err)
	} else if txStatus != status.Committed {
		t.Fatalf("status should be Committed but is %s", txStatus)
	}

	// Verify chain was created
	chains, err := vm.internalState.GetChains(testSubnet1.ID())
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
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	nodeID := keys[0].PublicKey().Address()

	createSubnetTx, err := vm.newCreateSubnetTx(
		1, // threshold
		[]ids.ShortID{ // control keys
			keys[0].PublicKey().Address(),
			keys[1].PublicKey().Address(),
		},
		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // payer
		keys[0].PublicKey().Address(),           // change addr
	)
	if err != nil {
		t.Fatal(err)
	} else if err := vm.blockBuilder.AddUnverifiedTx(createSubnetTx); err != nil {
		t.Fatal(err)
	} else if blk, err := vm.BuildBlock(); err != nil { // should contain proposal to create subnet
		t.Fatal(err)
	} else if err := blk.Verify(); err != nil {
		t.Fatal(err)
	} else if err := blk.Accept(); err != nil {
		t.Fatal(err)
	} else if _, txStatus, err := vm.internalState.GetTx(createSubnetTx.ID()); err != nil {
		t.Fatal(err)
	} else if txStatus != status.Committed {
		t.Fatalf("status should be Committed but is %s", txStatus)
	}

	subnets, err := vm.internalState.GetSubnets()
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, subnet := range subnets {
		if subnet.ID() == createSubnetTx.ID() {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("should have registered new subnet")
	}

	// Now that we've created a new subnet, add a validator to that subnet
	startTime := defaultValidateStartTime.Add(syncBound).Add(1 * time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)
	// [startTime, endTime] is subset of time keys[0] validates default subent so tx is valid
	if addValidatorTx, err := vm.newAddSubnetValidatorTx(
		defaultWeight,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		nodeID,
		createSubnetTx.ID(),
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty, // change addr
	); err != nil {
		t.Fatal(err)
	} else if err := vm.blockBuilder.AddUnverifiedTx(addValidatorTx); err != nil {
		t.Fatal(err)
	}

	blk, err := vm.BuildBlock() // should add validator to the new subnet
	if err != nil {
		t.Fatal(err)
	} else if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	// Assert preferences are correct
	// and accept the proposal/commit
	block := blk.(*ProposalBlock)
	options, err := block.Options()
	if err != nil {
		t.Fatal(err)
	}
	commit, ok := options[0].(*CommitBlock)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*AbortBlock); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if err := block.Accept(); err != nil { // Accept the block
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Verify(); err != nil {
		t.Fatal(err)
	} else if _, txStatus, err := abort.onAccept().GetTx(block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if txStatus != status.Aborted {
		t.Fatalf("status should be Aborted but is %s", txStatus)
	} else if err := commit.Accept(); err != nil { // add the validator to pending validator set
		t.Fatal(err)
	} else if _, txStatus, err := vm.internalState.GetTx(block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if txStatus != status.Committed {
		t.Fatalf("status should be Committed but is %s", txStatus)
	}

	pendingStakers := vm.internalState.PendingStakerChainState()
	vdr := pendingStakers.GetValidator(nodeID)
	_, exists := vdr.SubnetValidators()[createSubnetTx.ID()]
	if !exists {
		t.Fatal("should have added a pending validator")
	}

	// Advance time to when new validator should start validating
	// Create a block with an advance time tx that moves validator
	// from pending to current validator set
	vm.clock.Set(startTime)
	blk, err = vm.BuildBlock() // should be advance time tx
	if err != nil {
		t.Fatal(err)
	} else if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	// Assert preferences are correct
	// and accept the proposal/commit
	block = blk.(*ProposalBlock)
	options, err = block.Options()
	if err != nil {
		t.Fatal(err)
	}
	commit, ok = options[0].(*CommitBlock)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*AbortBlock); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if err := block.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Verify(); err != nil {
		t.Fatal(err)
	} else if _, txStatus, err := abort.onAccept().GetTx(block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if txStatus != status.Aborted {
		t.Fatalf("status should be Aborted but is %s", txStatus)
	} else if err := commit.Accept(); err != nil { // move validator addValidatorTx from pending to current
		t.Fatal(err)
	} else if _, txStatus, err := vm.internalState.GetTx(block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if txStatus != status.Committed {
		t.Fatalf("status should be Committed but is %s", txStatus)
	}

	pendingStakers = vm.internalState.PendingStakerChainState()
	vdr = pendingStakers.GetValidator(nodeID)
	_, exists = vdr.SubnetValidators()[createSubnetTx.ID()]
	if exists {
		t.Fatal("should have removed the pending validator")
	}

	currentStakers := vm.internalState.CurrentStakerChainState()
	cVDR, err := currentStakers.GetValidator(nodeID)
	if err != nil {
		t.Fatal(err)
	}
	_, exists = cVDR.SubnetValidators()[createSubnetTx.ID()]
	if !exists {
		t.Fatal("should have been added to the validator set")
	}

	// fast forward clock to time validator should stop validating
	vm.clock.Set(endTime)
	blk, err = vm.BuildBlock() // should be advance time tx
	if err != nil {
		t.Fatal(err)
	} else if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	// Assert preferences are correct
	// and accept the proposal/commit
	block = blk.(*ProposalBlock)
	options, err = block.Options()
	if err != nil {
		t.Fatal(err)
	}
	commit, ok = options[0].(*CommitBlock)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*AbortBlock); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if err := block.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Verify(); err != nil {
		t.Fatal(err)
	} else if _, txStatus, err := abort.onAccept().GetTx(block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if txStatus != status.Aborted {
		t.Fatalf("status should be Aborted but is %s", txStatus)
	} else if err := commit.Accept(); err != nil { // remove validator from current validator set
		t.Fatal(err)
	} else if _, txStatus, err := vm.internalState.GetTx(block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if txStatus != status.Committed {
		t.Fatalf("status should be Committed but is %s", txStatus)
	}

	pendingStakers = vm.internalState.PendingStakerChainState()
	vdr = pendingStakers.GetValidator(nodeID)
	_, exists = vdr.SubnetValidators()[createSubnetTx.ID()]
	if exists {
		t.Fatal("should have removed the pending validator")
	}

	currentStakers = vm.internalState.CurrentStakerChainState()
	cVDR, err = currentStakers.GetValidator(nodeID)
	if err != nil {
		t.Fatal(err)
	}
	_, exists = cVDR.SubnetValidators()[createSubnetTx.ID()]
	if exists {
		t.Fatal("should have removed from the validator set")
	}
}

// test asset import
func TestAtomicImport(t *testing.T) {
	vm, baseDB, _ := defaultVM()
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

	m := &atomic.Memory{}
	err := m.Initialize(logging.NoLog{}, prefixdb.New([]byte{5}, baseDB))
	if err != nil {
		t.Fatal(err)
	}
	vm.ctx.SharedMemory = m.NewSharedMemory(vm.ctx.ChainID)
	vm.AtomicUTXOManager = avax.NewAtomicUTXOManager(vm.ctx.SharedMemory, Codec)
	peerSharedMemory := m.NewSharedMemory(vm.ctx.XChainID)

	if _, err := vm.newImportTx(
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
	utxoBytes, err := Codec.Marshal(CodecVersion, utxo)
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

	tx, err := vm.newImportTx(
		vm.ctx.XChainID,
		recipientKey.PublicKey().Address(),
		[]*crypto.PrivateKeySECP256K1R{recipientKey},
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.blockBuilder.AddUnverifiedTx(tx); err != nil {
		t.Fatal(err)
	} else if blk, err := vm.BuildBlock(); err != nil {
		t.Fatal(err)
	} else if err := blk.Verify(); err != nil {
		t.Fatal(err)
	} else if err := blk.Accept(); err != nil {
		t.Fatal(err)
	} else if _, txStatus, err := vm.internalState.GetTx(tx.ID()); err != nil {
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

	tx := Tx{UnsignedTx: &UnsignedImportTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
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
	if err := tx.Sign(Codec, [][]*crypto.PrivateKeySECP256K1R{{}}); err != nil {
		t.Fatal(err)
	}

	preferred, err := vm.Preferred()
	if err != nil {
		t.Fatal(err)
	}
	preferredID := preferred.ID()
	preferredHeight := preferred.Height()

	blk, err := vm.newAtomicBlock(preferredID, preferredHeight+1, tx)
	if err != nil {
		t.Fatal(err)
	}

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

	_, txStatus, err := vm.internalState.GetTx(tx.ID())
	if err != nil {
		t.Fatal(err)
	}

	if txStatus != status.Committed {
		t.Fatalf("Wrong status returned. Expected %s; Got %s", status.Committed, txStatus)
	}
}

// test restarting the node
func TestRestartPartiallyAccepted(t *testing.T) {
	_, genesisBytes := defaultGenesis()
	db := manager.NewMemDB(version.DefaultVersion1_0_0)

	firstDB := db.NewPrefixDBManager([]byte{})
	firstVM := &VM{Factory: Factory{
		Chains:                 chains.MockManager{},
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
	}}
	firstVM.clock.Set(defaultGenesisTime)
	firstCtx := defaultContext()
	firstCtx.Lock.Lock()

	firstMsgChan := make(chan common.Message, 1)
	if err := firstVM.Initialize(firstCtx, firstDB, genesisBytes, nil, nil, firstMsgChan, nil, nil); err != nil {
		t.Fatal(err)
	}

	genesisID, err := firstVM.LastAccepted()
	if err != nil {
		t.Fatal(err)
	}

	firstAdvanceTimeTx, err := firstVM.newAdvanceTimeTx(defaultGenesisTime.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}

	preferred, err := firstVM.Preferred()
	if err != nil {
		t.Fatal(err)
	}
	preferredID := preferred.ID()
	preferredHeight := preferred.Height()

	firstAdvanceTimeBlk, err := firstVM.newProposalBlock(preferredID, preferredHeight+1, *firstAdvanceTimeTx)
	if err != nil {
		t.Fatal(err)
	}

	firstVM.clock.Set(defaultGenesisTime.Add(3 * time.Second))
	if err := firstAdvanceTimeBlk.Verify(); err != nil {
		t.Fatal(err)
	}

	options, err := firstAdvanceTimeBlk.Options()
	if err != nil {
		t.Fatal(err)
	}
	firstOption := options[0]
	secondOption := options[1]

	if err := firstOption.Verify(); err != nil {
		t.Fatal(err)
	} else if err := secondOption.Verify(); err != nil {
		t.Fatal(err)
	} else if err := firstAdvanceTimeBlk.Accept(); err != nil { // time advances to defaultGenesisTime.Add(time.Second)
		t.Fatal(err)
	}

	// Byte representation of block that proposes advancing time to defaultGenesisTime + 2 seconds
	secondAdvanceTimeBlkBytes := []byte{
		0, 0,
		0, 0, 0, 0,
		6, 150, 225, 43, 97, 69, 215, 238,
		150, 164, 249, 184, 2, 197, 216, 49,
		6, 78, 81, 50, 190, 8, 44, 165,
		219, 127, 96, 39, 235, 155, 17, 108,
		0, 0, 0, 0,
		0, 0, 0, 1,
		0, 0, 0, 19,
		0, 0, 0, 0, 95, 34, 234, 149,
		0, 0, 0, 0,
	}
	if _, err := firstVM.ParseBlock(secondAdvanceTimeBlkBytes); err != nil {
		t.Fatal(err)
	}

	if err := firstVM.Shutdown(); err != nil {
		t.Fatal(err)
	}
	firstCtx.Lock.Unlock()

	secondVM := &VM{Factory: Factory{
		Chains:                 chains.MockManager{},
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
	}}

	secondVM.clock.Set(defaultGenesisTime)
	secondCtx := defaultContext()
	secondCtx.Lock.Lock()
	defer func() {
		if err := secondVM.Shutdown(); err != nil {
			t.Fatal(err)
		}
		secondCtx.Lock.Unlock()
	}()

	secondDB := db.NewPrefixDBManager([]byte{})
	secondMsgChan := make(chan common.Message, 1)
	if err := secondVM.Initialize(secondCtx, secondDB, genesisBytes, nil, nil, secondMsgChan, nil, nil); err != nil {
		t.Fatal(err)
	}

	lastAccepted, err := secondVM.LastAccepted()
	if err != nil {
		t.Fatal(err)
	}
	if genesisID != lastAccepted {
		t.Fatalf("Shouldn't have changed the genesis")
	}
}

// test restarting the node
func TestRestartFullyAccepted(t *testing.T) {
	_, genesisBytes := defaultGenesis()

	db := manager.NewMemDB(version.DefaultVersion1_0_0)
	firstDB := db.NewPrefixDBManager([]byte{})
	firstVM := &VM{Factory: Factory{
		Chains:                 chains.MockManager{},
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
	}}

	firstVM.clock.Set(defaultGenesisTime)
	firstCtx := defaultContext()
	firstCtx.Lock.Lock()

	firstMsgChan := make(chan common.Message, 1)
	if err := firstVM.Initialize(firstCtx, firstDB, genesisBytes, nil, nil, firstMsgChan, nil, nil); err != nil {
		t.Fatal(err)
	}

	firstAdvanceTimeTx, err := firstVM.newAdvanceTimeTx(defaultGenesisTime.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}

	preferred, err := firstVM.Preferred()
	if err != nil {
		t.Fatal(err)
	}
	preferredID := preferred.ID()
	preferredHeight := preferred.Height()

	firstAdvanceTimeBlk, err := firstVM.newProposalBlock(preferredID, preferredHeight+1, *firstAdvanceTimeTx)
	if err != nil {
		t.Fatal(err)
	}
	firstVM.clock.Set(defaultGenesisTime.Add(3 * time.Second))
	if err := firstAdvanceTimeBlk.Verify(); err != nil {
		t.Fatal(err)
	}

	options, err := firstAdvanceTimeBlk.Options()
	if err != nil {
		t.Fatal(err)
	} else if err := options[0].Verify(); err != nil {
		t.Fatal(err)
	} else if err := options[1].Verify(); err != nil {
		t.Fatal(err)
	} else if err := firstAdvanceTimeBlk.Accept(); err != nil {
		t.Fatal(err)
	} else if err := options[0].Accept(); err != nil {
		t.Fatal(err)
	} else if err := options[1].Reject(); err != nil {
		t.Fatal(err)
	}

	// Byte representation of block that proposes advancing time to defaultGenesisTime + 2 seconds
	secondAdvanceTimeBlkBytes := []byte{
		0, 0,
		0, 0, 0, 0,
		6, 150, 225, 43, 97, 69, 215, 238,
		150, 164, 249, 184, 2, 197, 216, 49,
		6, 78, 81, 50, 190, 8, 44, 165,
		219, 127, 96, 39, 235, 155, 17, 108,
		0, 0, 0, 0,
		0, 0, 0, 1,
		0, 0, 0, 19,
		0, 0, 0, 0, 95, 34, 234, 149,
		0, 0, 0, 0,
	}
	if _, err := firstVM.ParseBlock(secondAdvanceTimeBlkBytes); err != nil {
		t.Fatal(err)
	}

	if err := firstVM.Shutdown(); err != nil {
		t.Fatal(err)
	}
	firstCtx.Lock.Unlock()

	secondVM := &VM{Factory: Factory{
		Chains:                 chains.MockManager{},
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
	}}

	secondVM.clock.Set(defaultGenesisTime)
	secondCtx := defaultContext()
	secondCtx.Lock.Lock()
	defer func() {
		if err := secondVM.Shutdown(); err != nil {
			t.Fatal(err)
		}
		secondCtx.Lock.Unlock()
	}()

	secondDB := db.NewPrefixDBManager([]byte{})
	secondMsgChan := make(chan common.Message, 1)
	if err := secondVM.Initialize(secondCtx, secondDB, genesisBytes, nil, nil, secondMsgChan, nil, nil); err != nil {
		t.Fatal(err)
	}
	lastAccepted, err := secondVM.LastAccepted()
	if err != nil {
		t.Fatal(err)
	}
	if options[0].ID() != lastAccepted {
		t.Fatalf("Should have changed the genesis")
	}
}

// test bootstrapping the node
func TestBootstrapPartiallyAccepted(t *testing.T) {
	_, genesisBytes := defaultGenesis()

	baseDBManager := manager.NewMemDB(version.DefaultVersion1_0_0)
	vmDBManager := baseDBManager.NewPrefixDBManager([]byte("vm"))
	bootstrappingDB := prefixdb.New([]byte("bootstrapping"), baseDBManager.Current().Database)

	blocked, err := queue.NewWithMissing(bootstrappingDB, "", prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}

	vm := &VM{Factory: Factory{
		Chains:                 chains.MockManager{},
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
	}}

	vm.clock.Set(defaultGenesisTime)
	ctx := defaultContext()
	consensusCtx := snow.DefaultConsensusContextTest()
	consensusCtx.Context = ctx
	consensusCtx.SetState(snow.Bootstrapping)
	ctx.Lock.Lock()

	msgChan := make(chan common.Message, 1)
	if err := vm.Initialize(ctx, vmDBManager, genesisBytes, nil, nil, msgChan, nil, nil); err != nil {
		t.Fatal(err)
	}

	preferred, err := vm.Preferred()
	if err != nil {
		t.Fatal(err)
	}
	preferredID := preferred.ID()
	preferredHeight := preferred.Height()

	advanceTimeTx, err := vm.newAdvanceTimeTx(defaultGenesisTime.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	advanceTimeBlk, err := vm.newProposalBlock(preferredID, preferredHeight+1, *advanceTimeTx)
	if err != nil {
		t.Fatal(err)
	}
	advanceTimeBlkID := advanceTimeBlk.ID()
	advanceTimeBlkBytes := advanceTimeBlk.Bytes()

	options, err := advanceTimeBlk.Options()
	if err != nil {
		t.Fatal(err)
	}
	advanceTimePreference := options[0]

	peerID := ids.ShortID{1, 2, 3, 4, 5, 4, 3, 2, 1}
	vdrs := validators.NewSet()
	if err := vdrs.AddWeight(peerID, 1); err != nil {
		t.Fatal(err)
	}
	beacons := vdrs

	timeoutManager := timeout.Manager{}
	benchlist := benchlist.NewNoBenchlist()
	err = timeoutManager.Initialize(
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
	if err != nil {
		t.Fatal(err)
	}
	go timeoutManager.Dispatch()

	chainRouter := &router.ChainRouter{}
	metrics := prometheus.NewRegistry()
	mc, err := message.NewCreator(metrics, true /*compressionEnabled*/, "dummyNamespace")
	assert.NoError(t, err)
	err = chainRouter.Initialize(ids.ShortEmpty, logging.NoLog{}, mc, &timeoutManager, time.Second, ids.Set{}, nil, router.HealthConfig{}, "", prometheus.NewRegistry())
	assert.NoError(t, err)

	externalSender := &sender.ExternalSenderTest{TB: t}
	externalSender.Default(true)

	// Passes messages from the consensus engine to the network
	sender := sender.Sender{}
	err = sender.Initialize(consensusCtx, mc, externalSender, chainRouter, &timeoutManager, 1, 1, 1)
	assert.NoError(t, err)

	var reqID uint32
	externalSender.SendF = func(msg message.OutboundMessage, nodeIDs ids.ShortSet, subnetID ids.ID, validatorOnly bool) ids.ShortSet {
		inMsg, err := mc.Parse(msg.Bytes(), ctx.NodeID, func() {})
		assert.NoError(t, err)
		assert.Equal(t, message.GetAcceptedFrontier, inMsg.Op())

		res := nodeIDs
		requestID, ok := inMsg.Get(message.RequestID).(uint32)
		assert.True(t, ok)

		reqID = requestID
		return res
	}

	isBootstrapped := false
	subnet := &common.SubnetTest{
		T:               t,
		IsBootstrappedF: func() bool { return isBootstrapped },
		BootstrappedF:   func(ids.ID) { isBootstrapped = true },
	}

	// The engine handles consensus
	consensus := &smcon.Topological{}
	commonCfg := common.Config{
		Ctx:                            consensusCtx,
		Validators:                     vdrs,
		Beacons:                        beacons,
		SampleK:                        beacons.Len(),
		StartupAlpha:                   (beacons.Weight() + 1) / 2,
		Alpha:                          (beacons.Weight() + 1) / 2,
		Sender:                         &sender,
		Subnet:                         subnet,
		AncestorsMaxContainersSent:     2000,
		AncestorsMaxContainersReceived: 2000,
		SharedCfg:                      &common.SharedConfig{},
	}

	snowGetHandler, err := snowgetter.New(vm, commonCfg)
	assert.NoError(t, err)

	bootstrapConfig := bootstrap.Config{
		Config:        commonCfg,
		AllGetsServer: snowGetHandler,
		Blocked:       blocked,
		VM:            vm,
		WeightTracker: tracker.NewWeightTracker(commonCfg.Beacons, commonCfg.StartupAlpha),
	}

	// Asynchronously passes messages from the network to the consensus engine
	handler, err := handler.New(
		mc,
		bootstrapConfig.Ctx,
		vdrs,
		msgChan,
		nil,
		time.Hour,
	)
	assert.NoError(t, err)

	bootstrapper, err := bootstrap.New(
		bootstrapConfig,
		func(lastReqID uint32) error {
			return handler.Consensus().Start(lastReqID + 1)
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	handler.SetBootstrapper(bootstrapper)

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
	if err != nil {
		t.Fatal(err)
	}
	handler.SetConsensus(engine)

	// Allow incoming messages to be routed to the new chain
	chainRouter.AddChain(handler)

	if err := bootstrapper.Start(0); err != nil {
		t.Fatal(err)
	}

	handler.Start(false)

	if err := bootstrapper.Connected(peerID, version.CurrentApp); err != nil {
		t.Fatal(err)
	}

	externalSender.SendF = func(msg message.OutboundMessage, nodeIDs ids.ShortSet, subnetID ids.ID, validatorOnly bool) ids.ShortSet {
		inMsg, err := mc.Parse(msg.Bytes(), ctx.NodeID, func() {})
		assert.NoError(t, err)
		assert.Equal(t, message.GetAccepted, inMsg.Op())

		res := nodeIDs
		requestID, ok := inMsg.Get(message.RequestID).(uint32)
		assert.True(t, ok)

		reqID = requestID
		return res
	}

	frontier := []ids.ID{advanceTimeBlkID}
	if err := bootstrapper.AcceptedFrontier(peerID, reqID, frontier); err != nil {
		t.Fatal(err)
	}

	externalSender.SendF = func(msg message.OutboundMessage, nodeIDs ids.ShortSet, subnetID ids.ID, validatorOnly bool) ids.ShortSet {
		inMsg, err := mc.Parse(msg.Bytes(), ctx.NodeID, func() {})
		assert.NoError(t, err)
		assert.Equal(t, message.GetAncestors, inMsg.Op())

		res := nodeIDs
		requestID, ok := inMsg.Get(message.RequestID).(uint32)
		assert.True(t, ok)
		reqID = requestID

		containerID, err := ids.ToID(inMsg.Get(message.ContainerID).([]byte))
		assert.NoError(t, err)
		if containerID != advanceTimeBlkID {
			t.Fatalf("wrong block requested")
		}

		return res
	}

	if err := bootstrapper.Accepted(peerID, reqID, frontier); err != nil {
		t.Fatal(err)
	}

	externalSender.SendF = nil
	externalSender.CantSend = false

	if err := bootstrapper.Ancestors(peerID, reqID, [][]byte{advanceTimeBlkBytes}); err != nil {
		t.Fatal(err)
	}

	preferred, err = vm.Preferred()
	if err != nil {
		t.Fatal(err)
	}

	if preferred.ID() != advanceTimePreference.ID() {
		t.Fatalf("wrong preference reported after bootstrapping to proposal block\nPreferred: %s\nExpected: %s\nGenesis: %s",
			preferred.ID(),
			advanceTimePreference.ID(),
			preferredID)
	}
	ctx.Lock.Unlock()

	chainRouter.Shutdown()
}

func TestUnverifiedParent(t *testing.T) {
	_, genesisBytes := defaultGenesis()

	dbManager := manager.NewMemDB(version.DefaultVersion1_0_0)

	vm := &VM{Factory: Factory{
		Chains:                 chains.MockManager{},
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
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
	if err := vm.Initialize(ctx, dbManager, genesisBytes, nil, nil, msgChan, nil, nil); err != nil {
		t.Fatal(err)
	}

	firstAdvanceTimeTx, err := vm.newAdvanceTimeTx(defaultGenesisTime.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}

	preferred, err := vm.Preferred()
	if err != nil {
		t.Fatal(err)
	}
	preferredID := preferred.ID()
	preferredHeight := preferred.Height()

	firstAdvanceTimeBlk, err := vm.newProposalBlock(preferredID, preferredHeight+1, *firstAdvanceTimeTx)
	if err != nil {
		t.Fatal(err)
	}

	vm.clock.Set(defaultGenesisTime.Add(2 * time.Second))
	if err := firstAdvanceTimeBlk.Verify(); err != nil {
		t.Fatal(err)
	}

	options, err := firstAdvanceTimeBlk.Options()
	if err != nil {
		t.Fatal(err)
	}
	firstOption := options[0]
	secondOption := options[1]

	secondAdvanceTimeTx, err := vm.newAdvanceTimeTx(defaultGenesisTime.Add(2 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	secondAdvanceTimeBlk, err := vm.newProposalBlock(firstOption.ID(), firstOption.(Block).Height()+1, *secondAdvanceTimeTx)
	if err != nil {
		t.Fatal(err)
	}

	if parentBlkID := secondAdvanceTimeBlk.Parent(); parentBlkID != firstOption.ID() {
		t.Fatalf("Wrong parent block ID returned")
	} else if err := firstOption.Verify(); err != nil {
		t.Fatal(err)
	} else if err := secondOption.Verify(); err != nil {
		t.Fatal(err)
	} else if err := secondAdvanceTimeBlk.Verify(); err != nil {
		t.Fatal(err)
	}
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

	tests := []struct {
		description    string
		startTime      time.Time
		endTime        time.Time
		validatorID    ids.ShortID
		expectedAmount uint64
	}{
		{
			description:    "startTime after validation period ends",
			startTime:      defaultValidateEndTime.Add(time.Minute),
			endTime:        defaultValidateEndTime.Add(2 * time.Minute),
			validatorID:    keys[0].PublicKey().Address(),
			expectedAmount: 0,
		},
		{
			description:    "startTime when validation period ends",
			startTime:      defaultValidateEndTime,
			endTime:        defaultValidateEndTime.Add(2 * time.Minute),
			validatorID:    keys[0].PublicKey().Address(),
			expectedAmount: defaultWeight,
		},
		{
			description:    "startTime before validation period ends",
			startTime:      defaultValidateEndTime.Add(-time.Minute),
			endTime:        defaultValidateEndTime.Add(2 * time.Minute),
			validatorID:    keys[0].PublicKey().Address(),
			expectedAmount: defaultWeight,
		},
		{
			description:    "endTime after validation period ends",
			startTime:      defaultValidateStartTime,
			endTime:        defaultValidateEndTime.Add(time.Minute),
			validatorID:    keys[0].PublicKey().Address(),
			expectedAmount: defaultWeight,
		},
		{
			description:    "endTime when validation period ends",
			startTime:      defaultValidateStartTime,
			endTime:        defaultValidateEndTime,
			validatorID:    keys[0].PublicKey().Address(),
			expectedAmount: defaultWeight,
		},
		{
			description:    "endTime before validation period ends",
			startTime:      defaultValidateStartTime,
			endTime:        defaultValidateEndTime.Add(-time.Minute),
			validatorID:    keys[0].PublicKey().Address(),
			expectedAmount: defaultWeight,
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			amount, err := vm.maxStakeAmount(vm.ctx.SubnetID, test.validatorID, test.startTime, test.endTime)
			if err != nil {
				t.Fatal(err)
			}
			if amount != test.expectedAmount {
				t.Fatalf("wrong max stake amount. Expected %d ; Returned %d",
					test.expectedAmount, amount)
			}
		})
	}
}

// Test that calling Verify on a block with an unverified parent doesn't cause a panic.
func TestUnverifiedParentPanic(t *testing.T) {
	_, genesisBytes := defaultGenesis()

	baseDBManager := manager.NewMemDB(version.DefaultVersion1_0_0)
	atomicDB := prefixdb.New([]byte{1}, baseDBManager.Current().Database)

	vm := &VM{Factory: Factory{
		Chains:                 chains.MockManager{},
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
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
	m := &atomic.Memory{}
	err := m.Initialize(logging.NoLog{}, atomicDB)
	if err != nil {
		panic(err)
	}
	vm.ctx.SharedMemory = m.NewSharedMemory(ctx.ChainID)

	key0 := keys[0]
	key1 := keys[1]
	addr0 := key0.PublicKey().Address()
	addr1 := key1.PublicKey().Address()

	addSubnetTx0, err := vm.newCreateSubnetTx(1, []ids.ShortID{addr0}, []*crypto.PrivateKeySECP256K1R{key0}, addr0)
	if err != nil {
		t.Fatal(err)
	}
	addSubnetTx1, err := vm.newCreateSubnetTx(1, []ids.ShortID{addr1}, []*crypto.PrivateKeySECP256K1R{key1}, addr1)
	if err != nil {
		t.Fatal(err)
	}
	addSubnetTx2, err := vm.newCreateSubnetTx(1, []ids.ShortID{addr1}, []*crypto.PrivateKeySECP256K1R{key1}, addr0)
	if err != nil {
		t.Fatal(err)
	}

	preferred, err := vm.Preferred()
	if err != nil {
		t.Fatal(err)
	}
	preferredID := preferred.ID()
	preferredHeight := preferred.Height()

	addSubnetBlk0, err := vm.newStandardBlock(preferredID, preferredHeight+1, []*Tx{addSubnetTx0})
	if err != nil {
		t.Fatal(err)
	}
	addSubnetBlk1, err := vm.newStandardBlock(preferredID, preferredHeight+1, []*Tx{addSubnetTx1})
	if err != nil {
		t.Fatal(err)
	}
	addSubnetBlk2, err := vm.newStandardBlock(addSubnetBlk1.ID(), preferredHeight+2, []*Tx{addSubnetTx2})
	if err != nil {
		t.Fatal(err)
	}

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
	assert := assert.New(t)

	vm, baseDB, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)

		vm.ctx.Lock.Unlock()
	}()

	newValidatorStartTime := defaultGenesisTime.Add(syncBound).Add(1 * time.Second)
	newValidatorEndTime := newValidatorStartTime.Add(defaultMinStakingDuration)

	key, err := vm.factory.NewPrivateKey()
	assert.NoError(err)

	nodeID := key.PublicKey().Address()

	// Create the tx to add a new validator
	addValidatorTx, err := vm.newAddValidatorTx(
		vm.MinValidatorStake,
		uint64(newValidatorStartTime.Unix()),
		uint64(newValidatorEndTime.Unix()),
		nodeID,
		nodeID,
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty,
	)
	assert.NoError(err)

	// Create the proposal block to add the new validator
	preferred, err := vm.Preferred()
	assert.NoError(err)

	preferredID := preferred.ID()
	preferredHeight := preferred.Height()

	addValidatorProposalBlk, err := vm.newProposalBlock(preferredID, preferredHeight+1, *addValidatorTx)
	assert.NoError(err)

	err = addValidatorProposalBlk.Verify()
	assert.NoError(err)

	// Get the commit block to add the new validator
	addValidatorProposalOptions, err := addValidatorProposalBlk.Options()
	assert.NoError(err)

	addValidatorProposalCommitIntf := addValidatorProposalOptions[0]
	addValidatorProposalCommit, ok := addValidatorProposalCommitIntf.(*CommitBlock)
	assert.True(ok)

	err = addValidatorProposalCommit.Verify()
	assert.NoError(err)

	// Verify that the new validator now in pending validator set
	{
		onAccept := addValidatorProposalCommit.onAccept()
		pendingStakers := onAccept.PendingStakerChainState()

		_, err := pendingStakers.GetValidatorTx(nodeID)
		assert.NoError(err)
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
	unsignedImportTx := &UnsignedImportTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
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
	signedImportTx := &Tx{UnsignedTx: unsignedImportTx}
	err = signedImportTx.Sign(Codec, [][]*crypto.PrivateKeySECP256K1R{
		{}, // There is one input, with no required signers
	})
	assert.NoError(err)

	// Create the standard block that will fail verification, and then be
	// re-verified.
	preferredID = addValidatorProposalCommit.ID()
	preferredHeight = addValidatorProposalCommit.Height()

	importBlk, err := vm.newStandardBlock(preferredID, preferredHeight+1, []*Tx{signedImportTx})
	assert.NoError(err)

	// Because the shared memory UTXO hasn't been populated, this block is
	// currently invalid.
	err = importBlk.Verify()
	assert.Error(err)

	// Because we no longer ever reject a block in verification, the status
	// should remain as processing.
	importBlkStatus := importBlk.Status()
	assert.Equal(choices.Processing, importBlkStatus)

	// Populate the shared memory UTXO.
	m := &atomic.Memory{}
	err = m.Initialize(logging.NoLog{}, prefixdb.New([]byte{5}, baseDB))
	assert.NoError(err)

	vm.ctx.SharedMemory = m.NewSharedMemory(vm.ctx.ChainID)
	vm.AtomicUTXOManager = avax.NewAtomicUTXOManager(vm.ctx.SharedMemory, Codec)
	peerSharedMemory := m.NewSharedMemory(vm.ctx.XChainID)

	utxoBytes, err := Codec.Marshal(CodecVersion, utxo)
	assert.NoError(err)

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
	assert.NoError(err)

	// Because the shared memory UTXO has now been populated, the block should
	// pass verification.
	err = importBlk.Verify()
	assert.NoError(err)

	// The status shouldn't have been changed during a successful verification.
	importBlkStatus = importBlk.Status()
	assert.Equal(choices.Processing, importBlkStatus)

	// Create the tx that would have moved the new validator from the pending
	// validator set into the current validator set.
	vm.clock.Set(newValidatorStartTime)
	advanceTimeTx, err := vm.newAdvanceTimeTx(newValidatorStartTime)
	assert.NoError(err)

	// Create the proposal block that should have moved the new validator from
	// the pending validator set into the current validator set.
	preferredID = importBlk.ID()
	preferredHeight = importBlk.Height()

	advanceTimeProposalBlk, err := vm.newProposalBlock(preferredID, preferredHeight+1, *advanceTimeTx)
	assert.NoError(err)

	err = advanceTimeProposalBlk.Verify()
	assert.NoError(err)

	// Get the commit block that advances the timestamp to the point that the
	// validator should be moved from the pending validator set into the current
	// validator set.
	advanceTimeProposalOptions, err := advanceTimeProposalBlk.Options()
	assert.NoError(err)

	advanceTimeProposalCommitIntf := advanceTimeProposalOptions[0]
	advanceTimeProposalCommit, ok := advanceTimeProposalCommitIntf.(*CommitBlock)
	assert.True(ok)

	err = advanceTimeProposalCommit.Verify()
	assert.NoError(err)

	// Accept all the blocks
	allBlocks := []smcon.Block{
		addValidatorProposalBlk,
		addValidatorProposalCommit,
		importBlk,
		advanceTimeProposalBlk,
		advanceTimeProposalCommit,
	}
	for _, blk := range allBlocks {
		err = blk.Accept()
		assert.NoError(err)

		status := blk.Status()
		assert.Equal(choices.Accepted, status)
	}

	// Force a reload of the state from the database.
	is, err := NewMeteredInternalState(
		vm,
		vm.dbManager.Current().Database,
		nil,
		prometheus.NewRegistry(),
	)
	assert.NoError(err)
	vm.internalState = is

	// Verify that new validator is now in the current validator set.
	{
		currentStakers := vm.internalState.CurrentStakerChainState()
		_, err = currentStakers.GetValidator(nodeID)
		assert.NoError(err)

		pendingStakers := vm.internalState.PendingStakerChainState()
		_, err := pendingStakers.GetValidatorTx(nodeID)
		assert.ErrorIs(err, database.ErrNotFound)

		currentTimestamp := vm.internalState.GetTimestamp()
		assert.Equal(newValidatorStartTime.Unix(), currentTimestamp.Unix())
	}
}

func TestRejectedStateRegressionInvalidValidatorReward(t *testing.T) {
	assert := assert.New(t)

	vm, baseDB, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)

		vm.ctx.Lock.Unlock()
	}()

	vm.internalState.SetCurrentSupply(defaultRewardConfig.SupplyCap / 2)

	newValidatorStartTime0 := defaultGenesisTime.Add(syncBound).Add(1 * time.Second)
	newValidatorEndTime0 := newValidatorStartTime0.Add(defaultMaxStakingDuration)

	nodeID0 := ids.GenerateTestShortID()

	// Create the tx to add the first new validator
	addValidatorTx0, err := vm.newAddValidatorTx(
		vm.MaxValidatorStake,
		uint64(newValidatorStartTime0.Unix()),
		uint64(newValidatorEndTime0.Unix()),
		nodeID0,
		nodeID0,
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty,
	)
	assert.NoError(err)

	// Create the proposal block to add the first new validator
	preferred, err := vm.Preferred()
	assert.NoError(err)

	preferredID := preferred.ID()
	preferredHeight := preferred.Height()

	addValidatorProposalBlk0, err := vm.newProposalBlock(preferredID, preferredHeight+1, *addValidatorTx0)
	assert.NoError(err)

	err = addValidatorProposalBlk0.Verify()
	assert.NoError(err)

	// Get the commit block to add the first new validator
	addValidatorProposalOptions0, err := addValidatorProposalBlk0.Options()
	assert.NoError(err)

	addValidatorProposalCommitIntf0 := addValidatorProposalOptions0[0]
	addValidatorProposalCommit0, ok := addValidatorProposalCommitIntf0.(*CommitBlock)
	assert.True(ok)

	err = addValidatorProposalCommit0.Verify()
	assert.NoError(err)

	// Verify that first new validator now in pending validator set
	{
		onAccept := addValidatorProposalCommit0.onAccept()
		pendingStakers := onAccept.PendingStakerChainState()

		_, err := pendingStakers.GetValidatorTx(nodeID0)
		assert.NoError(err)
	}

	// Create the tx that moves the first new validator from the pending
	// validator set into the current validator set.
	vm.clock.Set(newValidatorStartTime0)
	advanceTimeTx0, err := vm.newAdvanceTimeTx(newValidatorStartTime0)
	assert.NoError(err)

	// Create the proposal block that moves the first new validator from the
	// pending validator set into the current validator set.
	preferredID = addValidatorProposalCommit0.ID()
	preferredHeight = addValidatorProposalCommit0.Height()

	advanceTimeProposalBlk0, err := vm.newProposalBlock(preferredID, preferredHeight+1, *advanceTimeTx0)
	assert.NoError(err)

	err = advanceTimeProposalBlk0.Verify()
	assert.NoError(err)

	// Get the commit block that advances the timestamp to the point that the
	// first new validator should be moved from the pending validator set into
	// the current validator set.
	advanceTimeProposalOptions0, err := advanceTimeProposalBlk0.Options()
	assert.NoError(err)

	advanceTimeProposalCommitIntf0 := advanceTimeProposalOptions0[0]
	advanceTimeProposalCommit0, ok := advanceTimeProposalCommitIntf0.(*CommitBlock)
	assert.True(ok)

	err = advanceTimeProposalCommit0.Verify()
	assert.NoError(err)

	// Verify that the first new validator is now in the current validator set.
	{
		onAccept := advanceTimeProposalCommit0.onAccept()
		currentStakers := onAccept.CurrentStakerChainState()
		_, err = currentStakers.GetValidator(nodeID0)
		assert.NoError(err)

		pendingStakers := onAccept.PendingStakerChainState()
		_, err := pendingStakers.GetValidatorTx(nodeID0)
		assert.ErrorIs(err, database.ErrNotFound)

		currentTimestamp := onAccept.GetTimestamp()
		assert.Equal(newValidatorStartTime0.Unix(), currentTimestamp.Unix())
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
	unsignedImportTx := &UnsignedImportTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
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
	signedImportTx := &Tx{UnsignedTx: unsignedImportTx}
	err = signedImportTx.Sign(Codec, [][]*crypto.PrivateKeySECP256K1R{
		{}, // There is one input, with no required signers
	})
	assert.NoError(err)

	// Create the standard block that will fail verification, and then be
	// re-verified.
	preferredID = advanceTimeProposalCommit0.ID()
	preferredHeight = advanceTimeProposalCommit0.Height()

	importBlk, err := vm.newStandardBlock(preferredID, preferredHeight+1, []*Tx{signedImportTx})
	assert.NoError(err)

	// Because the shared memory UTXO hasn't been populated, this block is
	// currently invalid.
	err = importBlk.Verify()
	assert.Error(err)

	// Because we no longer ever reject a block in verification, the status
	// should remain as processing.
	importBlkStatus := importBlk.Status()
	assert.Equal(choices.Processing, importBlkStatus)

	// Populate the shared memory UTXO.
	m := &atomic.Memory{}
	err = m.Initialize(logging.NoLog{}, prefixdb.New([]byte{5}, baseDB))
	assert.NoError(err)

	vm.ctx.SharedMemory = m.NewSharedMemory(vm.ctx.ChainID)
	vm.AtomicUTXOManager = avax.NewAtomicUTXOManager(vm.ctx.SharedMemory, Codec)
	peerSharedMemory := m.NewSharedMemory(vm.ctx.XChainID)

	utxoBytes, err := Codec.Marshal(CodecVersion, utxo)
	assert.NoError(err)

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
	assert.NoError(err)

	// Because the shared memory UTXO has now been populated, the block should
	// pass verification.
	err = importBlk.Verify()
	assert.NoError(err)

	// The status shouldn't have been changed during a successful verification.
	importBlkStatus = importBlk.Status()
	assert.Equal(choices.Processing, importBlkStatus)

	newValidatorStartTime1 := newValidatorStartTime0.Add(syncBound).Add(1 * time.Second)
	newValidatorEndTime1 := newValidatorStartTime1.Add(defaultMaxStakingDuration)

	nodeID1 := ids.GenerateTestShortID()

	// Create the tx to add the second new validator
	addValidatorTx1, err := vm.newAddValidatorTx(
		vm.MaxValidatorStake,
		uint64(newValidatorStartTime1.Unix()),
		uint64(newValidatorEndTime1.Unix()),
		nodeID1,
		nodeID1,
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{keys[1]},
		ids.ShortEmpty,
	)
	assert.NoError(err)

	// Create the proposal block to add the second new validator
	preferredID = importBlk.ID()
	preferredHeight = importBlk.Height()

	addValidatorProposalBlk1, err := vm.newProposalBlock(preferredID, preferredHeight+1, *addValidatorTx1)
	assert.NoError(err)

	err = addValidatorProposalBlk1.Verify()
	assert.NoError(err)

	// Get the commit block to add the second new validator
	addValidatorProposalOptions1, err := addValidatorProposalBlk1.Options()
	assert.NoError(err)

	addValidatorProposalCommitIntf1 := addValidatorProposalOptions1[0]
	addValidatorProposalCommit1, ok := addValidatorProposalCommitIntf1.(*CommitBlock)
	assert.True(ok)

	err = addValidatorProposalCommit1.Verify()
	assert.NoError(err)

	// Verify that the second new validator now in pending validator set
	{
		onAccept := addValidatorProposalCommit1.onAccept()
		pendingStakers := onAccept.PendingStakerChainState()

		_, err := pendingStakers.GetValidatorTx(nodeID1)
		assert.NoError(err)
	}

	// Create the tx that moves the second new validator from the pending
	// validator set into the current validator set.
	vm.clock.Set(newValidatorStartTime1)
	advanceTimeTx1, err := vm.newAdvanceTimeTx(newValidatorStartTime1)
	assert.NoError(err)

	// Create the proposal block that moves the second new validator from the
	// pending validator set into the current validator set.
	preferredID = addValidatorProposalCommit1.ID()
	preferredHeight = addValidatorProposalCommit1.Height()

	advanceTimeProposalBlk1, err := vm.newProposalBlock(preferredID, preferredHeight+1, *advanceTimeTx1)
	assert.NoError(err)

	err = advanceTimeProposalBlk1.Verify()
	assert.NoError(err)

	// Get the commit block that advances the timestamp to the point that the
	// second new validator should be moved from the pending validator set into
	// the current validator set.
	advanceTimeProposalOptions1, err := advanceTimeProposalBlk1.Options()
	assert.NoError(err)

	advanceTimeProposalCommitIntf1 := advanceTimeProposalOptions1[0]
	advanceTimeProposalCommit1, ok := advanceTimeProposalCommitIntf1.(*CommitBlock)
	assert.True(ok)

	err = advanceTimeProposalCommit1.Verify()
	assert.NoError(err)

	// Verify that the second new validator is now in the current validator set.
	{
		onAccept := advanceTimeProposalCommit1.onAccept()
		currentStakers := onAccept.CurrentStakerChainState()
		_, err := currentStakers.GetValidator(nodeID1)
		assert.NoError(err)

		pendingStakers := onAccept.PendingStakerChainState()
		_, err = pendingStakers.GetValidatorTx(nodeID1)
		assert.ErrorIs(err, database.ErrNotFound)

		currentTimestamp := onAccept.GetTimestamp()
		assert.Equal(newValidatorStartTime1.Unix(), currentTimestamp.Unix())
	}

	// Accept all the blocks
	allBlocks := []smcon.Block{
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
		assert.NoError(err)

		status := blk.Status()
		assert.Equal(choices.Accepted, status)
	}

	// Force a reload of the state from the database.
	is, err := NewMeteredInternalState(
		vm,
		vm.dbManager.Current().Database,
		nil,
		prometheus.NewRegistry(),
	)
	assert.NoError(err)
	vm.internalState = is

	// Verify that validators are in the current validator set with the correct
	// reward calculated.
	{
		currentStakers := vm.internalState.CurrentStakerChainState()
		node0, err := currentStakers.GetValidator(nodeID0)
		assert.NoError(err)
		potentialReward := node0.PotentialReward()
		assert.Equal(uint64(60000000), potentialReward)

		node1, err := currentStakers.GetValidator(nodeID1)
		assert.NoError(err)
		potentialReward = node1.PotentialReward()
		assert.EqualValues(uint64(59999999), potentialReward)

		pendingStakers := vm.internalState.PendingStakerChainState()
		_, err = pendingStakers.GetValidatorTx(nodeID1)
		assert.ErrorIs(err, database.ErrNotFound)
		_, err = pendingStakers.GetValidatorTx(nodeID1)
		assert.ErrorIs(err, database.ErrNotFound)

		currentTimestamp := vm.internalState.GetTimestamp()
		assert.Equal(newValidatorStartTime1.Unix(), currentTimestamp.Unix())
	}
}
