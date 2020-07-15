// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/chains"
	"github.com/ava-labs/gecko/chains/atomic"
	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/database/prefixdb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowball"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/snow/engine/common/queue"
	"github.com/ava-labs/gecko/snow/engine/snowman/bootstrap"
	"github.com/ava-labs/gecko/snow/networking/router"
	"github.com/ava-labs/gecko/snow/networking/sender"
	"github.com/ava-labs/gecko/snow/networking/timeout"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/json"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/components/core"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
	"github.com/ava-labs/gecko/vms/timestampvm"

	smcon "github.com/ava-labs/gecko/snow/consensus/snowman"
	smeng "github.com/ava-labs/gecko/snow/engine/snowman"
)

var (
	// AVAX asset ID in tests
	avaxAssetID = ids.NewID([32]byte{'y', 'e', 'e', 't'})

	defaultTxFee = 1

	// chain timestamp at genesis
	defaultGenesisTime = time.Now().Round(time.Second)

	// time that genesis validators start validating
	defaultValidateStartTime = defaultGenesisTime

	// time that genesis validators stop validating
	defaultValidateEndTime = defaultValidateStartTime.Add(10 * MinimumStakingDuration)

	// each key controls an address that has [defaultBalance] AVAX at genesis
	keys []*crypto.PrivateKeySECP256K1R

	// balance of addresses that exist at genesis in defaultVM
	defaultBalance = 100 * MinimumStakeAmount

	// amount all genesis validators stake in defaultVM
	defaultStakeAmount uint64 = 100 * MinimumStakeAmount

	// non-default Subnet that exists at genesis in defaultVM
	// Its controlKeys are keys[0], keys[1], keys[2]
	testSubnet1            *CreateSubnetTx
	testSubnet1ControlKeys []*crypto.PrivateKeySECP256K1R
)

var (
	errShouldNotifyEngine = errors.New("should have notified engine of block ready")
	errShouldPrefCommit   = errors.New("should prefer to commit proposal")
	errShouldPrefAbort    = errors.New("should prefer to abort proposal")
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
	return ctx
}

// The UTXOs that exist at genesis in the default VM
func defaultGenesisUTXOs() []*ava.UTXO {
	utxos := []*ava.UTXO(nil)
	for i, key := range keys {
		utxos = append(utxos,
			&ava.UTXO{
				UTXOID: ava.UTXOID{
					TxID:        ids.Empty,
					OutputIndex: uint32(i),
				},
				Asset: ava.Asset{ID: avaxAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: defaultBalance,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{key.PublicKey().Address()},
					},
				},
			},
		)
	}
	return utxos
}

// Returns:
// 1) The genesis state
// 2) The byte representation of the default genesis for tests
func defaultGenesis() (*BuildGenesisArgs, []byte) {
	genesisUTXOs := make([]APIUTXO, len(keys))
	for i, key := range keys {
		genesisUTXOs[i] = APIUTXO{
			Amount:  json.Uint64(defaultStakeAmount),
			Address: key.PublicKey().Address(),
		}
	}

	genesisValidators := make([]APIDefaultSubnetValidator, len(keys))
	for i, key := range keys {
		weight := json.Uint64(defaultWeight)
		address := key.PublicKey().Address()
		genesisValidators[i] = APIDefaultSubnetValidator{
			APIValidator: APIValidator{
				StartTime: json.Uint64(defaultValidateStartTime.Unix()),
				EndTime:   json.Uint64(defaultValidateEndTime.Unix()),
				Weight:    &weight,
				Address:   &address,
				ID:        address,
			},
			Destination:       address,
			DelegationFeeRate: NumberOfShares,
		}
	}

	buildGenesisArgs := BuildGenesisArgs{
		NetworkID:   json.Uint32(testNetworkID),
		AvaxAssetID: avaxAssetID,
		UTXOs:       genesisUTXOs,
		Validators:  genesisValidators,
		Chains:      nil,
		Time:        json.Uint64(defaultGenesisTime.Unix()),
	}

	buildGenesisResponse := BuildGenesisReply{}
	platformvmSS := StaticService{}
	if err := platformvmSS.BuildGenesis(nil, &buildGenesisArgs, &buildGenesisResponse); err != nil {
		panic(fmt.Errorf("problem while building platform chain's genesis state: %w", err))
	}
	return &buildGenesisArgs, buildGenesisResponse.Bytes.Bytes
}

func defaultVM() *VM {
	vm := &VM{
		SnowmanVM:    &core.SnowmanVM{},
		chainManager: chains.MockManager{},
		avaxAssetID:  avaxAssetID,
		txFee:        1,
	}

	defaultSubnet := validators.NewSet() // TODO do we need this?
	vm.validators = validators.NewManager()
	vm.validators.PutValidatorSet(DefaultSubnetID, defaultSubnet)

	vm.clock.Set(defaultGenesisTime)
	db := prefixdb.New([]byte{0}, memdb.New())
	msgChan := make(chan common.Message, 1)
	ctx := defaultContext()
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()
	_, genesisBytes := defaultGenesis()
	if err := vm.Initialize(ctx, db, genesisBytes, msgChan, nil); err != nil {
		panic(err)
	}

	// Create a non-default subnet and store it in testSubnet1
	if tx, err := vm.newCreateSubnetTx(
		// control keys are keys[0], keys[1], keys[2]
		[]ids.ShortID{keys[0].PublicKey().Address(), keys[1].PublicKey().Address(), keys[2].PublicKey().Address()},
		// threshold; 2 sigs from keys[0], keys[1], keys[2] needed to add validator to this subnet
		2,
		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // pays tx fee
	); err != nil {
		panic(err)
	} else if err := vm.issueTx(tx); err != nil {
		panic(err)
	} else if blk, err := vm.BuildBlock(); err != nil {
		panic(err)
	} else if err := blk.Verify(); err != nil {
		panic(err)
	} else if err := blk.Accept(); err != nil {
		panic(err)
	} else {
		testSubnet1 = tx
	}

	return vm
}

