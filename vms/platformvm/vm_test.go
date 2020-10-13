// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/queue"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/bootstrap"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/sender"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/core"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/vms/timestampvm"

	smcon "github.com/ava-labs/avalanchego/snow/consensus/snowman"
	smeng "github.com/ava-labs/avalanchego/snow/engine/snowman"
)

var (
	defaultMinStakingDuration        = 24 * time.Hour
	defaultMaxStakingDuration        = 365 * 24 * time.Hour
	defaultMinDelegationFee   uint32 = 0

	// AVAX asset ID in tests
	avaxAssetID = ids.NewID([32]byte{'y', 'e', 'e', 't'})

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
	defaultMaxValidatorStake = SupplyCap
	defaultMinDelegatorStake = 1 * units.MilliAvax

	// amount all genesis validators have in defaultVM
	defaultBalance uint64 = 100 * defaultMinValidatorStake

	// subnet that exists at genesis in defaultVM
	// Its controlKeys are keys[0], keys[1], keys[2]
	testSubnet1            *UnsignedCreateSubnetTx
	testSubnet1ControlKeys []*crypto.PrivateKeySECP256K1R

	avmID = ids.Empty.Prefix(0)
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
	byteFormatter := formatting.CB58{}
	factory := crypto.FactorySECP256K1R{}
	for _, key := range []string{
		"24jUJ9vZexUM6expyMcT48LBx27k1m7xpraoV62oSQAHdziao5",
		"2MMvUMsxx6zsHSNXJdFD8yc5XkancvwyKPwpw4xUK3TCGDuNBY",
		"cxb7KpGWhDMALTjNNSJ7UQkkomPesyWAPUaWRGdyeBNzR6f35",
		"ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN",
		"2RWLv6YVEXDiWLpaCbXhhqxtLbnFaKQsWPSSMSPhpWo47uJAeV",
	} {
		ctx.Log.AssertNoError(byteFormatter.FromString(key))
		pk, err := factory.ToPrivateKey(byteFormatter.Bytes)
		ctx.Log.AssertNoError(err)
		keys = append(keys, pk.(*crypto.PrivateKeySECP256K1R))
	}
	testSubnet1ControlKeys = keys[0:3]
}

func defaultContext() *snow.Context {
	ctx := snow.DefaultContextTest()
	ctx.NetworkID = testNetworkID
	ctx.XChainID = avmID
	ctx.AVAXAssetID = avaxAssetID
	aliaser := &ids.Aliaser{}
	aliaser.Initialize()

	errs := wrappers.Errs{}
	errs.Add(
		aliaser.Alias(constants.PlatformChainID, "P"),
		aliaser.Alias(constants.PlatformChainID, constants.PlatformChainID.String()),
		aliaser.Alias(avmID, "X"),
		aliaser.Alias(avmID, avmID.String()),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
	ctx.BCLookup = aliaser
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
			DelegationFee: PercentDenominator,
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
	}

	buildGenesisResponse := BuildGenesisReply{}
	platformvmSS := StaticService{}
	if err := platformvmSS.BuildGenesis(nil, &buildGenesisArgs, &buildGenesisResponse); err != nil {
		panic(fmt.Errorf("problem while building platform chain's genesis state: %w", err))
	}
	return &buildGenesisArgs, buildGenesisResponse.Bytes.Bytes
}

func defaultVM() (*VM, database.Database) {
	vm := &VM{
		SnowmanVM:          &core.SnowmanVM{},
		chainManager:       chains.MockManager{},
		txFee:              defaultTxFee,
		minValidatorStake:  defaultMinValidatorStake,
		maxValidatorStake:  defaultMaxValidatorStake,
		minDelegatorStake:  defaultMinDelegatorStake,
		minStakeDuration:   defaultMinStakingDuration,
		maxStakeDuration:   defaultMaxStakingDuration,
		stakeMintingPeriod: defaultMaxStakingDuration,
	}

	baseDB := memdb.New()
	chainDB := prefixdb.New([]byte{0}, baseDB)
	atomicDB := prefixdb.New([]byte{1}, baseDB)

	vm.vdrMgr = validators.NewManager()

	vm.clock.Set(defaultGenesisTime)
	msgChan := make(chan common.Message, 1)
	ctx := defaultContext()

	m := &atomic.Memory{}
	m.Initialize(logging.NoLog{}, atomicDB)

	ctx.SharedMemory = m.NewSharedMemory(ctx.ChainID)

	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()
	_, genesisBytes := defaultGenesis()
	if err := vm.Initialize(ctx, chainDB, genesisBytes, msgChan, nil); err != nil {
		panic(err)
	}
	if err := vm.Bootstrapped(); err != nil {
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
	} else if err := vm.mempool.IssueTx(tx); err != nil {
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

	return vm, baseDB
}

// Ensure genesis state is parsed from bytes and stored correctly
func TestGenesis(t *testing.T) {
	vm, _ := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.Ctx.Lock.Unlock()
	}()

	// Ensure the genesis block has been accepted and stored
	genesisBlockID := vm.LastAccepted() // lastAccepted should be ID of genesis block
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
		utxos, _, _, err := vm.GetUTXOs(vm.DB, addrs, ids.ShortEmpty, ids.Empty, -1)
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
				if out.Amount() != uint64(utxo.Amount)-vm.txFee {
					t.Fatalf("expected UTXO to have value %d but has value %d", uint64(utxo.Amount)-vm.txFee, out.Amount())
				}
			} else {
				t.Fatalf("expected UTXO to have value %d but has value %d", uint64(utxo.Amount), out.Amount())
			}
		}
	}

	// Ensure current validator set of primary network is correct
	vdrSet, ok := vm.vdrMgr.GetValidators(constants.PrimaryNetworkID)
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
	if timestamp, err := vm.getTimestamp(vm.DB); err != nil {
		t.Fatal(err)
	} else if timestamp.Unix() != int64(genesisState.Time) {
		t.Fatalf("vm's time is incorrect. Expected %v got %v", genesisState.Time, timestamp)
	}

	// Ensure the new subnet we created exists
	if _, err := vm.getSubnet(vm.DB, testSubnet1.ID()); err != nil {
		t.Fatalf("expected subnet %s to exist", testSubnet1.ID())
	}
}

