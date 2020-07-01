// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

/*
import (
	"testing"
	"time"

	"github.com/ava-labs/gecko/ids"
)

func TestAddDefaultSubnetValidatorTxSyntacticVerify(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	// Case 1: tx is nil
	var tx *addDefaultSubnetValidatorTx
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have errored because tx is nil")
	}

	// Case 2: ID is nil
	tx, err := vm.newAddDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		defaultKey.PublicKey().Address(),
		defaultKey.PublicKey().Address(),
		NumberOfShares,
		testNetworkID,
		defaultKey,
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
		defaultNonce+1,
		defaultStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		defaultKey.PublicKey().Address(),
		defaultKey.PublicKey().Address(),
		NumberOfShares,
		testNetworkID+1,
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have errored because the wrong network ID was used")
	}

	// Case 4: Node ID is nil
	tx, err = vm.newAddDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		defaultKey.PublicKey().Address(),
		defaultKey.PublicKey().Address(),
		NumberOfShares,
		testNetworkID,
		defaultKey,
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
		defaultNonce+1,
		defaultStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		defaultKey.PublicKey().Address(),
		defaultKey.PublicKey().Address(),
		NumberOfShares,
		testNetworkID,
		defaultKey,
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
		defaultNonce+1,
		MinimumStakeAmount-1,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		defaultKey.PublicKey().Address(),
		defaultKey.PublicKey().Address(),
		NumberOfShares,
		testNetworkID,
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have errored because stake amount too small")
	}

	// Case 7: Too many shares
	tx, err = vm.newAddDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		defaultKey.PublicKey().Address(),
		defaultKey.PublicKey().Address(),
		NumberOfShares+1,
		testNetworkID,
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have errored because of too many shares")
	}

	// Case 8.1: Validation length is too short
	tx, err = vm.newAddDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateStartTime.Add(MinimumStakingDuration).Unix())-1,
		defaultKey.PublicKey().Address(),
		defaultKey.PublicKey().Address(),
		NumberOfShares,
		testNetworkID,
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have errored because validation length too short")
	}

	// Case 8.2: Validation length is negative
	tx, err = vm.newAddDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateStartTime.Unix())-1,
		defaultKey.PublicKey().Address(),
		defaultKey.PublicKey().Address(),
		NumberOfShares,
		testNetworkID,
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have errored because validation length too short")
	}

	// Case 9: Validation length is too long
	tx, err = vm.newAddDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateStartTime.Add(MaximumStakingDuration).Unix())+1,
		defaultKey.PublicKey().Address(),
		defaultKey.PublicKey().Address(),
		NumberOfShares,
		testNetworkID,
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have errored because validation length too long")
	}

	// Case 10: Valid
	tx, err = vm.newAddDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		defaultKey.PublicKey().Address(),
		defaultKey.PublicKey().Address(),
		NumberOfShares,
		testNetworkID,
		defaultKey,
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

	// Case 1: Validator's start time too early
	tx, err := vm.newAddDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultStakeAmount,
		uint64(defaultValidateStartTime.Unix())-1,
		uint64(defaultValidateEndTime.Unix()),
		defaultKey.PublicKey().Address(),
		defaultKey.PublicKey().Address(),
		NumberOfShares,
		testNetworkID,
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := tx.SemanticVerify(vm.DB); err == nil {
		t.Fatal("should've errored because start time too early")
	}

	// Case 2: Validator doesn't have enough $AVA to cover stake amount
	tx, err = vm.newAddDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultBalance-txFee+1,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		defaultKey.PublicKey().Address(),
		defaultKey.PublicKey().Address(),
		NumberOfShares,
		testNetworkID,
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := tx.SemanticVerify(vm.DB); err == nil {
		t.Fatal("should've errored because validator doesn't have enough $AVA to cover stake")
	}

	// Case 3: Validator already validating default subnet
	tx, err = vm.newAddDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		defaultKey.PublicKey().Address(), // node ID
		defaultKey.PublicKey().Address(), // destination
		NumberOfShares,
		testNetworkID,
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := tx.SemanticVerify(vm.DB); err == nil {
		t.Fatal("should've errored because validator already validating")
	}

	// Case 4: Validator in pending validator set of default subnet
	key, err := vm.factory.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	startTime := defaultGenesisTime.Add(1 * time.Second)
	tx, err = vm.newAddDefaultSubnetValidatorTx(
		defaultNonce+1,                                       // nonce
		defaultStakeAmount,                                   // stake amount
		uint64(startTime.Unix()),                             // start time
		uint64(startTime.Add(MinimumStakingDuration).Unix()), // end time
		key.PublicKey().Address(),                            // node ID
		defaultKey.PublicKey().Address(),                     // destination
		NumberOfShares,                                       // shares
		testNetworkID,                                        // network
		defaultKey,                                           // key
	)
	if err != nil {
		t.Fatal(err)
	}

	// Put validator in pending validator set
	err = vm.putPendingValidators(vm.DB,
		&EventHeap{
			SortByStartTime: true,
			Txs:             []TimedTx{tx},
		},
		DefaultSubnetID,
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, _, _, err := tx.SemanticVerify(vm.DB); err == nil {
		t.Fatal("should have failed because validator in pending validator set")
	}
}
*/
