// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package cchain implements the C-Chain VM atop [sae.VM]. It composes the
// C-Chain block-building hooks, the cross-chain transaction pool, and the avax
// JSON-RPC service that ingests Export and Import transactions.
package cchain

import (
	"encoding/json"
	"fmt"

	"github.com/ava-labs/avalanchego/graft/coreth/core"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	"github.com/ava-labs/avalanchego/snow"
)

// parseGenesis decodes the genesis bytes into a [*core.Genesis] and populates
// the Avalanche-specific config extras (network upgrades, Warp precompile
// schedule, Ethereum upgrade alignment).
func parseGenesis(ctx *snow.Context, b []byte) (*core.Genesis, error) {
	g := new(core.Genesis)
	if err := json.Unmarshal(b, g); err != nil {
		return nil, fmt.Errorf("unmarshalling genesis: %w", err)
	}

	configExtra := params.GetExtra(g.Config)
	configExtra.AvalancheContext = extras.AvalancheContext{
		SnowCtx: ctx,
	}
	configExtra.NetworkUpgrades = extras.GetNetworkUpgrades(ctx.NetworkUpgrades)

	// If Durango is scheduled, schedule the Warp Precompile at the same time.
	if configExtra.DurangoBlockTimestamp != nil {
		configExtra.PrecompileUpgrades = append(configExtra.PrecompileUpgrades, extras.PrecompileUpgrade{
			Config: warp.NewDefaultConfig(configExtra.DurangoBlockTimestamp),
		})
	}
	if err := configExtra.Verify(); err != nil {
		return nil, fmt.Errorf("invalid chain config: %w", err)
	}

	// Align the Ethereum upgrades to the Avalanche upgrades.
	if err := params.SetEthUpgrades(g.Config); err != nil {
		return nil, fmt.Errorf("aligning Ethereum upgrades: %w", err)
	}
	return g, nil
}