// Test method getTotalStake
func TestGetTotalStake(t *testing.T) {
	vm, _ := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.Ctx.Lock.Unlock()
	}()

	// Make sure stake is right after genesis
	stake, err := vm.getTotalStake()
	if err != nil {
		t.Fatal(err)
	}
	expectedStake := len(keys) * defaultWeight
	if expectedStake != int(stake) {
		t.Fatalf("expected total stake to be %d but is %d",
			expectedStake,
			stake,
		)
	}

	// add a validator
	vdrID := ids.GenerateTestShortID()
	vdrStakeAmt := vm.minValidatorStake
	addValidatorTx, err := vm.newAddValidatorTx(
		vdrStakeAmt,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		vdrID,
		ids.GenerateTestShortID(),
		0,
		keys,
		keys[0].PublicKey().Address(),
	)
	if err != nil {
		t.Fatal(err)
	}
	validatorRewardTx := &rewardTx{
		Reward: 100,
		Tx:     *addValidatorTx,
	}
	if err := vm.addStaker(vm.DB, constants.PrimaryNetworkID, validatorRewardTx); err != nil {
		t.Fatal(err)
	}

	// Ensure the stake increased
	oldStake := stake
	stake, err = vm.getTotalStake()
	if err != nil {
		t.Fatal(err)
	} else if stake != oldStake+vdrStakeAmt {
		t.Fatalf("excpected ottal stake to be %d but is %d",
			oldStake+vdrStakeAmt,
			stake,
		)
	}

	// add a delegator
	delegatorStakeAmt := 2 * vm.minDelegatorStake
	addDelegatorTx, err := vm.newAddDelegatorTx(
		delegatorStakeAmt,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		vdrID,
		ids.GenerateTestShortID(),
		keys,
		keys[0].PublicKey().Address(),
	)
	if err != nil {
		t.Fatal(err)
	}
	delegatorRewardTx := &rewardTx{
		Reward: 100,
		Tx:     *addDelegatorTx,
	}
	if err := vm.addStaker(vm.DB, constants.PrimaryNetworkID, delegatorRewardTx); err != nil {
		t.Fatal(err)
	}

	// Ensure the stake increased
	oldStake = stake
	stake, err = vm.getTotalStake()
	if err != nil {
		t.Fatal(err)
	} else if stake != oldStake+delegatorStakeAmt {
		t.Fatalf("expected total stake to be %d but is %d",
			oldStake+delegatorStakeAmt,
			stake,
		)
	}

	// Remove the delegator
	if err := vm.removeStaker(vm.DB, constants.PrimaryNetworkID, delegatorRewardTx); err != nil {
		t.Fatal(err)
	}
	// Ensure the stake decreased
	oldStake = stake
	stake, err = vm.getTotalStake()
	if err != nil {
		t.Fatal(err)
	} else if stake != oldStake-delegatorStakeAmt {
		t.Fatalf("expected total stake to be %d but is %d",
			oldStake-delegatorStakeAmt,
			stake,
		)
	}

	// Remove the validator
	if err := vm.removeStaker(vm.DB, constants.PrimaryNetworkID, validatorRewardTx); err != nil {
		t.Fatal(err)
	}
	// Ensure the stake decreased
	oldStake = stake
	stake, err = vm.getTotalStake()
	if err != nil {
		t.Fatal(err)
	} else if stake != oldStake-vdrStakeAmt {
		t.Fatalf("expected total stake to be %d but is %d",
			oldStake-vdrStakeAmt,
			stake,
		)
	}

}

// accept proposal to add validator to primary network
func TestAddValidatorCommit(t *testing.T) {
	vm, _ := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.Ctx.Lock.Unlock()
	}()

	startTime := defaultGenesisTime.Add(syncBound).Add(1 * time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)
	key, err := vm.factory.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	ID := key.PublicKey().Address()

	// create valid tx
	tx, err := vm.newAddValidatorTx(
		vm.minValidatorStake,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		ID,
		ID,
		PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		t.Fatal(err)
	}

	// trigger block creation
	if err := vm.mempool.IssueTx(tx); err != nil {
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
	commit, ok := options[0].(*Commit)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	}
	_, ok = options[1].(*Abort)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	} else if err := block.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if err := commit.Accept(); err != nil { // commit the proposal
		t.Fatal(err)
	} else if status, err := vm.getStatus(vm.DB, tx.ID()); err != nil {
		t.Fatal(err)
	} else if status != Committed {
		t.Fatalf("status of tx should be Committed but is %s", status)
	}

	// Verify that new validator now in pending validator set
	_, willBeValidator, err := vm.willBeValidator(vm.DB, constants.PrimaryNetworkID, ID)
	if err != nil {
		t.Fatal(err)
	}
	if !willBeValidator {
		t.Fatalf("Should have added validator to the pending queue")
	}
}

