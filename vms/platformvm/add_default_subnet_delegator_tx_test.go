// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"testing"
	"time"

	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
)

func TestAddDefaultSubnetDelegatorTxSyntacticVerify(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	nodeID := keys[0].PublicKey().Address()
	destination := nodeID

	// Case 1: tx is nil
	var tx *addDefaultSubnetDelegatorTx
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have errored because tx is nil")
	}

	// Case 2: Tx ID is nil
	tx, err := vm.newAddDefaultSubnetDelegatorTx(
		MinimumStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		destination,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	tx.id = ids.ID{}
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have errored because ID is nil")
	}

	// Case 3: Wrong network ID
	tx, err = vm.newAddDefaultSubnetDelegatorTx(
		MinimumStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		destination,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	tx.NetworkID = tx.NetworkID + 1
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have errored because the wrong network ID was used")
	}

	// Case 4: Missing Node ID
	tx, err = vm.newAddDefaultSubnetDelegatorTx(
		MinimumStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		destination,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	tx.NodeID = ids.ShortID{}
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have errored because NodeID is nil")
	}

	// Case 5: Not enough weight
	tx, err = vm.newAddDefaultSubnetDelegatorTx(
		MinimumStakeAmount-1,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		destination,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have errored because of not enough weight")
	}

	// Case 6: Validation length is too short
	tx, err = vm.newAddDefaultSubnetDelegatorTx(
		MinimumStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateStartTime.Add(MinimumStakingDuration).Unix())-1,
		nodeID,
		destination,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	err = tx.SyntacticVerify()
	if err == nil {
		t.Fatal("should have errored because validation length too short")
	}

	// Case 7: Validation length is too long
	tx, err = vm.newAddDefaultSubnetDelegatorTx(
		MinimumStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateStartTime.Add(MaximumStakingDuration).Unix())+1,
		nodeID,
		destination,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	err = tx.SyntacticVerify()
	if err == nil {
		t.Fatal("should have errored because validation length too long")
	}

	// Case 8: Valid
	tx, err = vm.newAddDefaultSubnetDelegatorTx(
		MinimumStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		destination,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.SyntacticVerify(); err != nil {
		t.Fatal(err)
	}
}

