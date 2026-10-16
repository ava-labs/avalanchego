// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cparams

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/snow"

	ethparams "github.com/ava-labs/libevm/params"
)

type ChainConfig struct {
	NetworkUpgrades

	PrecompileConfigs []precompileconfig.Config `json:"-"`
	SnowCtx           *snow.Context             `json:"-"`
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

func (c *ChainConfig) Description() string {
	if c == nil {
		return ""
	}
	var banner string

	banner += "Avalanche Upgrades:\n"
	banner += c.NetworkUpgrades.description()
	banner += "\n"

	// Each config is keyed by its precompile, e.g. {"warpConfig": {...}}.
	upgrades := make(map[string]precompileconfig.Config, len(c.PrecompileConfigs))
	for _, u := range c.PrecompileConfigs {
		upgrades[u.Key()] = u
	}
	upgradesJSON, err := json.Marshal(upgrades)
	if err != nil {
		upgradesJSON = []byte("cannot marshal precompile upgrades")
	}
	banner += "Precompiles: " + string(upgradesJSON)
	banner += "\n"
	return banner
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
