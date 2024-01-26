// (c) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

// MandatoryNetworkUpgrades contains timestamps that enable mandatory network upgrades.
// These upgrades are mandatory, meaning that if a node does not upgrade by the
// specified timestamp, it will be unable to participate in consensus.
// Avalanche specific network upgrades are also included here.
type MandatoryNetworkUpgrades struct {
	// SubnetEVMTimestamp is a placeholder that activates Avalanche Upgrades prior to ApricotPhase6 (nil = no fork, 0 = already activated)
	SubnetEVMTimestamp *uint64 `json:"subnetEVMTimestamp,omitempty"`
	// Durango activates the Shanghai Execution Spec Upgrade from Ethereum (https://github.com/ethereum/execution-specs/blob/master/network-upgrades/mainnet-upgrades/shanghai.md#included-eips)
	// and Avalanche Warp Messaging. (nil = no fork, 0 = already activated)
	// Note: EIP-4895 is excluded since withdrawals are not relevant to the Avalanche C-Chain or Subnets running the EVM.
	DurangoTimestamp *uint64 `json:"durangoTimestamp,omitempty"`
	// Cancun activates the Cancun upgrade from Ethereum. (nil = no fork, 0 = already activated)
	CancunTime *uint64 `json:"cancunTime,omitempty"`
}

func (m *MandatoryNetworkUpgrades) CheckMandatoryCompatible(newcfg *MandatoryNetworkUpgrades, time uint64) *ConfigCompatError {
	if isForkTimestampIncompatible(m.SubnetEVMTimestamp, newcfg.SubnetEVMTimestamp, time) {
		return newTimestampCompatError("SubnetEVM fork block timestamp", m.SubnetEVMTimestamp, newcfg.SubnetEVMTimestamp)
	}
	if isForkTimestampIncompatible(m.DurangoTimestamp, newcfg.DurangoTimestamp, time) {
		return newTimestampCompatError("Durango fork block timestamp", m.DurangoTimestamp, newcfg.DurangoTimestamp)
	}
	if isForkTimestampIncompatible(m.CancunTime, newcfg.CancunTime, time) {
		return newTimestampCompatError("Cancun fork block timestamp", m.CancunTime, m.CancunTime)
	}
	return nil
}

func (m *MandatoryNetworkUpgrades) mandatoryForkOrder() []fork {
	return []fork{
		{name: "subnetEVMTimestamp", timestamp: m.SubnetEVMTimestamp},
		{name: "durangoTimestamp", timestamp: m.DurangoTimestamp},
	}
}

// OptionalNetworkUpgrades includes overridable and optional Subnet-EVM network upgrades.
// These can be specified in genesis and upgrade configs.
// Timestamps can be different for each subnet network.
// TODO: once we add the first optional upgrade here, we should uncomment TestVMUpgradeBytesOptionalNetworkUpgrades
type OptionalNetworkUpgrades struct{}

func (n *OptionalNetworkUpgrades) CheckOptionalCompatible(newcfg *OptionalNetworkUpgrades, time uint64) *ConfigCompatError {
	return nil
}

func (n *OptionalNetworkUpgrades) optionalForkOrder() []fork {
	return []fork{}
}
