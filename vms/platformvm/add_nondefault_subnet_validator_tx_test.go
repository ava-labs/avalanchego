// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"reflect"
	"testing"
	"time"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
)

func TestAddNonDefaultSubnetValidatorTxSyntacticVerify(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	// Case 1: tx is nil
	var tx *addNonDefaultSubnetValidatorTx
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have errored because tx is nil")
	}

	// Case 2: Tx ID is nil
	tx, err := vm.newAddNonDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultWeight,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		defaultKey.PublicKey().Address(),
		testSubnet1.id,
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	tx.id = ids.ID{}
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have errored because ID is nil")
	}

	// Case 3: Wrong network ID
	tx, err = vm.newAddNonDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultWeight,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		defaultKey.PublicKey().Address(),
		testSubnet1.id,
		testNetworkID+1,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have errored because the wrong network ID was used")
	}

	// Case 4: Missing Node ID
	tx, err = vm.newAddNonDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultWeight,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		defaultKey.PublicKey().Address(),
		testSubnet1.id,
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	tx.NodeID = ids.ShortID{}
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have errored because NodeID is nil")
	}

	// Case 5: Missing Subnet ID
	tx, err = vm.newAddNonDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultWeight,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		defaultKey.PublicKey().Address(),
		testSubnet1.id,
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	tx.Subnet = ids.ID{}
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have errored because Subnet ID is nil")
	}

	// Case 6: No weight
	tx, err = vm.newAddNonDefaultSubnetValidatorTx(
		defaultNonce+1,
		0,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		defaultKey.PublicKey().Address(),
		testSubnet1.id,
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have errored because of no weight")
	}

	// Case 7: ControlSigs not sorted
	tx, err = vm.newAddNonDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultWeight,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix())-1,
		defaultKey.PublicKey().Address(),
		testSubnet1.id,
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey,
	)
	tx.ControlSigs[0], tx.ControlSigs[1] = tx.ControlSigs[1], tx.ControlSigs[0]
	if err != nil {
		t.Fatal(err)
	}
	err = tx.SyntacticVerify()
	if err == nil {
		t.Fatal("should have errored because addresses weren't sorted")
	}

	// Case 8: Validation length is too short
	tx, err = vm.newAddNonDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultWeight,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateStartTime.Add(MinimumStakingDuration).Unix())-1,
		defaultKey.PublicKey().Address(),
		testSubnet1.id,
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	err = tx.SyntacticVerify()
	if err == nil {
		t.Fatal("should have errored because validation length too short")
	}

	// Case 9: Validation length is too long
	tx, err = vm.newAddNonDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultWeight,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateStartTime.Add(MaximumStakingDuration).Unix())+1,
		defaultKey.PublicKey().Address(),
		testSubnet1.id,
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	err = tx.SyntacticVerify()
	if err == nil {
		t.Fatal("should have errored because validation length too long")
	}

	// Case 10: Valid
	tx, err = vm.newAddNonDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultWeight,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		defaultKey.PublicKey().Address(),
		testSubnet1.id,
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.SyntacticVerify(); err != nil {
		t.Fatal(err)
	}
}

