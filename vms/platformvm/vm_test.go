// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"bytes"
	"errors"
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
	"github.com/ava-labs/gecko/snow/networking/router"
	"github.com/ava-labs/gecko/snow/networking/sender"
	"github.com/ava-labs/gecko/snow/networking/timeout"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/components/core"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
	"github.com/ava-labs/gecko/vms/timestampvm"

	smcon "github.com/ava-labs/gecko/snow/consensus/snowman"
	smeng "github.com/ava-labs/gecko/snow/engine/snowman"
)

var (
	// chain timestamp at genesis
	defaultGenesisTime = time.Now().Round(time.Second)

	// time that genesis validators start validating
	defaultValidateStartTime = defaultGenesisTime.Add(1 * time.Second)

	// time that genesis validators stop validating
	defaultValidateEndTime = defaultValidateStartTime.Add(10 * MinimumStakingDuration)

	// each key corresponds to an account that has $AVA and a genesis validator
	keys []*crypto.PrivateKeySECP256K1R

	// amount all genesis validators stake in defaultVM
	defaultStakeAmount uint64

	// balance of accounts that exist at genesis in defaultVM
	defaultBalance = 100 * MinimumStakeAmount

	// At genesis this account has AVA and is validating the default subnet
	defaultKey *crypto.PrivateKeySECP256K1R

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

	defaultNonce  = 1
	defaultWeight = 1
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

	defaultStakeAmount = defaultBalance - txFee

	defaultKey = keys[0]

	testSubnet1ControlKeys = keys[0:3]

}

func defaultContext() *snow.Context {
	ctx := snow.DefaultContextTest()
	ctx.NetworkID = testNetworkID
	return ctx
}

func defaultVM() *VM {
	genesisAccounts := GenesisAccounts()
	genesisValidators := GenesisCurrentValidators()
	genesisChains := make([]*CreateChainTx, 0)

	genesisState := Genesis{
		Accounts:   genesisAccounts,
		Validators: genesisValidators,
		Chains:     genesisChains,
		Timestamp:  uint64(defaultGenesisTime.Unix()),
	}

	genesisBytes, err := Codec.Marshal(genesisState)
	if err != nil {
		panic(err)
	}

	vm := &VM{
		SnowmanVM:    &core.SnowmanVM{},
		chainManager: chains.MockManager{},
	}

	defaultSubnet := validators.NewSet()
	vm.validators = validators.NewManager()
	vm.validators.PutValidatorSet(DefaultSubnetID, defaultSubnet)

	vm.clock.Set(defaultGenesisTime)
	db := prefixdb.New([]byte{0}, memdb.New())
	msgChan := make(chan common.Message, 1)
	ctx := defaultContext()
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()
	if err := vm.Initialize(ctx, db, genesisBytes, msgChan, nil); err != nil {
		panic(err)
	}

	// Create 1 non-default subnet and store it in testSubnet1
	tx, err := vm.newCreateSubnetTx(
		testNetworkID,
		0,
		[]ids.ShortID{keys[0].PublicKey().Address(), keys[1].PublicKey().Address(), keys[2].PublicKey().Address()}, // control keys are keys[0], keys[1], keys[2]
		2, // threshold; 2 sigs from keys[0], keys[1], keys[2] needed to add validator to this subnet
		keys[0],
	)
	if err != nil {
		panic(err)
	}
	if testSubnet1 == nil {
		testSubnet1 = tx
	}
	if err := vm.putSubnets(vm.DB, []*CreateSubnetTx{tx}); err != nil {
		panic(err)
	}
	err = vm.putCurrentValidators(
		vm.DB,
		&EventHeap{
			SortByStartTime: false,
		},
		tx.id,
	)
	if err != nil {
		panic(err)
	}
	err = vm.putPendingValidators(
		vm.DB,
		&EventHeap{
			SortByStartTime: true,
		},
		tx.id,
	)
	if err != nil {
		panic(err)
	}

	subnets, err := vm.getSubnets(vm.DB)
	if err != nil {
		panic(err)
	}
	if len(subnets) == 0 {
		panic("no subnets found")
	} // end delete

	vm.registerDBTypes()

	return vm
}

