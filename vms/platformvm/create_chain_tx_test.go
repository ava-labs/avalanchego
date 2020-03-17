// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"testing"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/vms/avm"
)

// test method SyntacticVerify
func TestCreateChainTxSyntacticVerify(t *testing.T) {
	vm := defaultVM()

	// Case 1: tx is nil
	var tx *CreateChainTx
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have failed because tx is nil")
	}

	// Case 2: network ID is wrong
	tx, err := vm.newCreateChainTx(
		defaultNonce+1,
		testSubnet1.id,
		nil,
		avm.ID,
		nil,
		"chain name",
		testNetworkID+1,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	err = tx.SyntacticVerify()
	if err == nil {
		t.Fatal("should've errored because network ID is wrong")
	}

	// case 3: tx ID is empty
	tx, err = vm.newCreateChainTx(
		defaultNonce+1,
		testSubnet1.id,
		nil,
		avm.ID,
		nil,
		"chain name",
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	tx.id = ids.ID{}
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should've errored because tx ID is empty")
	}

	// Case 4: vm ID is empty
	tx, err = vm.newCreateChainTx(
		defaultNonce+1,
		testSubnet1.id,
		nil,
		avm.ID,
		nil,
		"chain name",
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	tx.VMID = ids.ID{}
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should've errored because tx ID is empty")
	}

	// Case 5: Control sigs not sorted
	tx, err = vm.newCreateChainTx(
		defaultNonce+1,
		testSubnet1.id,
		nil,
		avm.ID,
		nil,
		"chain name",
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	// Reverse signature order
	tx.ControlSigs[0], tx.ControlSigs[1] = tx.ControlSigs[1], tx.ControlSigs[0]
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should've errored because control sigs not sorted")
	}

	// Case 6: Control sigs not unique
	tx, err = vm.newCreateChainTx(
		defaultNonce+1,
		testSubnet1.id,
		nil,
		avm.ID,
		nil,
		"chain name",
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	tx.ControlSigs[0] = tx.ControlSigs[1]
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should've errored because control sigs not unique")
	}

	// Case 7: Control sigs are nil
	tx, err = vm.newCreateChainTx(
		defaultNonce+1,
		testSubnet1.id,
		nil,
		avm.ID,
		nil,
		"chain name",
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey,
	)
	if err != nil {
		t.Fatalf("should have passed verification but got %v", err)
	}
	tx.ControlSigs = nil
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should've errored because control sigs are nil")
	}

	// Case 8: Valid tx passes syntactic verification
	tx, err = vm.newCreateChainTx(
		defaultNonce+1,
		testSubnet1.id,
		nil,
		avm.ID,
		nil,
		"chain name",
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey,
	)
	if err != nil {
		t.Fatalf("should have passed verification but got %v", err)
	}
}

// Ensure SemanticVerify fails when there are not enough control sigs
func TestCreateChainTxInsufficientControlSigs(t *testing.T) {
	vm := defaultVM()

	// Case 1: No control sigs (2 are needed)
	tx, err := vm.newCreateChainTx(
		defaultNonce+1,
		testSubnet1.id,
		nil,
		avm.ID,
		nil,
		"chain name",
		testNetworkID,
		nil,
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = tx.SemanticVerify(vm.DB)
	if err == nil {
		t.Fatal("should have errored because there are no control sigs")
	}

	// Case 2: 1 control sig (2 are needed)
	tx, err = vm.newCreateChainTx(
		defaultNonce+1,
		testSubnet1.id,
		nil,
		avm.ID,
		nil,
		"chain name",
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0]},
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = tx.SemanticVerify(vm.DB)
	if err == nil {
		t.Fatal("should have errored because there are no control sigs")
	}
}

// Ensure SemanticVerify fails when an incorrect control signature is given
func TestCreateChainTxWrongControlSig(t *testing.T) {
	vm := defaultVM()

	// Generate new, random key to sign tx with
	factory := crypto.FactorySECP256K1R{}
	key, err := factory.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}

	tx, err := vm.newCreateChainTx(
		defaultNonce+1,
		testSubnet1.id,
		nil,
		avm.ID,
		nil,
		"chain name",
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], key.(*crypto.PrivateKeySECP256K1R)},
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = tx.SemanticVerify(vm.DB)
	if err == nil {
		t.Fatal("should have errored because incorrect control sig given")
	}
}

// Ensure SemanticVerify fails when the Subnet the blockchain specifies as
// its validator set doesn't exist
func TestCreateChainTxNoSuchSubnet(t *testing.T) {
	vm := defaultVM()

	tx, err := vm.newCreateChainTx(
		defaultNonce+1,
		ids.NewID([32]byte{1, 9, 124, 11, 20}), // pick some random ID for subnet
		nil,
		avm.ID,
		nil,
		"chain name",
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.SemanticVerify(vm.DB)
	if err == nil {
		t.Fatal("should have errored because Subnet doesn't exist")
	}
}

func TestCreateChainTxAlreadyExists(t *testing.T) {
	vm := defaultVM()

	// create a tx
	tx, err := vm.newCreateChainTx(
		defaultNonce+1,
		testSubnet1.id,
		nil,
		avm.ID,
		nil,
		"chain name",
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}

	// put the chain in existing chain list
	if err := vm.putChains(vm.DB, []*CreateChainTx{tx}); err != nil {
		t.Fatal(err)
	}

	_, err = tx.SemanticVerify(vm.DB)
	if err == nil {
		t.Fatalf("should have failed because there is already a chain with ID %s", tx.id)
	}
}

// Ensure valid tx passes semanticVerify
func TestCreateChainTxValid(t *testing.T) {
	vm := defaultVM()

	// create a valid tx
	tx, err := vm.newCreateChainTx(
		defaultNonce+1,
		testSubnet1.id,
		nil,
		avm.ID,
		nil,
		"chain name",
		testNetworkID,
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = tx.SemanticVerify(vm.DB)
	if err != nil {
		t.Fatalf("expected tx to pass verification but got error: %v", err)
	}
}
