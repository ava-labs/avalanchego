// (c) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"fmt"
	"reflect"

	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/coreth/utils"
)

type NetworkUpgrades struct {
	ApricotPhase1BlockTimestamp *uint64 `json:"apricotPhase1BlockTimestamp,omitempty"` // Apricot Phase 1 Block Timestamp (nil = no fork, 0 = already activated)
	// Apricot Phase 2 Block Timestamp (nil = no fork, 0 = already activated)
	// Apricot Phase 2 includes a modified version of the Berlin Hard Fork from Ethereum
	ApricotPhase2BlockTimestamp *uint64 `json:"apricotPhase2BlockTimestamp,omitempty"`
	// Apricot Phase 3 introduces dynamic fees and a modified version of the London Hard Fork from Ethereum (nil = no fork, 0 = already activated)
	ApricotPhase3BlockTimestamp *uint64 `json:"apricotPhase3BlockTimestamp,omitempty"`
	// Apricot Phase 4 introduces the notion of a block fee to the dynamic fee algorithm (nil = no fork, 0 = already activated)
	ApricotPhase4BlockTimestamp *uint64 `json:"apricotPhase4BlockTimestamp,omitempty"`
	// Apricot Phase 5 introduces a batch of atomic transactions with a maximum atomic gas limit per block. (nil = no fork, 0 = already activated)
	ApricotPhase5BlockTimestamp *uint64 `json:"apricotPhase5BlockTimestamp,omitempty"`
	// Apricot Phase Pre-6 deprecates the NativeAssetCall precompile (soft). (nil = no fork, 0 = already activated)
	ApricotPhasePre6BlockTimestamp *uint64 `json:"apricotPhasePre6BlockTimestamp,omitempty"`
	// Apricot Phase 6 deprecates the NativeAssetBalance and NativeAssetCall precompiles. (nil = no fork, 0 = already activated)
	ApricotPhase6BlockTimestamp *uint64 `json:"apricotPhase6BlockTimestamp,omitempty"`
	// Apricot Phase Post-6 deprecates the NativeAssetCall precompile (soft). (nil = no fork, 0 = already activated)
	ApricotPhasePost6BlockTimestamp *uint64 `json:"apricotPhasePost6BlockTimestamp,omitempty"`
	// Banff restricts import/export transactions to AVAX. (nil = no fork, 0 = already activated)
	BanffBlockTimestamp *uint64 `json:"banffBlockTimestamp,omitempty"`
	// Cortina increases the block gas limit to 15M. (nil = no fork, 0 = already activated)
	CortinaBlockTimestamp *uint64 `json:"cortinaBlockTimestamp,omitempty"`
	// Durango activates the Shanghai Execution Spec Upgrade from Ethereum (https://github.com/ethereum/execution-specs/blob/master/network-upgrades/mainnet-upgrades/shanghai.md#included-eips)
	// and Avalanche Warp Messaging. (nil = no fork, 0 = already activated)
	// Note: EIP-4895 is excluded since withdrawals are not relevant to the Avalanche C-Chain or Subnets running the EVM.
	DurangoBlockTimestamp *uint64 `json:"durangoBlockTimestamp,omitempty"`
	// Etna activates Cancun from Ethereum (https://github.com/ethereum/execution-specs/blob/master/network-upgrades/mainnet-upgrades/cancun.md#included-eips)
	// and reduces the min base fee. (nil = no fork, 0 = already activated)
	// Note: EIP-4844 BlobTxs are not enabled in the mempool and blocks are not
	// allowed to contain them. For details see https://github.com/avalanche-foundation/ACPs/pull/131
	EtnaTimestamp *uint64 `json:"etnaTimestamp,omitempty"`
	// FUpgrade is a placeholder for the next upgrade.
	// (nil = no fork, 0 = already activated)
	FUpgradeTimestamp *uint64 `json:"fUpgradeTimestamp,omitempty"`
}

func (n *NetworkUpgrades) Equal(other *NetworkUpgrades) bool {
	return reflect.DeepEqual(n, other)
}

