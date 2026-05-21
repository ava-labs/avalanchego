// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package constants

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/MetalBlockchain/metalgo/ids"
	"github.com/MetalBlockchain/metalgo/utils/set"
)

// Const variables to be exported
const (
	MainnetID uint32 = 1
	CascadeID uint32 = 2
	DenaliID  uint32 = 3
	EverestID uint32 = 4
	TahoeID   uint32 = 5

	TestnetID  uint32 = TahoeID
	UnitTestID uint32 = 10
	LocalID    uint32 = 12345

	MainnetName  = "mainnet"
	CascadeName  = "cascade"
	DenaliName   = "denali"
	EverestName  = "everest"
	TahoeName    = "tahoe"
	TestnetName  = "testnet"
	UnitTestName = "testing"
	LocalName    = "local"

	MainnetHRP  = "metal"
	CascadeHRP  = "cascade"
	DenaliHRP   = "denali"
	EverestHRP  = "everest"
	TahoeHRP    = "tahoe"
	UnitTestHRP = "testing"
	LocalHRP    = "local"
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
		TahoeID:    TahoeName,
		UnitTestID: UnitTestName,
		LocalID:    LocalName,
	}
	NetworkNameToNetworkID = map[string]uint32{
		MainnetName:  MainnetID,
		CascadeName:  CascadeID,
		DenaliName:   DenaliID,
		EverestName:  EverestID,
		TahoeName:    TahoeID,
		TestnetName:  TestnetID,
		UnitTestName: UnitTestID,
		LocalName:    LocalID,
	}

	NetworkIDToHRP = map[uint32]string{
		MainnetID:  MainnetHRP,
		CascadeID:  CascadeHRP,
		DenaliID:   DenaliHRP,
		EverestID:  EverestHRP,
		TahoeID:    TahoeHRP,
		UnitTestID: UnitTestHRP,
		LocalID:    LocalHRP,
	}
	NetworkHRPToNetworkID = map[string]uint32{
		MainnetHRP:  MainnetID,
		CascadeHRP:  CascadeID,
		DenaliHRP:   DenaliID,
		EverestHRP:  EverestID,
		TahoeHRP:    TahoeID,
		UnitTestHRP: UnitTestID,
		LocalHRP:    LocalID,
	}
	ProductionNetworkIDs = set.Of(MainnetID, TahoeID)

	ValidNetworkPrefix = "network-"

	ErrParseNetworkName = errors.New("failed to parse network name")
)

// GetHRP returns the Human-Readable-Part of bech32 addresses for a networkID
func GetHRP(networkID uint32) string {
	if hrp, ok := NetworkIDToHRP[networkID]; ok {
		return hrp
	}
	return FallbackHRP
}

// NetworkName returns a human readable name for the network with
// ID [networkID]
func NetworkName(networkID uint32) string {
	if name, exists := NetworkIDToNetworkName[networkID]; exists {
		return name
	}
	return fmt.Sprintf("network-%d", networkID)
}

// NetworkID returns the ID of the network with name [networkName]
func NetworkID(networkName string) (uint32, error) {
	networkName = strings.ToLower(networkName)
	if id, exists := NetworkNameToNetworkID[networkName]; exists {
		return id, nil
	}

	idStr := networkName
	if strings.HasPrefix(networkName, ValidNetworkPrefix) {
		idStr = networkName[len(ValidNetworkPrefix):]
	}
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%w: %q", ErrParseNetworkName, networkName)
	}
	return uint32(id), nil
}
