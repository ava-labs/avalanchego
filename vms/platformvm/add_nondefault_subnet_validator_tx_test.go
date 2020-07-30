// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"testing"
	"time"

	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/crypto"
)

// func TestAddNonDefaultSubnetValidatorTxSyntacticVerify(t *testing.T) {
// 	vm := defaultVM()
// 	vm.Ctx.Lock.Lock()
// 	defer func() {
// 		vm.Shutdown()
// 		vm.Ctx.Lock.Unlock()
// 	}()

// 	nodeID := keys[0].PublicKey().Address()

// 	// Case: tx is nil
// 	var tx *addNonDefaultSubnetValidatorTx
// 	if err := tx.SyntacticVerify(); err == nil {
// 		t.Fatal("should have errored because tx is nil")
// 	}

// 	// Case: Tx ID is nil
// 	tx, err := vm.newAddNonDefaultSubnetValidatorTx(
// 		defaultWeight,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateEndTime.Unix()),
// 		nodeID,
// 		testSubnet1.id,
// 		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	tx.id = ids.ID{}
// 	if err := tx.SyntacticVerify(); err == nil {
// 		t.Fatal("should have errored because ID is nil")
// 	}

// 	// Case: Wrong network ID
// 	tx, err = vm.newAddNonDefaultSubnetValidatorTx(
// 		defaultWeight,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateEndTime.Unix()),
// 		nodeID,
// 		testSubnet1.id,
// 		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	tx.NetworkID = tx.NetworkID + 1
// 	if err := tx.SyntacticVerify(); err == nil {
// 		t.Fatal("should have errored because the wrong network ID was used")
// 	}

// 	// Case: Missing Node ID
// 	tx, err = vm.newAddNonDefaultSubnetValidatorTx(
// 		defaultWeight,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateEndTime.Unix()),
// 		nodeID,
// 		testSubnet1.id,
// 		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	tx.NodeID = ids.ShortID{}
// 	if err := tx.SyntacticVerify(); err == nil {
// 		t.Fatal("should have errored because NodeID is empty")
// 	}

// 	// Case: Missing Subnet ID
// 	tx, err = vm.newAddNonDefaultSubnetValidatorTx(
// 		defaultWeight,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateEndTime.Unix()),
// 		nodeID,
// 		testSubnet1.id,
// 		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	tx.Subnet = ids.ID{}
// 	if err := tx.SyntacticVerify(); err == nil {
// 		t.Fatal("should have errored because Subnet ID is nil")
// 	}

// 	// Case: No weight
// 	tx, err = vm.newAddNonDefaultSubnetValidatorTx(
// 		0,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateEndTime.Unix()),
// 		nodeID,
// 		testSubnet1.id,
// 		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	if err := tx.SyntacticVerify(); err == nil {
// 		t.Fatal("should have errored because of no weight")
// 	}

// 	// Case: ControlSigs not sorted
// 	tx, err = vm.newAddNonDefaultSubnetValidatorTx(
// 		defaultWeight,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateEndTime.Unix())-1,
// 		nodeID,
// 		testSubnet1.id,
// 		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	)
// 	tx.ControlSigs[0], tx.ControlSigs[1] = tx.ControlSigs[1], tx.ControlSigs[0]
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	err = tx.SyntacticVerify()
// 	if err == nil {
// 		t.Fatal("should have errored because addresses weren't sorted")
// 	}

// 	// Case: Validation length is too short
// 	if tx, err = vm.newAddNonDefaultSubnetValidatorTx(
// 		defaultWeight,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateStartTime.Add(MinimumStakingDuration).Unix())-1,
// 		nodeID,
// 		testSubnet1.id,
// 		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	); err != nil {
// 		t.Fatal(err)
// 	} else if err := tx.SyntacticVerify(); err == nil {
// 		t.Fatal("should have errored because validation length too short")
// 	}

