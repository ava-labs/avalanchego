// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extras

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/upgrade"

	evmparams "github.com/ava-labs/avalanchego/graft/evm/params"
)

type (
	NetworkUpgrades = evmparams.NetworkUpgrades
	AvalancheRules  = evmparams.AvalancheRules
)

// SubnetEVMJSON encodes [NetworkUpgrades] in the Subnet-EVM format, with a
// single subnetEVMTimestamp for all upgrades prior to Durango.
type SubnetEVMJSON struct{}

var _ evmparams.NetworkUpgradesJSON = SubnetEVMJSON{}

type subnetEVMUpgradesJSON struct {
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
	// Helicon has no effect on Subnet-EVM by itself, but is included for completeness.
	HeliconTimestamp *uint64 `json:"heliconTimestamp,omitempty"`
	// Igloo is our next upcoming upgrade.
	IglooTimestamp *uint64 `json:"iglooTimestamp,omitempty"`
}

// subnetEVMTimestamp returns the timestamp that activates the Avalanche
// upgrades prior to Durango. The choice of timestamp is arbitrary.
func subnetEVMTimestamp(n *NetworkUpgrades) *uint64 {
	return n.ApricotPhase1BlockTimestamp
}

func equal[T comparable](t ...*T) bool {
	if len(t) == 0 {
		return true
	}
	first := t[0]
	for _, v := range t[1:] {
		if !ptrEqual(first, v) {
			return false
		}
	}
	return true
}

func ptrEqual[T comparable](x, y *T) bool {
	if x == nil {
		return y == nil
	}
	if y == nil {
		return x == nil
	}
	return *x == *y
}

var errPreDurangoMismatch = errors.New("pre-Durango upgrade timestamps must all equal the Subnet-EVM timestamp")

func (SubnetEVMJSON) Marshal(n *NetworkUpgrades) ([]byte, error) {
	if !equal(
		n.ApricotPhase1BlockTimestamp,
		n.ApricotPhase2BlockTimestamp,
		n.ApricotPhase3BlockTimestamp,
		n.ApricotPhase4BlockTimestamp,
		n.ApricotPhase5BlockTimestamp,
		n.ApricotPhasePre6BlockTimestamp,
		n.ApricotPhase6BlockTimestamp,
		n.ApricotPhasePost6BlockTimestamp,
		n.BanffBlockTimestamp,
		n.CortinaBlockTimestamp,
	) {
		return nil, fmt.Errorf("%w: %v", errPreDurangoMismatch, n)
	}

	return json.Marshal(subnetEVMUpgradesJSON{
		SubnetEVMTimestamp: subnetEVMTimestamp(n),
		DurangoTimestamp:   n.DurangoBlockTimestamp,
		EtnaTimestamp:      n.EtnaTimestamp,
		FortunaTimestamp:   n.FortunaTimestamp,
		GraniteTimestamp:   n.GraniteTimestamp,
		HeliconTimestamp:   n.HeliconTimestamp,
		IglooTimestamp:     n.IglooTimestamp,
	})
}

func (SubnetEVMJSON) Unmarshal(data []byte, n *NetworkUpgrades) error {
	tmp := subnetEVMUpgradesJSON{
		SubnetEVMTimestamp: subnetEVMTimestamp(n),
		DurangoTimestamp:   n.DurangoBlockTimestamp,
		EtnaTimestamp:      n.EtnaTimestamp,
		FortunaTimestamp:   n.FortunaTimestamp,
		GraniteTimestamp:   n.GraniteTimestamp,
		HeliconTimestamp:   n.HeliconTimestamp,
		IglooTimestamp:     n.IglooTimestamp,
	}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	n.ApricotPhase1BlockTimestamp = tmp.SubnetEVMTimestamp
	n.ApricotPhase2BlockTimestamp = tmp.SubnetEVMTimestamp
	n.ApricotPhase3BlockTimestamp = tmp.SubnetEVMTimestamp
	n.ApricotPhase4BlockTimestamp = tmp.SubnetEVMTimestamp
	n.ApricotPhase5BlockTimestamp = tmp.SubnetEVMTimestamp
	n.ApricotPhasePre6BlockTimestamp = tmp.SubnetEVMTimestamp
	n.ApricotPhase6BlockTimestamp = tmp.SubnetEVMTimestamp
	n.ApricotPhasePost6BlockTimestamp = tmp.SubnetEVMTimestamp
	n.BanffBlockTimestamp = tmp.SubnetEVMTimestamp
	n.CortinaBlockTimestamp = tmp.SubnetEVMTimestamp
	n.DurangoBlockTimestamp = tmp.DurangoTimestamp
	n.EtnaTimestamp = tmp.EtnaTimestamp
	n.FortunaTimestamp = tmp.FortunaTimestamp
	n.GraniteTimestamp = tmp.GraniteTimestamp
	n.HeliconTimestamp = tmp.HeliconTimestamp
	n.IglooTimestamp = tmp.IglooTimestamp
	return nil
}

// GetNetworkUpgrades returns the network upgrades for the specified avalanchego upgrades.
// Nil values are used to indicate optional upgrades.
func GetNetworkUpgrades(agoUpgrade upgrade.Config) NetworkUpgrades {
	return NetworkUpgrades{
		ApricotPhase1BlockTimestamp:     new(uint64),
		ApricotPhase2BlockTimestamp:     new(uint64),
		ApricotPhase3BlockTimestamp:     new(uint64),
		ApricotPhase4BlockTimestamp:     new(uint64),
		ApricotPhase5BlockTimestamp:     new(uint64),
		ApricotPhasePre6BlockTimestamp:  new(uint64),
		ApricotPhase6BlockTimestamp:     new(uint64),
		ApricotPhasePost6BlockTimestamp: new(uint64),
		BanffBlockTimestamp:             new(uint64),
		CortinaBlockTimestamp:           new(uint64),
		DurangoBlockTimestamp:           utils.TimeToNewUint64(agoUpgrade.DurangoTime),
		EtnaTimestamp:                   utils.TimeToNewUint64(agoUpgrade.EtnaTime),
		FortunaTimestamp:                nil, // Fortuna is optional and has no effect on Subnet-EVM
		GraniteTimestamp:                utils.TimeToNewUint64(agoUpgrade.GraniteTime),
		HeliconTimestamp:                utils.TimeToNewUint64(agoUpgrade.HeliconTime),
		IglooTimestamp:                  utils.TimeToNewUint64(agoUpgrade.IglooTime),
	}
}

// verifyNetworkUpgrades checks that the network upgrades are well formed.
func verifyNetworkUpgrades(n *NetworkUpgrades, agoUpgrades upgrade.Config) error {
	defaults := GetNetworkUpgrades(agoUpgrades)
	if err := verifyWithDefault(subnetEVMTimestamp(n), subnetEVMTimestamp(&defaults)); err != nil {
		return fmt.Errorf("subnetEVM fork block timestamp is invalid: %w", err)
	}
	if err := verifyWithDefault(n.DurangoBlockTimestamp, defaults.DurangoBlockTimestamp); err != nil {
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
	if err := verifyWithDefault(n.IglooTimestamp, defaults.IglooTimestamp); err != nil {
		return fmt.Errorf("igloo fork block timestamp is invalid: %w", err)
	}
	return nil
}

var (
	unscheduledActivation = uint64(upgrade.UnscheduledActivationTime.Unix())
	initiallyActiveTime   = uint64(upgrade.InitiallyActiveTime.Unix())

	errCannotBeNil       = errors.New("timestamp cannot be nil")
	errTimestampTooEarly = errors.New("provided timestamp must be greater than or equal to the default timestamp")
)

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