// Ensure genesis state is parsed from bytes and stored correctly
func TestGenesis(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
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
		addrSet := ids.ShortSet{}
		addrSet.Add(utxo.Address)
		utxos, err := vm.getUTXOs(vm.DB, addrSet)
		if err != nil {
			t.Fatal("couldn't find UTXO")
		} else if len(utxos) != 1 {
			t.Fatal("expected each address to have one UTXO")
		} else if out, ok := utxos[0].Out.(*secp256k1fx.TransferOutput); !ok {
			t.Fatal("expected utxo output to be type *secp256k1fx.TransferOutput")
		} else if out.Amount() != uint64(utxo.Amount) {
			if utxo.Address.Equals(keys[0].PublicKey().Address()) { // Address that paid tx fee to create testSubnet1 has less tokens
				if out.Amount() != uint64(utxo.Amount)-vm.txFee {
					t.Fatalf("expected UTXO to have value %d but has value %d", uint64(utxo.Amount)-vm.txFee, out.Amount())
				}
			} else {
				t.Fatalf("expected UTXO to have value %d but has value %d", uint64(utxo.Amount), out.Amount())
			}
		}
	}

	// Ensure current validator set of default subnet is correct
	currentValidators, err := vm.getCurrentValidators(vm.DB, DefaultSubnetID)
	if err != nil {
		t.Fatal(err)
	} else if len(currentValidators.Txs) != len(genesisState.Validators) {
		t.Fatal("vm's current validator set is wrong")
	} else if currentValidators.SortByStartTime == true {
		t.Fatal("vm's current validators should be sorted by end time")
	}
	currentSampler := validators.NewSet()
	currentSampler.Set(vm.getValidators(currentValidators))
	for _, key := range keys {
		if addr := key.PublicKey().Address(); !currentSampler.Contains(addr) {
			t.Fatalf("should have had validator with NodeID %s", addr)
		}
	}

	// Ensure pending validator set is correct (empty)
	if pendingValidators, err := vm.getPendingValidators(vm.DB, DefaultSubnetID); err != nil {
		t.Fatal(err)
	} else if pendingValidators.Len() != 0 {
		t.Fatal("vm's pending validator set should be empty")
	}

	// Ensure genesis timestamp is correct
	if timestamp, err := vm.getTimestamp(vm.DB); err != nil {
		t.Fatal(err)
	} else if timestamp.Unix() != int64(genesisState.Time) {
		t.Fatalf("vm's time is incorrect. Expected %v got %v", genesisState.Time, timestamp)
	}

	// Ensure the new subnet we created exists
	if _, err := vm.getSubnet(vm.DB, testSubnet1.ID()); err != nil {
		subnets, err := vm.getSubnets(vm.DB)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("subnets: %+v", subnets)
		t.Fatalf("expected subnet %s to exist", testSubnet1.ID())
	}
}

// accept proposal to add validator to default subnet
func TestAddDefaultSubnetValidatorCommit(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	startTime := defaultGenesisTime.Add(Delta).Add(1 * time.Second)
	endTime := startTime.Add(MinimumStakingDuration)
	key, err := vm.factory.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	ID := key.PublicKey().Address()

	// create valid tx
	tx, err := vm.newAddDefaultSubnetValidatorTx(
		MinimumStakeAmount,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		ID,
		ID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}

	// trigger block creation
	if err := vm.issueTx(tx); err != nil {
		t.Fatal(err)
	}
	blk, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	// Assert preferences are correct
	block := blk.(*ProposalBlock)
	options := block.Options()
	commit, ok := blk.(*ProposalBlock).Options()[0].(*Commit)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	}
	_, ok = options[1].(*Abort)
	if !ok {
		t.Fatal(errShouldPrefAbort)
	}

	if err := block.Verify(); err != nil {
		t.Fatal(err)
	}
	block.Accept()

	if err := commit.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := commit.Accept(); err != nil { // commit the proposal
		t.Fatal(err)
	}

	// Verify that new validator now in pending validator set
	pendingValidators, err := vm.getPendingValidators(vm.DB, DefaultSubnetID)
	if err != nil {
		t.Fatal(err)
	}
	pendingSampler := validators.NewSet()
	pendingSampler.Set(vm.getValidators(pendingValidators))
	if !pendingSampler.Contains(ID) {
		t.Fatalf("pending validator should have validator with ID %s", ID)
	}
}

