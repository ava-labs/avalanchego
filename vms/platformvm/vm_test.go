// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"bytes"
	"container/heap"
	"errors"
	"testing"
	"time"

	"github.com/ava-labs/gecko/chains"

	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/vms/components/core"
	"github.com/ava-labs/gecko/vms/timestampvm"
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
		ChainManager: chains.MockManager{},
	}

	defaultSubnet := validators.NewSet()
	vm.Validators = validators.NewManager()
	vm.Validators.PutValidatorSet(DefaultSubnetID, defaultSubnet)

	vm.clock.Set(defaultGenesisTime)
	db := memdb.New()
	msgChan := make(chan common.Message, 1)
	ctx := defaultContext()
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
		heap.Push(validators, validator)
	}
	return validators
}

// Ensure genesis state is parsed from bytes and stored correctly
func TestGenesis(t *testing.T) {
	vm := defaultVM()

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
	vm.Ctx.Lock.Lock()
	blk, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}
	vm.Ctx.Lock.Unlock()

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

// Reject proposal to add validator to default subnet
func TestAddDefaultSubnetValidatorReject(t *testing.T) {
	vm := defaultVM()
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
	vm.Ctx.Lock.Lock()
	blk, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}
	vm.Ctx.Lock.Unlock()

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
	vm.Ctx.Lock.Lock()
	blk, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}
	vm.Ctx.Lock.Unlock()

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
	vm.Ctx.Lock.Lock()
	blk, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}
	vm.Ctx.Lock.Unlock()

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

	// Fast forward clock to time for genesis validators to leave
	vm.clock.Set(defaultValidateEndTime)

	vm.Ctx.Lock.Lock()
	blk, err := vm.BuildBlock() // should contain proposal to advance time
	if err != nil {
		t.Fatal(err)
	}
	vm.Ctx.Lock.Unlock()

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

	vm.Ctx.Lock.Lock()
	blk, err = vm.BuildBlock() // should contain proposal to reward genesis validator
	if err != nil {
		t.Fatal(err)
	}
	vm.Ctx.Lock.Unlock()

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

	// Fast forward clock to time for genesis validators to leave
	vm.clock.Set(defaultValidateEndTime)

	vm.Ctx.Lock.Lock()
	blk, err := vm.BuildBlock() // should contain proposal to advance time
	if err != nil {
		t.Fatal(err)
	}
	vm.Ctx.Lock.Unlock()

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

	vm.Ctx.Lock.Lock()
	blk, err = vm.BuildBlock() // should contain proposal to reward genesis validator
	if err != nil {
		t.Fatal(err)
	}
	vm.Ctx.Lock.Unlock()

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

	if _, err := vm.BuildBlock(); err == nil {
		t.Fatalf("Should have errored on BuildBlock")
	}
}

// test acceptance of proposal to create a new chain
func TestCreateChain(t *testing.T) {
	vm := defaultVM()

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

	vm.Ctx.Lock.Lock()
	vm.unissuedDecisionTxs = append(vm.unissuedDecisionTxs, tx)
	blk, err := vm.BuildBlock() // should contain proposal to create chain
	if err != nil {
		t.Fatal(err)
	}
	vm.Ctx.Lock.Unlock()

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

	vm.Ctx.Lock.Lock()
	vm.unissuedDecisionTxs = append(vm.unissuedDecisionTxs, createSubnetTx)
	blk, err := vm.BuildBlock() // should contain proposal to create subnet
	if err != nil {
		t.Fatal(err)
	}
	vm.Ctx.Lock.Unlock()

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

	vm.Ctx.Lock.Lock()
	vm.unissuedEvents.Push(addValidatorTx)
	blk, err = vm.BuildBlock() // should add validator to the new subnet
	if err != nil {
		t.Fatal(err)
	}
	vm.Ctx.Lock.Unlock()

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

	vm.Ctx.Lock.Lock()
	blk, err = vm.BuildBlock() // should be advance time tx
	if err != nil {
		t.Fatal(err)
	}
	vm.Ctx.Lock.Unlock()

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
	vm.Ctx.Lock.Lock()
	blk, err = vm.BuildBlock() // should be advance time tx
	if err != nil {
		t.Fatal(err)
	}
	vm.Ctx.Lock.Unlock()

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
