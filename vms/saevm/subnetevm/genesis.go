// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnetevm

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/feemanager"
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

	// The legacy `FeeConfig` is inert under SAE (ACP-176 and ACP-224 own gas
	// pricing) and is left empty by SAE chain configs. The one consumer that
	// still reads it is a pre-Helicon `feeManager` activation whose config
	// omits `initialFeeConfig`: mirror the legacy plugin by substituting the
	// default so activation does not seed zeroed storage.
	if configExtra.FeeConfig == commontype.EmptyFeeConfig && feeManagerConfigured(configExtra) {
		ctx.Log.Info("no fee config given in genesis with feeManager configured; using the default fee config",
			zap.Reflect("defaultFeeConfig", subnetevmparams.DefaultFeeConfig),
		)
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

// feeManagerConfigured reports whether the legacy `feeManager` precompile
// appears anywhere in the chain config (genesis or upgrades).
func feeManagerConfigured(configExtra *extras.ChainConfig) bool {
	if _, ok := configExtra.GenesisPrecompiles[feemanager.ConfigKey]; ok {
		return true
	}
	for _, upgrade := range configExtra.PrecompileUpgrades {
		if upgrade.Key() == feemanager.ConfigKey {
			return true
		}
	}
	return false
}

var lastSyncKey = prefixdb.MakePrefix([]byte("lastSync"))

// readLastSync returns the RLP encoding of the last synchronously executed
// block, when one was recorded.
//
// TODO: nothing writes this key yet; transition support (materializing a
// legacy chain's tip as the last synchronous block) will reintroduce a
// writer.
func readLastSync(db database.KeyValueReader) ([]byte, error) {
	return db.Get(lastSyncKey)
}

// lastSynchronousBlock returns the block SAE resumes from: the recorded
// last synchronous block when one exists (see [readLastSync]), otherwise the
// genesis block.
func lastSynchronousBlock(db database.KeyValueReader, genesis *core.Genesis) (*types.Block, error) {
	lastSyncBytes, err := readLastSync(db)
	switch {
	case err == nil:
		lastSync := new(types.Block)
		if err := rlp.DecodeBytes(lastSyncBytes, lastSync); err != nil {
			return nil, fmt.Errorf("rlp.DecodeBytes(..., %T): %w", lastSync, err)
		}
		return lastSync, nil
	case errors.Is(err, database.ErrNotFound):
		return genesis.ToBlock(), nil
	default:
		return nil, err
	}
}