// verify invalid proposal to add validator to default subnet
func TestInvalidAddDefaultSubnetValidatorCommit(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	startTime := defaultGenesisTime.Add(-Delta).Add(-1 * time.Second)
	endTime := startTime.Add(MinimumStakingDuration)
	key, _ := vm.factory.NewPrivateKey()
	ID := key.PublicKey().Address()

	// create invalid tx
	tx, err := vm.newAddDefaultSubnetValidatorTx(
		MinimumStakeAmount,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		ID,
		ID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}

	blk, err := vm.newProposalBlock(vm.LastAccepted(), vm.preferredHeight()+1, tx)
	if err != nil {
		t.Fatal(err)
	} else if err := vm.State.PutBlock(vm.DB, blk); err != nil {
		t.Fatal(err)
	} else if err := vm.DB.Commit(); err != nil {
		t.Fatal(err)
	} else if err := blk.Verify(); err == nil {
		t.Fatalf("Should have errored during verification")
	} else if status := blk.Status(); status != choices.Rejected {
		t.Fatalf("Should have marked the block as rejected")
	}

	parsedBlk, err := vm.GetBlock(blk.ID())
	if err != nil {
		t.Fatal(err)
	} else if status := parsedBlk.Status(); status != choices.Rejected {
		t.Fatalf("Should have marked the block as rejected")
	}
}

// Reject proposal to add validator to default subnet
func TestAddDefaultSubnetValidatorReject(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	startTime := defaultGenesisTime.Add(Delta).Add(1 * time.Second)
	endTime := startTime.Add(MinimumStakingDuration)
	key, _ := vm.factory.NewPrivateKey()
	ID := key.PublicKey().Address()

	// create valid tx
	tx, err := vm.newAddDefaultSubnetValidatorTx(
		MinimumStakeAmount,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		ID,
		ID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}

	// trigger block creation
	if err := vm.issueTx(tx); err != nil {
		t.Fatal(err)
	}
	blk, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	// Assert preferences are correct
	block := blk.(*ProposalBlock)
	options := block.Options()
	commit, ok := blk.(*ProposalBlock).Options()[0].(*Commit)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	}
	abort, ok := options[1].(*Abort)
	if !ok {
		t.Fatal(errShouldPrefAbort)
	}

	if err := block.Verify(); err != nil {
		t.Fatal(err)
	}
	block.Accept()

	if err := commit.Verify(); err != nil { // should pass verification
		t.Fatal(err)
	} else if err := abort.Verify(); err != nil { // should pass verification
		t.Fatal(err)
	}

	if err := abort.Accept(); err != nil { // reject the proposal
		t.Fatal(err)
	}

	// Verify that new validator NOT in pending validator set
	pendingValidators, err := vm.getPendingValidators(vm.DB, DefaultSubnetID)
	if err != nil {
		t.Fatal(err)
	}
	pendingSampler := validators.NewSet()
	pendingSampler.Set(vm.getValidators(pendingValidators))
	if pendingSampler.Contains(ID) {
		t.Fatalf("should not have added validator to pending validator set")
	}
}

// Accept proposal to add validator to non-default subnet
func TestAddNonDefaultSubnetValidatorAccept(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	startTime := defaultValidateStartTime.Add(Delta).Add(1 * time.Second)
	endTime := startTime.Add(MinimumStakingDuration)

	// create valid tx
	// note that [startTime, endTime] is a subset of time that keys[0]
	// validates default subnet ([defaultValidateStartTime, defaultValidateEndTime])
	tx, err := vm.newAddNonDefaultSubnetValidatorTx(
		defaultWeight,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		keys[0].PublicKey().Address(),
		testSubnet1.id,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}

	// trigger block creation
	if err := vm.issueTx(tx); err != nil {
		t.Fatal(err)
	}
	blk, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	// Assert preferences are correct
	block := blk.(*ProposalBlock)
	options := block.Options()
	if commit, ok := blk.(*ProposalBlock).Options()[0].(*Commit); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*Abort); !ok {
		t.Fatal(errShouldPrefAbort)
	} else if err := block.Verify(); err != nil {
		t.Fatal(err)
	} else if err := block.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Verify(); err != nil {
		t.Fatal(err)
	} else if err := commit.Accept(); err != nil { // accept the proposal
		t.Fatal(err)
	}

	// Verify that new validator is in pending validator set
	pendingValidators, err := vm.getPendingValidators(vm.DB, testSubnet1.id)
	if err != nil {
		t.Fatal(err)
	}
	pendingSampler := validators.NewSet()
	pendingSampler.Set(vm.getValidators(pendingValidators))
	if !pendingSampler.Contains(keys[0].PublicKey().Address()) {
		t.Fatalf("should have added validator to pending validator set")
	}
}

