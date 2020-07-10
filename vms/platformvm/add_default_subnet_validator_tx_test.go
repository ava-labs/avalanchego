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

func TestAddDefaultSubnetValidatorTxSyntacticVerify(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	key, err := vm.factory.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	nodeID := key.PublicKey().Address()

	// Case 1: tx is nil
	var tx *addDefaultSubnetValidatorTx
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have errored because tx is nil")
	}

	// Case 2: ID is nil
	tx, err = vm.newAddDefaultSubnetValidatorTx(
		defaultStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		nodeID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	tx.id = ids.ID{}
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have errored because ID is nil")
	}

	// Case 3: Wrong Network ID
	tx, err = vm.newAddDefaultSubnetValidatorTx(
		defaultStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		nodeID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	tx.NetworkID = tx.NetworkID + 1
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have errored because the wrong network ID was used")
	}

	// Case 4: Node ID is nil
	tx, err = vm.newAddDefaultSubnetValidatorTx(
		defaultStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		nodeID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	tx.NodeID = ids.ShortID{}
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have errored because node ID is nil")
	}

	// Case 5: Destination ID is nil
	tx, err = vm.newAddDefaultSubnetValidatorTx(
		defaultStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		nodeID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	tx.Destination = ids.ShortID{}
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have errored because destination ID is nil")
	}

	// Case 6: Stake amount too small
	tx, err = vm.newAddDefaultSubnetValidatorTx(
		MinimumStakeAmount-1,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		nodeID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have errored because stake amount too small")
	}

	// Case 7: Too many shares
	tx, err = vm.newAddDefaultSubnetValidatorTx(
		defaultStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		nodeID,
		NumberOfShares+1,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have errored because of too many shares")
	}

	// Case 8.1: Validation length is too short
	tx, err = vm.newAddDefaultSubnetValidatorTx(
		defaultStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateStartTime.Add(MinimumStakingDuration).Unix())-1,
		nodeID,
		nodeID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have errored because validation length too short")
	}

	// Case 8.2: Validation length is negative
	tx, err = vm.newAddDefaultSubnetValidatorTx(
		defaultStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateStartTime.Unix())-1,
		nodeID,
		nodeID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have errored because validation length too short")
	}

	// Case 9: Validation length is too long
	tx, err = vm.newAddDefaultSubnetValidatorTx(
		defaultStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateStartTime.Add(MaximumStakingDuration).Unix())+1,
		nodeID,
		nodeID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have errored because validation length too long")
	}

	// Case 10: Valid
	tx, err = vm.newAddDefaultSubnetValidatorTx(
		defaultStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		nodeID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.SyntacticVerify(); err != nil {
		t.Fatal(err)
	}
}

// Test AddDefaultSubnetValidatorTx.SemanticVerify
func TestAddDefaultSubnetValidatorTxSemanticVerify(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()
	vDB := versiondb.New(vm.DB)

	key, err := vm.factory.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	nodeID := key.PublicKey().Address()

	// Case: Validator's start time too early
	if tx, err := vm.newAddDefaultSubnetValidatorTx(
		defaultStakeAmount,
		uint64(defaultValidateStartTime.Unix())-1,
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		nodeID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	); err != nil {
		t.Fatal(err)
	} else if _, _, _, _, err := tx.SemanticVerify(vDB); err == nil {
		t.Fatal("should've errored because start time too early")
	}
	vDB.Abort()

	// Case: Validator doesn't have enough tokens to cover stake amount
	if _, err := vm.newAddDefaultSubnetValidatorTx(
		defaultBalance-vm.txFee+1,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		nodeID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	); err == nil {
		t.Fatal("should've errored because validator doesn't have enough tokens to cover stake")
	}
	vDB.Abort()

	// Case: Validator already validating default subnet
	if tx, err := vm.newAddDefaultSubnetValidatorTx(
		defaultStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID, // node ID
		nodeID, // destination
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	); err != nil {
		t.Fatal(err)
	} else if _, _, _, _, err := tx.SemanticVerify(vDB); err == nil {
		t.Fatal("should've errored because validator already validating")
	}
	vDB.Abort()

	// Case: Validator in pending validator set of default subnet
	key2, err := vm.factory.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	startTime := defaultGenesisTime.Add(1 * time.Second)
	tx, err := vm.newAddDefaultSubnetValidatorTx(
		defaultStakeAmount,       // stake amount
		uint64(startTime.Unix()), // start time
		uint64(startTime.Add(MinimumStakingDuration).Unix()), // end time
		key2.PublicKey().Address(),                           // node ID
		nodeID,                                               // destination
		NumberOfShares,                                       // shares
		[]*crypto.PrivateKeySECP256K1R{keys[0]},              // key
	)
	if err != nil {
		t.Fatal(err)
	}

	// Put validator in pending validator set
	if err := vm.putPendingValidators(vDB,
		&EventHeap{
			SortByStartTime: true,
			Txs:             []TimedTx{tx},
		},
		DefaultSubnetID,
	); err != nil {
		t.Fatal(err)
	} else if _, _, _, _, err := tx.SemanticVerify(vDB); err == nil {
		t.Fatal("should have failed because validator in pending validator set")
	}
}
