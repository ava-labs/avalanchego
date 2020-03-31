// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"testing"

	"github.com/ava-labs/gecko/vms/avm"
	"github.com/ava-labs/gecko/vms/evm"
	"github.com/ava-labs/gecko/vms/platformvm"
	"github.com/ava-labs/gecko/vms/spchainvm"
	"github.com/ava-labs/gecko/vms/spdagvm"
)

func TestNetworkName(t *testing.T) {
	if name := NetworkName(MainnetID); name != MainnetName {
		t.Fatalf("NetworkID was incorrectly named. Result: %s ; Expected: %s", name, MainnetName)
	}
	if name := NetworkName(TestnetID); name != BorealisName {
		t.Fatalf("NetworkID was incorrectly named. Result: %s ; Expected: %s", name, BorealisName)
	}
	if name := NetworkName(BorealisID); name != BorealisName {
		t.Fatalf("NetworkID was incorrectly named. Result: %s ; Expected: %s", name, BorealisName)
	}
	if name := NetworkName(4294967295); name != "network-4294967295" {
		t.Fatalf("NetworkID was incorrectly named. Result: %s ; Expected: %s", name, "network-4294967295")
	}
}

func TestNetworkID(t *testing.T) {
	id, err := NetworkID(MainnetName)
	if err != nil {
		t.Fatal(err)
	}
	if id != MainnetID {
		t.Fatalf("Returned wrong network. Expected: %d ; Returned %d", MainnetID, id)
	}

	id, err = NetworkID(TestnetName)
	if err != nil {
		t.Fatal(err)
	}
	if id != TestnetID {
		t.Fatalf("Returned wrong network. Expected: %d ; Returned %d", TestnetID, id)
	}

	id, err = NetworkID(BorealisName)
	if err != nil {
		t.Fatal(err)
	}
	if id != TestnetID {
		t.Fatalf("Returned wrong network. Expected: %d ; Returned %d", TestnetID, id)
	}

	id, err = NetworkID("bOrEaLiS")
	if err != nil {
		t.Fatal(err)
	}
	if id != TestnetID {
		t.Fatalf("Returned wrong network. Expected: %d ; Returned %d", TestnetID, id)
	}

	id, err = NetworkID("network-4294967295")
	if err != nil {
		t.Fatal(err)
	}
	if id != 4294967295 {
		t.Fatalf("Returned wrong network. Expected: %d ; Returned %d", 4294967295, id)
	}

	id, err = NetworkID("4294967295")
	if err != nil {
		t.Fatal(err)
	}
	if id != 4294967295 {
		t.Fatalf("Returned wrong network. Expected: %d ; Returned %d", 4294967295, id)
	}

	if _, err := NetworkID("network-4294967296"); err == nil {
		t.Fatalf("Should have errored due to the network being too large.")
	}

	if _, err := NetworkID("4294967296"); err == nil {
		t.Fatalf("Should have errored due to the network being too large.")
	}

	if _, err := NetworkID("asdcvasdc-252"); err == nil {
		t.Fatalf("Should have errored due to the invalid input string.")
	}
}

func TestAliases(t *testing.T) {
	generalAliases, _, _ := Aliases(LocalID)
	if _, exists := generalAliases["vm/"+platformvm.ID.String()]; !exists {
		t.Fatalf("Should have a custom alias from the vm")
	} else if _, exists := generalAliases["vm/"+avm.ID.String()]; !exists {
		t.Fatalf("Should have a custom alias from the vm")
	} else if _, exists := generalAliases["vm/"+evm.ID.String()]; !exists {
		t.Fatalf("Should have a custom alias from the vm")
	} else if _, exists := generalAliases["vm/"+spdagvm.ID.String()]; !exists {
		t.Fatalf("Should have a custom alias from the vm")
	} else if _, exists := generalAliases["vm/"+spchainvm.ID.String()]; !exists {
		t.Fatalf("Should have a custom alias from the vm")
	}
}

func TestGenesis(t *testing.T) {
	genesisBytes, _ := Genesis(LocalID)
	genesis := platformvm.Genesis{}
	if err := platformvm.Codec.Unmarshal(genesisBytes, &genesis); err != nil {
		t.Fatal(err)
	}
}
