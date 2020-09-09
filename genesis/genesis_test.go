// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"fmt"
	"testing"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/constants"
	"github.com/ava-labs/gecko/vms/avm"
	"github.com/ava-labs/gecko/vms/platformvm"
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
			expectedID: "CQYQYFc5UchSXcxNK75UPfSkf9iXXvfZqftRhfXath4JaxQjR",
		},
		{
			networkID:  constants.DenaliID,
			vmID:       avm.ID,
			expectedID: "CPqbS52BCHgxdxhvH8ELMuEDbcduEDMEhfMQTLiUftrvwcf61",
		},
		{
			networkID:  constants.CascadeID,
			vmID:       avm.ID,
			expectedID: "2FMMbutr6ZR4deeJQ3U5uVgiuvxnfc7wGrkVv6y2o8JwrhG9gz",
		},
		{
			networkID:  constants.LocalID,
			vmID:       avm.ID,
			expectedID: "2P14wP6TYcetS5PAACK9nwSaGxY32qNvCQztw6jcHoNVNWMGcv",
		},
		{
			networkID:  constants.EverestID,
			vmID:       EVMID,
			expectedID: "2aUMsnB39oCq8D35Y8x1R12ghjHz26QPCphXE5YXsyey5Mfz3V",
		},
		{
			networkID:  constants.DenaliID,
			vmID:       EVMID,
			expectedID: "2GiRVhpWTmJcSEkHzMaVcnezUa68kKJouoqsH8NMR8tb8E4Ut5",
		},
		{
			networkID:  constants.CascadeID,
			vmID:       EVMID,
			expectedID: "RFK5ybh7KeoikDaR1oF1KNs6FApi4wuRQY1BV2WjumQ7Ast1X",
		},
		{
			networkID:  constants.LocalID,
			vmID:       EVMID,
			expectedID: "E4HBwwHxuroHfFrcnf8eTbEUAGWfG9gXYNnx6izxaL31SzUVH",
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