func (n *NetworkUpgrades) CheckNetworkUpgradesCompatible(newcfg *NetworkUpgrades, time uint64) *ConfigCompatError {
	if isForkTimestampIncompatible(n.ApricotPhase1BlockTimestamp, newcfg.ApricotPhase1BlockTimestamp, time) {
		return newTimestampCompatError("ApricotPhase1 fork block timestamp", n.ApricotPhase1BlockTimestamp, newcfg.ApricotPhase1BlockTimestamp)
	}
	if isForkTimestampIncompatible(n.ApricotPhase2BlockTimestamp, newcfg.ApricotPhase2BlockTimestamp, time) {
		return newTimestampCompatError("ApricotPhase2 fork block timestamp", n.ApricotPhase2BlockTimestamp, newcfg.ApricotPhase2BlockTimestamp)
	}
	if isForkTimestampIncompatible(n.ApricotPhase3BlockTimestamp, newcfg.ApricotPhase3BlockTimestamp, time) {
		return newTimestampCompatError("ApricotPhase3 fork block timestamp", n.ApricotPhase3BlockTimestamp, newcfg.ApricotPhase3BlockTimestamp)
	}
	if isForkTimestampIncompatible(n.ApricotPhase4BlockTimestamp, newcfg.ApricotPhase4BlockTimestamp, time) {
		return newTimestampCompatError("ApricotPhase4 fork block timestamp", n.ApricotPhase4BlockTimestamp, newcfg.ApricotPhase4BlockTimestamp)
	}
	if isForkTimestampIncompatible(n.ApricotPhase5BlockTimestamp, newcfg.ApricotPhase5BlockTimestamp, time) {
		return newTimestampCompatError("ApricotPhase5 fork block timestamp", n.ApricotPhase5BlockTimestamp, newcfg.ApricotPhase5BlockTimestamp)
	}
	if isForkTimestampIncompatible(n.ApricotPhasePre6BlockTimestamp, newcfg.ApricotPhasePre6BlockTimestamp, time) {
		return newTimestampCompatError("ApricotPhasePre6 fork block timestamp", n.ApricotPhasePre6BlockTimestamp, newcfg.ApricotPhasePre6BlockTimestamp)
	}
	if isForkTimestampIncompatible(n.ApricotPhase6BlockTimestamp, newcfg.ApricotPhase6BlockTimestamp, time) {
		return newTimestampCompatError("ApricotPhase6 fork block timestamp", n.ApricotPhase6BlockTimestamp, newcfg.ApricotPhase6BlockTimestamp)
	}
	if isForkTimestampIncompatible(n.ApricotPhasePost6BlockTimestamp, newcfg.ApricotPhasePost6BlockTimestamp, time) {
		return newTimestampCompatError("ApricotPhasePost6 fork block timestamp", n.ApricotPhasePost6BlockTimestamp, newcfg.ApricotPhasePost6BlockTimestamp)
	}
	if isForkTimestampIncompatible(n.BanffBlockTimestamp, newcfg.BanffBlockTimestamp, time) {
		return newTimestampCompatError("Banff fork block timestamp", n.BanffBlockTimestamp, newcfg.BanffBlockTimestamp)
	}
	if isForkTimestampIncompatible(n.CortinaBlockTimestamp, newcfg.CortinaBlockTimestamp, time) {
		return newTimestampCompatError("Cortina fork block timestamp", n.CortinaBlockTimestamp, newcfg.CortinaBlockTimestamp)
	}
	if isForkTimestampIncompatible(n.DurangoBlockTimestamp, newcfg.DurangoBlockTimestamp, time) {
		return newTimestampCompatError("Durango fork block timestamp", n.DurangoBlockTimestamp, newcfg.DurangoBlockTimestamp)
	}
	if isForkTimestampIncompatible(n.EtnaTimestamp, newcfg.EtnaTimestamp, time) {
		return newTimestampCompatError("Etna fork block timestamp", n.EtnaTimestamp, newcfg.EtnaTimestamp)
	}
	if isForkTimestampIncompatible(n.FUpgradeTimestamp, newcfg.FUpgradeTimestamp, time) {
		return newTimestampCompatError("F-Upgrade fork block timestamp", n.FUpgradeTimestamp, newcfg.FUpgradeTimestamp)
	}

	return nil
}

