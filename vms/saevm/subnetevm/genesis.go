// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnetevm

import (
	"encoding/json"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/feemanager/retirement"
	"github.com/ava-labs/avalanchego/snow"

	subnetevmparams "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
)

// parseGenesis parses the operator-supplied genesis and upgrade bytes into a
// [core.Genesis] whose chain config carries the Avalanche extras.
//
// The genesis-level [core.Genesis.Verify] is deliberately not called: its
// checks are either repeated by `configExtra.Verify` or validate the legacy
// fee machinery that SAE replaces with ACP-176/ACP-224.
func parseGenesis(ctx *snow.Context, genesisBytes []byte, upgradeBytes []byte) (*core.Genesis, error) {
	g := new(core.Genesis)
	if err := json.Unmarshal(genesisBytes, g); err != nil {
		return nil, fmt.Errorf("unmarshalling genesis: %w", err)
	}

	// Set the default chain config if not provided, mirroring the legacy
	// plugin.
	if g.Config == nil {
		g.Config = subnetevmparams.SubnetEVMDefaultChainConfig
	}

	// Populate the Avalanche config extras.
	configExtra := subnetevmparams.GetExtra(g.Config)
	configExtra.AvalancheContext = extras.AvalancheContext{
		SnowCtx: ctx,
	}
	// Set network upgrade defaults
	configExtra.SetDefaults(ctx.NetworkUpgrades)

	// Apply upgradeBytes (if any) by unmarshalling them into [chainConfig.UpgradeConfig].
	// Initializing the chain will verify upgradeBytes are compatible with existing values.
	// This should be called before configExtra.Verify().
	if len(upgradeBytes) > 0 {
		var upgradeConfig extras.UpgradeConfig
		if err := json.Unmarshal(upgradeBytes, &upgradeConfig); err != nil {
			return nil, fmt.Errorf("parsing upgrade bytes: %w", err)
		}
		configExtra.UpgradeConfig = upgradeConfig
	}
	if overrides := configExtra.UpgradeConfig.NetworkUpgradeOverrides; overrides != nil {
		configExtra.Override(overrides)
	}

	if configExtra.FeeConfig == commontype.EmptyFeeConfig {
		ctx.Log.Info("no fee config given in genesis; using the default fee config")
		configExtra.FeeConfig = subnetevmparams.DefaultFeeConfig
	}

	// Retire the legacy `feeManager` precompile at Helicon: reject
	// post-Helicon upgrades, normalize a stale genesis activation,
	// and inject the synthetic disable that wipes pre-existing
	// storage at the Helicon block.
	if heliconTS, ok := configExtra.NetworkUpgrades.ScheduledHeliconTimestamp(); ok {
		normalizedGenesisPrecompiles, err := retirement.ReconcileForHelicon(configExtra, g.Timestamp, heliconTS)
		if err != nil {
			return nil, err
		}
		configExtra.GenesisPrecompiles = normalizedGenesisPrecompiles
		configExtra.PrecompileUpgrades = retirement.ForceDisableAtHelicon(configExtra, heliconTS)
	}

	if err := configExtra.Verify(); err != nil {
		return nil, fmt.Errorf("invalid chain config: %w", err)
	}

	// Align all the Ethereum upgrades to the Avalanche upgrades
	if err := subnetevmparams.SetEthUpgrades(g.Config); err != nil {
		return nil, fmt.Errorf("setting eth upgrades: %w", err)
	}
	return g, nil
}

func readLastAcceptedHash(db ethdb.KeyValueReader, genesisHash common.Hash) common.Hash {
	if hash := rawdb.ReadHeadFastBlockHash(db); hash != (common.Hash{}) {
		return hash
	}
	return genesisHash
}