// verify invalid proposal to add validator to primary network
func TestInvalidAddValidatorCommit(t *testing.T) {
	vm, _ := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.Ctx.Lock.Unlock()
	}()

	startTime := defaultGenesisTime.Add(-syncBound).Add(-1 * time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)
	key, _ := vm.factory.NewPrivateKey()
	ID := key.PublicKey().Address()

	// create invalid tx
	if tx, err := vm.newAddValidatorTx(
		vm.minValidatorStake,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		ID,
		ID,
		PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty, // change addr
	); err != nil {
		t.Fatal(err)
	} else if preferredHeight, err := vm.preferredHeight(); err != nil {
		t.Fatal(err)
	} else if blk, err := vm.newProposalBlock(vm.LastAccepted(), preferredHeight+1, *tx); err != nil {
		t.Fatal(err)
	} else if err := vm.State.PutBlock(vm.DB, blk); err != nil {
		t.Fatal(err)
	} else if err := vm.DB.Commit(); err != nil {
		t.Fatal(err)
	} else if err := blk.Verify(); err == nil {
		t.Fatalf("Should have errored during verification")
	} else if status := blk.Status(); status != choices.Rejected {
		t.Fatalf("Should have marked the block as rejected")
	} else if _, ok := vm.droppedTxCache.Get(blk.Tx.ID()); !ok {
		t.Fatal("tx should be in dropped tx cache")
	} else if parsedBlk, err := vm.GetBlock(blk.ID()); err != nil {
		t.Fatal(err)
	} else if status := parsedBlk.Status(); status != choices.Rejected {
		t.Fatalf("Should have marked the block as rejected")
	}
}

// Reject proposal to add validator to primary network
func TestAddValidatorReject(t *testing.T) {
	vm, _ := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.Ctx.Lock.Unlock()
	}()

	startTime := defaultGenesisTime.Add(syncBound).Add(1 * time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)
	key, _ := vm.factory.NewPrivateKey()
	ID := key.PublicKey().Address()

	// create valid tx
	tx, err := vm.newAddValidatorTx(
		vm.minValidatorStake,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		ID,
		ID,
		PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		t.Fatal(err)
	}

	// trigger block creation
	if err := vm.mempool.IssueTx(tx); err != nil {
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
	} else if commit, ok := options[0].(*Commit); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*Abort); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if err := block.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil { // should pass verification
		t.Fatal(err)
	} else if status, err := vm.getStatus(commit.onAccept(), tx.ID()); err != nil {
		t.Fatal(err)
	} else if status != Committed {
		t.Fatalf("status should be Committed but is %s", status)
	} else if err := abort.Verify(); err != nil { // should pass verification
		t.Fatal(err)
	} else if err := abort.Accept(); err != nil { // reject the proposal
		t.Fatal(err)
	} else if status, err := vm.getStatus(vm.DB, tx.ID()); err != nil {
		t.Fatal(err)
	} else if status != Aborted {
		t.Fatalf("status should be Aborted but is %s", status)
	}

	// Verify that new validator NOT in pending validator set
	_, willBeValidator, err := vm.willBeValidator(vm.DB, constants.PrimaryNetworkID, ID)
	if err != nil {
		t.Fatal(err)
	}
	if willBeValidator {
		t.Fatalf("Shouldn't have added validator to the pending queue")
	}
}

// Accept proposal to add validator to subnet
func TestAddSubnetValidatorAccept(t *testing.T) {
	vm, _ := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.Ctx.Lock.Unlock()
	}()

	startTime := defaultValidateStartTime.Add(syncBound).Add(1 * time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)

	// create valid tx
	// note that [startTime, endTime] is a subset of time that keys[0]
	// validates primary network ([defaultValidateStartTime, defaultValidateEndTime])
	tx, err := vm.newAddSubnetValidatorTx(
		defaultWeight,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		keys[0].PublicKey().Address(),
		testSubnet1.ID(),
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		t.Fatal(err)
	}

	// trigger block creation
	if err := vm.mempool.IssueTx(tx); err != nil {
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
	commit, ok := options[0].(*Commit)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*Abort); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if err := block.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Verify(); err != nil {
		t.Fatal(err)
	} else if status, err := vm.getStatus(abort.onAccept(), tx.ID()); err != nil {
		t.Fatal(err)
	} else if status != Aborted {
		t.Fatalf("status should be Aborted but is %s", status)
	} else if err := commit.Accept(); err != nil { // accept the proposal
		t.Fatal(err)
	} else if status, err := vm.getStatus(vm.DB, tx.ID()); err != nil {
		t.Fatal(err)
	} else if status != Committed {
		t.Fatalf("status should be Committed but is %s", status)
	}

	// Verify that new validator is in pending validator set
	_, willBeValidator, err := vm.willBeValidator(vm.DB, testSubnet1.ID(), keys[0].PublicKey().Address())
	if err != nil {
		t.Fatal(err)
	}
	if !willBeValidator {
		t.Fatalf("Should have added validator to the pending queue")
	}
}

