// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extras

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/utils"
	"github.com/ava-labs/avalanchego/upgrade"

	ethparams "github.com/ava-labs/libevm/params"
)

var (
	unscheduledActivation = uint64(upgrade.UnscheduledActivationTime.Unix())
	initiallyActiveTime   = uint64(upgrade.InitiallyActiveTime.Unix())

	errCannotBeNil             = errors.New("timestamp cannot be nil")
	errTimestampTooEarly       = errors.New("provided timestamp must be greater than or equal to the default timestamp")
	errUnsupportedForkOrdering = errors.New("unsupported fork ordering")
)

// NetworkUpgrades contains timestamps that enable network upgrades.
// Avalanche specific network upgrades are also included here.
// (nil = no fork, 0 = already activated)
type NetworkUpgrades struct {
	// SubnetEVMTimestamp is a placeholder that activates Avalanche Upgrades prior to ApricotPhase6
	SubnetEVMTimestamp *uint64 `json:"subnetEVMTimestamp,omitempty"`
	// Durango activates the Shanghai Execution Spec Upgrade from Ethereum (https://github.com/ethereum/execution-specs/blob/master/network-upgrades/mainnet-upgrades/shanghai.md#included-eips)
	// and Avalanche Warp Messaging.
	// Note: EIP-4895 is excluded since withdrawals are not relevant to the Avalanche C-Chain or Subnets running the EVM.
	DurangoTimestamp *uint64 `json:"durangoTimestamp,omitempty"`
	// Placeholder for EtnaTimestamp
	EtnaTimestamp *uint64 `json:"etnaTimestamp,omitempty"`
	// Fortuna has no effect on Subnet-EVM by itself, but is included for completeness.
	FortunaTimestamp *uint64 `json:"fortunaTimestamp,omitempty"`
	// Granite adds a millisecond timestamp, precompile updates, and P-Chain epochs
	GraniteTimestamp *uint64 `json:"graniteTimestamp,omitempty"`
	// Helicon is a placeholder for the next upgrade
	HeliconTimestamp *uint64 `json:"heliconTimestamp,omitempty"`
}

func (n *NetworkUpgrades) Equal(other *NetworkUpgrades) bool {
	return reflect.DeepEqual(n, other)
}

func (n *NetworkUpgrades) checkNetworkUpgradesCompatible(newcfg *NetworkUpgrades, time uint64) *ethparams.ConfigCompatError {
	if isForkTimestampIncompatible(n.SubnetEVMTimestamp, newcfg.SubnetEVMTimestamp, time) {
		return ethparams.NewTimestampCompatError("SubnetEVM fork block timestamp", n.SubnetEVMTimestamp, newcfg.SubnetEVMTimestamp)
	}
	if isForkTimestampIncompatible(n.DurangoTimestamp, newcfg.DurangoTimestamp, time) {
		return ethparams.NewTimestampCompatError("Durango fork block timestamp", n.DurangoTimestamp, newcfg.DurangoTimestamp)
	}
	if isForkTimestampIncompatible(n.EtnaTimestamp, newcfg.EtnaTimestamp, time) {
		return ethparams.NewTimestampCompatError("Etna fork block timestamp", n.EtnaTimestamp, newcfg.EtnaTimestamp)
	}
	if isForkTimestampIncompatible(n.FortunaTimestamp, newcfg.FortunaTimestamp, time) {
		return ethparams.NewTimestampCompatError("Fortuna fork block timestamp", n.FortunaTimestamp, newcfg.FortunaTimestamp)
	}
	if isForkTimestampIncompatible(n.GraniteTimestamp, newcfg.GraniteTimestamp, time) {
		return ethparams.NewTimestampCompatError("Granite fork block timestamp", n.GraniteTimestamp, newcfg.GraniteTimestamp)
	}
	if isForkTimestampIncompatible(n.HeliconTimestamp, newcfg.HeliconTimestamp, time) {
		return ethparams.NewTimestampCompatError("Helicon fork block timestamp", n.HeliconTimestamp, newcfg.HeliconTimestamp)
	}

	return nil
}

func (n *NetworkUpgrades) forkOrder() []fork {
	return []fork{
		{name: "subnetEVMTimestamp", timestamp: n.SubnetEVMTimestamp},
		{name: "durangoTimestamp", timestamp: n.DurangoTimestamp},
		{name: "etnaTimestamp", timestamp: n.EtnaTimestamp},
		{name: "fortunaTimestamp", timestamp: n.FortunaTimestamp, optional: true},
		{name: "graniteTimestamp", timestamp: n.GraniteTimestamp},
		{name: "heliconTimestamp", timestamp: n.HeliconTimestamp},
	}
}

