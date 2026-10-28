// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cparams

import (
	"fmt"
	"math/big"

	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/upgrade"

	ethparams "github.com/ava-labs/libevm/params"
)

// ChainConfig defines all Avalanche-specific forks and network changes.
type ChainConfig struct {
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

	SnowCtx *snow.Context `json:"-"`
}

// FromUpgrades creates a new ChainConfig from the given [upgrade.Config].
func FromUpgrades(u upgrade.Config) *ChainConfig {
	return &ChainConfig{
		ApricotPhase1Timestamp:     new(upgrade.UnixTimestamp(u.ApricotPhase1Time)),
		ApricotPhase2Timestamp:     new(upgrade.UnixTimestamp(u.ApricotPhase2Time)),
		ApricotPhase3Timestamp:     new(upgrade.UnixTimestamp(u.ApricotPhase3Time)),
		ApricotPhase4Timestamp:     new(upgrade.UnixTimestamp(u.ApricotPhase4Time)),
		ApricotPhase5Timestamp:     new(upgrade.UnixTimestamp(u.ApricotPhase5Time)),
		ApricotPhasePre6Timestamp:  new(upgrade.UnixTimestamp(u.ApricotPhasePre6Time)),
		ApricotPhase6Timestamp:     new(upgrade.UnixTimestamp(u.ApricotPhase6Time)),
		ApricotPhasePost6Timestamp: new(upgrade.UnixTimestamp(u.ApricotPhasePost6Time)),
		BanffTimestamp:             new(upgrade.UnixTimestamp(u.BanffTime)),
		CortinaTimestamp:           new(upgrade.UnixTimestamp(u.CortinaTime)),
		DurangoTimestamp:           new(upgrade.UnixTimestamp(u.DurangoTime)),
		EtnaTimestamp:              new(upgrade.UnixTimestamp(u.EtnaTime)),
		FortunaTimestamp:           new(upgrade.UnixTimestamp(u.FortunaTime)),
		GraniteTimestamp:           new(upgrade.UnixTimestamp(u.GraniteTime)),
		HeliconTimestamp:           new(upgrade.UnixTimestamp(u.HeliconTime)),
		IglooTimestamp:             new(upgrade.UnixTimestamp(u.IglooTime)),
	}
}

// CheckConfigCompatible supplements [ethparams.ChainConfig.CheckCompatible]
// with Avalanche-specific network upgrades.
//
//nolint:revive // General-purpose types lose the meaning of args if unused ones are removed
func (c *ChainConfig) CheckConfigCompatible(newEth *ethparams.ChainConfig, headNumber *big.Int, head uint64) *ethparams.ConfigCompatError {
	if c == nil {
		return nil
	}
	newCfg, ok := newEth.Hooks().(*ChainConfig)
	if !ok || newCfg == nil {
		// Proper registration of the extras on the libevm side should prevent this from happening.
		return ethparams.NewTimestampCompatError(
			fmt.Sprintf("ChainConfig.Hooks() is not a non-nil %T, got %T", c, newEth.Hooks()),
			new(uint64),
			nil,
		)
	}

	oldForks := c.forks()
	newForks := newCfg.forks()
	for i, oldFork := range oldForks {
		newFork := newForks[i]
		if ptrEqual(oldFork.time, newFork.time) {
			continue
		}

		// Either fork could retroactively deactivate an upgrade
		if isTimestampForked(oldFork.time, head) || isTimestampForked(newFork.time, head) {
			return ethparams.NewTimestampCompatError(
				fmt.Sprintf("%v fork block timestamp", oldFork.name),
				oldFork.time,
				newFork.time,
			)
		}
	}

	return nil
}

func ptrEqual[T comparable](x, y *T) bool {
	if x == nil || y == nil {
		return x == y
	}
	return *x == *y
}

func (*ChainConfig) Description() string {
	return "ChainConfig.Description() not implemented"
}