// 	// Case: Validation length is too long
// 	if tx, err = vm.newAddNonDefaultSubnetValidatorTx(
// 		defaultWeight,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateStartTime.Add(MaximumStakingDuration).Unix())+1,
// 		nodeID,
// 		testSubnet1.id,
// 		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	); err != nil {
// 		t.Fatal(err)
// 	} else if err := tx.SyntacticVerify(); err == nil {
// 		t.Fatal("should have errored because validation length too long")
// 	}

// 	// Case: Valid
// 	if tx, err = vm.newAddNonDefaultSubnetValidatorTx(
// 		defaultWeight,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateEndTime.Unix()),
// 		nodeID,
// 		testSubnet1.id,
// 		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	); err != nil {
// 		t.Fatal(err)
// 	} else if err := tx.SyntacticVerify(); err != nil {
// 		t.Fatal(err)
// 	}
// }

// func TestAddNonDefaultSubnetValidatorTxSemanticVerify(t *testing.T) {
// 	vm := defaultVM()
// 	vm.Ctx.Lock.Lock()
// 	defer func() {
// 		vm.Shutdown()
// 		vm.Ctx.Lock.Unlock()
// 	}()

// 	nodeID := keys[0].PublicKey().Address()

// 	// Case: Proposed validator currently validating default subnet
// 	// but stops validating non-default subnet after stops validating default subnet
// 	// (note that keys[0] is a genesis validator)
// 	if tx, err := vm.newAddNonDefaultSubnetValidatorTx(
// 		defaultWeight,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateEndTime.Unix())+1,
// 		nodeID,
// 		testSubnet1.id,
// 		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	); err != nil {
// 		t.Fatal(err)
// 	} else if _, _, _, _, err := tx.SemanticVerify(vm.DB); err == nil {
// 		t.Fatal("should have failed because validator stops validating default subnet earlier than non-default subnet")
// 	}

// 	// Case: Proposed validator currently validating default subnet
// 	// and proposed non-default subnet validation period is subset of
// 	// default subnet validation period
// 	// (note that keys[0] is a genesis validator)
// 	if tx, err := vm.newAddNonDefaultSubnetValidatorTx(
// 		defaultWeight,
// 		uint64(defaultValidateStartTime.Unix()+1),
// 		uint64(defaultValidateEndTime.Unix()),
// 		nodeID,
// 		testSubnet1.id,
// 		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	); err != nil {
// 		t.Fatal(err)
// 	} else if _, _, _, _, err = tx.SemanticVerify(vm.DB); err != nil {
// 		t.Fatal(err)
// 	}

// 	// Add a validator to pending validator set of default subnet
// 	key, err := vm.factory.NewPrivateKey()
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	pendingDSValidatorID := key.PublicKey().Address()

// 	// starts validating default subnet 10 seconds after genesis
// 	DSStartTime := defaultGenesisTime.Add(10 * time.Second)
// 	DSEndTime := DSStartTime.Add(5 * MinimumStakingDuration)

// 	addDSTx, err := vm.newAddDefaultSubnetValidatorTx(
// 		MinimumStakeAmount,                      // stake amount
// 		uint64(DSStartTime.Unix()),              // start time
// 		uint64(DSEndTime.Unix()),                // end time
// 		pendingDSValidatorID,                    // node ID
// 		nodeID,                                  // destination
// 		NumberOfShares,                          // shares
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // key
// 	)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	// Case: Proposed validator isn't in pending or current validator sets
// 	if tx, err := vm.newAddNonDefaultSubnetValidatorTx(
// 		defaultWeight,
// 		uint64(DSStartTime.Unix()), // start validating non-default subnet before default subnet
// 		uint64(DSEndTime.Unix()),
// 		pendingDSValidatorID,
// 		testSubnet1.id,
// 		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	); err != nil {
// 		t.Fatal(err)
// 	} else if _, _, _, _, err = tx.SemanticVerify(vm.DB); err == nil {
// 		t.Fatal("should have failed because validator not in the current or pending validator sets of the default subnet")
// 	}

