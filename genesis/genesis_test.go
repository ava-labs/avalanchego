// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"fmt"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/platformvm"
)

func TestNetworkName(t *testing.T) {
	if name := NetworkName(constants.ManhattanID); name != constants.ManhattanName {
		t.Fatalf("NetworkID was incorrectly named. Result: %s ; Expected: %s", name, constants.ManhattanName)
	}
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
	if name := NetworkName(constants.TestnetID); name != constants.ManhattanName {
		t.Fatalf("NetworkID was incorrectly named. Result: %s ; Expected: %s", name, constants.ManhattanName)
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
	genesisBytes, _, err := Genesis(constants.LocalID)
	if err != nil {
		t.Fatal(err)
	}
	generalAliases, _, _, err := Aliases(genesisBytes)
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
	genesisBytes, _, err := Genesis(constants.MainnetID)
	if err != nil {
		t.Fatal(err)
	}
	genesis := platformvm.Genesis{}
	if err := platformvm.GenesisCodec.Unmarshal(genesisBytes, &genesis); err != nil {
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
			networkID:  constants.ManhattanID,
			vmID:       avm.ID,
			expectedID: "2G6XnaMqqFj6tkYPw8i3eHFoHnQqfo2yaS5BAtNEL2Knayq6qP",
		},
		{
			networkID:  constants.MainnetID,
			vmID:       avm.ID,
			expectedID: "gA3rXYDWtU5fKkyid2xyQLiQbcEx6JvgqhWzrnZpdL6c4RN3x",
		},
		{
			networkID:  constants.LocalID,
			vmID:       avm.ID,
			expectedID: "2eNy1mUFdmaxXNj1eQHUe7Np4gju9sJsEtWQ4MX3ToiNKuADed",
		},
		{
			networkID:  constants.ManhattanID,
			vmID:       EVMID,
			expectedID: "2LcZK7Cp7LQNbvEbPfFvpr5qJqUjodghDndbWDZ7e6KYGMQ4jG",
		},
		{
			networkID:  constants.MainnetID,
			vmID:       EVMID,
			expectedID: "TkJfBJ3WTUc9NH9X7avkD3PLU2skEtsqN94NhE7zJNGzjJ4F",
		},
		{
			networkID:  constants.LocalID,
			vmID:       EVMID,
			expectedID: "26sSDdFXoKeShAqVfvugUiUQKhMZtHYDLeBqmBfNfcdjziTrZA",
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
			networkID:  constants.ManhattanID,
			expectedID: "2PQ2p3uTKfpS8Kmpss76NtDxdzikZw7uQj4x4ZKYHNRqWu5fMj",
		},
		{
			networkID:  constants.MainnetID,
			expectedID: "2F1rBxCF8dm3cBxPAJmRmvcNcSx5PHAXf5wdKAjGYRWRMm3Y3y",
		},
		{
			networkID:  constants.LocalID,
			expectedID: "2fombhL7aGPwj3KH4bfrmJwW6PVnMobf9Y2fn9GwxiAAJyFDbe",
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