// Reject proposal to add validator to non-default subnet
func TestAddNonDefaultSubnetValidatorReject(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	startTime := defaultValidateStartTime.Add(Delta).Add(1 * time.Second)
	endTime := startTime.Add(MinimumStakingDuration)
	key, _ := vm.factory.NewPrivateKey()
	ID := key.PublicKey().Address()

	// create valid tx
	// note that [startTime, endTime] is a subset of time that keys[0]
	// validates default subnet ([defaultValidateStartTime, defaultValidateEndTime])
	tx, err := vm.newAddNonDefaultSubnetValidatorTx(
		defaultWeight,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		keys[0].PublicKey().Address(),
		testSubnet1.id,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[1], testSubnet1ControlKeys[2]},
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}

	// trigger block creation
	if err := vm.issueTx(tx); err != nil {
		t.Fatal(err)
	}
	blk, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	// Assert preferences are correct
	block := blk.(*ProposalBlock)
	options := block.Options()
	commit, ok := blk.(*ProposalBlock).Options()[0].(*Commit)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*Abort); !ok {
		t.Fatal(errShouldPrefAbort)
	} else if err := block.Verify(); err != nil {
		t.Fatal(err)
	} else if err := block.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Accept(); err != nil { // reject the proposal
		t.Fatal(err)
	}

	// Verify that new validator NOT in pending validator set
	pendingValidators, err := vm.getPendingValidators(vm.DB, testSubnet1.id)
	if err != nil {
		t.Fatal(err)
	}
	pendingSampler := validators.NewSet()
	pendingSampler.Set(vm.getValidators(pendingValidators))
	if pendingSampler.Contains(ID) {
		t.Fatalf("should not have added validator to pending validator set")
	}
}

// Test case where default subnet validator rewarded
func TestRewardValidatorAccept(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	// Fast forward clock to time for genesis validators to leave
	vm.clock.Set(defaultValidateEndTime)

	blk, err := vm.BuildBlock() // should contain proposal to advance time
	if err != nil {
		t.Fatal(err)
	}

	// Assert preferences are correct
	block := blk.(*ProposalBlock)
	options := block.Options()
	if commit, ok := blk.(*ProposalBlock).Options()[0].(*Commit); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*Abort); !ok {
		t.Fatal(errShouldPrefAbort)
	} else if err := block.Verify(); err != nil {
		t.Fatal(err)
	} else if err := block.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Verify(); err != nil {
		t.Fatal(err)
	} else if err := commit.Accept(); err != nil { // advance the timestamp
		t.Fatal(err)
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
	}
	// Assert preferences are correct
	block = blk.(*ProposalBlock)
	options = block.Options()
	if commit, ok := blk.(*ProposalBlock).Options()[0].(*Commit); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*Abort); !ok {
		t.Fatal(errShouldPrefAbort)
	} else if err := block.Verify(); err != nil {
		t.Fatal(err)
	} else if err := block.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Verify(); err != nil {
		t.Fatal(err)
	} else if err := commit.Accept(); err != nil { // reward the genesis validator
		t.Fatal(err)
	} else if currentValidators, err := vm.getCurrentValidators(vm.DB, DefaultSubnetID); err != nil {
		// Verify that genesis validator was rewarded and removed from current validator set
		t.Fatal(err)
	} else if currentValidators.Len() != len(keys)-1 {
		t.Fatal("should have removed a genesis validator")
	}
}

// Test case where default subnet validator not rewarded
func TestRewardValidatorReject(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	// Fast forward clock to time for genesis validators to leave
	vm.clock.Set(defaultValidateEndTime)

	blk, err := vm.BuildBlock() // should contain proposal to advance time
	if err != nil {
		t.Fatal(err)
	}

	// Assert preferences are correct
	block := blk.(*ProposalBlock)
	options := block.Options()
	commit, ok := blk.(*ProposalBlock).Options()[0].(*Commit)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	}
	abort, ok := options[1].(*Abort)
	if !ok {
		t.Fatal(errShouldPrefAbort)
	}

	if err := block.Verify(); err != nil {
		t.Fatal(err)
	}
	block.Accept()

	if err := commit.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := abort.Verify(); err != nil {
		t.Fatal(err)
	}

	if err := commit.Accept(); err != nil { // advance the timestamp
		t.Fatal(err)
	}

	// Verify that chain's timestamp has advanced
	timestamp, err := vm.getTimestamp(vm.DB)
	if err != nil {
		t.Fatal(err)
	}
	if !timestamp.Equal(defaultValidateEndTime) {
		t.Fatal("expected timestamp to have advanced")
	}

	blk, err = vm.BuildBlock() // should contain proposal to reward genesis validator
	if err != nil {
		t.Fatal(err)
	}

	// Assert preferences are correct
	block = blk.(*ProposalBlock)
	options = block.Options()
	commit, ok = blk.(*ProposalBlock).Options()[0].(*Commit)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	}
	abort, ok = options[1].(*Abort)
	if !ok {
		t.Fatal(errShouldPrefAbort)
	}

	if err := block.Verify(); err != nil {
		t.Fatal(err)
	}
	block.Accept()

	if err := commit.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := abort.Verify(); err != nil {
		t.Fatal(err)
	}

	if err := abort.Accept(); err != nil { // do not reward the genesis validator
		t.Fatal(err)
	}

	// Verify that genesis validator was removed from current validator set
	currentValidators, err := vm.getCurrentValidators(vm.DB, DefaultSubnetID)
	if err != nil {
		t.Fatal(err)
	}
	if currentValidators.Len() != len(keys)-1 {
		t.Fatal("should have removed a genesis validator")
	}
}

// Ensure BuildBlock errors when there is no block to build
func TestUnneededBuildBlock(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()
	if _, err := vm.BuildBlock(); err == nil {
		t.Fatalf("Should have errored on BuildBlock")
	}
}