// Reject proposal to add validator to subnet
func TestAddSubnetValidatorReject(t *testing.T) {
	vm, _ := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.Ctx.Lock.Unlock()
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
	if err := vm.mempool.IssueTx(tx); err != nil {
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
	commit, ok := options[0].(*Commit)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*Abort); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if err := block.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if status, err := vm.getStatus(commit.onAccept(), tx.ID()); err != nil {
		t.Fatal(err)
	} else if status != Committed {
		t.Fatalf("status should be Committed but is %s", status)
	} else if err := abort.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Accept(); err != nil { // reject the proposal
		t.Fatal(err)
	} else if status, err := vm.getStatus(vm.DB, tx.ID()); err != nil {
		t.Fatal(err)
	} else if status != Aborted {
		t.Fatalf("status should be Aborted but is %s", status)
	}

	// Verify that new validator NOT in pending validator set
	_, willBeValidator, err := vm.willBeValidator(vm.DB, testSubnet1.ID(), nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if willBeValidator {
		t.Fatalf("Shouldn't have added validator to the pending queue")
	}
}

// Test case where primary network validator rewarded
func TestRewardValidatorAccept(t *testing.T) {
	vm, _ := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.Ctx.Lock.Unlock()
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
	commit, ok := options[0].(*Commit)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*Abort); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if err := block.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Verify(); err != nil {
		t.Fatal(err)
	} else if status, err := vm.getStatus(abort.onAccept(), block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if status != Aborted {
		t.Fatalf("status should be Aborted but is %s", status)
	} else if err := commit.Accept(); err != nil { // advance the timestamp
		t.Fatal(err)
	} else if status, err := vm.getStatus(vm.DB, block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if status != Committed {
		t.Fatalf("status should be Committed but is %s", status)
	}

	// Verify that chain's timestamp has advanced
	if timestamp, err := vm.getTimestamp(vm.DB); err != nil {
		t.Fatal(err)
	} else if !timestamp.Equal(defaultValidateEndTime) {
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
	commit, ok = options[0].(*Commit)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*Abort); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if err := block.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Verify(); err != nil {
		t.Fatal(err)
	} else if status, err := vm.getStatus(abort.onAccept(), block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if status != Aborted {
		t.Fatalf("status should be Aborted but is %s", status)
	} else if err := commit.Accept(); err != nil { // reward the genesis validator
		t.Fatal(err)
	} else if status, err := vm.getStatus(vm.DB, block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if status != Committed {
		t.Fatalf("status should be Committed but is %s", status)
	} else if _, isValidator, err := vm.isValidator(vm.DB, constants.PrimaryNetworkID, keys[1].PublicKey().Address()); err != nil {
		// Verify that genesis validator was rewarded and removed from current validator set
		t.Fatal(err)
	} else if isValidator {
		t.Fatal("should have removed a genesis validator")
	}
}

// Test case where primary network validator not rewarded
func TestRewardValidatorReject(t *testing.T) {
	vm, _ := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.Ctx.Lock.Unlock()
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
	} else if commit, ok := options[0].(*Commit); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*Abort); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if err := block.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Verify(); err != nil {
		t.Fatal(err)
	} else if status, err := vm.getStatus(abort.onAccept(), block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if status != Aborted {
		t.Fatalf("status should be Aborted but is %s", status)
	} else if err := commit.Accept(); err != nil { // advance the timestamp
		t.Fatal(err)
	} else if status, err := vm.getStatus(vm.DB, block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if status != Committed {
		t.Fatalf("status should be Committed but is %s", status)
	} else if timestamp, err := vm.getTimestamp(vm.DB); err != nil { // Verify that chain's timestamp has advanced
		t.Fatal(err)
	} else if !timestamp.Equal(defaultValidateEndTime) {
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
	} else if commit, ok := options[0].(*Commit); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*Abort); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if err := blk.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if status, err := vm.getStatus(commit.onAccept(), block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if status != Committed {
		t.Fatalf("status should be Committed but is %s", status)
	} else if err := abort.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Accept(); err != nil { // do not reward the genesis validator
		t.Fatal(err)
	} else if status, err := vm.getStatus(vm.DB, block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if status != Aborted {
		t.Fatalf("status should be Aborted but is %s", status)
	} else if _, isValidator, err := vm.isValidator(vm.DB, constants.PrimaryNetworkID, keys[1].PublicKey().Address()); err != nil {
		// Verify that genesis validator was removed from current validator set
		t.Fatal(err)
	} else if isValidator {
		t.Fatal("should have removed a genesis validator")
	}
}

// Test case where primary network validator is preferred to be rewarded
func TestRewardValidatorPreferred(t *testing.T) {
	vm, _ := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.Ctx.Lock.Unlock()
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
	} else if commit, ok := options[0].(*Commit); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*Abort); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if err := block.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Verify(); err != nil {
		t.Fatal(err)
	} else if status, err := vm.getStatus(abort.onAccept(), block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if status != Aborted {
		t.Fatalf("status should be Aborted but is %s", status)
	} else if err := commit.Accept(); err != nil { // advance the timestamp
		t.Fatal(err)
	} else if status, err := vm.getStatus(vm.DB, block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if status != Committed {
		t.Fatalf("status should be Committed but is %s", status)
	} else if timestamp, err := vm.getTimestamp(vm.DB); err != nil { // Verify that chain's timestamp has advanced
		t.Fatal(err)
	} else if !timestamp.Equal(defaultValidateEndTime) {
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
	} else if commit, ok := options[0].(*Commit); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*Abort); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if err := blk.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if status, err := vm.getStatus(commit.onAccept(), block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if status != Committed {
		t.Fatalf("status should be Committed but is %s", status)
	} else if err := abort.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Accept(); err != nil { // do not reward the genesis validator
		t.Fatal(err)
	} else if status, err := vm.getStatus(vm.DB, block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if status != Aborted {
		t.Fatalf("status should be Aborted but is %s", status)
	} else if _, isValidator, err := vm.isValidator(vm.DB, constants.PrimaryNetworkID, keys[1].PublicKey().Address()); err != nil {
		// Verify that genesis validator was removed from current validator set
		t.Fatal(err)
	} else if isValidator {
		t.Fatal("should have removed a genesis validator")
	}
}

// Ensure BuildBlock errors when there is no block to build
func TestUnneededBuildBlock(t *testing.T) {
	vm, _ := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.Ctx.Lock.Unlock()
	}()
	if _, err := vm.BuildBlock(); err == nil {
		t.Fatalf("Should have errored on BuildBlock")
	}
}

// test acceptance of proposal to create a new chain
func TestCreateChain(t *testing.T) {
	vm, _ := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.Ctx.Lock.Unlock()
	}()

	tx, err := vm.newCreateChainTx(
		testSubnet1.ID(),
		nil,
		timestampvm.ID,
		nil,
		"name",
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		t.Fatal(err)
	} else if err := vm.mempool.IssueTx(tx); err != nil {
		t.Fatal(err)
	} else if blk, err := vm.BuildBlock(); err != nil { // should contain proposal to create chain
		t.Fatal(err)
	} else if err := blk.Verify(); err != nil {
		t.Fatal(err)
	} else if err := blk.Accept(); err != nil {
		t.Fatal(err)
	} else if status, err := vm.getStatus(vm.DB, tx.ID()); err != nil {
		t.Fatal(err)
	} else if status != Committed {
		t.Fatalf("status should be Committed but is %s", status)
	}

	// Verify chain was created
	chains, err := vm.getChains(vm.DB)
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
	vm, _ := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.Ctx.Lock.Unlock()
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
	} else if err := vm.mempool.IssueTx(createSubnetTx); err != nil {
		t.Fatal(err)
	} else if blk, err := vm.BuildBlock(); err != nil { // should contain proposal to create subnet
		t.Fatal(err)
	} else if err := blk.Verify(); err != nil {
		t.Fatal(err)
	} else if err := blk.Accept(); err != nil {
		t.Fatal(err)
	} else if status, err := vm.getStatus(vm.DB, createSubnetTx.ID()); err != nil {
		t.Fatal(err)
	} else if status != Committed {
		t.Fatalf("status should be Committed but is %s", status)
	} else if _, err := vm.getSubnet(vm.DB, createSubnetTx.ID()); err != nil {
		t.Fatal("should've created new subnet but didn't")
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
	} else if err := vm.mempool.IssueTx(addValidatorTx); err != nil {
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
	commit, ok := options[0].(*Commit)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*Abort); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if err := block.Accept(); err != nil { // Accept the block
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Verify(); err != nil {
		t.Fatal(err)
	} else if status, err := vm.getStatus(abort.onAccept(), block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if status != Aborted {
		t.Fatalf("status should be Aborted but is %s", status)
	} else if err := commit.Accept(); err != nil { // add the validator to pending validator set
		t.Fatal(err)
	} else if status, err := vm.getStatus(vm.DB, block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if status != Committed {
		t.Fatalf("status should be Committed but is %s", status)
	} else if _, willBeValidator, err := vm.willBeValidator(vm.DB, createSubnetTx.ID(), nodeID); err != nil {
		// Verify that validator was added to the pending validator set
		t.Fatal(err)
	} else if !willBeValidator {
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
	commit, ok = options[0].(*Commit)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*Abort); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if err := block.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Verify(); err != nil {
		t.Fatal(err)
	} else if status, err := vm.getStatus(abort.onAccept(), block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if status != Aborted {
		t.Fatalf("status should be Aborted but is %s", status)
	} else if err := commit.Accept(); err != nil { // move validator addValidatorTx from pending to current
		t.Fatal(err)
	} else if status, err := vm.getStatus(vm.DB, block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if status != Committed {
		t.Fatalf("status should be Committed but is %s", status)
	} else if _, willBeValidator, err := vm.willBeValidator(vm.DB, createSubnetTx.ID(), nodeID); err != nil {
		// Verify that validator was removed from the pending validator set
		t.Fatal(err)
	} else if willBeValidator {
		t.Fatal("should have removed the pending validator")
	} else if _, isValidator, err := vm.isValidator(vm.DB, createSubnetTx.ID(), nodeID); err != nil {
		// Verify that validator was added to the validator set
		t.Fatal(err)
	} else if !isValidator {
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
	commit, ok = options[0].(*Commit)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*Abort); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if err := block.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Verify(); err != nil {
		t.Fatal(err)
	} else if status, err := vm.getStatus(abort.onAccept(), block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if status != Aborted {
		t.Fatalf("status should be Aborted but is %s", status)
	} else if err := commit.Accept(); err != nil { // remove validator from current validator set
		t.Fatal(err)
	} else if status, err := vm.getStatus(vm.DB, block.Tx.ID()); err != nil {
		t.Fatal(err)
	} else if status != Committed {
		t.Fatalf("status should be Committed but is %s", status)
	} else if _, willBeValidator, err := vm.willBeValidator(vm.DB, createSubnetTx.ID(), nodeID); err != nil {
		// Verify that validator was removed from the pending validator set
		t.Fatal(err)
	} else if willBeValidator {
		t.Fatal("should have removed the pending validator")
	} else if _, isValidator, err := vm.isValidator(vm.DB, createSubnetTx.ID(), nodeID); err != nil {
		// Verify that validator was added to the validator set
		t.Fatal(err)
	} else if isValidator {
		t.Fatal("should have removed from the validator set")
	}
}

// test asset import
func TestAtomicImport(t *testing.T) {
	vm, baseDB := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.Ctx.Lock.Unlock()
	}()

	utxoID := avax.UTXOID{
		TxID:        ids.Empty.Prefix(1),
		OutputIndex: 1,
	}
	amount := uint64(50000)
	recipientKey := keys[1]

	m := &atomic.Memory{}
	m.Initialize(logging.NoLog{}, prefixdb.New([]byte{5}, baseDB))
	vm.Ctx.SharedMemory = m.NewSharedMemory(vm.Ctx.ChainID)
	peerSharedMemory := m.NewSharedMemory(vm.Ctx.XChainID)

	if _, err := vm.newImportTx(
		vm.Ctx.XChainID,
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
	utxoBytes, err := vm.codec.Marshal(utxo)
	if err != nil {
		t.Fatal(err)
	}

	if err := peerSharedMemory.Put(vm.Ctx.ChainID, []*atomic.Element{{
		Key:   utxo.InputID().Bytes(),
		Value: utxoBytes,
		Traits: [][]byte{
			recipientKey.PublicKey().Address().Bytes(),
		},
	}}); err != nil {
		t.Fatal(err)
	}

	tx, err := vm.newImportTx(
		vm.Ctx.XChainID,
		recipientKey.PublicKey().Address(),
		[]*crypto.PrivateKeySECP256K1R{recipientKey},
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.mempool.IssueTx(tx); err != nil {
		t.Fatal(err)
	} else if blk, err := vm.BuildBlock(); err != nil {
		t.Fatal(err)
	} else if err := blk.Verify(); err != nil {
		t.Fatal(err)
	} else if err := blk.Accept(); err != nil {
		t.Fatal(err)
	} else if status, err := vm.getStatus(vm.DB, tx.ID()); err != nil {
		t.Fatal(err)
	} else if status != Committed {
		t.Fatalf("status should be Committed but is %s", status)
	}

	if _, err := vm.Ctx.SharedMemory.Get(vm.Ctx.XChainID, [][]byte{utxoID.InputID().Bytes()}); err == nil {
		t.Fatalf("shouldn't have been able to read the utxo")
	}
}

// test optimistic asset import
func TestOptimisticAtomicImport(t *testing.T) {
	vm, _ := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.Ctx.Lock.Unlock()
	}()

	tx := Tx{UnsignedTx: &UnsignedImportTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    vm.Ctx.NetworkID,
			BlockchainID: vm.Ctx.ChainID,
		}},
		SourceChain: vm.Ctx.XChainID,
		ImportedInputs: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID:        ids.Empty.Prefix(1),
				OutputIndex: 1,
			},
			Asset: avax.Asset{ID: vm.Ctx.AVAXAssetID},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
			},
		}},
	}}
	if err := tx.Sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{}}); err != nil {
		t.Fatal(err)
	}

	preferredHeight, err := vm.preferredHeight()
	if err != nil {
		t.Fatal(err)
	}

	blk, err := vm.newAtomicBlock(vm.Preferred(), preferredHeight+1, tx)
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(); err == nil {
		t.Fatalf("Block should have failed verification due to missing UTXOs")
	}

	if err := vm.Bootstrapping(); err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(); err != nil {
		t.Fatal(err)
	}

	if err := vm.Bootstrapped(); err != nil {
		t.Fatal(err)
	}

	status, err := vm.getStatus(vm.DB, tx.ID())
	if err != nil {
		t.Fatal(err)
	}

	if status != Committed {
		t.Fatalf("Wrong status returned. Expected %s; Got %s", Committed, status)
	}
}