// 	if err := vm.putPendingValidators(
// 		vm.DB,
// 		&EventHeap{
// 			SortByStartTime: true,
// 			Txs:             []TimedTx{addDSTx},
// 		},
// 		constants.DefaultSubnetID,
// 	); err != nil {
// 		t.Fatal(err)
// 	}
// 	// Node with ID key.PublicKey().Address() now a pending validator for default subnet

// 	// Case: Proposed validator is pending validator of default subnet
// 	// but starts validating non-default subnet before default subnet
// 	if tx, err := vm.newAddNonDefaultSubnetValidatorTx(
// 		defaultWeight,
// 		uint64(DSStartTime.Unix())-1, // start validating non-default subnet before default subnet
// 		uint64(DSEndTime.Unix()),
// 		pendingDSValidatorID,
// 		testSubnet1.id,
// 		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	); err != nil {
// 		t.Fatal(err)
// 	} else if _, _, _, _, err := tx.SemanticVerify(vm.DB); err == nil {
// 		t.Fatal("should have failed because validator starts validating non-default " +
// 			"subnet before starting to validate default subnet")
// 	}

// 	// Case: Proposed validator is pending validator of default subnet
// 	// but stops validating non-default subnet after default subnet
// 	if tx, err := vm.newAddNonDefaultSubnetValidatorTx(
// 		defaultWeight,
// 		uint64(DSStartTime.Unix()),
// 		uint64(DSEndTime.Unix())+1, // stop validating non-default subnet after stopping validating default subnet
// 		pendingDSValidatorID,
// 		testSubnet1.id,
// 		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	); err != nil {
// 		t.Fatal(err)
// 	} else if _, _, _, _, err = tx.SemanticVerify(vm.DB); err == nil {
// 		t.Fatal("should have failed because validator stops validating non-default " +
// 			"subnet after stops validating default subnet")
// 	}

// 	// Case: Proposed validator is pending validator of default subnet
// 	// and period validating non-default subnet is subset of time validating default subnet
// 	if tx, err := vm.newAddNonDefaultSubnetValidatorTx(
// 		defaultWeight,
// 		uint64(DSStartTime.Unix()), // same start time as for default subnet
// 		uint64(DSEndTime.Unix()),   // same end time as for default subnet
// 		pendingDSValidatorID,
// 		testSubnet1.id,
// 		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	); err != nil {
// 		t.Fatal(err)
// 	} else if _, _, _, _, err := tx.SemanticVerify(vm.DB); err != nil {
// 		t.Fatalf("should have passed verification")
// 	}

// 	// Case: Proposed validator start validating at/before current timestamp
// 	// First, advance the timestamp
// 	newTimestamp := defaultGenesisTime.Add(2 * time.Second)
// 	if err := vm.putTimestamp(vm.DB, newTimestamp); err != nil {
// 		t.Fatal(err)
// 	}

// 	if tx, err := vm.newAddNonDefaultSubnetValidatorTx(
// 		defaultWeight,                                           // weight
// 		uint64(newTimestamp.Unix()),                             // start time
// 		uint64(newTimestamp.Add(MinimumStakingDuration).Unix()), // end time
// 		nodeID,         // node ID
// 		testSubnet1.id, // subnet ID
// 		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // tx fee payer
// 	); err != nil {
// 		t.Fatal(err)
// 	} else if _, _, _, _, err := tx.SemanticVerify(vm.DB); err == nil {
// 		t.Fatal("should have failed verification because starts validating at current timestamp")
// 	}

// 	// reset the timestamp
// 	if err := vm.putTimestamp(vm.DB, defaultGenesisTime); err != nil {
// 		t.Fatal(err)
// 	}

