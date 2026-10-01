// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"fmt"
	"reflect"
	"strconv"

	ethparams "github.com/ava-labs/libevm/params"
)

// NetworkUpgrades tracks the timestamps of all the Avalanche upgrades.
//
// For each upgrade, a nil value means the fork hasn't happened and is not
// scheduled. A pointer to 0 means the fork has already activated.
//
// There is no default JSON encoding; it is determined by the codec registered
// with [RegisterJSON], as each VM serializes upgrades differently.
type NetworkUpgrades struct {
	ApricotPhase1BlockTimestamp *uint64 // Apricot Phase 1 Block Timestamp
	// Apricot Phase 2 Block Timestamp includes a modified version of the Berlin
	// Hard Fork.
	ApricotPhase2BlockTimestamp *uint64
	// Apricot Phase 3 introduces dynamic fees and a modified version of the
	// London Hard Fork.
	ApricotPhase3BlockTimestamp *uint64
	// Apricot Phase 4 introduces the notion of a block fee to the dynamic fee
	// algorithm.
	ApricotPhase4BlockTimestamp *uint64
	// Apricot Phase 5 introduces a batch of atomic transactions with a maximum
	// atomic gas limit per block.
	ApricotPhase5BlockTimestamp *uint64
	// Apricot Phase Pre-6 deprecates the NativeAssetCall precompile (soft).
	ApricotPhasePre6BlockTimestamp *uint64
	// Apricot Phase 6 deprecates the NativeAssetBalance and NativeAssetCall
	// precompiles.
	ApricotPhase6BlockTimestamp *uint64
	// Apricot Phase Post-6 deprecates the NativeAssetCall precompile (soft).
	ApricotPhasePost6BlockTimestamp *uint64
	// Banff restricts import/export transactions to AVAX.
	BanffBlockTimestamp *uint64
	// Cortina increases the block gas limit to 15M. Also represents subnet-evm activation.
	CortinaBlockTimestamp *uint64
	// Durango activates Avalanche Warp Messaging and the Shanghai Execution
	// Spec Upgrade (https://github.com/ethereum/execution-specs/blob/master/network-upgrades/mainnet-upgrades/shanghai.md#included-eips).
	//
	// Note: EIP-4895 is excluded since withdrawals are not relevant to the
	// Avalanche C-Chain or Subnets running the EVM.
	DurangoBlockTimestamp *uint64
	// Etna activates Cancun (https://github.com/ethereum/execution-specs/blob/master/network-upgrades/mainnet-upgrades/cancun.md#included-eips)
	// and reduces the min base fee.
	// Note: EIP-4844 BlobTxs are not enabled in the mempool and blocks are not
	// allowed to contain them. For details see https://github.com/avalanche-foundation/ACPs/pull/131
	EtnaTimestamp *uint64
	// Fortuna modifies the gas price mechanism based on ACP-176. Optional on L1s.
	FortunaTimestamp *uint64
	// Granite adds a millisecond timestamp, precompile updates, and P-Chain epochs
	GraniteTimestamp *uint64
	// Helicon activates async execution (ACP-194) and the dynamic minimum gas
	// price (ACP-283).
	HeliconTimestamp *uint64
	// Igloo is our next upcoming upgrade.
	IglooTimestamp *uint64
}

func (n *NetworkUpgrades) Equal(other *NetworkUpgrades) bool {
	return reflect.DeepEqual(n, other)
}