// test acceptance of proposal to create a new chain
func TestCreateChain(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	tx, err := vm.newCreateChainTx(
		testSubnet1.id,
		nil,
		timestampvm.ID,
		nil,
		"name",
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	} else if err := vm.issueTx(tx); err != nil {
		t.Fatal(err)
	} else if blk, err := vm.BuildBlock(); err != nil { // should contain proposal to create chain
		t.Fatal(err)
	} else if err := blk.Verify(); err != nil {
		t.Fatal(err)
	} else if err := blk.Accept(); err != nil {
		t.Fatal(err)
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
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	nodeID := keys[0].PublicKey().Address()

	createSubnetTx, err := vm.newCreateSubnetTx(
		[]ids.ShortID{ // control keys
			keys[0].PublicKey().Address(),
			keys[1].PublicKey().Address(),
		},
		1,                                       // threshold
		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // payer
	)
	if err != nil {
		t.Fatal(err)
	} else if err := vm.issueTx(createSubnetTx); err != nil {
		t.Fatal(err)
	} else if blk, err := vm.BuildBlock(); err != nil { // should contain proposal to create subnet
		t.Fatal(err)
	} else if err := blk.Verify(); err != nil {
		t.Fatal(err)
	} else if err := blk.Accept(); err != nil {
		t.Fatal(err)
	} else if _, err := vm.getSubnet(vm.DB, createSubnetTx.ID()); err != nil {
		t.Fatal("should've created new subnet but didn't")
	}

	// Now that we've created a new subnet, add a validator to that subnet
	startTime := defaultValidateStartTime.Add(Delta).Add(1 * time.Second)
	endTime := startTime.Add(MinimumStakingDuration)
	// [startTime, endTime] is subset of time keys[0] validates default subent so tx is valid
	if addValidatorTx, err := vm.newAddNonDefaultSubnetValidatorTx(
		defaultWeight,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		nodeID,
		createSubnetTx.id,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	); err != nil {
		t.Fatal(err)
	} else if err := vm.issueTx(addValidatorTx); err != nil {
		t.Fatal(err)
	}

	blk, err := vm.BuildBlock() // should add validator to the new subnet
	if err != nil {
		t.Fatal(err)
	}

	// Assert preferences are correct
	// and accept the proposal/commit
	block := blk.(*ProposalBlock)
	options := block.Options()
	commit, ok := blk.(*ProposalBlock).Options()[0].(*Commit)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*Abort); !ok {
		t.Fatal(errShouldPrefAbort)
	} else if err := block.Verify(); err != nil { // Accept the block
		t.Fatal(err)
	} else if err := block.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Verify(); err != nil {
		t.Fatal(err)
	} else if err := commit.Accept(); err != nil { // add the validator to pending validator set
		t.Fatal(err)
	}

	// Verify validator is in pending validator set
	pendingValidators, err := vm.getPendingValidators(vm.DB, createSubnetTx.id)
	if err != nil {
		t.Fatal(err)
	}
	foundNewValidator := false
	for _, tx := range pendingValidators.Txs {
		if tx.Vdr().ID().Equals(nodeID) {
			foundNewValidator = true
			break
		}
	}
	if !foundNewValidator {
		t.Fatal("didn't add validator to new subnet's pending validator set")
	}

	// Advance time to when new validator should start validating
	// Create a block with an advance time tx that moves validator
	// from pending to current validator set
	vm.clock.Set(startTime)
	blk, err = vm.BuildBlock() // should be advance time tx
	if err != nil {
		t.Fatal(err)
	}

	// Assert preferences are correct
	// and accept the proposal/commit
	block = blk.(*ProposalBlock)
	options = block.Options()
	if commit, ok = blk.(*ProposalBlock).Options()[0].(*Commit); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*Abort); !ok {
		t.Fatal(errShouldPrefAbort)
	} else if err := block.Verify(); err != nil {
		t.Fatal(err)
	} else if err := block.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Verify(); err != nil {
		t.Fatal(err)
	} else if err := commit.Accept(); err != nil { // move validator addValidatorTx from pending to current
		t.Fatal(err)
	}

	// Verify validator no longer in pending validator set
	// Verify validator is in pending validator set
	if pendingValidators, err = vm.getPendingValidators(vm.DB, createSubnetTx.id); err != nil {
		t.Fatal(err)
	} else if pendingValidators.Len() != 0 {
		t.Fatal("pending validator set should be empty")
	}

	// Verify validator is in current validator set
	currentValidators, err := vm.getCurrentValidators(vm.DB, createSubnetTx.id)
	if err != nil {
		t.Fatal(err)
	}
	foundNewValidator = false
	for _, tx := range currentValidators.Txs {
		if tx.Vdr().ID().Equals(nodeID) {
			foundNewValidator = true
			break
		}
	}
	if !foundNewValidator {
		t.Fatal("didn't add validator to new subnet's current validator set")
	}

	// fast forward clock to time validator should stop validating
	vm.clock.Set(endTime)
	blk, err = vm.BuildBlock() // should be advance time tx
	if err != nil {
		t.Fatal(err)
	}

	// Assert preferences are correct
	// and accept the proposal/commit
	block = blk.(*ProposalBlock)
	options = block.Options()
	if commit, ok = blk.(*ProposalBlock).Options()[0].(*Commit); !ok {
		t.Fatal(errShouldPrefCommit)
	} else if abort, ok := options[1].(*Abort); !ok {
		t.Fatal(errShouldPrefAbort)
	} else if err := block.Verify(); err != nil {
		t.Fatal(err)
	} else if err := block.Accept(); err != nil {
		t.Fatal(err)
	} else if err := commit.Verify(); err != nil {
		t.Fatal(err)
	} else if err := abort.Verify(); err != nil {
		t.Fatal(err)
	} else if err := commit.Accept(); err != nil { // remove validator from current validator set
		t.Fatal(err)
	}
	// pending validators and current validator should be empty
	if pendingValidators, err = vm.getPendingValidators(vm.DB, createSubnetTx.id); err != nil {
		t.Fatal(err)
	} else if pendingValidators.Len() != 0 {
		t.Fatal("pending validator set should be empty")
	} else if currentValidators, err = vm.getCurrentValidators(vm.DB, createSubnetTx.id); err != nil {
		t.Fatal(err)
	} else if currentValidators.Len() != 0 {
		t.Fatal("pending validator set should be empty")
	}
}

