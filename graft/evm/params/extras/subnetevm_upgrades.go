// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extras

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/upgrade"

	avagoUtils "github.com/ava-labs/avalanchego/utils"
	ethparams "github.com/ava-labs/libevm/params"
)

type SubnetEVMNetworkUpgrades struct {
	// SubnetEVMTimestamp is a placeholder that activates Avalanche Upgrades prior to ApricotPhase6
	SubnetEVMTimestamp *uint64 `json:"subnetEVMTimestamp,omitempty"`
	// Durango activates the Shanghai Execution Spec Upgrade from Ethereum (https://github.com/ethereum/execution-specs/blob/master/network-upgrades/mainnet-upgrades/shanghai.md#included-eips)
	// and Avalanche Warp Messaging.
	// Note: EIP-4895 is excluded since withdrawals are not relevant to the Avalanche C-Chain or Subnets running the EVM.
	DurangoTimestamp *uint64 `json:"durangoTimestamp,omitempty"`
}

func (s *SubnetEVMNetworkUpgrades) checkNetworkUpgradesCompatible(newcfg *SubnetEVMNetworkUpgrades, time uint64) *ethparams.ConfigCompatError {
	switch {
	case s == nil && newcfg == nil:
		return nil
	case s == nil && newcfg != nil:
		return ethparams.NewTimestampCompatError("expected nil subnetEVMNetworkUpgrade", nil, avagoUtils.PointerTo[uint64](0))
	case s != nil && newcfg == nil:
		return ethparams.NewTimestampCompatError("expected non-nil subnetEVMNetworkUpgrade", avagoUtils.PointerTo[uint64](0), nil)
	}

	if isForkTimestampIncompatible(s.SubnetEVMTimestamp, newcfg.SubnetEVMTimestamp, time) {
		return ethparams.NewTimestampCompatError("SubnetEVM fork block timestamp", s.SubnetEVMTimestamp, newcfg.SubnetEVMTimestamp)
	}
	if isForkTimestampIncompatible(s.DurangoTimestamp, newcfg.DurangoTimestamp, time) {
		return ethparams.NewTimestampCompatError("Durango fork block timestamp", s.DurangoTimestamp, newcfg.DurangoTimestamp)
	}
	return nil
}

func (s *SubnetEVMNetworkUpgrades) forkOrder() []Fork {
	return []Fork{
		{Name: "subnetEVMTimestamp", Timestamp: s.SubnetEVMTimestamp},
		{Name: "durangoTimestamp", Timestamp: s.DurangoTimestamp},
	}
}

func (s *SubnetEVMNetworkUpgrades) Description() string {
	var banner string
	banner += fmt.Sprintf(" - SubnetEVM Timestamp:          @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.10.0)\n", ptrToString(s.SubnetEVMTimestamp))
	banner += fmt.Sprintf(" - Durango Timestamp:            @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.11.0)\n", ptrToString(s.DurangoTimestamp))
	return banner
}

// SetSubnetEVMDefaults sets the default values for the SubnetEVM network upgrades.
// This overrides deactivating the network upgrade by providing a timestamp of nil value.
func (n *NetworkUpgrades) SetSubnetEVMDefaults(agoUpgrades upgrade.Config) {
	defaults := GetSubnetEVMNetworkUpgrades(agoUpgrades)
	if n.SubnetEVMNetworkUpgrades == nil {
		n.SubnetEVMNetworkUpgrades = &SubnetEVMNetworkUpgrades{}
	}
	// If the network upgrade is not set, set it to the default value.
	// If the network upgrade is set to 0, we also treat it as nil and set it default.
	// Invariant: This is because in prior versions, upgrades were not modifiable and were directly set to their default values.
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
	if n.FortunaTimestamp == nil || *n.FortunaTimestamp == 0 {
		n.FortunaTimestamp = defaults.FortunaTimestamp
	}
	if n.GraniteTimestamp == nil || *n.GraniteTimestamp == 0 {
		n.GraniteTimestamp = defaults.GraniteTimestamp
	}
	if n.HeliconTimestamp == nil || *n.HeliconTimestamp == 0 {
		n.HeliconTimestamp = defaults.HeliconTimestamp
	}
}