func TestAddNonDefaultSubnetValidatorTxSemanticVerify(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	// Case 1: Proposed validator currently validating default subnet
	// but stops validating non-default subnet after stops validating default subnet
	// (note that defaultKey is a genesis validator)
	tx, err := vm.newAddNonDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultWeight,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix())+1,
		defaultKey.PublicKey().Address(),
		testSubnet1.id,
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, _, err = tx.SemanticVerify(vm.DB)
	if err == nil {
		t.Fatal("should have failed because validator stops validating default subnet earlier than non-default subnet")
	}

	// Case 2: Proposed validator currently validating default subnet
	// and proposed non-default subnet validation period is subset of
	// default subnet validation period
	// (note that defaultKey is a genesis validator)
	tx, err = vm.newAddNonDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultWeight,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		defaultKey.PublicKey().Address(),
		testSubnet1.id,
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, _, err = tx.SemanticVerify(vm.DB)
	if err != nil {
		t.Log(testSubnet1.id)
		subnets, err := vm.getSubnets(vm.DB)
		if err != nil {
			t.Fatal(err)
		}
		if len(subnets) == 0 {
			t.Fatal("no subnets found")
		}
		t.Logf("subnets[0].ID: %v", subnets[0].id)
		t.Fatal(err)
	}

	// Add a validator to pending validator set of default subnet
	key, err := vm.factory.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	pendingDSValidatorID := key.PublicKey().Address()

	// starts validating default subnet 10 seconds after genesis
	DSStartTime := defaultGenesisTime.Add(10 * time.Second)
	DSEndTime := DSStartTime.Add(5 * MinimumStakingDuration)

	addDSTx, err := vm.newAddDefaultSubnetValidatorTx(
		defaultNonce+1,                   // nonce
		defaultStakeAmount,               // stake amount
		uint64(DSStartTime.Unix()),       // start time
		uint64(DSEndTime.Unix()),         // end time
		pendingDSValidatorID,             // node ID
		defaultKey.PublicKey().Address(), // destination
		NumberOfShares,                   // subnet
		testNetworkID,                    // network
		defaultKey,                       // key
	)
	if err != nil {
		t.Fatal(err)
	}

	// Case 3: Proposed validator isn't in pending or current validator sets
	tx, err = vm.newAddNonDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultWeight,
		uint64(DSStartTime.Unix()), // start validating non-default subnet before default subnet
		uint64(DSEndTime.Unix()),
		pendingDSValidatorID,
		testSubnet1.id,
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, _, err = tx.SemanticVerify(vm.DB)
	if err == nil {
		t.Fatal("should have failed because validator not in the current or pending validator sets of the default subnet")
	}

	err = vm.putPendingValidators(
		vm.DB,
		&EventHeap{
			SortByStartTime: true,
			Txs:             []TimedTx{addDSTx},
		},
		defaultSubnetID,
	)
	if err != nil {
		t.Fatal(err)
	}
	// Node with ID key.PublicKey().Address() now a pending validator for default subnet

	// Case 4: Proposed validator is pending validator of default subnet
	// but starts validating non-default subnet before default subnet
	tx, err = vm.newAddNonDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultWeight,
		uint64(DSStartTime.Unix())-1, // start validating non-default subnet before default subnet
		uint64(DSEndTime.Unix()),
		pendingDSValidatorID,
		testSubnet1.id,
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, _, err = tx.SemanticVerify(vm.DB)
	if err == nil {
		t.Fatal("should have failed because validator starts validating non-default " +
			"subnet before starting to validate default subnet")
	}

	// Case 5: Proposed validator is pending validator of default subnet
	// but stops validating non-default subnet after default subnet
	tx, err = vm.newAddNonDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultWeight,
		uint64(DSStartTime.Unix()),
		uint64(DSEndTime.Unix())+1, // stop validating non-default subnet after stopping validating default subnet
		pendingDSValidatorID,
		testSubnet1.id,
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, _, err = tx.SemanticVerify(vm.DB)
	if err == nil {
		t.Fatal("should have failed because validator stops validating non-default " +
			"subnet after stops validating default subnet")
	}

	// Case 6: Proposed validator is pending validator of default subnet
	// and period validating non-default subnet is subset of time validating default subnet
	tx, err = vm.newAddNonDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultWeight,
		uint64(DSStartTime.Unix()), // same start time as for default subnet
		uint64(DSEndTime.Unix()),   // same end time as for default subnet
		pendingDSValidatorID,
		testSubnet1.id,
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, _, err = tx.SemanticVerify(vm.DB)
	if err != nil {
		t.Fatalf("should have passed verification")
	}

	// Case 7: Proposed validator start validating at/before current timestamp
	// First, advance the timestamp
	newTimestamp := defaultGenesisTime.Add(2 * time.Second)
	if err := vm.putTimestamp(vm.DB, newTimestamp); err != nil {
		t.Fatal(err)
	}

	tx, err = vm.newAddNonDefaultSubnetValidatorTx(
		defaultNonce+1,                                          // nonce
		defaultWeight,                                           // weight
		uint64(newTimestamp.Unix()),                             // start time
		uint64(newTimestamp.Add(MinimumStakingDuration).Unix()), // end time
		defaultKey.PublicKey().Address(),                        // node ID
		testSubnet1.id,                                          // subnet ID
		testNetworkID,                                           // network ID
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey, // tx fee payer
	)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, _, err = tx.SemanticVerify(vm.DB)
	if err == nil {
		t.Fatal("should have failed verification because starts validating at current timestamp")
	}

	// reset the timestamp
	if err := vm.putTimestamp(vm.DB, defaultGenesisTime); err != nil {
		t.Fatal(err)
	}

	// Case 7: Account that pays tx fee doesn't have enough $AVA to pay tx fee
	txFeeSaved := txFee
	txFee = 1 // Do this so test works even when txFee is 0

	// Create new key whose account has no $AVA
	factory := crypto.FactorySECP256K1R{}
	newAcctKey, err := factory.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}

	tx, err = vm.newAddNonDefaultSubnetValidatorTx(
		1,                                       // nonce (new account has nonce 0 so use nonce 1)
		defaultWeight,                           // weight
		uint64(defaultValidateStartTime.Unix()), // start time
		uint64(defaultValidateEndTime.Unix()),   // end time
		defaultKey.PublicKey().Address(),        // node ID
		testSubnet1.id,                          // subnet ID
		testNetworkID,                           // network ID
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		newAcctKey.(*crypto.PrivateKeySECP256K1R), // tx fee payer
	)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, _, err = tx.SemanticVerify(vm.DB)
	if err == nil {
		t.Fatal("should have failed verification because payer account has no $AVA to pay fee")
	}
	txFee = txFeeSaved // Reset tx fee

	// Case 8: Proposed validator already validating the non-default subnet
	// First, add validator as validator of non-default subnet
	tx, err = vm.newAddNonDefaultSubnetValidatorTx(
		defaultNonce+1,                          // nonce
		defaultWeight,                           // weight
		uint64(defaultValidateStartTime.Unix()), // start time
		uint64(defaultValidateEndTime.Unix()),   // end time
		defaultKey.PublicKey().Address(),        // node ID
		testSubnet1.id,                          // subnet ID
		testNetworkID,                           // network ID
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey, // tx fee payer
	)
	if err != nil {
		t.Fatal(err)
	}

	err = vm.putCurrentValidators(vm.DB,
		&EventHeap{
			SortByStartTime: false,
			Txs:             []TimedTx{tx},
		},
		testSubnet1.id,
	)
	// Node with ID nodeIDKey.PublicKey().Address() now validating subnet with ID testSubnet1.ID

	tx, err = vm.newAddNonDefaultSubnetValidatorTx(
		defaultNonce+1,                          // nonce
		defaultWeight,                           // weight
		uint64(defaultValidateStartTime.Unix()), // start time
		uint64(defaultValidateEndTime.Unix()),   // end time
		defaultKey.PublicKey().Address(),        // node ID
		testSubnet1.id,                          // subnet ID
		testNetworkID,                           // network ID
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey, // tx fee payer
	)
	if err != nil {
		t.Fatal(err)
	}

	_, _, _, _, err = tx.SemanticVerify(vm.DB)
	if err == nil {
		t.Fatal("should have failed verification because validator already validating the specified subnet")
	}

	// reset validator heap
	err = vm.putCurrentValidators(vm.DB,
		&EventHeap{
			SortByStartTime: false,
		},
		testSubnet1.id,
	)

	// Case 9: Too many signatures
	tx, err = vm.newAddNonDefaultSubnetValidatorTx(
		defaultNonce+1,                                                  // nonce
		defaultWeight,                                                   // weight
		uint64(defaultGenesisTime.Unix()),                               // start time
		uint64(defaultGenesisTime.Add(MinimumStakingDuration).Unix())+1, // end time
		keys[0].PublicKey().Address(),                                   // node ID
		testSubnet1.id,                                                  // subnet ID
		testNetworkID,                                                   // network ID
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1], testSubnet1ControlKeys[2]},
		defaultKey, // tx fee payer
	)
	if err != nil {
		t.Fatal(err)
	}

	_, _, _, _, err = tx.SemanticVerify(vm.DB)
	if err == nil {
		t.Fatal("should have failed verification because tx has 3 signatures but only 2 needed")
	}

	// Case 10: Too few signatures
	tx, err = vm.newAddNonDefaultSubnetValidatorTx(
		defaultNonce+1,                                                // nonce
		defaultWeight,                                                 // weight
		uint64(defaultGenesisTime.Unix()),                             // start time
		uint64(defaultGenesisTime.Add(MinimumStakingDuration).Unix()), // end time
		keys[0].PublicKey().Address(),                                 // node ID
		testSubnet1.id,                                                // subnet ID
		testNetworkID,                                                 // network ID
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[2]},
		defaultKey, // tx fee payer
	)
	if err != nil {
		t.Fatal(err)
	}

	_, _, _, _, err = tx.SemanticVerify(vm.DB)
	if err == nil {
		t.Fatal("should have failed verification because tx has 1 signatures but 2 needed")
	}

	// Case 10: Control Signature from invalid key
	tx, err = vm.newAddNonDefaultSubnetValidatorTx(
		defaultNonce+1,                                                // nonce
		defaultWeight,                                                 // weight
		uint64(defaultGenesisTime.Unix()),                             // start time
		uint64(defaultGenesisTime.Add(MinimumStakingDuration).Unix()), // end time
		keys[0].PublicKey().Address(),                                 // node ID
		testSubnet1.id,                                                // subnet ID
		testNetworkID,                                                 // network ID
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], keys[3]},
		defaultKey, // tx fee payer
	)
	if err != nil {
		t.Fatal(err)
	}

	_, _, _, _, err = tx.SemanticVerify(vm.DB)
	if err == nil {
		t.Fatal("should have failed verification because tx has control sig from non-control key")
	}

	// Case 11: Proposed validator in pending validator set for subnet
	// First, add validator to pending validator set of subnet
	tx, err = vm.newAddNonDefaultSubnetValidatorTx(
		defaultNonce+1,                                                  // nonce
		defaultWeight,                                                   // weight
		uint64(defaultGenesisTime.Unix())+1,                             // start time
		uint64(defaultGenesisTime.Add(MinimumStakingDuration).Unix())+1, // end time
		defaultKey.PublicKey().Address(),                                // node ID
		testSubnet1.id,                                                  // subnet ID
		testNetworkID,                                                   // network ID
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey, // tx fee payer
	)
	if err != nil {
		t.Fatal(err)
	}

	err = vm.putPendingValidators(vm.DB,
		&EventHeap{
			SortByStartTime: true,
			Txs:             []TimedTx{tx},
		},
		testSubnet1.id,
	)
	// Node with ID nodeIDKey.PublicKey().Address() now pending validator for subnet with ID testSubnet1.ID

	_, _, _, _, err = tx.SemanticVerify(vm.DB)
	if err == nil {
		t.Fatal("should have failed verification because validator already in pending validator set of the specified subnet")
	}
}

// Test that marshalling/unmarshalling works
func TestAddNonDefaultSubnetValidatorMarshal(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	// valid tx
	tx, err := vm.newAddNonDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultWeight,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		defaultKey.PublicKey().Address(),
		testSubnet1.id,
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}

	txBytes, err := Codec.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}

	var unmarshaledTx addNonDefaultSubnetValidatorTx
	if err := Codec.Unmarshal(txBytes, &unmarshaledTx); err != nil {
		t.Fatal(err)
	}
	if err := unmarshaledTx.initialize(vm); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(tx, &unmarshaledTx) {
		t.Log(tx)
		t.Log(&unmarshaledTx)
		t.Fatal("should be equal")
	}
}
