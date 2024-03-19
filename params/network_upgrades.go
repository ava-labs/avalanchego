// (c) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"fmt"
	"reflect"
	"strconv"

	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/subnet-evm/utils"
)

// NetworkUpgrades contains timestamps that enable network upgrades.
// Avalanche specific network upgrades are also included here.
type NetworkUpgrades struct {
	// SubnetEVMTimestamp is a placeholder that activates Avalanche Upgrades prior to ApricotPhase6 (nil = no fork, 0 = already activated)
	SubnetEVMTimestamp *uint64 `json:"subnetEVMTimestamp,omitempty"`
	// Durango activates the Shanghai Execution Spec Upgrade from Ethereum (https://github.com/ethereum/execution-specs/blob/master/network-upgrades/mainnet-upgrades/shanghai.md#included-eips)
	// and Avalanche Warp Messaging. (nil = no fork, 0 = already activated)
	// Note: EIP-4895 is excluded since withdrawals are not relevant to the Avalanche C-Chain or Subnets running the EVM.
	DurangoTimestamp *uint64 `json:"durangoTimestamp,omitempty"`
}

func (n *NetworkUpgrades) Equal(other *NetworkUpgrades) bool {
	return reflect.DeepEqual(n, other)
}

func (n *NetworkUpgrades) CheckNetworkUpgradesCompatible(newcfg *NetworkUpgrades, time uint64) *ConfigCompatError {
	if isForkTimestampIncompatible(n.SubnetEVMTimestamp, newcfg.SubnetEVMTimestamp, time) {
		return newTimestampCompatError("SubnetEVM fork block timestamp", n.SubnetEVMTimestamp, newcfg.SubnetEVMTimestamp)
	}
	if isForkTimestampIncompatible(n.DurangoTimestamp, newcfg.DurangoTimestamp, time) {
		return newTimestampCompatError("Durango fork block timestamp", n.DurangoTimestamp, newcfg.DurangoTimestamp)
	}
	return nil
}

func (n *NetworkUpgrades) forkOrder() []fork {
	return []fork{
		{name: "subnetEVMTimestamp", timestamp: n.SubnetEVMTimestamp},
		{name: "durangoTimestamp", timestamp: n.DurangoTimestamp},
	}
}

// setDefaults sets the default values for the network upgrades.
// This overrides deactivating the network upgrade by providing a timestamp of nil value.
func (n *NetworkUpgrades) setDefaults(networkID uint32) {
	defaults := getDefaultNetworkUpgrades(networkID)
	if n.SubnetEVMTimestamp == nil {
		n.SubnetEVMTimestamp = defaults.SubnetEVMTimestamp
	}
	if n.DurangoTimestamp == nil {
		n.DurangoTimestamp = defaults.DurangoTimestamp
	}
}

// VerifyNetworkUpgrades checks that the network upgrades are well formed.
func (n *NetworkUpgrades) VerifyNetworkUpgrades(networkID uint32) error {
	defaults := getDefaultNetworkUpgrades(networkID)
	if isNilOrSmaller(n.SubnetEVMTimestamp, *defaults.SubnetEVMTimestamp) {
		return fmt.Errorf("SubnetEVM fork block timestamp (%v) must be greater than or equal to %v", nilOrValueStr(n.SubnetEVMTimestamp), *defaults.SubnetEVMTimestamp)
	}
	if isNilOrSmaller(n.DurangoTimestamp, *defaults.DurangoTimestamp) {
		return fmt.Errorf("Durango fork block timestamp (%v) must be greater than or equal to %v", nilOrValueStr(n.DurangoTimestamp), *defaults.DurangoTimestamp)
	}
	return nil
}

func (n *NetworkUpgrades) Override(o *NetworkUpgrades) {
	if o.SubnetEVMTimestamp != nil {
		n.SubnetEVMTimestamp = o.SubnetEVMTimestamp
	}
	if o.DurangoTimestamp != nil {
		n.DurangoTimestamp = o.DurangoTimestamp
	}
}

// getDefaultNetworkUpgrades returns the network upgrades for the specified network ID.
// These should not return nil values.
func getDefaultNetworkUpgrades(networkID uint32) NetworkUpgrades {
	return NetworkUpgrades{
		SubnetEVMTimestamp: utils.NewUint64(0),
		DurangoTimestamp:   getUpgradeTime(networkID, version.DurangoTimes),
	}
}

func isNilOrSmaller(a *uint64, b uint64) bool {
	if a == nil {
		return true
	}
	return *a < b
}

func nilOrValueStr(a *uint64) string {
	if a == nil {
		return "nil"
	}
	return strconv.FormatUint(*a, 10)
}