// The returned accounts have nil for their vm field
func GenesisAccounts() []Account {
	accounts := []Account(nil)
	for _, key := range keys {
		accounts = append(accounts,
			newAccount(
				key.PublicKey().Address(), // address
				defaultNonce,              // nonce
				defaultBalance,            // balance
			))
	}
	return accounts
}

// Returns the validators validating at genesis in tests
func GenesisCurrentValidators() *EventHeap {
	vm := &VM{}
	validators := &EventHeap{SortByStartTime: false}
	for _, key := range keys {
		validator, _ := vm.newAddDefaultSubnetValidatorTx(
			defaultNonce,                            // nonce
			defaultStakeAmount,                      // weight
			uint64(defaultValidateStartTime.Unix()), // start time
			uint64(defaultValidateEndTime.Unix()),   // end time
			key.PublicKey().Address(),               // nodeID
			key.PublicKey().Address(),               // destination
			NumberOfShares,                          // shares
			testNetworkID,                           // network ID
			key,                                     // key paying tx fee and stake
		)
		validators.Add(validator)
	}
	return validators
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
	genesisBlock, err := vm.getBlock(genesisBlockID)
	if err != nil {
		t.Fatalf("couldn't get genesis block: %v", err)
	}
	if genesisBlock.Status() != choices.Accepted {
		t.Fatal("genesis block should be accepted")
	}

	// Ensure all the genesis accounts are stored
	for _, account := range GenesisAccounts() {
		vmAccount, err := vm.getAccount(vm.DB, account.Address)
		if err != nil {
			t.Fatal("couldn't find account in vm's db")
		}
		if !vmAccount.Address.Equals(account.Address) {
			t.Fatal("account IDs should match")
		}
		if vmAccount.Balance != account.Balance {
			t.Fatal("balances should match")
		}
		if vmAccount.Nonce != account.Nonce {
			t.Fatal("nonces should match")
		}
	}

	// Ensure current validator set of default subnet is correct
	currentValidators, err := vm.getCurrentValidators(vm.DB, DefaultSubnetID)
	if err != nil {
		t.Fatal(err)
	}
	if len(currentValidators.Txs) != len(keys) {
		t.Fatal("vm's current validator set is wrong")
	}
	if currentValidators.SortByStartTime == true {
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
	pendingValidators, err := vm.getPendingValidators(vm.DB, DefaultSubnetID)
	if err != nil {
		t.Fatal(err)
	}
	if pendingValidators.Len() != 0 {
		t.Fatal("vm's pending validator set should be empty")
	}

	// Ensure genesis timestamp is correct
	time, err := vm.getTimestamp(vm.DB)
	if err != nil {
		t.Fatal(err)
	}
	if !time.Equal(defaultGenesisTime) {
		t.Fatalf("vm's time is incorrect. Expected %s got %s", defaultGenesisTime, time)
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
	key, _ := vm.factory.NewPrivateKey()
	ID := key.PublicKey().Address()

	// create valid tx
	tx, err := vm.newAddDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultStakeAmount,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		ID,
		ID,
		NumberOfShares,
		testNetworkID,
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}

	// trigger block creation
	vm.unissuedEvents.Add(tx)
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
	commit.Accept() // commit the proposal

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
		defaultNonce+1,
		defaultStakeAmount,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		ID,
		ID,
		NumberOfShares,
		testNetworkID,
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}

	blk, err := vm.newProposalBlock(vm.LastAccepted(), tx)
	if err != nil {
		t.Fatal(err)
	}
	if err := vm.State.PutBlock(vm.DB, blk); err != nil {
		t.Fatal(err)
	}
	if err := vm.DB.Commit(); err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(); err == nil {
		t.Fatalf("Should have errored during verification")
	}

	if status := blk.Status(); status != choices.Rejected {
		t.Fatalf("Should have marked the block as rejected")
	}

	parsedBlk, err := vm.GetBlock(blk.ID())
	if err != nil {
		t.Fatal(err)
	}

	if status := parsedBlk.Status(); status != choices.Rejected {
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
		defaultNonce+1,
		defaultStakeAmount,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		ID,
		ID,
		NumberOfShares,
		testNetworkID,
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}

	// trigger block creation
	vm.unissuedEvents.Add(tx)
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
	}
	if err := abort.Verify(); err != nil { // should pass verification
		t.Fatal(err)
	}

	abort.Accept() // reject the proposal

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
		defaultNonce+1,
		defaultWeight,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		keys[0].PublicKey().Address(),
		testSubnet1.id,
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		keys[0],
	)
	if err != nil {
		t.Fatal(err)
	}

	// trigger block creation
	vm.unissuedEvents.Add(tx)
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

	if err := commit.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := abort.Verify(); err != nil {
		t.Fatal(err)
	}

	commit.Accept() // accept the proposal

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
		defaultNonce+1,
		defaultWeight,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		keys[0].PublicKey().Address(),
		testSubnet1.id,
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[1], testSubnet1ControlKeys[2]},
		keys[0],
	)
	if err != nil {
		t.Fatal(err)
	}

	// trigger block creation
	vm.unissuedEvents.Add(tx)
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

	if err := commit.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := abort.Verify(); err != nil {
		t.Fatal(err)
	}

	abort.Accept() // reject the proposal

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

	commit.Accept() // advance the timestamp

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

	commit.Accept() // reward the genesis validator

	// Verify that genesis validator was rewarded and removed from current validator set
	currentValidators, err := vm.getCurrentValidators(vm.DB, DefaultSubnetID)
	if err != nil {
		t.Fatal(err)
	}
	if currentValidators.Len() != len(keys)-1 {
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

	commit.Accept() // advance the timestamp

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

	abort.Accept() // do not reward the genesis validator

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
		defaultNonce+1,
		testSubnet1.id,
		nil,
		timestampvm.ID,
		nil,
		"name",
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		keys[0],
	)
	if err != nil {
		t.Fatal(err)
	}

	vm.unissuedDecisionTxs = append(vm.unissuedDecisionTxs, tx)
	blk, err := vm.BuildBlock() // should contain proposal to create chain
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	blk.Accept()

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

	// Verify tx fee was deducted
	account, err := vm.getAccount(vm.DB, tx.PayerAddress)
	if err != nil {
		t.Fatal(err)
	}
	if account.Balance != defaultBalance-txFee {
		t.Fatal("should have deducted txFee from balance")
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

	createSubnetTx, err := vm.newCreateSubnetTx(
		testNetworkID,
		defaultNonce+1,
		[]ids.ShortID{
			keys[0].PublicKey().Address(),
			keys[1].PublicKey().Address(),
		},
		1,       // threshold
		keys[0], // payer
	)
	if err != nil {
		t.Fatal(err)
	}

	vm.unissuedDecisionTxs = append(vm.unissuedDecisionTxs, createSubnetTx)
	blk, err := vm.BuildBlock() // should contain proposal to create subnet
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	blk.Accept()

	// Verify new subnet was created
	subnets, err := vm.getSubnets(vm.DB)
	if err != nil {
		t.Fatal(err)
	}
	foundNewSubnet := false
	for _, subnet := range subnets {
		if bytes.Equal(subnet.Bytes(), createSubnetTx.Bytes()) {
			foundNewSubnet = true
		}
	}
	if !foundNewSubnet {
		t.Fatal("should've created new subnet but didn't")
	}

	// Verify tx fee was deducted
	account, err := vm.getAccount(vm.DB, createSubnetTx.key.Address())
	if err != nil {
		t.Fatal(err)
	}
	if account.Balance != defaultBalance-txFee {
		t.Fatal("should have deducted txFee from balance")
	}

	// Now that we've created a new subnet, add a validator to that subnet
	startTime := defaultValidateStartTime.Add(Delta).Add(1 * time.Second)
	endTime := startTime.Add(MinimumStakingDuration)
	// [startTime, endTime] is subset of time keys[0] validates default subent so tx is valid
	addValidatorTx, err := vm.newAddNonDefaultSubnetValidatorTx(
		defaultNonce+2,
		defaultWeight,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		keys[0].PublicKey().Address(),
		createSubnetTx.id,
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		keys[0],
	)
	if err != nil {
		t.Fatal(err)
	}

	// Verify tx is valid
	_, _, _, _, err = addValidatorTx.SemanticVerify(vm.DB)
	if err != nil {
		t.Fatal(err)
	}

	vm.unissuedEvents.Add(addValidatorTx)
	blk, err = vm.BuildBlock() // should add validator to the new subnet
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
	}
	abort, ok := options[1].(*Abort)
	if !ok {
		t.Fatal(errShouldPrefAbort)
	}

	// Accept the block
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
	commit.Accept() // add the validator to pending validator set

	// Verify validator is in pending validator set
	pendingValidators, err := vm.getPendingValidators(vm.DB, createSubnetTx.id)
	if err != nil {
		t.Fatal(err)
	}
	foundNewValidator := false
	for _, tx := range pendingValidators.Txs {
		if tx.ID().Equals(addValidatorTx.ID()) {
			foundNewValidator = true
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
	commit, ok = blk.(*ProposalBlock).Options()[0].(*Commit)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	}
	abort, ok = options[1].(*Abort)
	if !ok {
		t.Fatal(errShouldPrefAbort)
	}

	// Accept the block
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
	commit.Accept() // move validator addValidatorTx from pending to current

	// Verify validator no longer in pending validator set
	// Verify validator is in pending validator set
	pendingValidators, err = vm.getPendingValidators(vm.DB, createSubnetTx.id)
	if err != nil {
		t.Fatal(err)
	}
	if pendingValidators.Len() != 0 {
		t.Fatal("pending validator set should be empty")
	}

	// Verify validator is in current validator set
	currentValidators, err := vm.getCurrentValidators(vm.DB, createSubnetTx.id)
	if err != nil {
		t.Fatal(err)
	}
	foundNewValidator = false
	for _, tx := range currentValidators.Txs {
		if tx.ID().Equals(addValidatorTx.ID()) {
			foundNewValidator = true
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
	commit, ok = blk.(*ProposalBlock).Options()[0].(*Commit)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	}
	abort, ok = options[1].(*Abort)
	if !ok {
		t.Fatal(errShouldPrefAbort)
	}

	// Accept the block
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
	commit.Accept() // remove validator from current validator set

	// pending validators and current validator should be empty
	pendingValidators, err = vm.getPendingValidators(vm.DB, createSubnetTx.id)
	if err != nil {
		t.Fatal(err)
	}
	if pendingValidators.Len() != 0 {
		t.Fatal("pending validator set should be empty")
	}
	currentValidators, err = vm.getCurrentValidators(vm.DB, createSubnetTx.id)
	if err != nil {
		t.Fatal(err)
	}
	if currentValidators.Len() != 0 {
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

	vm.unissuedAtomicTxs = append(vm.unissuedAtomicTxs, tx)
	if _, err := vm.BuildBlock(); err == nil {
		t.Fatalf("should have errored due to missing utxos")
	}

	// Provide the avm UTXO:

	smDB := vm.Ctx.SharedMemory.GetDatabase(avmID)

	utxo := &ava.UTXO{
		UTXOID: utxoID,
		Asset:  ava.Asset{ID: assetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: amount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{key.PublicKey().Address()},
			},
		},
	}

	state := ava.NewPrefixedState(smDB, Codec)
	if err := state.FundAVMUTXO(utxo); err != nil {
		t.Fatal(err)
	}

	vm.Ctx.SharedMemory.ReleaseDatabase(avmID)

	vm.unissuedAtomicTxs = append(vm.unissuedAtomicTxs, tx)
	blk, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	blk.Accept()

	smDB = vm.Ctx.SharedMemory.GetDatabase(avmID)
	defer vm.Ctx.SharedMemory.ReleaseDatabase(avmID)

	state = ava.NewPrefixedState(smDB, vm.codec)
	if _, err := state.AVMUTXO(utxoID.InputID()); err == nil {
		t.Fatalf("shouldn't have been able to read the utxo")
	}
}

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

// test restarting the node
func TestRestartPartiallyAccepted(t *testing.T) {
	genesisAccounts := GenesisAccounts()
	genesisValidators := GenesisCurrentValidators()
	genesisChains := make([]*CreateChainTx, 0)

	genesisState := Genesis{
		Accounts:   genesisAccounts,
		Validators: genesisValidators,
		Chains:     genesisChains,
		Timestamp:  uint64(defaultGenesisTime.Unix()),
	}

	genesisBytes, err := Codec.Marshal(genesisState)
	if err != nil {
		t.Fatal(err)
	}

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
	firstAdvanceTimeBlk, err := firstVM.newProposalBlock(firstVM.Preferred(), firstAdvanceTimeTx)
	if err != nil {
		t.Fatal(err)
	}

	firstVM.clock.Set(defaultGenesisTime.Add(2 * time.Second))
	if err := firstAdvanceTimeBlk.Verify(); err != nil {
		t.Fatal(err)
	}

	options := firstAdvanceTimeBlk.Options()
	firstOption := options[0]
	secondOption := options[1]

	if err := firstOption.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := secondOption.Verify(); err != nil {
		t.Fatal(err)
	}

	firstAdvanceTimeBlk.Accept()

	secondAdvanceTimeBlkBytes := []byte{
		0x00, 0x00, 0x00, 0x00, 0xad, 0x64, 0x34, 0x49,
		0xa5, 0x05, 0xd8, 0xda, 0xc6, 0xd1, 0xb8, 0x2c,
		0x5c, 0xe6, 0x06, 0x81, 0xf3, 0x54, 0xbf, 0x0f,
		0xf7, 0xc4, 0xb1, 0xc2, 0xa9, 0x6e, 0x92, 0xc1,
		0xd8, 0xd8, 0xf0, 0xce, 0x00, 0x00, 0x00, 0x18,
		0x00, 0x00, 0x00, 0x00, 0x5e, 0xa7, 0xbc, 0x7c,
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
	genesisAccounts := GenesisAccounts()
	genesisValidators := GenesisCurrentValidators()
	genesisChains := make([]*CreateChainTx, 0)

	genesisState := Genesis{
		Accounts:   genesisAccounts,
		Validators: genesisValidators,
		Chains:     genesisChains,
		Timestamp:  uint64(defaultGenesisTime.Unix()),
	}

	genesisBytes, err := Codec.Marshal(genesisState)
	if err != nil {
		t.Fatal(err)
	}

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
	firstAdvanceTimeBlk, err := firstVM.newProposalBlock(firstVM.Preferred(), firstAdvanceTimeTx)
	if err != nil {
		t.Fatal(err)
	}

	firstVM.clock.Set(defaultGenesisTime.Add(2 * time.Second))
	if err := firstAdvanceTimeBlk.Verify(); err != nil {
		t.Fatal(err)
	}

	options := firstAdvanceTimeBlk.Options()
	firstOption := options[0]
	secondOption := options[1]

	if err := firstOption.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := secondOption.Verify(); err != nil {
		t.Fatal(err)
	}

	firstAdvanceTimeBlk.Accept()
	firstOption.Accept()
	secondOption.Reject()

	secondAdvanceTimeBlkBytes := []byte{
		0x00, 0x00, 0x00, 0x00, 0xad, 0x64, 0x34, 0x49,
		0xa5, 0x05, 0xd8, 0xda, 0xc6, 0xd1, 0xb8, 0x2c,
		0x5c, 0xe6, 0x06, 0x81, 0xf3, 0x54, 0xbf, 0x0f,
		0xf7, 0xc4, 0xb1, 0xc2, 0xa9, 0x6e, 0x92, 0xc1,
		0xd8, 0xd8, 0xf0, 0xce, 0x00, 0x00, 0x00, 0x18,
		0x00, 0x00, 0x00, 0x00, 0x5e, 0xa7, 0xbc, 0x7c,
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

	if lastAccepted := secondVM.LastAccepted(); !firstOption.ID().Equals(lastAccepted) {
		t.Fatalf("Should have changed the genesis")
	}
}

// test bootstrapping the node
func TestBootstrapPartiallyAccepted(t *testing.T) {
	genesisAccounts := GenesisAccounts()
	genesisValidators := GenesisCurrentValidators()
	genesisChains := make([]*CreateChainTx, 0)

	genesisState := Genesis{
		Accounts:   genesisAccounts,
		Validators: genesisValidators,
		Chains:     genesisChains,
		Timestamp:  uint64(defaultGenesisTime.Unix()),
	}

	genesisBytes, err := Codec.Marshal(genesisState)
	if err != nil {
		t.Fatal(err)
	}

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
	advanceTimeBlk, err := vm.newProposalBlock(vm.Preferred(), advanceTimeTx)
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
	timeoutManager.Initialize(2 * time.Second)
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
		BootstrapConfig: smeng.BootstrapConfig{
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
	externalSender.GetAcceptedFrontierF = func(_ ids.ShortSet, _ ids.ID, requestID uint32) {
		*reqID = requestID
	}

	engine.Startup()

	externalSender.GetAcceptedFrontierF = nil
	externalSender.GetAcceptedF = func(_ ids.ShortSet, _ ids.ID, requestID uint32, _ ids.Set) {
		*reqID = requestID
	}

	frontier := ids.Set{}
	frontier.Add(advanceTimeBlkID)
	engine.AcceptedFrontier(peerID, *reqID, frontier)

	externalSender.GetAcceptedF = nil
	externalSender.GetAncestorsF = func(_ ids.ShortID, _ ids.ID, requestID uint32, containerID ids.ID) {
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
	genesisAccounts := GenesisAccounts()
	genesisValidators := GenesisCurrentValidators()
	genesisChains := make([]*CreateChainTx, 0)

	genesisState := Genesis{
		Accounts:   genesisAccounts,
		Validators: genesisValidators,
		Chains:     genesisChains,
		Timestamp:  uint64(defaultGenesisTime.Unix()),
	}

	genesisBytes, err := Codec.Marshal(genesisState)
	if err != nil {
		t.Fatal(err)
	}

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
	firstAdvanceTimeBlk, err := vm.newProposalBlock(vm.Preferred(), firstAdvanceTimeTx)
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
	secondAdvanceTimeBlk, err := vm.newProposalBlock(firstOption.ID(), secondAdvanceTimeTx)
	if err != nil {
		t.Fatal(err)
	}

	parentBlk := secondAdvanceTimeBlk.Parent()
	if parentBlkID := parentBlk.ID(); !parentBlkID.Equals(firstOption.ID()) {
		t.Fatalf("Wrong parent block ID returned")
	}

	if err := firstOption.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := secondOption.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := secondAdvanceTimeBlk.Verify(); err != nil {
		t.Fatal(err)
	}
}
