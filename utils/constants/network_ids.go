// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package constants

import (
	"regexp"

	"github.com/ava-labs/avalanchego/ids"
)

// Const variables to be exported
const (
	MainnetID  uint32 = 1
	CascadeID  uint32 = 2
	DenaliID   uint32 = 3
	EverestID  uint32 = 4
	TestnetID  uint32 = EverestID
	LocalID    uint32 = 12345
	UnitTestID uint32 = 10

	MainnetName  = "mainnet"
	CascadeName  = "cascade"
	DenaliName   = "denali"
	EverestName  = "everest"
	TestnetName  = "testnet"
	LocalName    = "local"
	UnitTestName = "testing"

	MainnetHRP  = "avax"
	CascadeHRP  = "cascade"
	DenaliHRP   = "denali"
	EverestHRP  = "everest"
	LocalHRP    = "local"
	UnitTestHRP = "testing"
	FallbackHRP = "custom"
)

// Variables to be exported
var (
	PrimaryNetworkID = ids.Empty
	PlatformChainID  = ids.Empty

	NetworkIDToNetworkName = map[uint32]string{
		MainnetID:  MainnetName,
		CascadeID:  CascadeName,
		DenaliID:   DenaliName,
		EverestID:  EverestName,
		LocalID:    LocalName,
		UnitTestID: UnitTestName,
	}
	NetworkNameToNetworkID = map[string]uint32{
		MainnetName:  MainnetID,
		CascadeName:  CascadeID,
		DenaliName:   DenaliID,
		EverestName:  EverestID,
		TestnetName:  TestnetID,
		LocalName:    LocalID,
		UnitTestName: UnitTestID,
	}

	NetworkIDToHRP = map[uint32]string{
		MainnetID:  MainnetHRP,
		CascadeID:  CascadeHRP,
		DenaliID:   DenaliHRP,
		EverestID:  EverestHRP,
		LocalID:    LocalHRP,
		UnitTestID: UnitTestHRP,
	}
	NetworkHRPToNetworkID = map[string]uint32{
		MainnetHRP:  MainnetID,
		CascadeHRP:  CascadeID,
		DenaliHRP:   DenaliID,
		EverestHRP:  EverestID,
		LocalHRP:    LocalID,
		UnitTestHRP: UnitTestID,
	}

	ValidNetworkName = regexp.MustCompile(`network-[0-9]+`)
)

// GetHRP returns the Human-Readable-Part of bech32 addresses for a networkID
func GetHRP(networkID uint32) string {
	if hrp, ok := NetworkIDToHRP[networkID]; ok {
		return hrp
	}
	return FallbackHRP
}