func (n *NetworkUpgrades) forkOrder() []fork {
	return []fork{
		{name: "apricotPhase1BlockTimestamp", timestamp: n.ApricotPhase1BlockTimestamp},
		{name: "apricotPhase2BlockTimestamp", timestamp: n.ApricotPhase2BlockTimestamp},
		{name: "apricotPhase3BlockTimestamp", timestamp: n.ApricotPhase3BlockTimestamp},
		{name: "apricotPhase4BlockTimestamp", timestamp: n.ApricotPhase4BlockTimestamp},
		{name: "apricotPhase5BlockTimestamp", timestamp: n.ApricotPhase5BlockTimestamp},
		{name: "apricotPhasePre6BlockTimestamp", timestamp: n.ApricotPhasePre6BlockTimestamp},
		{name: "apricotPhase6BlockTimestamp", timestamp: n.ApricotPhase6BlockTimestamp},
		{name: "apricotPhasePost6BlockTimestamp", timestamp: n.ApricotPhasePost6BlockTimestamp},
		{name: "banffBlockTimestamp", timestamp: n.BanffBlockTimestamp},
		{name: "cortinaBlockTimestamp", timestamp: n.CortinaBlockTimestamp},
		{name: "durangoBlockTimestamp", timestamp: n.DurangoBlockTimestamp},
		{name: "etnaTimestamp", timestamp: n.EtnaTimestamp},
		{name: "fUpgradeTimestamp", timestamp: n.FUpgradeTimestamp},
	}
}

// IsApricotPhase1 returns whether [time] represents a block
// with a timestamp after the Apricot Phase 1 upgrade time.
func (n *NetworkUpgrades) IsApricotPhase1(time uint64) bool {
	return isTimestampForked(n.ApricotPhase1BlockTimestamp, time)
}

// IsApricotPhase2 returns whether [time] represents a block
// with a timestamp after the Apricot Phase 2 upgrade time.
func (n *NetworkUpgrades) IsApricotPhase2(time uint64) bool {
	return isTimestampForked(n.ApricotPhase2BlockTimestamp, time)
}

// IsApricotPhase3 returns whether [time] represents a block
// with a timestamp after the Apricot Phase 3 upgrade time.
func (n *NetworkUpgrades) IsApricotPhase3(time uint64) bool {
	return isTimestampForked(n.ApricotPhase3BlockTimestamp, time)
}

// IsApricotPhase4 returns whether [time] represents a block
// with a timestamp after the Apricot Phase 4 upgrade time.
func (n *NetworkUpgrades) IsApricotPhase4(time uint64) bool {
	return isTimestampForked(n.ApricotPhase4BlockTimestamp, time)
}

// IsApricotPhase5 returns whether [time] represents a block
// with a timestamp after the Apricot Phase 5 upgrade time.
func (n *NetworkUpgrades) IsApricotPhase5(time uint64) bool {
	return isTimestampForked(n.ApricotPhase5BlockTimestamp, time)
}

// IsApricotPhasePre6 returns whether [time] represents a block
// with a timestamp after the Apricot Phase Pre 6 upgrade time.
func (n *NetworkUpgrades) IsApricotPhasePre6(time uint64) bool {
	return isTimestampForked(n.ApricotPhasePre6BlockTimestamp, time)
}

// IsApricotPhase6 returns whether [time] represents a block
// with a timestamp after the Apricot Phase 6 upgrade time.
func (n *NetworkUpgrades) IsApricotPhase6(time uint64) bool {
	return isTimestampForked(n.ApricotPhase6BlockTimestamp, time)
}

// IsApricotPhasePost6 returns whether [time] represents a block
// with a timestamp after the Apricot Phase 6 Post upgrade time.
func (n *NetworkUpgrades) IsApricotPhasePost6(time uint64) bool {
	return isTimestampForked(n.ApricotPhasePost6BlockTimestamp, time)
}

// IsBanff returns whether [time] represents a block
// with a timestamp after the Banff upgrade time.
func (n *NetworkUpgrades) IsBanff(time uint64) bool {
	return isTimestampForked(n.BanffBlockTimestamp, time)
}

// IsCortina returns whether [time] represents a block
// with a timestamp after the Cortina upgrade time.
func (n *NetworkUpgrades) IsCortina(time uint64) bool {
	return isTimestampForked(n.CortinaBlockTimestamp, time)
}

// IsDurango returns whether [time] represents a block
// with a timestamp after the Durango upgrade time.
func (n *NetworkUpgrades) IsDurango(time uint64) bool {
	return isTimestampForked(n.DurangoBlockTimestamp, time)
}

// IsEtna returns whether [time] represents a block
// with a timestamp after the Etna upgrade time.
func (n *NetworkUpgrades) IsEtna(time uint64) bool {
	return isTimestampForked(n.EtnaTimestamp, time)
}

// IsFUpgrade returns whether [time] represents a block
// with a timestamp after the F upgrade time.
func (n *NetworkUpgrades) IsFUpgrade(time uint64) bool {
	return isTimestampForked(n.FUpgradeTimestamp, time)
}

