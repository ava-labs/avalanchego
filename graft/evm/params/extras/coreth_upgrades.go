// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extras

import (
	"fmt"

	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/upgrade"

	avagoUtils "github.com/ava-labs/avalanchego/utils"
	ethparams "github.com/ava-labs/libevm/params"
)

type CorethNetworkUpgrades struct {
	ApricotPhase1BlockTimestamp *uint64 `json:"apricotPhase1BlockTimestamp,omitempty"` // Apricot Phase 1 Block Timestamp
	// Apricot Phase 2 Block Timestamp includes a modified version of the Berlin
	// Hard Fork.
	ApricotPhase2BlockTimestamp *uint64 `json:"apricotPhase2BlockTimestamp,omitempty"`
	// Apricot Phase 3 introduces dynamic fees and a modified version of the
	// London Hard Fork.
	ApricotPhase3BlockTimestamp *uint64 `json:"apricotPhase3BlockTimestamp,omitempty"`
	// Apricot Phase 4 introduces the notion of a block fee to the dynamic fee
	// algorithm.
	ApricotPhase4BlockTimestamp *uint64 `json:"apricotPhase4BlockTimestamp,omitempty"`
	// Apricot Phase 5 introduces a batch of atomic transactions with a maximum
	// atomic gas limit per block.
	ApricotPhase5BlockTimestamp *uint64 `json:"apricotPhase5BlockTimestamp,omitempty"`
	// Apricot Phase Pre-6 deprecates the NativeAssetCall precompile (soft).
	ApricotPhasePre6BlockTimestamp *uint64 `json:"apricotPhasePre6BlockTimestamp,omitempty"`
	// Apricot Phase 6 deprecates the NativeAssetBalance and NativeAssetCall
	// precompiles.
	ApricotPhase6BlockTimestamp *uint64 `json:"apricotPhase6BlockTimestamp,omitempty"`
	// Apricot Phase Post-6 deprecates the NativeAssetCall precompile (soft).
	ApricotPhasePost6BlockTimestamp *uint64 `json:"apricotPhasePost6BlockTimestamp,omitempty"`
	// Banff restricts import/export transactions to AVAX.
	BanffBlockTimestamp *uint64 `json:"banffBlockTimestamp,omitempty"`
	// Cortina increases the block gas limit to 15M.
	CortinaBlockTimestamp *uint64 `json:"cortinaBlockTimestamp,omitempty"`
	// Durango activates Avalanche Warp Messaging and the Shanghai Execution
	// Spec Upgrade (https://github.com/ethereum/execution-specs/blob/master/network-upgrades/mainnet-upgrades/shanghai.md#included-eips).
	//
	// Note: EIP-4895 is excluded since withdrawals are not relevant to the
	// Avalanche C-Chain or Subnets running the EVM.
	DurangoBlockTimestamp *uint64 `json:"durangoBlockTimestamp,omitempty"`
}

func (c *CorethNetworkUpgrades) Description() string {
	var banner string
	banner += fmt.Sprintf(" - Apricot Phase 1 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.3.0)\n", ptrToString(c.ApricotPhase1BlockTimestamp))
	banner += fmt.Sprintf(" - Apricot Phase 2 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.4.0)\n", ptrToString(c.ApricotPhase2BlockTimestamp))
	banner += fmt.Sprintf(" - Apricot Phase 3 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.5.0)\n", ptrToString(c.ApricotPhase3BlockTimestamp))
	banner += fmt.Sprintf(" - Apricot Phase 4 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.6.0)\n", ptrToString(c.ApricotPhase4BlockTimestamp))
	banner += fmt.Sprintf(" - Apricot Phase 5 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0)\n", ptrToString(c.ApricotPhase5BlockTimestamp))
	banner += fmt.Sprintf(" - Apricot Phase P6 Timestamp        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.8.0)\n", ptrToString(c.ApricotPhasePre6BlockTimestamp))
	banner += fmt.Sprintf(" - Apricot Phase 6 Timestamp:        @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.8.0)\n", ptrToString(c.ApricotPhase6BlockTimestamp))
	banner += fmt.Sprintf(" - Apricot Phase Post-6 Timestamp:   @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.8.0)\n", ptrToString(c.ApricotPhasePost6BlockTimestamp))
	banner += fmt.Sprintf(" - Banff Timestamp:                  @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0)\n", ptrToString(c.BanffBlockTimestamp))
	banner += fmt.Sprintf(" - Cortina Timestamp:                @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.10.0)\n", ptrToString(c.CortinaBlockTimestamp))
	banner += fmt.Sprintf(" - Durango Timestamp:                @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.11.0)\n", ptrToString(c.DurangoBlockTimestamp))
	return banner
}

