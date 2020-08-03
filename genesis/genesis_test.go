// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"testing"

	"github.com/ava-labs/gecko/vms/avm"
	"github.com/ava-labs/gecko/vms/platformvm"
	"github.com/ava-labs/gecko/vms/spchainvm"
	"github.com/ava-labs/gecko/vms/spdagvm"
)

func TestNetworkName(t *testing.T) {
	if name := NetworkName(MainnetID); name != MainnetName {
		t.Fatalf("NetworkID was incorrectly named. Result: %s ; Expected: %s", name, MainnetName)
	}
	if name := NetworkName(CascadeID); name != CascadeName {
		t.Fatalf("NetworkID was incorrectly named. Result: %s ; Expected: %s", name, CascadeName)
	}
	if name := NetworkName(DenaliID); name != DenaliName {
		t.Fatalf("NetworkID was incorrectly named. Result: %s ; Expected: %s", name, DenaliName)
	}
	if name := NetworkName(EverestID); name != EverestName {
		t.Fatalf("NetworkID was incorrectly named. Result: %s ; Expected: %s", name, EverestName)
	}
	if name := NetworkName(TestnetID); name != EverestName {
		t.Fatalf("NetworkID was incorrectly named. Result: %s ; Expected: %s", name, EverestName)
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

	id, err = NetworkID(CascadeName)
	if err != nil {
		t.Fatal(err)
	}
	if id != CascadeID {
		t.Fatalf("Returned wrong network. Expected: %d ; Returned %d", CascadeID, id)
	}

	id, err = NetworkID("cAsCaDe")
	if err != nil {
		t.Fatal(err)
	}
	if id != CascadeID {
		t.Fatalf("Returned wrong network. Expected: %d ; Returned %d", CascadeID, id)
	}

	id, err = NetworkID(DenaliName)
	if err != nil {
		t.Fatal(err)
	}
	if id != DenaliID {
		t.Fatalf("Returned wrong network. Expected: %d ; Returned %d", DenaliID, id)
	}

	id, err = NetworkID("dEnAlI")
	if err != nil {
		t.Fatal(err)
	}
	if id != DenaliID {
		t.Fatalf("Returned wrong network. Expected: %d ; Returned %d", DenaliID, id)
	}

	id, err = NetworkID(TestnetName)
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
	genesisBytes, _, err := Genesis(LocalID)
	if err != nil {
		t.Fatal(err)
	}
	genesis := platformvm.Genesis{}
	if err := platformvm.Codec.Unmarshal(genesisBytes, &genesis); err != nil {
		t.Fatal(err)
	}
}

// TODO: Uncomment when the genesis is finalized

// func TestVMGenesis(t *testing.T) {
// 	tests := []struct {
// 		networkID  uint32
// 		vmID       ids.ID
// 		expectedID string
// 	}{
// 		{
// 			networkID:  EverestID,
// 			vmID:       avm.ID,
// 			expectedID: "Qr1HjshGTPR94z1cuHoKPykJhDoRDuoRDZiR8ayxn27CXLU7T",
// 		},
// 		{
// 			networkID:  DenaliID,
// 			vmID:       avm.ID,
// 			expectedID: "gUadbMhXxsTmRFiDTouhiw3Kp1shRZ178tGxrfW1dGw7neGbX",
// 		},
// 		{
// 			networkID:  CascadeID,
// 			vmID:       avm.ID,
// 			expectedID: "oHeDJzpCh8sJS4scRtS9oQLgeYwNQywFcsgoZFicA662CcoZ4",
// 		},
// 		{
// 			networkID:  LocalID,
// 			vmID:       avm.ID,
// 			expectedID: "bSyUk9ZN4phchjkswxFvJC4cFPrdWQdSZADiLDGQW8ze5TtRN",
// 		},
// 		{
// 			networkID:  EverestID,
// 			vmID:       EVMID,
// 			expectedID: "htVdB6qqhyM5iHA8FBrXrxBvtNm9tAmaupkWHv79JzS7LF5Su",
// 		},
// 		{
// 			networkID:  DenaliID,
// 			vmID:       EVMID,
// 			expectedID: "2k2qmKmR8tFozCyALxcLGot5s7CZFaNZPmcsoHv9Z31iuWVuoC",
// 		},
// 		{
// 			networkID:  CascadeID,
// 			vmID:       EVMID,
// 			expectedID: "2mecQcyhZmBSwJgWCZfXGhVCAsn8fNhieenGYZj1LvqWmyWAfN",
// 		},
// 		{
// 			networkID:  LocalID,
// 			vmID:       EVMID,
// 			expectedID: "zbv2btTBZyodqpoPyq8rNNeF9gDQqGuiQiiQAP4vQiLbMf5c5",
// 		},
// 	}

// 	for _, test := range tests {
// 		genesisTx, err := VMGenesis(test.networkID, test.vmID)
// 		if err != nil {
// 			t.Fatal(err)
// 		}
// 		if result := genesisTx.ID().String(); test.expectedID != result {
// 			t.Fatalf("%s genesisID with networkID %d was expected to be %s but was %s",
// 				test.vmID,
// 				test.networkID,
// 				test.expectedID,
// 				result)
// 		}
// 	}
// }

// func TestAVAXAssetID(t *testing.T) {
// 	tests := []struct {
// 		networkID  uint32
// 		expectedID string
// 	}{
// 		{
// 			networkID:  EverestID,
// 			expectedID: "2JttwD4vZYYUETj12MMcCnGzcf1WBfSi6fPrHeZVdCXj5y4iYM",
// 		},
// 		{
// 			networkID:  DenaliID,
// 			expectedID: "2JttwD4vZYYUETj12MMcCnGzcf1WBfSi6fPrHeZVdCXj5y4iYM",
// 		},
// 		{
// 			networkID:  CascadeID,
// 			expectedID: "2JttwD4vZYYUETj12MMcCnGzcf1WBfSi6fPrHeZVdCXj5y4iYM",
// 		},
// 		{
// 			networkID:  LocalID,
// 			expectedID: "Q4KMfeLXTBgBpr367pRUuBzLoJqs2QtkvVJ6r4zV12HVj5K7X",
// 		},
// 	}

// 	for _, test := range tests {
// 		_, avaxAssetID, err := Genesis(test.networkID)
// 		if err != nil {
// 			t.Fatal(err)
// 		}
// 		if result := avaxAssetID.String(); test.expectedID != result {
// 			t.Fatalf("AVA assetID with networkID %d was expected to be %s but was %s",
// 				test.networkID,
// 				test.expectedID,
// 				result)
// 		}
// 	}
// }