// 	// Create new key with no tokens
// 	factory := crypto.FactorySECP256K1R{}
// 	newAcctKey, err := factory.NewPrivateKey()
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	if _, err := vm.newAddNonDefaultSubnetValidatorTx(
// 		MinimumStakeAmount,                      // weight
// 		uint64(defaultValidateStartTime.Unix()), // start time
// 		uint64(defaultValidateEndTime.Unix()),   // end time
// 		nodeID,         // node ID
// 		testSubnet1.id, // subnet ID
// 		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
// 		[]*crypto.PrivateKeySECP256K1R{newAcctKey.(*crypto.PrivateKeySECP256K1R)}, // tx fee payer
// 	); err == nil {
// 		t.Fatal("should have failed verification because payer address has no tokens to pay fee")
// 	}

// 	// Case: Proposed validator already validating the non-default subnet
// 	// First, add validator as validator of non-default subnet
// 	if tx, err := vm.newAddNonDefaultSubnetValidatorTx(
// 		defaultWeight,                           // weight
// 		uint64(defaultValidateStartTime.Unix()), // start time
// 		uint64(defaultValidateEndTime.Unix()),   // end time
// 		nodeID,         // node ID
// 		testSubnet1.id, // subnet ID
// 		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // tx fee payer
// 	); err != nil {
// 		t.Fatal(err)
// 	} else if err := vm.putCurrentValidators(vm.DB,
// 		&EventHeap{
// 			SortByStartTime: false,
// 			Txs:             []TimedTx{tx},
// 		},
// 		testSubnet1.id,
// 	); err != nil {
// 		t.Fatal(err)
// 	}
// 	// Node with ID nodeIDKey.PublicKey().Address() now validating subnet with ID testSubnet1.ID

// 	if tx, err := vm.newAddNonDefaultSubnetValidatorTx(
// 		defaultWeight,                           // weight
// 		uint64(defaultValidateStartTime.Unix()), // start time
// 		uint64(defaultValidateEndTime.Unix()),   // end time
// 		nodeID,         // node ID
// 		testSubnet1.id, // subnet ID
// 		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // tx fee payer
// 	); err != nil {
// 		t.Fatal(err)
// 	} else if _, _, _, _, err := tx.SemanticVerify(vm.DB); err == nil {
// 		t.Fatal("should have failed verification because validator already validating the specified subnet")
// 	} else if err := vm.putCurrentValidators(vm.DB, // reset validator heap
// 		&EventHeap{
// 			SortByStartTime: false,
// 		},
// 		testSubnet1.id,
// 	); err != nil {
// 		t.Fatal(err)
// 	}

// 	// Case: Too many signatures
// 	if tx, err := vm.newAddNonDefaultSubnetValidatorTx(
// 		defaultWeight,                                                   // weight
// 		uint64(defaultGenesisTime.Unix()),                               // start time
// 		uint64(defaultGenesisTime.Add(MinimumStakingDuration).Unix())+1, // end time
// 		nodeID,         // node ID
// 		testSubnet1.id, // subnet ID
// 		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1], testSubnet1ControlKeys[2]},
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // tx fee payer
// 	); err != nil {
// 		t.Fatal(err)
// 	} else if _, _, _, _, err := tx.SemanticVerify(vm.DB); err == nil {
// 		t.Fatal("should have failed verification because tx has 3 signatures but only 2 needed")
// 	}

// 	// Case: Too few signatures
// 	tx, err := vm.newAddNonDefaultSubnetValidatorTx(
// 		defaultWeight,                                                 // weight
// 		uint64(defaultGenesisTime.Unix()),                             // start time
// 		uint64(defaultGenesisTime.Add(MinimumStakingDuration).Unix()), // end time
// 		nodeID,         // node ID
// 		testSubnet1.id, // subnet ID
// 		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[2]},
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // tx fee payer
// 	)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	tx.ControlSigs = tx.ControlSigs[0:1] // remove a control sig
// 	if _, _, _, _, err = tx.SemanticVerify(vm.DB); err == nil {
// 		t.Fatal("should have failed verification because not enough control sigs")
// 	}

// 	// Case: Control Signature from invalid key (keys[3] is not a control key)
// 	tx, err = vm.newAddNonDefaultSubnetValidatorTx(
// 		defaultWeight,                                                 // weight
// 		uint64(defaultGenesisTime.Unix()),                             // start time
// 		uint64(defaultGenesisTime.Add(MinimumStakingDuration).Unix()), // end time
// 		nodeID,         // node ID
// 		testSubnet1.id, // subnet ID
// 		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], keys[1]},
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // tx fee payer
// 	)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	// Replace a valid signature with one from keys[3]
// 	sig, err := keys[3].SignHash(hashing.ComputeHash256(tx.unsignedBytes))
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	copy(tx.ControlSigs[0][:], sig)
// 	crypto.SortSECP2561RSigs(tx.ControlSigs)
// 	if _, _, _, _, err = tx.SemanticVerify(vm.DB); err == nil {
// 		t.Fatal("should have failed verification because a control sig is invalid")
// 	}

// 	// Case: Proposed validator in pending validator set for subnet
// 	// First, add validator to pending validator set of subnet
// 	if tx, err := vm.newAddNonDefaultSubnetValidatorTx(
// 		defaultWeight,                                                   // weight
// 		uint64(defaultGenesisTime.Unix())+1,                             // start time
// 		uint64(defaultGenesisTime.Add(MinimumStakingDuration).Unix())+1, // end time
// 		nodeID,         // node ID
// 		testSubnet1.id, // subnet ID
// 		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // tx fee payer
// 	); err != nil {
// 		t.Fatal(err)
// 	} else if err = vm.putPendingValidators(vm.DB, // Node ID nodeIDKey.PublicKey().Address() now pending
// 		&EventHeap{ // validator for subnet testSubnet1.ID
// 			SortByStartTime: true,
// 			Txs:             []TimedTx{tx},
// 		},
// 		testSubnet1.id,
// 	); err != nil {
// 		t.Fatal(err)
// 	} else if _, _, _, _, err = tx.SemanticVerify(vm.DB); err == nil {
// 		t.Fatal("should have failed verification because validator already in pending validator set of the specified subnet")
// 	}
// }

// // Test that marshalling/unmarshalling works
// func TestAddNonDefaultSubnetValidatorMarshal(t *testing.T) {
// 	vm := defaultVM()
// 	vm.Ctx.Lock.Lock()
// 	defer func() {
// 		vm.Shutdown()
// 		vm.Ctx.Lock.Unlock()
// 	}()

// 	var unmarshaledTx addNonDefaultSubnetValidatorTx

// 	// valid tx
// 	tx, err := vm.newAddNonDefaultSubnetValidatorTx(
// 		defaultWeight,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateEndTime.Unix()),
// 		keys[0].PublicKey().Address(),
// 		testSubnet1.id,
// 		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	)
// 	if err != nil {
// 		t.Fatal(err)
// 	} else if txBytes, err := Codec.Marshal(tx); err != nil {
// 		t.Fatal(err)
// 	} else if err := Codec.Unmarshal(txBytes, &unmarshaledTx); err != nil {
// 		t.Fatal(err)
// 	} else if err := unmarshaledTx.initialize(vm); err != nil {
// 		t.Fatal(err)
// 	}
// 	if tx.Memo == nil { // reflect.DeepEqual considers []byte{} and nil to be different so change nil to []byte{}
// 		tx.Memo = []byte{}
// 	}
// 	if !reflect.DeepEqual(*tx, unmarshaledTx) {
// 		t.Fatal("should be equal")
// 	}
// }

// Accept proposal to add validator to non-default subnet
func TestAddNonDefaultSubnetValidatorAccept(t *testing.T) {
	vm := defaultVM(t)
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
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1], keys[0]},
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