func (c *CorethNetworkUpgrades) checkNetworkUpgradesCompatible(newcfg *CorethNetworkUpgrades, time uint64) *ethparams.ConfigCompatError {
	switch {
	case c == nil && newcfg == nil:
		return nil
	case c == nil && newcfg != nil:
		return ethparams.NewTimestampCompatError("expected nil corethNetworkUpgrade", nil, avagoUtils.PointerTo[uint64](0))
	case c != nil && newcfg == nil:
		return ethparams.NewTimestampCompatError("expected non-nil corethNetworkUpgrade", avagoUtils.PointerTo[uint64](0), nil)
	}

	if isForkTimestampIncompatible(c.ApricotPhase1BlockTimestamp, newcfg.ApricotPhase1BlockTimestamp, time) {
		return ethparams.NewTimestampCompatError("ApricotPhase1 fork block timestamp", c.ApricotPhase1BlockTimestamp, newcfg.ApricotPhase1BlockTimestamp)
	}
	if isForkTimestampIncompatible(c.ApricotPhase2BlockTimestamp, newcfg.ApricotPhase2BlockTimestamp, time) {
		return ethparams.NewTimestampCompatError("ApricotPhase2 fork block timestamp", c.ApricotPhase2BlockTimestamp, newcfg.ApricotPhase2BlockTimestamp)
	}
	if isForkTimestampIncompatible(c.ApricotPhase3BlockTimestamp, newcfg.ApricotPhase3BlockTimestamp, time) {
		return ethparams.NewTimestampCompatError("ApricotPhase3 fork block timestamp", c.ApricotPhase3BlockTimestamp, newcfg.ApricotPhase3BlockTimestamp)
	}
	if isForkTimestampIncompatible(c.ApricotPhase4BlockTimestamp, newcfg.ApricotPhase4BlockTimestamp, time) {
		return ethparams.NewTimestampCompatError("ApricotPhase4 fork block timestamp", c.ApricotPhase4BlockTimestamp, newcfg.ApricotPhase4BlockTimestamp)
	}
	if isForkTimestampIncompatible(c.ApricotPhase5BlockTimestamp, newcfg.ApricotPhase5BlockTimestamp, time) {
		return ethparams.NewTimestampCompatError("ApricotPhase5 fork block timestamp", c.ApricotPhase5BlockTimestamp, newcfg.ApricotPhase5BlockTimestamp)
	}
	if isForkTimestampIncompatible(c.ApricotPhasePre6BlockTimestamp, newcfg.ApricotPhasePre6BlockTimestamp, time) {
		return ethparams.NewTimestampCompatError("ApricotPhasePre6 fork block timestamp", c.ApricotPhasePre6BlockTimestamp, newcfg.ApricotPhasePre6BlockTimestamp)
	}
	if isForkTimestampIncompatible(c.ApricotPhase6BlockTimestamp, newcfg.ApricotPhase6BlockTimestamp, time) {
		return ethparams.NewTimestampCompatError("ApricotPhase6 fork block timestamp", c.ApricotPhase6BlockTimestamp, newcfg.ApricotPhase6BlockTimestamp)
	}
	if isForkTimestampIncompatible(c.ApricotPhasePost6BlockTimestamp, newcfg.ApricotPhasePost6BlockTimestamp, time) {
		return ethparams.NewTimestampCompatError("ApricotPhasePost6 fork block timestamp", c.ApricotPhasePost6BlockTimestamp, newcfg.ApricotPhasePost6BlockTimestamp)
	}
	if isForkTimestampIncompatible(c.BanffBlockTimestamp, newcfg.BanffBlockTimestamp, time) {
		return ethparams.NewTimestampCompatError("Banff fork block timestamp", c.BanffBlockTimestamp, newcfg.BanffBlockTimestamp)
	}
	if isForkTimestampIncompatible(c.CortinaBlockTimestamp, newcfg.CortinaBlockTimestamp, time) {
		return ethparams.NewTimestampCompatError("Cortina fork block timestamp", c.CortinaBlockTimestamp, newcfg.CortinaBlockTimestamp)
	}
	if isForkTimestampIncompatible(c.DurangoBlockTimestamp, newcfg.DurangoBlockTimestamp, time) {
		return ethparams.NewTimestampCompatError("Durango fork block timestamp", c.DurangoBlockTimestamp, newcfg.DurangoBlockTimestamp)
	}
	return nil
}

func (c *CorethNetworkUpgrades) forkOrder() []Fork {
	return []Fork{
		{Name: "apricotPhase1BlockTimestamp", Timestamp: c.ApricotPhase1BlockTimestamp},
		{Name: "apricotPhase2BlockTimestamp", Timestamp: c.ApricotPhase2BlockTimestamp},
		{Name: "apricotPhase3BlockTimestamp", Timestamp: c.ApricotPhase3BlockTimestamp},
		{Name: "apricotPhase4BlockTimestamp", Timestamp: c.ApricotPhase4BlockTimestamp},
		{Name: "apricotPhase5BlockTimestamp", Timestamp: c.ApricotPhase5BlockTimestamp},
		{Name: "apricotPhasePre6BlockTimestamp", Timestamp: c.ApricotPhasePre6BlockTimestamp},
		{Name: "apricotPhase6BlockTimestamp", Timestamp: c.ApricotPhase6BlockTimestamp},
		{Name: "apricotPhasePost6BlockTimestamp", Timestamp: c.ApricotPhasePost6BlockTimestamp},
		{Name: "banffBlockTimestamp", Timestamp: c.BanffBlockTimestamp},
		{Name: "cortinaBlockTimestamp", Timestamp: c.CortinaBlockTimestamp},
		{Name: "durangoBlockTimestamp", Timestamp: c.DurangoBlockTimestamp},
	}
}

func GetCorethNetworkUpgrades(agoUpgrade upgrade.Config) NetworkUpgrades {
	return NetworkUpgrades{
		CorethNetworkUpgrades: &CorethNetworkUpgrades{
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
		},
		EtnaTimestamp:    utils.TimeToNewUint64(agoUpgrade.EtnaTime),
		FortunaTimestamp: utils.TimeToNewUint64(agoUpgrade.FortunaTime),
		GraniteTimestamp: utils.TimeToNewUint64(agoUpgrade.GraniteTime),
		HeliconTimestamp: utils.TimeToNewUint64(agoUpgrade.HeliconTime),
	}
}
