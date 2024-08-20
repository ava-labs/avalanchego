// (c) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"fmt"
	"reflect"

	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/subnet-evm/utils"
)

var (
	errCannotBeNil = fmt.Errorf("timestamp cannot be nil")
)

// NetworkUpgrades contains timestamps that enable network upgrades.
// Avalanche specific network upgrades are also included here.
type NetworkUpgrades struct {
	// SubnetEVMTimestamp is a placeholder that activates Avalanche Upgrades prior to ApricotPhase6
	SubnetEVMTimestamp *uint64 `json:"subnetEVMTimestamp,omitempty"`
	// Durango activates the Shanghai Execution Spec Upgrade from Ethereum (https://github.com/ethereum/execution-specs/blob/master/network-upgrades/mainnet-upgrades/shanghai.md#included-eips)
	// and Avalanche Warp Messaging.
	// Note: EIP-4895 is excluded since withdrawals are not relevant to the Avalanche C-Chain or Subnets running the EVM.
	DurangoTimestamp *uint64 `json:"durangoTimestamp,omitempty"`
	// Placeholder for EtnaTimestamp
	EtnaTimestamp *uint64 `json:"etnaTimestamp,omitempty"`
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
	if isForkTimestampIncompatible(n.EtnaTimestamp, newcfg.EtnaTimestamp, time) {
		return newTimestampCompatError("Etna fork block timestamp", n.EtnaTimestamp, newcfg.EtnaTimestamp)
	}
	return nil
}

func (n *NetworkUpgrades) forkOrder() []fork {
	return []fork{
		{name: "subnetEVMTimestamp", timestamp: n.SubnetEVMTimestamp},
		{name: "durangoTimestamp", timestamp: n.DurangoTimestamp},
		{name: "etnaTimestamp", timestamp: n.EtnaTimestamp},
	}
}

// setDefaults sets the default values for the network upgrades.
// This overrides deactivating the network upgrade by providing a timestamp of nil value.
func (n *NetworkUpgrades) setDefaults(agoUpgrades upgrade.Config) {
	defaults := getDefaultNetworkUpgrades(agoUpgrades)
	// If the network upgrade is not set, set it to the default value.
	// If the network upgrade is set to 0, we also treat it as nil and set it default.
	// This is because in prior versions, upgrades were not modifiable and were directly set to their default values.
	// Most of the tools and configurations just provide these as 0, so it is safer to treat 0 as nil and set to default
	// to prevent premature activations of the network upgrades for live networks.
	if n.SubnetEVMTimestamp == nil || *n.SubnetEVMTimestamp == 0 {
		n.SubnetEVMTimestamp = defaults.SubnetEVMTimestamp
	}
	if n.DurangoTimestamp == nil || *n.DurangoTimestamp == 0 {
		n.DurangoTimestamp = defaults.DurangoTimestamp
	}
	if n.EtnaTimestamp == nil || *n.EtnaTimestamp == 0 {
		n.EtnaTimestamp = defaults.EtnaTimestamp
	}
}

// verifyNetworkUpgrades checks that the network upgrades are well formed.
func (n *NetworkUpgrades) verifyNetworkUpgrades(agoUpgrades upgrade.Config) error {
	defaults := getDefaultNetworkUpgrades(agoUpgrades)
	if err := verifyWithDefault(n.SubnetEVMTimestamp, defaults.SubnetEVMTimestamp); err != nil {
		return fmt.Errorf("SubnetEVM fork block timestamp is invalid: %w", err)
	}
	if err := verifyWithDefault(n.DurangoTimestamp, defaults.DurangoTimestamp); err != nil {
		return fmt.Errorf("Durango fork block timestamp is invalid: %w", err)
	}
	if err := verifyWithDefault(n.EtnaTimestamp, defaults.EtnaTimestamp); err != nil {
		return fmt.Errorf("Etna fork block timestamp is invalid: %w", err)
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
	if o.EtnaTimestamp != nil {
		n.EtnaTimestamp = o.EtnaTimestamp
	}
}

// IsSubnetEVM returns whether [time] represents a block
// with a timestamp after the SubnetEVM upgrade time.
func (n *NetworkUpgrades) IsSubnetEVM(time uint64) bool {
	return isTimestampForked(n.SubnetEVMTimestamp, time)
}

// IsDurango returns whether [time] represents a block
// with a timestamp after the Durango upgrade time.
func (n *NetworkUpgrades) IsDurango(time uint64) bool {
	return isTimestampForked(n.DurangoTimestamp, time)
}

// IsEtna returns whether [time] represents a block
// with a timestamp after the Etna upgrade time.
func (n *NetworkUpgrades) IsEtna(time uint64) bool {
	return isTimestampForked(n.EtnaTimestamp, time)
}

func (n *NetworkUpgrades) Description() string {
	var banner string
	banner += fmt.Sprintf(" - SubnetEVM Timestamp:          @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.10.0)\n", ptrToString(n.SubnetEVMTimestamp))
	banner += fmt.Sprintf(" - Durango Timestamp:            @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.11.0)\n", ptrToString(n.DurangoTimestamp))
	banner += fmt.Sprintf(" - Etna Timestamp:           @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.12.0)\n", ptrToString(n.EtnaTimestamp))
	return banner
}

type AvalancheRules struct {
	IsSubnetEVM bool
	IsDurango   bool
	IsEtna      bool
}

func (n *NetworkUpgrades) GetAvalancheRules(time uint64) AvalancheRules {
	return AvalancheRules{
		IsSubnetEVM: n.IsSubnetEVM(time),
		IsDurango:   n.IsDurango(time),
		IsEtna:      n.IsEtna(time),
	}
}

// getDefaultNetworkUpgrades returns the network upgrades for the specified avalanchego upgrades.
// These should not return nil values.
func getDefaultNetworkUpgrades(agoUpgrade upgrade.Config) NetworkUpgrades {
	return NetworkUpgrades{
		SubnetEVMTimestamp: utils.NewUint64(0),
		DurangoTimestamp:   utils.TimeToNewUint64(agoUpgrade.DurangoTime),
		EtnaTimestamp:      utils.TimeToNewUint64(agoUpgrade.EtnaTime),
	}
}

// verifyWithDefault checks that the provided timestamp is greater than or equal to the default timestamp.
func verifyWithDefault(configTimestamp *uint64, defaultTimestamp *uint64) error {
	if configTimestamp == nil {
		return errCannotBeNil
	}

	if *configTimestamp < *defaultTimestamp {
		return fmt.Errorf("provided timestamp (%d) must be greater than or equal to the default timestamp (%d)", *configTimestamp, *defaultTimestamp)
	}
	return nil
}
