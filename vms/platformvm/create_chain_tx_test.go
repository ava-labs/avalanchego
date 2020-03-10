// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"testing"

	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
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
		nil,
		avm.ID,
		nil,
		"chain name",
		testNetworkID+1,
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	err = tx.SyntacticVerify()
	t.Log(err)
	if err == nil {
		t.Fatal("should've errored because network ID is wrong")
	}

	// case 3: tx ID is empty
	tx, err = vm.newCreateChainTx(
		defaultNonce+1,
		nil,
		avm.ID,
		nil,
		"chain name",
		testNetworkID,
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
		nil,
		avm.ID,
		nil,
		"chain name",
		testNetworkID,
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	tx.VMID = ids.ID{}
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should've errored because tx ID is empty")
	}
}

func TestSemanticVerify(t *testing.T) {
	vm := defaultVM()

	// create a tx
	tx, err := vm.newCreateChainTx(
		defaultNonce+1,
		nil,
		avm.ID,
		nil,
		"chain name",
		testNetworkID,
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}

	newDB := versiondb.New(vm.DB)

	_, err = tx.SemanticVerify(newDB)
	if err != nil {
		t.Fatal(err)
	}

	chains, err := vm.getChains(newDB)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range chains {
		if c.ID().Equals(tx.ID()) {
			return
		}
	}
	t.Fatalf("Should have added the chain to the set of chains")
}

func TestSemanticVerifyAlreadyExisting(t *testing.T) {
	vm := defaultVM()

	// create a tx
	tx, err := vm.newCreateChainTx(
		defaultNonce+1,
		nil,
		avm.ID,
		nil,
		"chain name",
		testNetworkID,
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}

	// put the chain in existing chain
	if err := vm.putChains(vm.DB, []*CreateChainTx{tx}); err != nil {
		t.Fatal(err)
	}

	_, err = tx.SemanticVerify(vm.DB)
	if err == nil {
		t.Fatalf("should have failed because there is already a chain with ID %s", tx.id)
	}
}