func TestAddDefaultSubnetDelegatorTxSemanticVerify(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()
	nodeID := keys[0].PublicKey().Address()
	destination := nodeID
	vdb := versiondb.New(vm.DB)

	// Case 1: Proposed validator currently validating default subnet
	// but stops validating non-default subnet after stops validating default subnet
	// (note that defaultKey is a genesis validator)
	tx, err := vm.newAddDefaultSubnetDelegatorTx(
		MinimumStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix())+1,
		nodeID,
		destination,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, _, err = tx.SemanticVerify(vdb)
	if err == nil {
		t.Fatal("should have failed because validator stops validating default subnet earlier than non-default subnet")
	}
	vdb.Abort()

	// Case 2: Proposed validator currently validating default subnet
	// and proposed non-default subnet validation period is subset of
	// default subnet validation period
	// (note that keys[0] is a genesis validator)
	tx, err = vm.newAddDefaultSubnetDelegatorTx(
		MinimumStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix())+1,
		nodeID,
		destination,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, _, err = tx.SemanticVerify(vdb)
	if err == nil {
		t.Fatalf("should have failed because the end time is outside the default subnets end time")
	}
	vdb.Abort()

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
		MinimumStakeAmount,                      // stake amount
		uint64(DSStartTime.Unix()),              // start time
		uint64(DSEndTime.Unix()),                // end time
		pendingDSValidatorID,                    // node ID
		destination,                             // destination
		NumberOfShares,                          // subnet
		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // key
	)
	if err != nil {
		t.Fatal(err)
	}

	// Case 3: Proposed validator isn't in pending or current validator sets
	tx, err = vm.newAddDefaultSubnetDelegatorTx(
		MinimumStakeAmount,
		uint64(DSStartTime.Unix()),
		uint64(DSEndTime.Unix()),
		pendingDSValidatorID,
		destination,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, _, err = tx.SemanticVerify(vdb)
	if err == nil {
		t.Fatal("should have failed because validator not in the current or pending validator sets of the default subnet")
	}
	vdb.Abort()

	if err := vm.putPendingValidators(
		vm.DB,
		&EventHeap{
			SortByStartTime: true,
			Txs:             []TimedTx{addDSTx},
		},
		DefaultSubnetID,
	); err != nil {
		t.Fatal(err)
	}
	// Node with ID key.PublicKey().Address() now a pending validator for default subnet

	// Case 4: Proposed validator is pending validator of default subnet
	// but starts validating non-default subnet before default subnet
	tx, err = vm.newAddDefaultSubnetDelegatorTx(
		MinimumStakeAmount,
		uint64(DSStartTime.Unix())-1, // start validating non-default subnet before default subnet
		uint64(DSEndTime.Unix()),
		pendingDSValidatorID,
		destination,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, _, err = tx.SemanticVerify(vdb)
	if err == nil {
		t.Fatal("should have failed because validator starts validating non-default " +
			"subnet before starting to validate default subnet")
	}
	vdb.Abort()

	// Case 5: Proposed validator is pending validator of default subnet
	// but stops validating non-default subnet after default subnet
	tx, err = vm.newAddDefaultSubnetDelegatorTx(
		MinimumStakeAmount,
		uint64(DSStartTime.Unix()),
		uint64(DSEndTime.Unix())+1, // stop validating non-default subnet after stopping validating default subnet
		pendingDSValidatorID,
		destination,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, _, err = tx.SemanticVerify(vdb)
	if err == nil {
		t.Fatal("should have failed because validator stops validating non-default " +
			"subnet after stops validating default subnet")
	}
	vdb.Abort()

	// Case 6: Proposed validator is pending validator of default subnet
	// and period validating non-default subnet is subset of time validating default subnet
	tx, err = vm.newAddDefaultSubnetDelegatorTx(
		MinimumStakeAmount,
		uint64(DSStartTime.Unix()), // same start time as for default subnet
		uint64(DSEndTime.Unix()),   // same end time as for default subnet
		pendingDSValidatorID,
		destination,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, _, err = tx.SemanticVerify(vdb)
	if err != nil {
		t.Fatalf("should have passed verification")
	}
	vdb.Abort()

	// Case 7: Proposed validator start validating at/before current timestamp
	// First, advance the timestamp
	newTimestamp := defaultGenesisTime.Add(2 * time.Second)
	if err := vm.putTimestamp(vm.DB, newTimestamp); err != nil {
		t.Fatal(err)
	}

	tx, err = vm.newAddDefaultSubnetDelegatorTx(
		MinimumStakeAmount,                                      // weight
		uint64(newTimestamp.Unix()),                             // start time
		uint64(newTimestamp.Add(MinimumStakingDuration).Unix()), // end time
		nodeID,                                  // node ID
		destination,                             // destination
		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // tx fee payer
	)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, _, err = tx.SemanticVerify(vdb)
	if err == nil {
		t.Fatal("should have failed verification because starts validating at current timestamp")
	}
	vdb.Abort()

	// reset the timestamp
	if err := vm.putTimestamp(vm.DB, defaultGenesisTime); err != nil {
		t.Fatal(err)
	}

	// Case 7: Account that pays tx fee doesn't have enough $AVA to pay tx fee
	txFeeSaved := vm.txFee
	vm.txFee = 1 // Do this so test works even when txFee is 0

	// Create new key whose account has no $AVA
	factory := crypto.FactorySECP256K1R{}
	newAcctKey, err := factory.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}

	tx, err = vm.newAddDefaultSubnetDelegatorTx(
		MinimumStakeAmount,                      // weight
		uint64(defaultValidateStartTime.Unix()), // start time
		uint64(defaultValidateEndTime.Unix()),   // end time
		nodeID,                                  // node ID
		destination,                             // destination
		[]*crypto.PrivateKeySECP256K1R{newAcctKey.(*crypto.PrivateKeySECP256K1R)}, // tx fee payer
	)
	if err == nil {
		t.Fatal("should have failed verification because payer account has no $AVA to pay fee")
	}

	vm.txFee = txFeeSaved // Reset tx fee
}
