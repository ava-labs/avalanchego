// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cparams

import (
	"fmt"
	"strconv"

	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"
)

// NetworkUpgrades tracks the timestamps of all the Avalanche upgrades.
//
// For each upgrade, a nil value means the fork hasn't happened and is not
// scheduled. A pointer to 0 means the fork has already activated.
type NetworkUpgrades struct {
	ApricotPhase1Timestamp *uint64 `json:"apricotPhase1BlockTimestamp,omitempty"` // Apricot Phase 1 Block Timestamp
	// Apricot Phase 2 Block Timestamp includes a modified version of the Berlin
	// Hard Fork.
	ApricotPhase2Timestamp *uint64 `json:"apricotPhase2BlockTimestamp,omitempty"`
	// Apricot Phase 3 introduces dynamic fees and a modified version of the
	// London Hard Fork.
	ApricotPhase3Timestamp *uint64 `json:"apricotPhase3BlockTimestamp,omitempty"`
	// Apricot Phase 4 introduces the notion of a block fee to the dynamic fee
	// algorithm.
	ApricotPhase4Timestamp *uint64 `json:"apricotPhase4BlockTimestamp,omitempty"`
	// Apricot Phase 5 introduces a batch of atomic transactions with a maximum
	// atomic gas limit per block.
	ApricotPhase5Timestamp *uint64 `json:"apricotPhase5BlockTimestamp,omitempty"`
	// Apricot Phase Pre-6 deprecates the NativeAssetCall precompile (soft).
	ApricotPhasePre6Timestamp *uint64 `json:"apricotPhasePre6BlockTimestamp,omitempty"`
	// Apricot Phase 6 deprecates the NativeAssetBalance and NativeAssetCall
	// precompiles.
	ApricotPhase6Timestamp *uint64 `json:"apricotPhase6BlockTimestamp,omitempty"`
	// Apricot Phase Post-6 deprecates the NativeAssetCall precompile (soft).
	ApricotPhasePost6Timestamp *uint64 `json:"apricotPhasePost6BlockTimestamp,omitempty"`
	// Banff restricts import/export transactions to AVAX.
	BanffTimestamp *uint64 `json:"banffBlockTimestamp,omitempty"`
	// Cortina increases the block gas limit to 15M.
	CortinaTimestamp *uint64 `json:"cortinaBlockTimestamp,omitempty"`
	// Durango activates Avalanche Warp Messaging and the Shanghai Execution
	// Spec Upgrade (https://github.com/ethereum/execution-specs/blob/master/network-upgrades/mainnet-upgrades/shanghai.md#included-eips).
	//
	// Note: EIP-4895 is excluded since withdrawals are not relevant to the
	// Avalanche C-Chain or Subnets running the EVM.
	DurangoTimestamp *uint64 `json:"durangoBlockTimestamp,omitempty"`
	// Etna activates Cancun (https://github.com/ethereum/execution-specs/blob/master/network-upgrades/mainnet-upgrades/cancun.md#included-eips)
	// and reduces the min base fee.
	// Note: EIP-4844 BlobTxs are not enabled in the mempool and blocks are not
	// allowed to contain them. For details see https://github.com/avalanche-foundation/ACPs/pull/131
	EtnaTimestamp *uint64 `json:"etnaTimestamp,omitempty"`
	// Fortuna modifies the gas price mechanism based on ACP-176
	FortunaTimestamp *uint64 `json:"fortunaTimestamp,omitempty"`
	// Granite adds a millisecond timestamp, precompile updates, and P-Chain epochs
	GraniteTimestamp *uint64 `json:"graniteTimestamp,omitempty"`
	// Helicon activates continuous execution (ACP-194) and the dynamic minimum
	// gas price (ACP-283).
	HeliconTimestamp *uint64 `json:"heliconTimestamp,omitempty"`
	// Igloo is a placeholder for the next upgrade.
	IglooTimestamp *uint64 `json:"iglooTimestamp,omitempty"`
}

