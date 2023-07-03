// (c) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"math/big"

	"github.com/ava-labs/avalanchego/utils/constants"
)

var (
	LocalNetworkUpgrades = MandatoryNetworkUpgrades{
		SubnetEVMTimestamp: big.NewInt(0),
		DUpgradeTimestamp:  big.NewInt(0),
	}

	FujiNetworkUpgrades = MandatoryNetworkUpgrades{
		SubnetEVMTimestamp: big.NewInt(0),
		//	DUpgradeTimestamp:           big.NewInt(0), // TODO: Uncomment and set this to the correct value
	}

	MainnetNetworkUpgrades = MandatoryNetworkUpgrades{
		SubnetEVMTimestamp: big.NewInt(0),
		//	DUpgradeTimestamp:           big.NewInt(0), // TODO: Uncomment and set this to the correct value
	}
)

// MandatoryNetworkUpgrades contains timestamps that enable mandatory network upgrades.
// These upgrades are mandatory, meaning that if a node does not upgrade by the
// specified timestamp, it will be unable to participate in consensus.
// Avalanche specific network upgrades are also included here.
type MandatoryNetworkUpgrades struct {
	SubnetEVMTimestamp *big.Int `json:"subnetEVMTimestamp,omitempty"` // initial subnet-evm upgrade (nil = no fork, 0 = already activated)
	DUpgradeTimestamp  *big.Int `json:"dUpgradeTimestamp,omitempty"`  // A placeholder for the latest avalanche forks (nil = no fork, 0 = already activated)
}

func (m *MandatoryNetworkUpgrades) CheckMandatoryCompatible(newcfg *MandatoryNetworkUpgrades, headTimestamp *big.Int) *ConfigCompatError {
	if isForkIncompatible(m.SubnetEVMTimestamp, newcfg.SubnetEVMTimestamp, headTimestamp) {
		return newCompatError("SubnetEVM fork block timestamp", m.SubnetEVMTimestamp, newcfg.SubnetEVMTimestamp)
	}
	if isForkIncompatible(m.DUpgradeTimestamp, newcfg.DUpgradeTimestamp, headTimestamp) {
		return newCompatError("DUpgrade fork block timestamp", m.DUpgradeTimestamp, newcfg.DUpgradeTimestamp)
	}
	return nil
}

func (m *MandatoryNetworkUpgrades) mandatoryForkOrder() []fork {
	return []fork{
		{name: "subnetEVMTimestamp", block: m.SubnetEVMTimestamp},
		{name: "dUpgradeTimestamp", block: m.DUpgradeTimestamp},
	}
}

func GetMandatoryNetworkUpgrades(networkID uint32) MandatoryNetworkUpgrades {
	switch networkID {
	case constants.FujiID:
		return FujiNetworkUpgrades
	case constants.MainnetID:
		return MainnetNetworkUpgrades
	default:
		return LocalNetworkUpgrades
	}
}

// OptionalNetworkUpgrades includes overridable and optional Subnet-EVM network upgrades.
// These can be specified in genesis and upgrade configs.
// Timestamps can be different for each subnet network.
// TODO: once we add the first optional upgrade here, we should uncomment TestVMUpgradeBytesOptionalNetworkUpgrades
type OptionalNetworkUpgrades struct{}

func (n *OptionalNetworkUpgrades) CheckOptionalCompatible(newcfg *OptionalNetworkUpgrades, headTimestamp *big.Int) *ConfigCompatError {
	return nil
}

func (n *OptionalNetworkUpgrades) optionalForkOrder() []fork {
	return []fork{}
}
