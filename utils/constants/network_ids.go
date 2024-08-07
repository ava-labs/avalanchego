// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************
// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package constants

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
)

// Const variables to be exported
const (
	MainnetID uint32 = 1
	FujiID    uint32 = 5

	CaminoID     uint32 = 1000
	ColumbusID   uint32 = 1001
	KopernikusID uint32 = 1002

	TestnetID  uint32 = ColumbusID
	UnitTestID uint32 = 10
	LocalID    uint32 = 12345

	MainnetName = "mainnet"
	FujiName    = "fuji"

	CaminoName     = "camino"
	ColumbusName   = "columbus"
	KopernikusName = "kopernikus"
	TestnetName    = "testnet"
	UnitTestName   = "testing"
	LocalName      = "local"

	MainnetHRP    = "avax"
	FujiHRP       = "fuji"
	CaminoHRP     = "camino"
	ColumbusHRP   = "columbus"
	KopernikusHRP = "kopernikus"
	UnitTestHRP   = "testing"
	LocalHRP      = "local"
	FallbackHRP   = "custom"
)

// Variables to be exported
var (
	PrimaryNetworkID = ids.Empty
	PlatformChainID  = ids.Empty

	NetworkIDToNetworkName = map[uint32]string{
		MainnetID:    MainnetName,
		FujiID:       FujiName,
		CaminoID:     CaminoName,
		ColumbusID:   ColumbusName,
		KopernikusID: KopernikusName,
		UnitTestID:   UnitTestName,
		LocalID:      LocalName,
	}
	NetworkNameToNetworkID = map[string]uint32{
		MainnetName:    MainnetID,
		FujiName:       FujiID,
		CaminoName:     CaminoID,
		ColumbusName:   ColumbusID,
		KopernikusName: KopernikusID,
		TestnetName:    TestnetID,
		UnitTestName:   UnitTestID,
		LocalName:      LocalID,
	}

	NetworkIDToHRP = map[uint32]string{
		MainnetID:    MainnetHRP,
		FujiID:       FujiHRP,
		CaminoID:     CaminoHRP,
		ColumbusID:   ColumbusHRP,
		KopernikusID: KopernikusHRP,
		UnitTestID:   UnitTestHRP,
		LocalID:      LocalHRP,
	}
	NetworkHRPToNetworkID = map[string]uint32{
		MainnetHRP:    MainnetID,
		FujiHRP:       FujiID,
		CaminoHRP:     CaminoID,
		ColumbusHRP:   ColumbusID,
		KopernikusHRP: KopernikusID,
		UnitTestHRP:   UnitTestID,
		LocalHRP:      LocalID,
	}

	ValidNetworkPrefix = "network-"
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
		return 0, fmt.Errorf("failed to parse %q as a network name", networkName)
	}
	return uint32(id), nil
}

func IsActiveNetwork(networkID uint32) bool {
	return networkID == MainnetID ||
		networkID == FujiID ||
		networkID == ColumbusID ||
		networkID == CaminoID ||
		networkID == KopernikusID
}