func (n *NetworkUpgrades) description() string {
	var banner string
	banner += fmt.Sprintf(" - Apricot Phase 1 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.3.0)\n", ptrToString(n.ApricotPhase1Timestamp))
	banner += fmt.Sprintf(" - Apricot Phase 2 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.4.0)\n", ptrToString(n.ApricotPhase2Timestamp))
	banner += fmt.Sprintf(" - Apricot Phase 3 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.5.0)\n", ptrToString(n.ApricotPhase3Timestamp))
	banner += fmt.Sprintf(" - Apricot Phase 4 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.6.0)\n", ptrToString(n.ApricotPhase4Timestamp))
	banner += fmt.Sprintf(" - Apricot Phase 5 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0)\n", ptrToString(n.ApricotPhase5Timestamp))
	banner += fmt.Sprintf(" - Apricot Phase Pre-6 Timestamp:    @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.8.0)\n", ptrToString(n.ApricotPhasePre6Timestamp))
	banner += fmt.Sprintf(" - Apricot Phase 6 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.8.0)\n", ptrToString(n.ApricotPhase6Timestamp))
	banner += fmt.Sprintf(" - Apricot Phase Post-6 Timestamp:   @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.8.0)\n", ptrToString(n.ApricotPhasePost6Timestamp))
	banner += fmt.Sprintf(" - Banff Timestamp:                  @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0)\n", ptrToString(n.BanffTimestamp))
	banner += fmt.Sprintf(" - Cortina Timestamp:                @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.10.0)\n", ptrToString(n.CortinaTimestamp))
	banner += fmt.Sprintf(" - Durango Timestamp:                @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.11.0)\n", ptrToString(n.DurangoTimestamp))
	banner += fmt.Sprintf(" - Etna Timestamp:                   @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.12.0)\n", ptrToString(n.EtnaTimestamp))
	banner += fmt.Sprintf(" - Fortuna Timestamp:                @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.13.0)\n", ptrToString(n.FortunaTimestamp))
	banner += fmt.Sprintf(" - Granite Timestamp:                @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.14.0)\n", ptrToString(n.GraniteTimestamp))
	banner += fmt.Sprintf(" - Helicon Timestamp:                @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.15.0)\n", ptrToString(n.HeliconTimestamp))
	banner += fmt.Sprintf(" - Igloo Timestamp:                  @%-10v (Unreleased)\n", ptrToString(n.IglooTimestamp))
	return banner
}

func ptrToString(val *uint64) string {
	if val == nil {
		return "nil"
	}
	return strconv.FormatUint(*val, 10)
}

// isTimestampForked returns whether a fork scheduled at timestamp t is active
// at the given head timestamp.
func isTimestampForked(t *uint64, head uint64) bool {
	if t == nil {
		return false
	}
	return *t <= head
}

type fork struct {
	name string
	time *uint64
}

// A list of all network upgrades in chronological order. Timestamps are NOT
// guaranteed to be in ascending order.
func (n *NetworkUpgrades) forks() []fork {
	return []fork{
		{name: "Apricot Phase 1", time: n.ApricotPhase1Timestamp},
		{name: "Apricot Phase 2", time: n.ApricotPhase2Timestamp},
		{name: "Apricot Phase 3", time: n.ApricotPhase3Timestamp},
		{name: "Apricot Phase 4", time: n.ApricotPhase4Timestamp},
		{name: "Apricot Phase 5", time: n.ApricotPhase5Timestamp},
		{name: "Apricot Phase Pre 6", time: n.ApricotPhasePre6Timestamp},
		{name: "Apricot Phase 6", time: n.ApricotPhase6Timestamp},
		{name: "Apricot Phase Post 6", time: n.ApricotPhasePost6Timestamp},
		{name: "Banff", time: n.BanffTimestamp},
		{name: "Cortina", time: n.CortinaTimestamp},
		{name: "Durango", time: n.DurangoTimestamp},
		{name: "Etna", time: n.EtnaTimestamp},
		{name: "Fortuna", time: n.FortunaTimestamp},
		{name: "Granite", time: n.GraniteTimestamp},
		{name: "Helicon", time: n.HeliconTimestamp},
		{name: "Igloo", time: n.IglooTimestamp},
	}
}

func (n *NetworkUpgrades) Rules(timestamp uint64) AvalancheRules {
	return AvalancheRules{
		IsApricotPhase1:    n.IsApricotPhase1(timestamp),
		IsApricotPhase2:    n.IsApricotPhase2(timestamp),
		IsApricotPhasePre6: n.IsApricotPhasePre6(timestamp),
		IsApricotPhase6:    n.IsApricotPhase6(timestamp),
		IsBanff:            n.IsBanff(timestamp),
		IsDurango:          n.IsDurango(timestamp),
		IsFortuna:          n.IsFortuna(timestamp),
		IsGranite:          n.IsGranite(timestamp),
		IsHelicon:          n.IsHelicon(timestamp),
		IsIgloo:            n.IsIgloo(timestamp),
	}
}