func (c *ChainConfig) CheckConfigForkOrder() error {
	if c == nil {
		return nil
	}

	forks := c.forks()
	for i := 1; i < len(forks); i++ {
		prev, cur := forks[i-1], forks[i]
		if cur.time == nil {
			continue
		}
		if prev.time == nil {
			return fmt.Errorf("unsupported fork ordering: %s not enabled, but %s enabled at timestamp %d",
				prev.name, cur.name, *cur.time)
		}
		if *prev.time > *cur.time {
			return fmt.Errorf("unsupported fork ordering: %s enabled at timestamp %d, but %s enabled at timestamp %d",
				prev.name, *prev.time, cur.name, *cur.time)
		}
	}
	return nil
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
func (c *ChainConfig) forks() []fork {
	return []fork{
		{name: "Apricot Phase 1", time: c.ApricotPhase1Timestamp},
		{name: "Apricot Phase 2", time: c.ApricotPhase2Timestamp},
		{name: "Apricot Phase 3", time: c.ApricotPhase3Timestamp},
		{name: "Apricot Phase 4", time: c.ApricotPhase4Timestamp},
		{name: "Apricot Phase 5", time: c.ApricotPhase5Timestamp},
		{name: "Apricot Phase Pre 6", time: c.ApricotPhasePre6Timestamp},
		{name: "Apricot Phase 6", time: c.ApricotPhase6Timestamp},
		{name: "Apricot Phase Post 6", time: c.ApricotPhasePost6Timestamp},
		{name: "Banff", time: c.BanffTimestamp},
		{name: "Cortina", time: c.CortinaTimestamp},
		{name: "Durango", time: c.DurangoTimestamp},
		{name: "Etna", time: c.EtnaTimestamp},
		{name: "Fortuna", time: c.FortunaTimestamp},
		{name: "Granite", time: c.GraniteTimestamp},
		{name: "Helicon", time: c.HeliconTimestamp},
		{name: "Igloo", time: c.IglooTimestamp},
	}
}

func (c *ChainConfig) Rules(timestamp uint64) AvalancheRules {
	return AvalancheRules{
		IsApricotPhase1:    c.IsApricotPhase1(timestamp),
		IsApricotPhase2:    c.IsApricotPhase2(timestamp),
		IsApricotPhasePre6: c.IsApricotPhasePre6(timestamp),
		IsApricotPhase6:    c.IsApricotPhase6(timestamp),
		IsBanff:            c.IsBanff(timestamp),
		IsDurango:          c.IsDurango(timestamp),
		IsFortuna:          c.IsFortuna(timestamp),
		IsGranite:          c.IsGranite(timestamp),
		IsHelicon:          c.IsHelicon(timestamp),
		IsIgloo:            c.IsIgloo(timestamp),
	}
}

// IsApricotPhase1 returns whether time represents a block with a timestamp
// after the Apricot Phase 1 upgrade time.
func (c *ChainConfig) IsApricotPhase1(time uint64) bool {
	return isTimestampForked(c.ApricotPhase1Timestamp, time)
}

// IsApricotPhase2 returns whether time represents a block with a timestamp
// after the Apricot Phase 2 upgrade time.
func (c *ChainConfig) IsApricotPhase2(time uint64) bool {
	return isTimestampForked(c.ApricotPhase2Timestamp, time)
}

// IsApricotPhase3 returns whether time represents a block with a timestamp
// after the Apricot Phase 3 upgrade time.
func (c *ChainConfig) IsApricotPhase3(time uint64) bool {
	return isTimestampForked(c.ApricotPhase3Timestamp, time)
}

// IsApricotPhase5 returns whether time represents a block with a timestamp
// after the Apricot Phase 5 upgrade time.
func (c *ChainConfig) IsApricotPhase5(time uint64) bool {
	return isTimestampForked(c.ApricotPhase5Timestamp, time)
}

// IsApricotPhasePre6 returns whether time represents a block with a timestamp
// after the Apricot Phase Pre 6 upgrade time.
func (c *ChainConfig) IsApricotPhasePre6(time uint64) bool {
	return isTimestampForked(c.ApricotPhasePre6Timestamp, time)
}

// IsApricotPhase6 returns whether time represents a block with a timestamp
// after the Apricot Phase 6 upgrade time.
func (c *ChainConfig) IsApricotPhase6(time uint64) bool {
	return isTimestampForked(c.ApricotPhase6Timestamp, time)
}

// IsBanff returns whether time represents a block with a timestamp after the
// Banff upgrade time.
func (c *ChainConfig) IsBanff(time uint64) bool {
	return isTimestampForked(c.BanffTimestamp, time)
}

// IsDurango returns whether time represents a block with a timestamp after the
// Durango upgrade time.
func (c *ChainConfig) IsDurango(time uint64) bool {
	return isTimestampForked(c.DurangoTimestamp, time)
}

// IsEtna returns whether time represents a block with a timestamp after the
// Etna upgrade time.
func (c *ChainConfig) IsEtna(time uint64) bool {
	return isTimestampForked(c.EtnaTimestamp, time)
}

// IsFortuna returns whether time represents a block with a timestamp after the
// Fortuna upgrade time.
func (c *ChainConfig) IsFortuna(time uint64) bool {
	return isTimestampForked(c.FortunaTimestamp, time)
}

// IsGranite returns whether time represents a block with a timestamp after the
// Granite upgrade time.
func (c *ChainConfig) IsGranite(time uint64) bool {
	return isTimestampForked(c.GraniteTimestamp, time)
}

// IsHelicon returns whether time represents a block with a timestamp after the
// Helicon upgrade time.
func (c *ChainConfig) IsHelicon(time uint64) bool {
	return isTimestampForked(c.HeliconTimestamp, time)
}

// IsIgloo returns whether time represents a block with a timestamp after the
// Igloo upgrade time.
func (c *ChainConfig) IsIgloo(time uint64) bool {
	return isTimestampForked(c.IglooTimestamp, time)
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