// test asset import
func TestAtomicImport(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	avmID := ids.Empty.Prefix(0)
	vm.avm = avmID
	utxoID := ava.UTXOID{
		TxID:        ids.Empty.Prefix(1),
		OutputIndex: 1,
	}
	amount := uint64(50000)
	recipientKey := keys[1]

	sm := &atomic.SharedMemory{}
	sm.Initialize(logging.NoLog{}, prefixdb.New([]byte{0}, vm.DB.GetDatabase()))
	vm.Ctx.SharedMemory = sm.NewBlockchainSharedMemory(vm.Ctx.ChainID)

	if _, err := vm.newImportTx(
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		recipientKey,
	); err == nil {
		t.Fatalf("should have errored due to missing utxos")
	}

	// Provide the avm UTXO
	smDB := vm.Ctx.SharedMemory.GetDatabase(avmID)
	utxo := &ava.UTXO{
		UTXOID: utxoID,
		Asset:  ava.Asset{ID: avaxAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: amount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{recipientKey.PublicKey().Address()},
			},
		},
	}
	state := ava.NewPrefixedState(smDB, Codec)
	if err := state.FundAVMUTXO(utxo); err != nil {
		t.Fatal(err)
	}
	vm.Ctx.SharedMemory.ReleaseDatabase(avmID)

	tx, err := vm.newImportTx(
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		recipientKey,
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.issueTx(tx); err != nil {
		t.Fatal(err)
	} else if blk, err := vm.BuildBlock(); err != nil {
		t.Fatal(err)
	} else if err := blk.Verify(); err != nil {
		t.Fatal(err)
	} else if err := blk.Accept(); err != nil {
		t.Fatal(err)
	}

	smDB = vm.Ctx.SharedMemory.GetDatabase(avmID)
	defer vm.Ctx.SharedMemory.ReleaseDatabase(avmID)
	state = ava.NewPrefixedState(smDB, vm.codec)
	if _, err := state.AVMUTXO(utxoID.InputID()); err == nil {
		t.Fatalf("shouldn't have been able to read the utxo")
	}
}

/* TODO I don't know what this is supposed to test but it's broken.
   Should we keep this?
// test optimistic asset import
func TestOptimisticAtomicImport(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	avmID := ids.Empty.Prefix(0)
	utxoID := ava.UTXOID{
		TxID:        ids.Empty.Prefix(1),
		OutputIndex: 1,
	}
	assetID := ids.Empty.Prefix(2)
	amount := uint64(50000)
	key := keys[0]

	sm := &atomic.SharedMemory{}
	sm.Initialize(logging.NoLog{}, prefixdb.New([]byte{0}, vm.DB.GetDatabase()))

	vm.Ctx.SharedMemory = sm.NewBlockchainSharedMemory(vm.Ctx.ChainID)

	tx, err := vm.newImportTx(
		defaultNonce+1,
		testNetworkID,
		[]*ava.TransferableInput{&ava.TransferableInput{
			UTXOID: utxoID,
			Asset:  ava.Asset{ID: assetID},
			In: &secp256k1fx.TransferInput{
				Amt:   amount,
				Input: secp256k1fx.Input{SigIndices: []uint32{0}},
			},
		}},
		[][]*crypto.PrivateKeySECP256K1R{[]*crypto.PrivateKeySECP256K1R{key}},
		key,
	)
	if err != nil {
		t.Fatal(err)
	}

	vm.ava = assetID
	vm.avm = avmID

	blk, err := vm.newAtomicBlock(vm.Preferred(), tx)
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(); err == nil {
		t.Fatalf("should have errored due to an invalid atomic utxo")
	}

	previousAccount, err := vm.getAccount(vm.DB, key.PublicKey().Address())
	if err != nil {
		t.Fatal(err)
	}

	blk.Accept()

	newAccount, err := vm.getAccount(vm.DB, key.PublicKey().Address())
	if err != nil {
		t.Fatal(err)
	}

	if newAccount.Balance != previousAccount.Balance+amount {
		t.Fatalf("failed to provide funds")
	}
}
*/

