// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"testing"
	"time"

	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/constants"
	"github.com/ava-labs/gecko/utils/crypto"
)

// func TestAddDefaultSubnetValidatorTxSyntacticVerify(t *testing.T) {
// 	vm := defaultVM()
// 	vm.Ctx.Lock.Lock()
// 	defer func() {
// 		vm.Shutdown()
// 		vm.Ctx.Lock.Unlock()
// 	}()

// 	key, err := vm.factory.NewPrivateKey()
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	nodeID := key.PublicKey().Address()

// 	// Case 1: tx is nil
// 	var tx *addDefaultSubnetValidatorTx
// 	if err := tx.SyntacticVerify(); err == nil {
// 		t.Fatal("should have errored because tx is nil")
// 	}

// 	// Case 2: ID is nil
// 	tx, err = vm.newAddDefaultSubnetValidatorTx(
// 		MinimumStakeAmount,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateEndTime.Unix()),
// 		nodeID,
// 		nodeID,
// 		NumberOfShares,
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	tx.id = ids.ID{}
// 	if err := tx.SyntacticVerify(); err == nil {
// 		t.Fatal("should have errored because ID is nil")
// 	}

// 	// Case 3: Wrong Network ID
// 	tx, err = vm.newAddDefaultSubnetValidatorTx(
// 		MinimumStakeAmount,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateEndTime.Unix()),
// 		nodeID,
// 		nodeID,
// 		NumberOfShares,
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	tx.NetworkID = tx.NetworkID + 1
// 	if err := tx.SyntacticVerify(); err == nil {
// 		t.Fatal("should have errored because the wrong network ID was used")
// 	}

// 	// Case 4: Node ID is nil
// 	tx, err = vm.newAddDefaultSubnetValidatorTx(
// 		MinimumStakeAmount,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateEndTime.Unix()),
// 		nodeID,
// 		nodeID,
// 		NumberOfShares,
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	tx.NodeID = ids.ShortID{}
// 	if err := tx.SyntacticVerify(); err == nil {
// 		t.Fatal("should have errored because node ID is nil")
// 	}

// 	// Case 5: Destination ID is nil
// 	tx, err = vm.newAddDefaultSubnetValidatorTx(
// 		MinimumStakeAmount,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateEndTime.Unix()),
// 		nodeID,
// 		nodeID,
// 		NumberOfShares,
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	tx.Destination = ids.ShortID{}
// 	if err := tx.SyntacticVerify(); err == nil {
// 		t.Fatal("should have errored because destination ID is nil")
// 	}