func (n *NetworkUpgrades) Description() string {
	var banner string
	banner += fmt.Sprintf(" - Apricot Phase 1 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.3.0)\n", ptrToString(n.ApricotPhase1BlockTimestamp))
	banner += fmt.Sprintf(" - Apricot Phase 2 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.4.0)\n", ptrToString(n.ApricotPhase2BlockTimestamp))
	banner += fmt.Sprintf(" - Apricot Phase 3 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.5.0)\n", ptrToString(n.ApricotPhase3BlockTimestamp))
	banner += fmt.Sprintf(" - Apricot Phase 4 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.6.0)\n", ptrToString(n.ApricotPhase4BlockTimestamp))
	banner += fmt.Sprintf(" - Apricot Phase 5 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0)\n", ptrToString(n.ApricotPhase5BlockTimestamp))
	banner += fmt.Sprintf(" - Apricot Phase P6 Timestamp        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.8.0)\n", ptrToString(n.ApricotPhasePre6BlockTimestamp))
	banner += fmt.Sprintf(" - Apricot Phase 6 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.8.0)\n", ptrToString(n.ApricotPhase6BlockTimestamp))
	banner += fmt.Sprintf(" - Apricot Phase Post-6 Timestamp:   @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.8.0\n", ptrToString(n.ApricotPhasePost6BlockTimestamp))
	banner += fmt.Sprintf(" - Banff Timestamp:                  @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0)\n", ptrToString(n.BanffBlockTimestamp))
	banner += fmt.Sprintf(" - Cortina Timestamp:                @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.10.0)\n", ptrToString(n.CortinaBlockTimestamp))
	banner += fmt.Sprintf(" - Durango Timestamp:                @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.11.0)\n", ptrToString(n.DurangoBlockTimestamp))
	banner += fmt.Sprintf(" - Etna Timestamp:                   @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.12.0)\n", ptrToString(n.EtnaTimestamp))
	banner += fmt.Sprintf(" - F-Upgrade Timestamp:              @%-10v (Unscheduled)\n", ptrToString(n.FUpgradeTimestamp))
	return banner
}

func GetNetworkUpgrades(agoUpgrade upgrade.Config) NetworkUpgrades {
	return NetworkUpgrades{
		ApricotPhase1BlockTimestamp:     utils.TimeToNewUint64(agoUpgrade.ApricotPhase1Time),
		ApricotPhase2BlockTimestamp:     utils.TimeToNewUint64(agoUpgrade.ApricotPhase2Time),
		ApricotPhase3BlockTimestamp:     utils.TimeToNewUint64(agoUpgrade.ApricotPhase3Time),
		ApricotPhase4BlockTimestamp:     utils.TimeToNewUint64(agoUpgrade.ApricotPhase4Time),
		ApricotPhase5BlockTimestamp:     utils.TimeToNewUint64(agoUpgrade.ApricotPhase5Time),
		ApricotPhasePre6BlockTimestamp:  utils.TimeToNewUint64(agoUpgrade.ApricotPhasePre6Time),
		ApricotPhase6BlockTimestamp:     utils.TimeToNewUint64(agoUpgrade.ApricotPhase6Time),
		ApricotPhasePost6BlockTimestamp: utils.TimeToNewUint64(agoUpgrade.ApricotPhasePost6Time),
		BanffBlockTimestamp:             utils.TimeToNewUint64(agoUpgrade.BanffTime),
		CortinaBlockTimestamp:           utils.TimeToNewUint64(agoUpgrade.CortinaTime),
		DurangoBlockTimestamp:           utils.TimeToNewUint64(agoUpgrade.DurangoTime),
		EtnaTimestamp:                   utils.TimeToNewUint64(agoUpgrade.EtnaTime),
		FUpgradeTimestamp:               utils.TimeToNewUint64(agoUpgrade.FUpgradeTime),
	}
}

type AvalancheRules struct {
	IsApricotPhase1, IsApricotPhase2, IsApricotPhase3, IsApricotPhase4, IsApricotPhase5 bool
	IsApricotPhasePre6, IsApricotPhase6, IsApricotPhasePost6                            bool
	IsBanff                                                                             bool
	IsCortina                                                                           bool
	IsDurango                                                                           bool
	IsEtna                                                                              bool
	IsFUpgrade                                                                          bool
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
		IsFUpgrade:          n.IsFUpgrade(timestamp),
	}
}