// test restarting the node
func TestRestartPartiallyAccepted(t *testing.T) {
	_, genesisBytes := defaultGenesis()
	db := memdb.New()

	firstVM := &VM{
		SnowmanVM:    &core.SnowmanVM{},
		chainManager: chains.MockManager{},
	}
	firstDefaultSubnet := validators.NewSet()
	firstVM.validators = validators.NewManager()
	firstVM.validators.PutValidatorSet(DefaultSubnetID, firstDefaultSubnet)
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
	firstAdvanceTimeBlk, err := firstVM.newProposalBlock(firstVM.Preferred(), firstVM.preferredHeight()+1, firstAdvanceTimeTx)
	if err != nil {
		t.Fatal(err)
	}

	firstVM.clock.Set(defaultGenesisTime.Add(3 * time.Second))
	if err := firstAdvanceTimeBlk.Verify(); err != nil {
		t.Fatal(err)
	}

	options := firstAdvanceTimeBlk.Options()
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
	secondAdvanceTimeBlkBytes := []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 175, 165,
		179, 18, 84, 54, 209, 73, 77, 66, 108, 26, 59, 20, 86, 210, 143, 238, 39, 220,
		52, 243, 166, 149, 166, 139, 210, 93, 199, 143, 58, 199, 0, 0, 0, 25, 0, 0, 0,
		0, 95, 12, 157, 133,
	}
	if _, err := firstVM.ParseBlock(secondAdvanceTimeBlkBytes); err != nil {
		t.Fatal(err)
	}

	firstVM.Shutdown()
	firstCtx.Lock.Unlock()

	secondVM := &VM{
		SnowmanVM:    &core.SnowmanVM{},
		chainManager: chains.MockManager{},
	}

	secondDefaultSubnet := validators.NewSet()
	secondVM.validators = validators.NewManager()
	secondVM.validators.PutValidatorSet(DefaultSubnetID, secondDefaultSubnet)

	secondVM.clock.Set(defaultGenesisTime)
	secondCtx := defaultContext()
	secondCtx.Lock.Lock()
	defer func() {
		secondVM.Shutdown()
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
		SnowmanVM:    &core.SnowmanVM{},
		chainManager: chains.MockManager{},
	}

	firstDefaultSubnet := validators.NewSet()
	firstVM.validators = validators.NewManager()
	firstVM.validators.PutValidatorSet(DefaultSubnetID, firstDefaultSubnet)

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
	firstAdvanceTimeBlk, err := firstVM.newProposalBlock(firstVM.Preferred(), firstVM.preferredHeight()+1, firstAdvanceTimeTx)
	if err != nil {
		t.Fatal(err)
	}
	firstVM.clock.Set(defaultGenesisTime.Add(3 * time.Second))
	if err := firstAdvanceTimeBlk.Verify(); err != nil {
		t.Fatal(err)
	}
	options := firstAdvanceTimeBlk.Options()
	if err := options[0].Verify(); err != nil {
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

	/* This code, when uncommented, prints [secondAdvanceTimeBlkBytes]
	secondAdvanceTimeTx, err := firstVM.newAdvanceTimeTx(defaultGenesisTime.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	secondAdvanceTimeBlk, err := firstVM.newProposalBlock(firstVM.Preferred(), firstVM.preferredHeight()+1, secondAdvanceTimeTx)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatal(secondAdvanceTimeBlk.Bytes())
	*/

	// Byte representation of block that proposes advancing time to defaultGenesisTime + 2 seconds
	secondAdvanceTimeBlkBytes := []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 175, 165,
		179, 18, 84, 54, 209, 73, 77, 66, 108, 26, 59, 20, 86, 210, 143, 238, 39, 220,
		52, 243, 166, 149, 166, 139, 210, 93, 199, 143, 58, 199, 0, 0, 0, 25, 0, 0, 0,
		0, 95, 12, 157, 133,
	}
	if _, err := firstVM.ParseBlock(secondAdvanceTimeBlkBytes); err != nil {
		t.Fatal(err)
	}

	firstVM.Shutdown()
	firstCtx.Lock.Unlock()

	secondVM := &VM{
		SnowmanVM:    &core.SnowmanVM{},
		chainManager: chains.MockManager{},
	}

	secondDefaultSubnet := validators.NewSet()
	secondVM.validators = validators.NewManager()
	secondVM.validators.PutValidatorSet(DefaultSubnetID, secondDefaultSubnet)

	secondVM.clock.Set(defaultGenesisTime)
	secondCtx := defaultContext()
	secondCtx.Lock.Lock()
	defer func() {
		secondVM.Shutdown()
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
		SnowmanVM:    &core.SnowmanVM{},
		chainManager: chains.MockManager{},
	}

	defaultSubnet := validators.NewSet()
	vm.validators = validators.NewManager()
	vm.validators.PutValidatorSet(DefaultSubnetID, defaultSubnet)

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
	advanceTimeBlk, err := vm.newProposalBlock(vm.Preferred(), vm.preferredHeight()+1, advanceTimeTx)
	if err != nil {
		t.Fatal(err)
	}
	advanceTimeBlkID := advanceTimeBlk.ID()
	advanceTimeBlkBytes := advanceTimeBlk.Bytes()

	advanceTimePreference := advanceTimeBlk.Options()[0]

	peerID := ids.NewShortID([20]byte{1, 2, 3, 4, 5, 4, 3, 2, 1})
	vdrs := validators.NewSet()
	vdrs.Add(validators.NewValidator(peerID, 1))
	beacons := vdrs

	timeoutManager := timeout.Manager{}
	timeoutManager.Initialize("", prometheus.NewRegistry())
	go timeoutManager.Dispatch()

	chainRouter := &router.ChainRouter{}
	chainRouter.Initialize(logging.NoLog{}, &timeoutManager, time.Hour, time.Second)

	externalSender := &sender.ExternalSenderTest{T: t}
	externalSender.Default(true)

	// Passes messages from the consensus engine to the network
	sender := sender.Sender{}

	sender.Initialize(ctx, externalSender, chainRouter, &timeoutManager)

	// The engine handles consensus
	engine := smeng.Transitive{}
	engine.Initialize(smeng.Config{
		Config: bootstrap.Config{
			Config: common.Config{
				Context:    ctx,
				Validators: vdrs,
				Beacons:    beacons,
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

	// Asynchronously passes messages from the network to the consensus engine
	handler := &router.Handler{}
	handler.Initialize(
		&engine,
		msgChan,
		1000,
		"",
		prometheus.NewRegistry(),
	)

	// Allow incoming messages to be routed to the new chain
	chainRouter.AddChain(handler)
	go ctx.Log.RecoverAndPanic(handler.Dispatch)

	reqID := new(uint32)
	externalSender.GetAcceptedFrontierF = func(_ ids.ShortSet, _ ids.ID, requestID uint32, _ time.Time) {
		*reqID = requestID
	}

	engine.Startup()

	externalSender.GetAcceptedFrontierF = nil
	externalSender.GetAcceptedF = func(_ ids.ShortSet, _ ids.ID, requestID uint32, _ time.Time, _ ids.Set) {
		*reqID = requestID
	}

	frontier := ids.Set{}
	frontier.Add(advanceTimeBlkID)
	engine.AcceptedFrontier(peerID, *reqID, frontier)

	externalSender.GetAcceptedF = nil
	externalSender.GetAncestorsF = func(_ ids.ShortID, _ ids.ID, requestID uint32, _ time.Time, containerID ids.ID) {
		*reqID = requestID
		if !containerID.Equals(advanceTimeBlkID) {
			t.Fatalf("wrong block requested")
		}
	}

	engine.Accepted(peerID, *reqID, frontier)

	externalSender.GetF = nil
	externalSender.CantPushQuery = false
	externalSender.CantPullQuery = false

	engine.MultiPut(peerID, *reqID, [][]byte{advanceTimeBlkBytes})

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
		SnowmanVM:    &core.SnowmanVM{},
		chainManager: chains.MockManager{},
	}

	defaultSubnet := validators.NewSet()
	vm.validators = validators.NewManager()
	vm.validators.PutValidatorSet(DefaultSubnetID, defaultSubnet)

	vm.clock.Set(defaultGenesisTime)
	ctx := defaultContext()
	ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
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
	firstAdvanceTimeBlk, err := vm.newProposalBlock(vm.Preferred(), vm.preferredHeight()+1, firstAdvanceTimeTx)
	if err != nil {
		t.Fatal(err)
	}

	vm.clock.Set(defaultGenesisTime.Add(2 * time.Second))
	if err := firstAdvanceTimeBlk.Verify(); err != nil {
		t.Fatal(err)
	}

	options := firstAdvanceTimeBlk.Options()
	firstOption := options[0]
	secondOption := options[1]

	secondAdvanceTimeTx, err := vm.newAdvanceTimeTx(defaultGenesisTime.Add(2 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	secondAdvanceTimeBlk, err := vm.newProposalBlock(firstOption.ID(), firstOption.(Block).Height()+1, secondAdvanceTimeTx)
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
	vm := &VM{}
	if _, err := vm.ParseAddress("P-Bg6e45gxCUTLXcfUuoy3go2U6V3bRZ5jH"); err != nil {
		t.Fatal(err)
	}
}

func TestParseAddressInvalid(t *testing.T) {
	vm := &VM{}
	tests := []struct {
		in   string
		want error
	}{
		{"", errEmptyAddress},
		{"+", errInvalidAddressSeperator},
		{"P", errInvalidAddressSeperator},
		{"-", errEmptyAddressPrefix},
		{"P-", errEmptyAddressSuffix},
		{"X-Bg6e45gxCUTLXcfUuoy3go2U6V3bRZ5jH", errInvalidAddressPrefix},
		{"P-Bg6e45gxCUTLXcfUuoy", errInvalidAddress}, //truncated
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			_, err := vm.ParseAddress(tt.in)
			if !errors.Is(err, tt.want) {
				t.Errorf("want %q, got %q", tt.want, err)
			}
		})
	}
}

func TestFormatAddress(t *testing.T) {
	vm := &VM{}
	tests := []struct {
		label string
		in    ids.ShortID
		want  string
	}{
		{"keys[0]", keys[0].PublicKey().Address(), "P-Q4MzFZZDPHRPAHFeDs3NiyyaZDvxHKivf"},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			if addrStr := vm.FormatAddress(tt.in); addrStr != tt.want {
				t.Errorf("want %q, got %q", tt.want, addrStr)
			}
		})
	}
}