// 	// Case 6: Stake amount too small
// 	if tx, err := vm.newAddDefaultSubnetValidatorTx(
// 		MinimumStakeAmount-1,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateEndTime.Unix()),
// 		nodeID,
// 		nodeID,
// 		NumberOfShares,
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	); err != nil {
// 		t.Fatal(err)
// 	} else if err := tx.SyntacticVerify(); err == nil {
// 		t.Fatal("should have errored because stake amount too small")
// 	}

// 	// Case 7: Too many shares
// 	if tx, err := vm.newAddDefaultSubnetValidatorTx(
// 		MinimumStakeAmount,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateEndTime.Unix()),
// 		nodeID,
// 		nodeID,
// 		NumberOfShares+1,
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	); err != nil {
// 		t.Fatal(err)
// 	} else if err := tx.SyntacticVerify(); err == nil {
// 		t.Fatal("should have errored because of too many shares")
// 	}

// 	// Case 8.1: Validation length is too short
// 	if tx, err = vm.newAddDefaultSubnetValidatorTx(
// 		MinimumStakeAmount,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateStartTime.Add(MinimumStakingDuration).Unix())-1,
// 		nodeID,
// 		nodeID,
// 		NumberOfShares,
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	); err != nil {
// 		t.Fatal(err)
// 	} else if err := tx.SyntacticVerify(); err == nil {
// 		t.Fatal("should have errored because validation length too short")
// 	}

// 	// Case 8.2: Validation length is negative
// 	if tx, err := vm.newAddDefaultSubnetValidatorTx(
// 		MinimumStakeAmount,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateStartTime.Unix())-1,
// 		nodeID,
// 		nodeID,
// 		NumberOfShares,
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	); err != nil {
// 		t.Fatal(err)
// 	} else if err := tx.SyntacticVerify(); err == nil {
// 		t.Fatal("should have errored because validation length too short")
// 	}

// 	// Case 9: Validation length is too long
// 	if tx, err = vm.newAddDefaultSubnetValidatorTx(
// 		MinimumStakeAmount,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateStartTime.Add(MaximumStakingDuration).Unix())+1,
// 		nodeID,
// 		nodeID,
// 		NumberOfShares,
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	); err != nil {
// 		t.Fatal(err)
// 	} else if err := tx.SyntacticVerify(); err == nil {
// 		t.Fatal("should have errored because validation length too long")
// 	}

// 	// Case 10: Valid
// 	if tx, err := vm.newAddDefaultSubnetValidatorTx(
// 		MinimumStakeAmount,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateEndTime.Unix()),
// 		nodeID,
// 		nodeID,
// 		NumberOfShares,
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	); err != nil {
// 		t.Fatal(err)
// 	} else if err := tx.SyntacticVerify(); err != nil {
// 		t.Fatal(err)
// 	}
// }

// // Test AddDefaultSubnetValidatorTx.SemanticVerify
// func TestAddDefaultSubnetValidatorTxSemanticVerify(t *testing.T) {
// 	vm := defaultVM()
// 	vm.Ctx.Lock.Lock()
// 	defer func() {
// 		vm.Shutdown()
// 		vm.Ctx.Lock.Unlock()
// 	}()
// 	vDB := versiondb.New(vm.DB)

// 	key, err := vm.factory.NewPrivateKey()
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	nodeID := key.PublicKey().Address()

// 	// Case: Validator's start time too early
// 	if tx, err := vm.newAddDefaultSubnetValidatorTx(
// 		MinimumStakeAmount,
// 		uint64(defaultValidateStartTime.Unix())-1,
// 		uint64(defaultValidateEndTime.Unix()),
// 		nodeID,
// 		nodeID,
// 		NumberOfShares,
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	); err != nil {
// 		t.Fatal(err)
// 	} else if _, _, _, _, err := tx.SemanticVerify(vDB); err == nil {
// 		t.Fatal("should've errored because start time too early")
// 	}
// 	vDB.Abort()

// 	// Case: Validator already validating default subnet
// 	if tx, err := vm.newAddDefaultSubnetValidatorTx(
// 		MinimumStakeAmount,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateEndTime.Unix()),
// 		nodeID, // node ID
// 		nodeID, // destination
// 		NumberOfShares,
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	); err != nil {
// 		t.Fatal(err)
// 	} else if _, _, _, _, err := tx.SemanticVerify(vDB); err == nil {
// 		t.Fatal("should've errored because validator already validating")
// 	}
// 	vDB.Abort()

// 	// Case: Validator in pending validator set of default subnet
// 	key2, err := vm.factory.NewPrivateKey()
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	startTime := defaultGenesisTime.Add(1 * time.Second)
// 	tx, err := vm.newAddDefaultSubnetValidatorTx(
// 		MinimumStakeAmount,                                   // stake amount
// 		uint64(startTime.Unix()),                             // start time
// 		uint64(startTime.Add(MinimumStakingDuration).Unix()), // end time
// 		key2.PublicKey().Address(),                           // node ID
// 		nodeID,                                  // destination
// 		NumberOfShares,                          // shares
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // key
// 	)
// 	if err != nil {
// 		t.Fatal(err)
// 	} else if err := vm.putPendingValidators(vDB, // Put validator in pending validator set
// 		&EventHeap{
// 			SortByStartTime: true,
// 			Txs:             []TimedTx{tx},
// 		},
// 		constants.DefaultSubnetID,
// 	); err != nil {
// 		t.Fatal(err)
// 	} else if _, _, _, _, err := tx.SemanticVerify(vDB); err == nil {
// 		t.Fatal("should have failed because validator in pending validator set")
// 	}
// 	vDB.Abort()

// 	// Case: Validator doesn't have enough tokens to cover stake amount
// 	if _, err := vm.newAddDefaultSubnetValidatorTx( // create the tx
// 		MinimumStakeAmount,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateEndTime.Unix()),
// 		nodeID,
// 		nodeID,
// 		NumberOfShares,
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	); err != nil {
// 		t.Fatal(err)
// 	}
// 	// Remove all UTXOs owned by keys[0]
// 	utxoIDs, err := vm.getReferencingUTXOs(vDB, keys[0].PublicKey().Address().Bytes())
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	for _, utxoID := range utxoIDs.List() {
// 		if err := vm.removeUTXO(vDB, utxoID); err != nil {
// 			t.Fatal(err)
// 		}
// 	}
// 	// Now keys[0] has no funds
// 	if _, _, _, _, err := tx.SemanticVerify(vDB); err == nil {
// 		t.Fatal("should have failed because tx fee paying key has no funds")
// 	}
// 	vDB.Abort()
// }

// accept proposal to add validator to default subnet
func TestAddDefaultSubnetValidatorCommit(t *testing.T) {
	vm := defaultVM(t)
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
	pendingValidators, err := vm.getPendingValidators(vm.DB, constants.DefaultSubnetID)
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
	vm := defaultVM(t)
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

	preferredHeight, err := vm.preferredHeight()
	if err != nil {
		t.Fatal(err)
	}
	blk, err := vm.newProposalBlock(vm.LastAccepted(), preferredHeight+1, *tx)
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
func TestAddDefaultSubnetValidatorAbort(t *testing.T) {
	vm := defaultVM(t)
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
	options, err := block.Options()
	if err != nil {
		t.Fatal(err)
	}
	commit, ok := options[0].(*Commit)
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

	if err := abort.Accept(); err != nil { // abort the proposal
		t.Fatal(err)
	}

	// Verify that new validator NOT in pending validator set
	pendingValidators, err := vm.getPendingValidators(vm.DB, constants.DefaultSubnetID)
	if err != nil {
		t.Fatal(err)
	}
	pendingSampler := validators.NewSet()
	pendingSampler.Set(vm.getValidators(pendingValidators))
	if pendingSampler.Contains(ID) {
		t.Fatalf("should not have added validator to pending validator set")
	}
}