// SetDefaults sets the default values for the network upgrades.
// This overrides deactivating the network upgrade by providing a timestamp of nil value.
func (n *NetworkUpgrades) SetDefaults(defaults NetworkUpgrades) {
	// If the network upgrade is not set, set it to the default value.
	// If the network upgrade is set to 0, we also treat it as nil and set it default.
	// Invariant: This is because in prior versions, upgrades were not modifiable and were directly set to their default values.
	// Most of the tools and configurations just provide these as 0, so it is safer to treat 0 as nil and set to default
	// to prevent premature activations of the network upgrades for live networks.
	if n.ApricotPhase1BlockTimestamp == nil || *n.ApricotPhase1BlockTimestamp == 0 {
		n.ApricotPhase1BlockTimestamp = defaults.ApricotPhase1BlockTimestamp
	}
	if n.ApricotPhase2BlockTimestamp == nil || *n.ApricotPhase2BlockTimestamp == 0 {
		n.ApricotPhase2BlockTimestamp = defaults.ApricotPhase2BlockTimestamp
	}
	if n.ApricotPhase3BlockTimestamp == nil || *n.ApricotPhase3BlockTimestamp == 0 {
		n.ApricotPhase3BlockTimestamp = defaults.ApricotPhase3BlockTimestamp
	}
	if n.ApricotPhase4BlockTimestamp == nil || *n.ApricotPhase4BlockTimestamp == 0 {
		n.ApricotPhase4BlockTimestamp = defaults.ApricotPhase4BlockTimestamp
	}
	if n.ApricotPhase5BlockTimestamp == nil || *n.ApricotPhase5BlockTimestamp == 0 {
		n.ApricotPhase5BlockTimestamp = defaults.ApricotPhase5BlockTimestamp
	}
	if n.ApricotPhasePre6BlockTimestamp == nil || *n.ApricotPhasePre6BlockTimestamp == 0 {
		n.ApricotPhasePre6BlockTimestamp = defaults.ApricotPhasePre6BlockTimestamp
	}
	if n.ApricotPhase6BlockTimestamp == nil || *n.ApricotPhase6BlockTimestamp == 0 {
		n.ApricotPhase6BlockTimestamp = defaults.ApricotPhase6BlockTimestamp
	}
	if n.ApricotPhasePost6BlockTimestamp == nil || *n.ApricotPhasePost6BlockTimestamp == 0 {
		n.ApricotPhasePost6BlockTimestamp = defaults.ApricotPhasePost6BlockTimestamp
	}
	if n.BanffBlockTimestamp == nil || *n.BanffBlockTimestamp == 0 {
		n.BanffBlockTimestamp = defaults.BanffBlockTimestamp
	}
	if n.CortinaBlockTimestamp == nil || *n.CortinaBlockTimestamp == 0 {
		n.CortinaBlockTimestamp = defaults.CortinaBlockTimestamp
	}
	if n.DurangoBlockTimestamp == nil || *n.DurangoBlockTimestamp == 0 {
		n.DurangoBlockTimestamp = defaults.DurangoBlockTimestamp
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
	if n.IglooTimestamp == nil || *n.IglooTimestamp == 0 {
		n.IglooTimestamp = defaults.IglooTimestamp
	}
}

// Override sets every upgrade timestamp that is non-nil in o.
func (n *NetworkUpgrades) Override(o *NetworkUpgrades) {
	if o == nil {
		return
	}

	if o.ApricotPhase1BlockTimestamp != nil {
		n.ApricotPhase1BlockTimestamp = o.ApricotPhase1BlockTimestamp
	}
	if o.ApricotPhase2BlockTimestamp != nil {
		n.ApricotPhase2BlockTimestamp = o.ApricotPhase2BlockTimestamp
	}
	if o.ApricotPhase3BlockTimestamp != nil {
		n.ApricotPhase3BlockTimestamp = o.ApricotPhase3BlockTimestamp
	}
	if o.ApricotPhase4BlockTimestamp != nil {
		n.ApricotPhase4BlockTimestamp = o.ApricotPhase4BlockTimestamp
	}
	if o.ApricotPhase5BlockTimestamp != nil {
		n.ApricotPhase5BlockTimestamp = o.ApricotPhase5BlockTimestamp
	}
	if o.ApricotPhasePre6BlockTimestamp != nil {
		n.ApricotPhasePre6BlockTimestamp = o.ApricotPhasePre6BlockTimestamp
	}
	if o.ApricotPhase6BlockTimestamp != nil {
		n.ApricotPhase6BlockTimestamp = o.ApricotPhase6BlockTimestamp
	}
	if o.ApricotPhasePost6BlockTimestamp != nil {
		n.ApricotPhasePost6BlockTimestamp = o.ApricotPhasePost6BlockTimestamp
	}
	if o.BanffBlockTimestamp != nil {
		n.BanffBlockTimestamp = o.BanffBlockTimestamp
	}
	if o.CortinaBlockTimestamp != nil {
		n.CortinaBlockTimestamp = o.CortinaBlockTimestamp
	}
	if o.DurangoBlockTimestamp != nil {
		n.DurangoBlockTimestamp = o.DurangoBlockTimestamp
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
	if o.IglooTimestamp != nil {
		n.IglooTimestamp = o.IglooTimestamp
	}
}

// CheckCompatible returns an error if newcfg reschedules an upgrade that is
// already active at time, under either n or newcfg.
func (n *NetworkUpgrades) CheckCompatible(newcfg *NetworkUpgrades, time uint64) *ethparams.ConfigCompatError {
	if isForkTimestampIncompatible(n.ApricotPhase1BlockTimestamp, newcfg.ApricotPhase1BlockTimestamp, time) {
		return ethparams.NewTimestampCompatError("ApricotPhase1 fork block timestamp", n.ApricotPhase1BlockTimestamp, newcfg.ApricotPhase1BlockTimestamp)
	}
	if isForkTimestampIncompatible(n.ApricotPhase2BlockTimestamp, newcfg.ApricotPhase2BlockTimestamp, time) {
		return ethparams.NewTimestampCompatError("ApricotPhase2 fork block timestamp", n.ApricotPhase2BlockTimestamp, newcfg.ApricotPhase2BlockTimestamp)
	}
	if isForkTimestampIncompatible(n.ApricotPhase3BlockTimestamp, newcfg.ApricotPhase3BlockTimestamp, time) {
		return ethparams.NewTimestampCompatError("ApricotPhase3 fork block timestamp", n.ApricotPhase3BlockTimestamp, newcfg.ApricotPhase3BlockTimestamp)
	}
	if isForkTimestampIncompatible(n.ApricotPhase4BlockTimestamp, newcfg.ApricotPhase4BlockTimestamp, time) {
		return ethparams.NewTimestampCompatError("ApricotPhase4 fork block timestamp", n.ApricotPhase4BlockTimestamp, newcfg.ApricotPhase4BlockTimestamp)
	}
	if isForkTimestampIncompatible(n.ApricotPhase5BlockTimestamp, newcfg.ApricotPhase5BlockTimestamp, time) {
		return ethparams.NewTimestampCompatError("ApricotPhase5 fork block timestamp", n.ApricotPhase5BlockTimestamp, newcfg.ApricotPhase5BlockTimestamp)
	}
	if isForkTimestampIncompatible(n.ApricotPhasePre6BlockTimestamp, newcfg.ApricotPhasePre6BlockTimestamp, time) {
		return ethparams.NewTimestampCompatError("ApricotPhasePre6 fork block timestamp", n.ApricotPhasePre6BlockTimestamp, newcfg.ApricotPhasePre6BlockTimestamp)
	}
	if isForkTimestampIncompatible(n.ApricotPhase6BlockTimestamp, newcfg.ApricotPhase6BlockTimestamp, time) {
		return ethparams.NewTimestampCompatError("ApricotPhase6 fork block timestamp", n.ApricotPhase6BlockTimestamp, newcfg.ApricotPhase6BlockTimestamp)
	}
	if isForkTimestampIncompatible(n.ApricotPhasePost6BlockTimestamp, newcfg.ApricotPhasePost6BlockTimestamp, time) {
		return ethparams.NewTimestampCompatError("ApricotPhasePost6 fork block timestamp", n.ApricotPhasePost6BlockTimestamp, newcfg.ApricotPhasePost6BlockTimestamp)
	}
	if isForkTimestampIncompatible(n.BanffBlockTimestamp, newcfg.BanffBlockTimestamp, time) {
		return ethparams.NewTimestampCompatError("Banff fork block timestamp", n.BanffBlockTimestamp, newcfg.BanffBlockTimestamp)
	}
	if isForkTimestampIncompatible(n.CortinaBlockTimestamp, newcfg.CortinaBlockTimestamp, time) {
		return ethparams.NewTimestampCompatError("Cortina fork block timestamp", n.CortinaBlockTimestamp, newcfg.CortinaBlockTimestamp)
	}
	if isForkTimestampIncompatible(n.DurangoBlockTimestamp, newcfg.DurangoBlockTimestamp, time) {
		return ethparams.NewTimestampCompatError("Durango fork block timestamp", n.DurangoBlockTimestamp, newcfg.DurangoBlockTimestamp)
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
	if isForkTimestampIncompatible(n.IglooTimestamp, newcfg.IglooTimestamp, time) {
		return ethparams.NewTimestampCompatError("Igloo fork block timestamp", n.IglooTimestamp, newcfg.IglooTimestamp)
	}

	return nil
}

// IsApricotPhase1 returns whether [time] represents a block
// with a timestamp after the Apricot Phase 1 upgrade time.
func (n NetworkUpgrades) IsApricotPhase1(time uint64) bool {
	return isTimestampForked(n.ApricotPhase1BlockTimestamp, time)
}

// IsApricotPhase2 returns whether [time] represents a block
// with a timestamp after the Apricot Phase 2 upgrade time.
func (n NetworkUpgrades) IsApricotPhase2(time uint64) bool {
	return isTimestampForked(n.ApricotPhase2BlockTimestamp, time)
}

// IsApricotPhase3 returns whether [time] represents a block
// with a timestamp after the Apricot Phase 3 upgrade time.
func (n *NetworkUpgrades) IsApricotPhase3(time uint64) bool {
	return isTimestampForked(n.ApricotPhase3BlockTimestamp, time)
}

// IsApricotPhase4 returns whether [time] represents a block
// with a timestamp after the Apricot Phase 4 upgrade time.
func (n NetworkUpgrades) IsApricotPhase4(time uint64) bool {
	return isTimestampForked(n.ApricotPhase4BlockTimestamp, time)
}

// IsApricotPhase5 returns whether [time] represents a block
// with a timestamp after the Apricot Phase 5 upgrade time.
func (n NetworkUpgrades) IsApricotPhase5(time uint64) bool {
	return isTimestampForked(n.ApricotPhase5BlockTimestamp, time)
}

// IsApricotPhasePre6 returns whether [time] represents a block
// with a timestamp after the Apricot Phase Pre 6 upgrade time.
func (n NetworkUpgrades) IsApricotPhasePre6(time uint64) bool {
	return isTimestampForked(n.ApricotPhasePre6BlockTimestamp, time)
}

// IsApricotPhase6 returns whether [time] represents a block
// with a timestamp after the Apricot Phase 6 upgrade time.
func (n NetworkUpgrades) IsApricotPhase6(time uint64) bool {
	return isTimestampForked(n.ApricotPhase6BlockTimestamp, time)
}

// IsApricotPhasePost6 returns whether [time] represents a block
// with a timestamp after the Apricot Phase 6 Post upgrade time.
func (n NetworkUpgrades) IsApricotPhasePost6(time uint64) bool {
	return isTimestampForked(n.ApricotPhasePost6BlockTimestamp, time)
}

// IsBanff returns whether [time] represents a block
// with a timestamp after the Banff upgrade time.
func (n NetworkUpgrades) IsBanff(time uint64) bool {
	return isTimestampForked(n.BanffBlockTimestamp, time)
}

// IsCortina returns whether [time] represents a block
// with a timestamp after the Cortina upgrade time.
func (n NetworkUpgrades) IsCortina(time uint64) bool {
	return isTimestampForked(n.CortinaBlockTimestamp, time)
}

// IsDurango returns whether [time] represents a block
// with a timestamp after the Durango upgrade time.
func (n NetworkUpgrades) IsDurango(time uint64) bool {
	return isTimestampForked(n.DurangoBlockTimestamp, time)
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

// IsIgloo returns whether [time] represents a block
// with a timestamp after the Igloo upgrade time.
func (n *NetworkUpgrades) IsIgloo(time uint64) bool {
	return isTimestampForked(n.IglooTimestamp, time)
}

func (n *NetworkUpgrades) GetAvalancheRules(timestamp uint64) AvalancheRules {
	return AvalancheRules{
		IsApricotPhase1:     n.IsApricotPhase1(timestamp),
		IsApricotPhase2:     n.IsApricotPhase2(timestamp),
		IsApricotPhase3:     n.IsApricotPhase3(timestamp),
		IsApricotPhase4:     n.IsApricotPhase4(timestamp),
		IsApricotPhase5:     n.IsApricotPhase5(timestamp),
		IsApricotPhasePre6:  n.IsApricotPhasePre6(timestamp),
		IsApricotPhase6:     n.IsApricotPhase6(timestamp),
		IsApricotPhasePost6: n.IsApricotPhasePost6(timestamp),
		IsBanff:             n.IsBanff(timestamp),
		IsCortina:           n.IsCortina(timestamp),
		IsDurango:           n.IsDurango(timestamp),
		IsEtna:              n.IsEtna(timestamp),
		IsFortuna:           n.IsFortuna(timestamp),
		IsGranite:           n.IsGranite(timestamp),
		IsHelicon:           n.IsHelicon(timestamp),
		IsIgloo:             n.IsIgloo(timestamp),
	}
}

// Description returns a human-readable summary of the upgrade timestamps.
func (n *NetworkUpgrades) Description() string {
	var banner string
	banner += fmt.Sprintf(" - Apricot Phase 1 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.3.0)\n", ptrToString(n.ApricotPhase1BlockTimestamp))
	banner += fmt.Sprintf(" - Apricot Phase 2 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.4.0)\n", ptrToString(n.ApricotPhase2BlockTimestamp))
	banner += fmt.Sprintf(" - Apricot Phase 3 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.5.0)\n", ptrToString(n.ApricotPhase3BlockTimestamp))
	banner += fmt.Sprintf(" - Apricot Phase 4 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.6.0)\n", ptrToString(n.ApricotPhase4BlockTimestamp))
	banner += fmt.Sprintf(" - Apricot Phase 5 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0)\n", ptrToString(n.ApricotPhase5BlockTimestamp))
	banner += fmt.Sprintf(" - Apricot Phase P6 Timestamp:       @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.8.0)\n", ptrToString(n.ApricotPhasePre6BlockTimestamp))
	banner += fmt.Sprintf(" - Apricot Phase 6 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.8.0)\n", ptrToString(n.ApricotPhase6BlockTimestamp))
	banner += fmt.Sprintf(" - Apricot Phase Post-6 Timestamp:   @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.8.0)\n", ptrToString(n.ApricotPhasePost6BlockTimestamp))
	banner += fmt.Sprintf(" - Banff Timestamp:                  @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0)\n", ptrToString(n.BanffBlockTimestamp))
	banner += fmt.Sprintf(" - Cortina Timestamp:                @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.10.0)\n", ptrToString(n.CortinaBlockTimestamp))
	banner += fmt.Sprintf(" - Durango Timestamp:                @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.11.0)\n", ptrToString(n.DurangoBlockTimestamp))
	banner += fmt.Sprintf(" - Etna Timestamp:                   @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.12.0)\n", ptrToString(n.EtnaTimestamp))
	banner += fmt.Sprintf(" - Fortuna Timestamp:                @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.13.0)\n", ptrToString(n.FortunaTimestamp))
	banner += fmt.Sprintf(" - Granite Timestamp:                @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.14.0)\n", ptrToString(n.GraniteTimestamp))
	banner += fmt.Sprintf(" - Helicon Timestamp:                @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.15.0)\n", ptrToString(n.HeliconTimestamp))
	banner += fmt.Sprintf(" - Igloo Timestamp:                  @%-10v (Unscheduled)\n", ptrToString(n.IglooTimestamp))
	return banner
}

func ptrToString(val *uint64) string {
	if val == nil {
		return "nil"
	}
	return strconv.FormatUint(*val, 10)
}

// isTimestampForked returns whether a fork scheduled at timestamp s is active
// at the given head timestamp.
func isTimestampForked(s *uint64, head uint64) bool {
	if s == nil {
		return false
	}
	return *s <= head
}

// isForkTimestampIncompatible returns true if a fork scheduled at timestamp s1
// cannot be rescheduled to timestamp s2 because head is already past the fork.
func isForkTimestampIncompatible(s1, s2 *uint64, head uint64) bool {
	return (isTimestampForked(s1, head) || isTimestampForked(s2, head)) && !configTimestampEqual(s1, s2)
}

func configTimestampEqual(x, y *uint64) bool {
	if x == nil {
		return y == nil
	}
	if y == nil {
		return x == nil
	}
	return *x == *y
}
