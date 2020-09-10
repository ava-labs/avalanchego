// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"fmt"
	"testing"

	"github.com/ava-labs/avalanche-go/ids"
	"github.com/ava-labs/avalanche-go/utils/constants"
	"github.com/ava-labs/avalanche-go/vms/avm"
	"github.com/ava-labs/avalanche-go/vms/platformvm"
)

func TestNetworkName(t *testing.T) {
	if name := NetworkName(constants.MainnetID); name != constants.MainnetName {
		t.Fatalf("NetworkID was incorrectly named. Result: %s ; Expected: %s", name, constants.MainnetName)
	}
	if name := NetworkName(constants.CascadeID); name != constants.CascadeName {
		t.Fatalf("NetworkID was incorrectly named. Result: %s ; Expected: %s", name, constants.CascadeName)
	}
	if name := NetworkName(constants.DenaliID); name != constants.DenaliName {
		t.Fatalf("NetworkID was incorrectly named. Result: %s ; Expected: %s", name, constants.DenaliName)
	}
	if name := NetworkName(constants.EverestID); name != constants.EverestName {
		t.Fatalf("NetworkID was incorrectly named. Result: %s ; Expected: %s", name, constants.EverestName)
	}
	if name := NetworkName(constants.TestnetID); name != constants.EverestName {
		t.Fatalf("NetworkID was incorrectly named. Result: %s ; Expected: %s", name, constants.EverestName)
	}
	if name := NetworkName(4294967295); name != "network-4294967295" {
		t.Fatalf("NetworkID was incorrectly named. Result: %s ; Expected: %s", name, "network-4294967295")
	}
}

func TestNetworkID(t *testing.T) {
	id, err := NetworkID(constants.MainnetName)
	if err != nil {
		t.Fatal(err)
	}
	if id != constants.MainnetID {
		t.Fatalf("Returned wrong network. Expected: %d ; Returned %d", constants.MainnetID, id)
	}

	id, err = NetworkID(constants.CascadeName)
	if err != nil {
		t.Fatal(err)
	}
	if id != constants.CascadeID {
		t.Fatalf("Returned wrong network. Expected: %d ; Returned %d", constants.CascadeID, id)
	}

	id, err = NetworkID("cAsCaDe")
	if err != nil {
		t.Fatal(err)
	}
	if id != constants.CascadeID {
		t.Fatalf("Returned wrong network. Expected: %d ; Returned %d", constants.CascadeID, id)
	}

	id, err = NetworkID(constants.DenaliName)
	if err != nil {
		t.Fatal(err)
	}
	if id != constants.DenaliID {
		t.Fatalf("Returned wrong network. Expected: %d ; Returned %d", constants.DenaliID, id)
	}

	id, err = NetworkID("dEnAlI")
	if err != nil {
		t.Fatal(err)
	}
	if id != constants.DenaliID {
		t.Fatalf("Returned wrong network. Expected: %d ; Returned %d", constants.DenaliID, id)
	}

	id, err = NetworkID(constants.TestnetName)
	if err != nil {
		t.Fatal(err)
	}
	if id != constants.TestnetID {
		t.Fatalf("Returned wrong network. Expected: %d ; Returned %d", constants.TestnetID, id)
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
	generalAliases, _, _, err := Aliases(constants.LocalID)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := generalAliases["vm/"+platformvm.ID.String()]; !exists {
		t.Fatalf("Should have a custom alias from the vm")
	} else if _, exists := generalAliases["vm/"+avm.ID.String()]; !exists {
		t.Fatalf("Should have a custom alias from the vm")
	} else if _, exists := generalAliases["vm/"+EVMID.String()]; !exists {
		t.Fatalf("Should have a custom alias from the vm")
	}
}

func TestGenesis(t *testing.T) {
	genesisBytes, _, err := Genesis(constants.LocalID)
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
			networkID:  constants.EverestID,
			vmID:       avm.ID,
			expectedID: "jnUjZSRt16TcRnZzmh5aMhavwVHz3zBrSN8GfFMTQkzUnoBxC",
		},
		{
			networkID:  constants.DenaliID,
			vmID:       avm.ID,
			expectedID: "2ku4ACFiovQnp2bDQ9MUib3q4mChtHZioAn3LqNfQA3ovA1dHP",
		},
		{
			networkID:  constants.CascadeID,
			vmID:       avm.ID,
			expectedID: "oMrNBf9aQ7kPDEPxfww7rsUntfJ3w7qNTowdRzMPejAJNC2p5",
		},
		{
			networkID:  constants.LocalID,
			vmID:       avm.ID,
			expectedID: "v4hFSZTNNVdyomeMoXa77dAz4CdxU3cziSb45TB7mfXUmy7C7",
		},
		{
			networkID:  constants.EverestID,
			vmID:       EVMID,
			expectedID: "saMG5YgNsFxzjz4NMkEkt3bAH6hVxWdZkWcEnGB3Z15pcAmsK",
		},
		{
			networkID:  constants.DenaliID,
			vmID:       EVMID,
			expectedID: "8ghyBLuZmgciT5f2nDiHBdjze5rqf5xPxHMYu41xxqENYwhFB",
		},
		{
			networkID:  constants.CascadeID,
			vmID:       EVMID,
			expectedID: "uZeMkrvmMMkw31hnJ31heDTPCFcdwT2qP7XefqiPkJjnLMonw",
		},
		{
			networkID:  constants.LocalID,
			vmID:       EVMID,
			expectedID: "2m6aMgMBJWsmT4Hv448n6sNAwGMFfugBvdU6PdY5oxZge4qb1W",
		},
	}

	for _, test := range tests {
		name := fmt.Sprintf("%s-%s",
			constants.NetworkIDToNetworkName[test.networkID],
			test.vmID,
		)
		t.Run(name, func(t *testing.T) {
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
		})
	}
}

func TestAVAXAssetID(t *testing.T) {
	tests := []struct {
		networkID  uint32
		expectedID string
	}{
		{
			networkID:  constants.EverestID,
			expectedID: "nznftJBicce1PfWQeNEVBmDyweZZ6zcM3p78z9Hy9Hhdhfaxm",
		},
		{
			networkID:  constants.DenaliID,
			expectedID: "bi7tCZzj11ExAL1wsSKeSdFmJcS2S3rEL5GtPfrTWk6SS4yG6",
		},
		{
			networkID:  constants.CascadeID,
			expectedID: "bi7tCZzj11ExAL1wsSKeSdFmJcS2S3rEL5GtPfrTWk6SS4yG6",
		},
		{
			networkID:  constants.LocalID,
			expectedID: "SSUAMrVdqYuvybAMGNitTYSAnE4T5fVdVDB82ped1qQ9f8DDM",
		},
	}

	for _, test := range tests {
		t.Run(constants.NetworkIDToNetworkName[test.networkID], func(t *testing.T) {
			_, avaxAssetID, err := Genesis(test.networkID)
			if err != nil {
				t.Fatal(err)
			}
			if result := avaxAssetID.String(); test.expectedID != result {
				t.Fatalf("AVAX assetID with networkID %d was expected to be %s but was %s",
					test.networkID,
					test.expectedID,
					result)
			}
		})
	}
}