// SetDefaults sets the default values for the network upgrades.
// This overrides deactivating the network upgrade by providing a timestamp of nil value.
func (n *NetworkUpgrades) SetDefaults(agoUpgrades upgrade.Config) {
	defaults := GetNetworkUpgrades(agoUpgrades)
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

// verifyNetworkUpgrades checks that the network upgrades are well formed.
func (n *NetworkUpgrades) verifyNetworkUpgrades(agoUpgrades upgrade.Config) error {
	defaults := GetNetworkUpgrades(agoUpgrades)
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
	if o.SubnetEVMTimestamp != nil {
		n.SubnetEVMTimestamp = o.SubnetEVMTimestamp
	}
	if o.DurangoTimestamp != nil {
		n.DurangoTimestamp = o.DurangoTimestamp
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

// IsSubnetEVM returns whether [time] represents a block
// with a timestamp after the SubnetEVM upgrade time.
func (n NetworkUpgrades) IsSubnetEVM(time uint64) bool {
	return isTimestampForked(n.SubnetEVMTimestamp, time)
}

// IsDurango returns whether [time] represents a block
// with a timestamp after the Durango upgrade time.
func (n NetworkUpgrades) IsDurango(time uint64) bool {
	return isTimestampForked(n.DurangoTimestamp, time)
}

// IsEtna returns whether [time] represents a block
// with a timestamp after the Etna upgrade time.
func (n NetworkUpgrades) IsEtna(time uint64) bool {
	return isTimestampForked(n.EtnaTimestamp, time)
}

// IsFortuna returns whether [time] represents a block
// with a timestamp after the Fortuna upgrade time.
func (n *NetworkUpgrades) IsFortuna(time uint64) bool {
	return isTimestampForked(n.FortunaTimestamp, time)
}

// IsGranite returns whether [time] represents a block
// with a timestamp after the Granite upgrade time.
func (n *NetworkUpgrades) IsGranite(time uint64) bool {
	return isTimestampForked(n.GraniteTimestamp, time)
}

// IsHelicon returns whether [time] represents a block
// with a timestamp after the Helicon upgrade time.
func (n *NetworkUpgrades) IsHelicon(time uint64) bool {
	return isTimestampForked(n.HeliconTimestamp, time)
}

func (n *NetworkUpgrades) Description() string {
	var banner string
	banner += fmt.Sprintf(" - SubnetEVM Timestamp:          @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.10.0)\n", ptrToString(n.SubnetEVMTimestamp))
	banner += fmt.Sprintf(" - Durango Timestamp:            @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.11.0)\n", ptrToString(n.DurangoTimestamp))
	banner += fmt.Sprintf(" - Etna Timestamp:               @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.12.0)\n", ptrToString(n.EtnaTimestamp))
	banner += fmt.Sprintf(" - Fortuna Timestamp:            @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.13.0)\n", ptrToString(n.FortunaTimestamp))
	banner += fmt.Sprintf(" - Granite Timestamp:            @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.14.0)\n", ptrToString(n.GraniteTimestamp))
	banner += fmt.Sprintf(" - Helicon Timestamp:            @%-10v (Unscheduled)\n", ptrToString(n.HeliconTimestamp))
	return banner
}

type AvalancheRules struct {
	IsSubnetEVM bool
	IsDurango   bool
	IsEtna      bool
	IsFortuna   bool
	IsGranite   bool
	IsHelicon   bool
}

// IsGraniteActivated is used by the warp precompile to determine which gas costs to use.
func (a AvalancheRules) IsGraniteActivated() bool {
	return a.IsGranite
}

// IsDurangoActivated is used by the warp precompile to determine which gas costs to use.
func (a AvalancheRules) IsDurangoActivated() bool {
	return a.IsDurango
}

func (n *NetworkUpgrades) GetAvalancheRules(time uint64) AvalancheRules {
	return AvalancheRules{
		IsSubnetEVM: n.IsSubnetEVM(time),
		IsDurango:   n.IsDurango(time),
		IsEtna:      n.IsEtna(time),
		IsFortuna:   n.IsFortuna(time),
		IsGranite:   n.IsGranite(time),
		IsHelicon:   n.IsHelicon(time),
	}
}

// GetNetworkUpgrades returns the network upgrades for the specified avalanchego upgrades.
// Nil values are used to indicate optional upgrades.
func GetNetworkUpgrades(agoUpgrade upgrade.Config) NetworkUpgrades {
	return NetworkUpgrades{
		SubnetEVMTimestamp: utils.NewUint64(0),
		DurangoTimestamp:   utils.TimeToNewUint64(agoUpgrade.DurangoTime),
		EtnaTimestamp:      utils.TimeToNewUint64(agoUpgrade.EtnaTime),
		FortunaTimestamp:   nil, // Fortuna is optional and has no effect on Subnet-EVM
		GraniteTimestamp:   utils.TimeToNewUint64(agoUpgrade.GraniteTime),
		HeliconTimestamp:   utils.TimeToNewUint64(agoUpgrade.HeliconTime),
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
		return errCannotBeNil
	}

	if *configTimestamp == 0 && *defaultTimestamp <= initiallyActiveTime {
		return nil
	}

	if *configTimestamp < *defaultTimestamp {
		return fmt.Errorf("%w: provided timestamp %d, default timestamp %d", errTimestampTooEarly, *configTimestamp, *defaultTimestamp)
	}
	return nil
}

func ptrToString(val *uint64) string {
	if val == nil {
		return "nil"
	}
	return strconv.FormatUint(*val, 10)
}
