// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"testing"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/vms/avm"
	"github.com/ava-labs/gecko/vms/platformvm"
	"github.com/ava-labs/gecko/vms/spchainvm"
	"github.com/ava-labs/gecko/vms/spdagvm"
)

func TestNetworkName(t *testing.T) {
	if name := NetworkName(MainnetID); name != MainnetName {
		t.Fatalf("NetworkID was incorrectly named. Result: %s ; Expected: %s", name, MainnetName)
	}
	if name := NetworkName(TestnetID); name != CascadeName {
		t.Fatalf("NetworkID was incorrectly named. Result: %s ; Expected: %s", name, CascadeName)
	}
	if name := NetworkName(CascadeID); name != CascadeName {
		t.Fatalf("NetworkID was incorrectly named. Result: %s ; Expected: %s", name, CascadeName)
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

	id, err = NetworkID(CascadeName)
	if err != nil {
		t.Fatal(err)
	}
	if id != TestnetID {
		t.Fatalf("Returned wrong network. Expected: %d ; Returned %d", TestnetID, id)
	}

	id, err = NetworkID("cAsCaDe")
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
	generalAliases, _, _, _ := Aliases(LocalID)
	if _, exists := generalAliases["vm/"+platformvm.ID.String()]; !exists {
		t.Fatalf("Should have a custom alias from the vm")
	} else if _, exists := generalAliases["vm/"+avm.ID.String()]; !exists {
		t.Fatalf("Should have a custom alias from the vm")
	} else if _, exists := generalAliases["vm/"+EVMID.String()]; !exists {
		t.Fatalf("Should have a custom alias from the vm")
	} else if _, exists := generalAliases["vm/"+spdagvm.ID.String()]; !exists {
		t.Fatalf("Should have a custom alias from the vm")
	} else if _, exists := generalAliases["vm/"+spchainvm.ID.String()]; !exists {
		t.Fatalf("Should have a custom alias from the vm")
	}
}

func TestGenesis(t *testing.T) {
	genesisBytes, err := Genesis(LocalID)
	if err != nil {
		t.Fatal(err)
	}
	genesis := platformvm.Genesis{}
	if err := platformvm.Codec.Unmarshal(genesisBytes, &genesis); err != nil {
		t.Fatal(err)
	}
}

func TestVMGenesis(t *testing.T) {
	tests := []struct {
		networkID  uint32
		vmID       ids.ID
		expectedID string
	}{
		{
			networkID:  CascadeID,
			vmID:       avm.ID,
			expectedID: "4ktRjsAKxgMr2aEzv9SWmrU7Xk5FniHUrVCX4P1TZSfTLZWFM",
		},
		{
			networkID:  LocalID,
			vmID:       avm.ID,
			expectedID: "4R5p2RXDGLqaifZE4hHWH9owe34pfoBULn1DrQTWivjg8o4aH",
		},
		{
			networkID:  CascadeID,
			vmID:       EVMID,
			expectedID: "2mUYSXfLrDtigwbzj1LxKVsHwELghc5sisoXrzJwLqAAQHF4i",
		},
		{
			networkID:  LocalID,
			vmID:       EVMID,
			expectedID: "tZGm6RCkeGpVETUTp11DW3UYFZmm69zfqxchpHrSF7wgy8rmw",
		},
	}

	for _, test := range tests {
		genesisTx, err := VMGenesis(test.networkID, test.vmID)
		if err != nil {
			t.Fatal(err)
		}
		if result := genesisTx.ID().String(); test.expectedID != result {
			t.Fatalf("%s genesisID with networkID %d was expected to be %s but was %s",
				test.vmID,
				test.networkID,
				test.expectedID,
				result)
		}
	}
}

func TestAVAAssetID(t *testing.T) {
	tests := []struct {
		networkID  uint32
		expectedID string
	}{
		{
			networkID:  CascadeID,
			expectedID: "21d7KVtPrubc5fHr6CGNcgbUb4seUjmZKr35ZX7BZb5iP8pXWA",
		},
		{
			networkID:  LocalID,
			expectedID: "n8XH5JY1EX5VYqDeAhB4Zd4GKxi9UNQy6oPpMsCAj1Q6xkiiL",
		},
	}

	for _, test := range tests {
		avaID, err := AVAAssetID(test.networkID)
		if err != nil {
			t.Fatal(err)
		}
		if result := avaID.String(); test.expectedID != result {
			t.Fatalf("AVA assetID with networkID %d was expected to be %s but was %s",
				test.networkID,
				test.expectedID,
				result)
		}
	}
}