// GetNetworkUpgrades returns the network upgrades for the specified avalanchego upgrades.
// Nil values are used to indicate optional upgrades.
func GetSubnetEVMNetworkUpgrades(agoUpgrade upgrade.Config) NetworkUpgrades {
	return NetworkUpgrades{
		SubnetEVMNetworkUpgrades: &SubnetEVMNetworkUpgrades{
			SubnetEVMTimestamp: avagoUtils.PointerTo[uint64](0),
			DurangoTimestamp:   utils.TimeToNewUint64(agoUpgrade.DurangoTime),
		},
		EtnaTimestamp:    utils.TimeToNewUint64(agoUpgrade.EtnaTime),
		FortunaTimestamp: nil, // Fortuna is optional and has no effect on Subnet-EVM
		GraniteTimestamp: utils.TimeToNewUint64(agoUpgrade.GraniteTime),
		HeliconTimestamp: utils.TimeToNewUint64(agoUpgrade.HeliconTime),
	}
}

var (
	unscheduledActivation = uint64(upgrade.UnscheduledActivationTime.Unix())
	initiallyActiveTime   = uint64(upgrade.InitiallyActiveTime.Unix())

	ErrCannotBeNil             = errors.New("timestamp cannot be nil")
	errTimestampTooEarly       = errors.New("provided timestamp must be greater than or equal to the default timestamp")
	ErrUnsupportedForkOrdering = errors.New("unsupported fork ordering")
)

// verifyNetworkUpgrades checks that the network upgrades are well formed.
func (n *NetworkUpgrades) VerifyNetworkUpgrades(agoUpgrades upgrade.Config) error {
	defaults := GetSubnetEVMNetworkUpgrades(agoUpgrades)
	if err := verifyWithDefault(n.SubnetEVMTimestamp, defaults.SubnetEVMTimestamp); err != nil {
		return fmt.Errorf("subnetEVM fork block timestamp is invalid: %w", err)
	}
	if err := verifyWithDefault(n.DurangoTimestamp, defaults.DurangoTimestamp); err != nil {
		return fmt.Errorf("durango fork block timestamp is invalid: %w", err)
	}
	if err := verifyWithDefault(n.EtnaTimestamp, defaults.EtnaTimestamp); err != nil {
		return fmt.Errorf("etna fork block timestamp is invalid: %w", err)
	}
	if err := verifyWithDefault(n.FortunaTimestamp, defaults.FortunaTimestamp); err != nil {
		return fmt.Errorf("fortuna fork block timestamp is invalid: %w", err)
	}
	if err := verifyWithDefault(n.GraniteTimestamp, defaults.GraniteTimestamp); err != nil {
		return fmt.Errorf("granite fork block timestamp is invalid: %w", err)
	}
	if err := verifyWithDefault(n.HeliconTimestamp, defaults.HeliconTimestamp); err != nil {
		return fmt.Errorf("helicon fork block timestamp is invalid: %w", err)
	}
	return nil
}

func (n *NetworkUpgrades) Override(o *NetworkUpgrades) {
	if o.SubnetEVMNetworkUpgrades != nil {
		if n.SubnetEVMNetworkUpgrades == nil {
			n.SubnetEVMNetworkUpgrades = &SubnetEVMNetworkUpgrades{}
		}
		if o.SubnetEVMTimestamp != nil {
			n.SubnetEVMTimestamp = o.SubnetEVMTimestamp
		}
		if o.DurangoTimestamp != nil {
			n.DurangoTimestamp = o.DurangoTimestamp
		}
	}
	if o.EtnaTimestamp != nil {
		n.EtnaTimestamp = o.EtnaTimestamp
	}
	if o.FortunaTimestamp != nil {
		n.FortunaTimestamp = o.FortunaTimestamp
	}
	if o.GraniteTimestamp != nil {
		n.GraniteTimestamp = o.GraniteTimestamp
	}
	if o.HeliconTimestamp != nil {
		n.HeliconTimestamp = o.HeliconTimestamp
	}
}

// verifyWithDefault checks that the provided timestamp is greater than or equal to the default timestamp.
func verifyWithDefault(configTimestamp *uint64, defaultTimestamp *uint64) error {
	if defaultTimestamp == nil {
		return nil
	}

	// handle avalanche edge-cases:
	// nil -> error unless default is unscheduled
	// 0  -> allowed for initially-active defaults
	// non-zero -> must be >= default.
	if configTimestamp == nil {
		if *defaultTimestamp >= unscheduledActivation {
			return nil
		}
		return ErrCannotBeNil
	}

	if *configTimestamp == 0 && *defaultTimestamp <= initiallyActiveTime {
		return nil
	}

	if *configTimestamp < *defaultTimestamp {
		return fmt.Errorf("%w: provided timestamp %d, default timestamp %d", errTimestampTooEarly, *configTimestamp, *defaultTimestamp)
	}
	return nil
}