// test restarting the node
func TestRestartPartiallyAccepted(t *testing.T) {
	_, genesisBytes := defaultGenesis()
	db := memdb.New()

	firstVM := &VM{
		SnowmanVM:          &core.SnowmanVM{},
		chainManager:       chains.MockManager{},
		minStakeDuration:   defaultMinStakingDuration,
		maxStakeDuration:   defaultMaxStakingDuration,
		stakeMintingPeriod: defaultMaxStakingDuration,
	}
	firstVM.vdrMgr = validators.NewManager()
	firstVM.clock.Set(defaultGenesisTime)
	firstCtx := defaultContext()
	firstCtx.Lock.Lock()

	firstMsgChan := make(chan common.Message, 1)
	if err := firstVM.Initialize(firstCtx, db, genesisBytes, firstMsgChan, nil); err != nil {
		t.Fatal(err)
	}

	genesisID := firstVM.LastAccepted()

	firstAdvanceTimeTx, err := firstVM.newAdvanceTimeTx(defaultGenesisTime.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	preferredHeight, err := firstVM.preferredHeight()
	if err != nil {
		t.Fatal(err)
	}
	firstAdvanceTimeBlk, err := firstVM.newProposalBlock(firstVM.Preferred(), preferredHeight+1, *firstAdvanceTimeTx)
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

	secondVM := &VM{
		SnowmanVM:          &core.SnowmanVM{},
		chainManager:       chains.MockManager{},
		minStakeDuration:   defaultMinStakingDuration,
		maxStakeDuration:   defaultMaxStakingDuration,
		stakeMintingPeriod: defaultMaxStakingDuration,
	}

	secondVM.vdrMgr = validators.NewManager()

	secondVM.clock.Set(defaultGenesisTime)
	secondCtx := defaultContext()
	secondCtx.Lock.Lock()
	defer func() {
		if err := secondVM.Shutdown(); err != nil {
			t.Fatal(err)
		}
		secondCtx.Lock.Unlock()
	}()

	secondMsgChan := make(chan common.Message, 1)
	if err := secondVM.Initialize(secondCtx, db, genesisBytes, secondMsgChan, nil); err != nil {
		t.Fatal(err)
	}

	if lastAccepted := secondVM.LastAccepted(); !genesisID.Equals(lastAccepted) {
		t.Fatalf("Shouldn't have changed the genesis")
	}
}

// test restarting the node
func TestRestartFullyAccepted(t *testing.T) {
	_, genesisBytes := defaultGenesis()

	db := memdb.New()

	firstVM := &VM{
		SnowmanVM:          &core.SnowmanVM{},
		chainManager:       chains.MockManager{},
		minStakeDuration:   defaultMinStakingDuration,
		maxStakeDuration:   defaultMaxStakingDuration,
		stakeMintingPeriod: defaultMaxStakingDuration,
	}

	firstVM.vdrMgr = validators.NewManager()

	firstVM.clock.Set(defaultGenesisTime)
	firstCtx := defaultContext()
	firstCtx.Lock.Lock()

	firstMsgChan := make(chan common.Message, 1)
	if err := firstVM.Initialize(firstCtx, db, genesisBytes, firstMsgChan, nil); err != nil {
		t.Fatal(err)
	}

	firstAdvanceTimeTx, err := firstVM.newAdvanceTimeTx(defaultGenesisTime.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	preferredHeight, err := firstVM.preferredHeight()
	if err != nil {
		t.Fatal(err)
	}
	firstAdvanceTimeBlk, err := firstVM.newProposalBlock(firstVM.Preferred(), preferredHeight+1, *firstAdvanceTimeTx)
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

	/*
		//This code, when uncommented, prints [secondAdvanceTimeBlkBytes]
		secondAdvanceTimeTx, err := firstVM.newAdvanceTimeTx(defaultGenesisTime.Add(2 * time.Second))
		if err != nil {
			t.Fatal(err)
		}
		preferredHeight, err = firstVM.preferredHeight()
		if err != nil {
			t.Fatal(err)
		}
		secondAdvanceTimeBlk, err := firstVM.newProposalBlock(firstVM.Preferred(), preferredHeight+1, *secondAdvanceTimeTx)
		if err != nil {
			t.Fatal(err)
		}
		t.Fatal(secondAdvanceTimeBlk.Bytes())
	*/

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

	secondVM := &VM{
		SnowmanVM:          &core.SnowmanVM{},
		chainManager:       chains.MockManager{},
		minStakeDuration:   defaultMinStakingDuration,
		maxStakeDuration:   defaultMaxStakingDuration,
		stakeMintingPeriod: defaultMaxStakingDuration,
	}

	secondVM.vdrMgr = validators.NewManager()

	secondVM.clock.Set(defaultGenesisTime)
	secondCtx := defaultContext()
	secondCtx.Lock.Lock()
	defer func() {
		if err := secondVM.Shutdown(); err != nil {
			t.Fatal(err)
		}
		secondCtx.Lock.Unlock()
	}()

	secondMsgChan := make(chan common.Message, 1)
	if err := secondVM.Initialize(secondCtx, db, genesisBytes, secondMsgChan, nil); err != nil {
		t.Fatal(err)
	} else if lastAccepted := secondVM.LastAccepted(); !options[0].ID().Equals(lastAccepted) {
		t.Fatalf("Should have changed the genesis")
	}
}

// test bootstrapping the node
func TestBootstrapPartiallyAccepted(t *testing.T) {
	_, genesisBytes := defaultGenesis()

	db := memdb.New()
	vmDB := prefixdb.New([]byte("vm"), db)
	bootstrappingDB := prefixdb.New([]byte("bootstrapping"), db)

	blocked, err := queue.New(bootstrappingDB)
	if err != nil {
		t.Fatal(err)
	}

	vm := &VM{
		SnowmanVM:          &core.SnowmanVM{},
		chainManager:       chains.MockManager{},
		minStakeDuration:   defaultMinStakingDuration,
		maxStakeDuration:   defaultMaxStakingDuration,
		stakeMintingPeriod: defaultMaxStakingDuration,
	}

	vm.vdrMgr = validators.NewManager()

	vm.clock.Set(defaultGenesisTime)
	ctx := defaultContext()
	ctx.Lock.Lock()

	msgChan := make(chan common.Message, 1)
	if err := vm.Initialize(ctx, vmDB, genesisBytes, msgChan, nil); err != nil {
		t.Fatal(err)
	}

	genesisID := vm.Preferred()

	advanceTimeTx, err := vm.newAdvanceTimeTx(defaultGenesisTime.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	preferredHeight, err := vm.preferredHeight()
	if err != nil {
		t.Fatal(err)
	}
	advanceTimeBlk, err := vm.newProposalBlock(vm.Preferred(), preferredHeight+1, *advanceTimeTx)
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

	peerID := ids.NewShortID([20]byte{1, 2, 3, 4, 5, 4, 3, 2, 1})
	vdrs := validators.NewSet()
	if err := vdrs.AddWeight(peerID, 1); err != nil {
		t.Fatal(err)
	}
	beacons := vdrs

	timeoutManager := timeout.Manager{}
	benchlist := benchlist.NewNoBenchlist()
	err = timeoutManager.Initialize(&timer.AdaptiveTimeoutConfig{
		InitialTimeout: time.Millisecond,
		MinimumTimeout: time.Millisecond,
		MaximumTimeout: 10 * time.Second,
		TimeoutInc:     2 * time.Millisecond,
		TimeoutDec:     time.Millisecond,
		Namespace:      "",
		Registerer:     prometheus.NewRegistry(),
	}, benchlist)
	if err != nil {
		t.Fatal(err)
	}
	go timeoutManager.Dispatch()

	chainRouter := &router.ChainRouter{}
	chainRouter.Initialize(ids.ShortEmpty, logging.NoLog{}, &timeoutManager, time.Hour, time.Second, ids.Set{}, nil)

	externalSender := &sender.ExternalSenderTest{T: t}
	externalSender.Default(true)

	// Passes messages from the consensus engine to the network
	sender := sender.Sender{}

	sender.Initialize(ctx, externalSender, chainRouter, &timeoutManager)

	reqID := new(uint32)
	externalSender.GetAcceptedFrontierF = func(_ ids.ShortSet, _ ids.ID, requestID uint32, _ time.Time) {
		*reqID = requestID
	}

	// The engine handles consensus
	engine := smeng.Transitive{}
	err = engine.Initialize(smeng.Config{
		Config: bootstrap.Config{
			Config: common.Config{
				Ctx:        ctx,
				Validators: vdrs,
				Beacons:    beacons,
				SampleK:    int(beacons.Weight()),
				Alpha:      uint64(beacons.Len()/2 + 1),
				Sender:     &sender,
			},
			Blocked: blocked,
			VM:      vm,
		},
		Params: snowball.Parameters{
			Metrics:           prometheus.NewRegistry(),
			K:                 1,
			Alpha:             1,
			BetaVirtuous:      20,
			BetaRogue:         20,
			ConcurrentRepolls: 1,
		},
		Consensus: &smcon.Topological{},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Asynchronously passes messages from the network to the consensus engine
	handler := &router.Handler{}
	handler.Initialize(
		&engine,
		vdrs,
		msgChan,
		1024,
		router.DefaultMaxNonStakerPendingMsgs,
		router.DefaultStakerPortion,
		router.DefaultStakerPortion,
		"",
		prometheus.NewRegistry(),
	)

	// Allow incoming messages to be routed to the new chain
	chainRouter.AddChain(handler)
	go ctx.Log.RecoverAndPanic(handler.Dispatch)

	externalSender.GetAcceptedFrontierF = nil
	externalSender.GetAcceptedF = func(_ ids.ShortSet, _ ids.ID, requestID uint32, _ time.Time, _ ids.Set) {
		*reqID = requestID
	}

	frontier := ids.Set{}
	frontier.Add(advanceTimeBlkID)
	if err := engine.AcceptedFrontier(peerID, *reqID, frontier); err != nil {
		t.Fatal(err)
	}

	externalSender.GetAcceptedF = nil
	externalSender.GetAncestorsF = func(_ ids.ShortID, _ ids.ID, requestID uint32, _ time.Time, containerID ids.ID) {
		*reqID = requestID
		if !containerID.Equals(advanceTimeBlkID) {
			t.Fatalf("wrong block requested")
		}
	}

	if err := engine.Accepted(peerID, *reqID, frontier); err != nil {
		t.Fatal(err)
	}

	externalSender.GetF = nil
	externalSender.CantPushQuery = false
	externalSender.CantPullQuery = false

	if err := engine.MultiPut(peerID, *reqID, [][]byte{advanceTimeBlkBytes}); err != nil {
		t.Fatal(err)
	}

	externalSender.CantPushQuery = true

	if pref := vm.Preferred(); !pref.Equals(advanceTimePreference.ID()) {
		t.Fatalf("wrong preference reported after bootstrapping to proposal block\nPreferred: %s\nExpected: %s\nGenesis: %s",
			pref,
			advanceTimePreference.ID(),
			genesisID)
	}
	ctx.Lock.Unlock()

	chainRouter.Shutdown()
}

func TestUnverifiedParent(t *testing.T) {
	_, genesisBytes := defaultGenesis()

	db := memdb.New()

	vm := &VM{
		SnowmanVM:          &core.SnowmanVM{},
		chainManager:       chains.MockManager{},
		minStakeDuration:   defaultMinStakingDuration,
		maxStakeDuration:   defaultMaxStakingDuration,
		stakeMintingPeriod: defaultMaxStakingDuration,
	}

	vm.vdrMgr = validators.NewManager()

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
	if err := vm.Initialize(ctx, db, genesisBytes, msgChan, nil); err != nil {
		t.Fatal(err)
	}

	firstAdvanceTimeTx, err := vm.newAdvanceTimeTx(defaultGenesisTime.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	preferredHeight, err := vm.preferredHeight()
	if err != nil {
		t.Fatal(err)
	}
	firstAdvanceTimeBlk, err := vm.newProposalBlock(vm.Preferred(), preferredHeight+1, *firstAdvanceTimeTx)
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

	parentBlk := secondAdvanceTimeBlk.Parent()
	if parentBlkID := parentBlk.ID(); !parentBlkID.Equals(firstOption.ID()) {
		t.Fatalf("Wrong parent block ID returned")
	} else if err := firstOption.Verify(); err != nil {
		t.Fatal(err)
	} else if err := secondOption.Verify(); err != nil {
		t.Fatal(err)
	} else if err := secondAdvanceTimeBlk.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestParseAddress(t *testing.T) {
	vm, _ := defaultVM()
	if _, err := vm.ParseLocalAddress(testAddress); err != nil {
		t.Fatal(err)
	}
}

func TestParseAddressInvalid(t *testing.T) {
	vm, _ := defaultVM()
	tests := []struct {
		in   string
		want string
	}{
		{"", "no separator found in address"},
		{"+", "no separator found in address"},
		{"P", "no separator found in address"},
		{"-", "invalid bech32 string length 0"},
		{"P-", "invalid bech32 string length 0"},
		{
			in:   "X-testing18jma8ppw3nhx5r4ap8clazz0dps7rv5umpc36y",
			want: "expected chainID to be \"11111111111111111111111111111111LpoYY\" but was \"LUC1cmcxnfNR9LdkACS2ccGKLEK7SYqB4gLLTycQfg1koyfSq\"",
		},
		{
			in:   "P-testing18jma8ppw3nhx5r4ap", // truncated
			want: "checksum failed. Expected qwqey4, got x5r4ap.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			_, err := vm.ParseLocalAddress(tt.in)
			if err.Error() != tt.want {
				t.Errorf("want %q, got %q", tt.want, err)
			}
		})
	}
}

func TestFormatAddress(t *testing.T) {
	vm, _ := defaultVM()
	tests := []struct {
		label string
		in    ids.ShortID
		want  string
	}{
		{"keys[3]", keys[3].PublicKey().Address(), testAddress},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			addrStr, err := vm.FormatLocalAddress(tt.in)
			if err != nil {
				t.Errorf("problem formatting address: %w", err)
			}
			if addrStr != tt.want {
				t.Errorf("want %q, got %q", tt.want, addrStr)
			}
		})
	}
}

func TestNextValidatorStartTime(t *testing.T) {
	vm, _ := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.Ctx.Lock.Unlock()
	}()

	currentTime, err := vm.getTimestamp(vm.DB)
	assert.NoError(t, err)

	startTime := currentTime.Add(time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)

	tx, err := vm.newAddValidatorTx(
		vm.minValidatorStake,      // stake amount
		uint64(startTime.Unix()),  // start time
		uint64(endTime.Unix()),    // end time
		vm.Ctx.NodeID,             // node ID
		ids.GenerateTestShortID(), // reward address
		PercentDenominator,        // shares
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty, // change addr // key
	)
	assert.NoError(t, err)

	err = vm.enqueueStaker(vm.DB, constants.PrimaryNetworkID, tx)
	assert.NoError(t, err)

	nextStaker, err := vm.nextStakerStart(vm.DB, constants.PrimaryNetworkID)
	assert.NoError(t, err)
	assert.Equal(
		t,
		tx.ID().Bytes(),
		nextStaker.ID().Bytes(),
		"should have marked the new tx as the next validator to be added",
	)
}

func TestMaxStakeAmount(t *testing.T) {
	vm, _ := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.Ctx.Lock.Unlock()
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
			amount, err := vm.maxStakeAmount(vm.DB, vm.Ctx.SubnetID, test.validatorID, test.startTime, test.endTime)
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
