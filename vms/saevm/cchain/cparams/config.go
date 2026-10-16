// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cparams

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/snow"

	ethparams "github.com/ava-labs/libevm/params"
)

type ChainConfig struct {
	NetworkUpgrades

	PrecompileUpgrades []PrecompileUpgrade `json:"-"`
	SnowCtx            *snow.Context       `json:"-"`
}

//nolint:revive // General-purpose types lose the meaning of args if unused ones are removed
func (c *ChainConfig) CheckConfigCompatible(newEth *ethparams.ChainConfig, headNumber *big.Int, headTimestamp uint64) *ethparams.ConfigCompatError {
	if c == nil {
		return nil
	}
	newCfg, ok := newEth.Hooks().(*ChainConfig)
	if !ok || newCfg == nil {
		// Proper registration of the extras on the libevm side should prevent this from happening.
		// Return an error to prevent the chain from starting, just in case.
		return ethparams.NewTimestampCompatError(
			fmt.Sprintf("ChainConfig.Hooks() is not a non-nil %T, got %T", c, newEth.Hooks()),
			utils.PointerTo[uint64](0),
			nil,
		)
	}

	return c.checkUpgradesCompatible(&newCfg.NetworkUpgrades, headTimestamp)
}

func (c *ChainConfig) Description() string {
	if c == nil {
		return ""
	}
	var banner string

	banner += "Avalanche Upgrades:\n"
	banner += c.NetworkUpgrades.description()
	banner += "\n"

	upgradeConfigBytes, err := json.Marshal(marshalablePrecompileUpgrades{c.PrecompileUpgrades})
	if err != nil {
		upgradeConfigBytes = []byte("cannot marshal PrecompileUpgrades")
	}
	banner += "Upgrade Config: " + string(upgradeConfigBytes)
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
		if cur.timestamp == nil {
			continue
		}
		if prev.timestamp == nil {
			return fmt.Errorf("unsupported fork ordering: %s not enabled, but %s enabled at timestamp %d",
				prev.name, cur.name, *cur.timestamp)
		}
		if *prev.timestamp > *cur.timestamp {
			return fmt.Errorf("unsupported fork ordering: %s enabled at timestamp %d, but %s enabled at timestamp %d",
				prev.name, *prev.timestamp, cur.name, *cur.timestamp)
		}
	}
	return nil
}