// IsApricotPhase1 returns whether time represents a block with a timestamp
// after the Apricot Phase 1 upgrade time.
func (n *NetworkUpgrades) IsApricotPhase1(time uint64) bool {
	return isTimestampForked(n.ApricotPhase1Timestamp, time)
}

// IsApricotPhase2 returns whether time represents a block with a timestamp
// after the Apricot Phase 2 upgrade time.
func (n *NetworkUpgrades) IsApricotPhase2(time uint64) bool {
	return isTimestampForked(n.ApricotPhase2Timestamp, time)
}

// IsApricotPhase3 returns whether time represents a block with a timestamp
// after the Apricot Phase 3 upgrade time.
func (n *NetworkUpgrades) IsApricotPhase3(time uint64) bool {
	return isTimestampForked(n.ApricotPhase3Timestamp, time)
}

// IsApricotPhase5 returns whether time represents a block with a timestamp
// after the Apricot Phase 5 upgrade time.
func (n *NetworkUpgrades) IsApricotPhase5(time uint64) bool {
	return isTimestampForked(n.ApricotPhase5Timestamp, time)
}

// IsApricotPhasePre6 returns whether time represents a block with a timestamp
// after the Apricot Phase Pre 6 upgrade time.
func (n *NetworkUpgrades) IsApricotPhasePre6(time uint64) bool {
	return isTimestampForked(n.ApricotPhasePre6Timestamp, time)
}

// IsApricotPhase6 returns whether time represents a block with a timestamp
// after the Apricot Phase 6 upgrade time.
func (n *NetworkUpgrades) IsApricotPhase6(time uint64) bool {
	return isTimestampForked(n.ApricotPhase6Timestamp, time)
}

// IsBanff returns whether time represents a block with a timestamp after the
// Banff upgrade time.
func (n *NetworkUpgrades) IsBanff(time uint64) bool {
	return isTimestampForked(n.BanffTimestamp, time)
}

// IsDurango returns whether time represents a block with a timestamp after the
// Durango upgrade time.
func (n *NetworkUpgrades) IsDurango(time uint64) bool {
	return isTimestampForked(n.DurangoTimestamp, time)
}

// IsEtna returns whether time represents a block with a timestamp after the
// Etna upgrade time.
func (n *NetworkUpgrades) IsEtna(time uint64) bool {
	return isTimestampForked(n.EtnaTimestamp, time)
}

// IsFortuna returns whether time represents a block with a timestamp after the
// Fortuna upgrade time.
func (n *NetworkUpgrades) IsFortuna(time uint64) bool {
	return isTimestampForked(n.FortunaTimestamp, time)
}

// IsGranite returns whether time represents a block with a timestamp after the
// Granite upgrade time.
func (n *NetworkUpgrades) IsGranite(time uint64) bool {
	return isTimestampForked(n.GraniteTimestamp, time)
}

// IsHelicon returns whether time represents a block with a timestamp after the
// Helicon upgrade time.
func (n *NetworkUpgrades) IsHelicon(time uint64) bool {
	return isTimestampForked(n.HeliconTimestamp, time)
}

// IsIgloo returns whether time represents a block with a timestamp after the
// Igloo upgrade time.
func (n *NetworkUpgrades) IsIgloo(time uint64) bool {
	return isTimestampForked(n.IglooTimestamp, time)
}

// AvalancheRules defines the activation status of various Avalanche network
// upgrades.
type AvalancheRules struct {
	IsApricotPhase1    bool
	IsApricotPhase2    bool
	IsApricotPhasePre6 bool
	IsApricotPhase6    bool
	IsBanff            bool
	IsDurango          bool
	IsFortuna          bool
	IsGranite          bool
	IsHelicon          bool
	IsIgloo            bool
}

var _ precompileconfig.Rules = AvalancheRules{}

func (a AvalancheRules) IsGraniteActivated() bool {
	return a.IsGranite
}

func (a AvalancheRules) IsDurangoActivated() bool {
	return a.IsDurango
}
