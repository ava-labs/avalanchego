// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Hardcoded network IDs
var (
	MainnetID uint32 = 1
	TestnetID uint32 = 2
	CascadeID uint32 = 2
	LocalID   uint32 = 12345

	MainnetName = "mainnet"
	TestnetName = "testnet"
	CascadeName = "cascade"
	LocalName   = "local"

	NetworkIDToNetworkName = map[uint32]string{
		MainnetID: MainnetName,
		TestnetID: CascadeName,
		LocalID:   LocalName,
	}
	NetworkNameToNetworkID = map[string]uint32{
		MainnetName: MainnetID,
		TestnetName: TestnetID,
		CascadeName: CascadeID,
		LocalName:   LocalID,
	}

	validNetworkName = regexp.MustCompile(`network-[0-9]+`)
)

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

	if id, err := strconv.ParseUint(networkName, 10, 0); err == nil {
		if id > math.MaxUint32 {
			return 0, fmt.Errorf("NetworkID %s not in [0, 2^32)", networkName)
		}
		return uint32(id), nil
	}
	if validNetworkName.MatchString(networkName) {
		if id, err := strconv.Atoi(networkName[8:]); err == nil {
			if id > math.MaxUint32 {
				return 0, fmt.Errorf("NetworkID %s not in [0, 2^32)", networkName)
			}
			return uint32(id), nil
		}
	}

	return 0, fmt.Errorf("Failed to parse %s as a network name", networkName)
}
