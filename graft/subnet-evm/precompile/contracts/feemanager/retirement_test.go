// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package feemanager_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/feemanager"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/feemanager/feemanagertest"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"
)

const helicon = uint64(100)

// TestRetirementCases drives the canonical
// [feemanagertest.RetirementCases] table through the same helper
// composition `parseGenesis` uses, without spinning up a VM. Asserts
// the post-reconcile chain config matches each case's expected
// `GenesisPrecompiles` and `PrecompileUpgrades`.
func TestRetirementCases(t *testing.T) {
	for _, tc := range feemanagertest.RetirementCases(helicon) {
		t.Run(tc.Name, func(t *testing.T) {
			cfg, err := simulateParseGenesis(t, tc, helicon)
			require.ErrorIs(t, err, tc.WantErr)
			if err != nil {
				return
			}
			require.Equal(t, tc.WantGenesisPrecompiles, cfg.GenesisPrecompiles,
				"post-reconcile GenesisPrecompiles mismatch")
			require.Equal(t, tc.WantPrecompileUpgrades, cfg.PrecompileUpgrades,
				"post-reconcile PrecompileUpgrades mismatch")
		})
	}
}

// simulateParseGenesis applies the same `feeManager`-related helper
// composition that `parseGenesis` runs on each VM:
//  1. [feemanager.ReconcileForHelicon] on the chain config.
//  2. [feemanager.ForceDisableAtHelicon] on the post-reconcile config.
//  3. [extras.ChainConfig.Verify] on the post-reconcile config; this
//     covers the per-entry [feemanager.Config.Verify] pass that
//     enforces the post-Helicon enable rejection.
//
// Returns the post-reconcile chain config (so callers can assert on
// `GenesisPrecompiles` + `PrecompileUpgrades`) and the first error
// produced; nil when the case is accepted.
//
// The `cfg.GenesisPrecompiles` defaults to `extras.Precompiles{}`
// when the case has no input genesis, mirroring the JSON unmarshal
// path that `vm.Initialize` runs (which always produces a non-nil
// map via [extras.Precompiles.UnmarshalJSON]).
func simulateParseGenesis(t *testing.T, tc feemanagertest.RetirementCase, helicon uint64) (*extras.ChainConfig, error) {
	t.Helper()

	genesisPrecompiles := tc.GenesisPrecompiles
	if genesisPrecompiles == nil {
		genesisPrecompiles = extras.Precompiles{}
	}

	chainConfig := *extras.TestHeliconChainConfig
	chainConfig.HeliconTimestamp = utils.PointerTo(helicon)
	// SnowCtx must be non-nil because [extras.ChainConfig.Verify] reads
	// `c.SnowCtx.NetworkUpgrades`. We mirror the chain config (all
	// pre-Helicon forks at time 0, Helicon at `helicon`) so
	// `verifyNetworkUpgrades` accepts every per-fork timestamp.
	agoUpgrades := upgradetest.GetConfigWithUpgradeTime(upgradetest.Helicon, time.Unix(0, 0))
	chainConfig.AvalancheContext = extras.AvalancheContext{
		SnowCtx: &snow.Context{NetworkUpgrades: agoUpgrades},
	}
	chainConfig.GenesisPrecompiles = genesisPrecompiles
	chainConfig.PrecompileUpgrades = tc.Upgrades

	genesis, err := feemanager.ReconcileForHelicon(&chainConfig, tc.GenesisTimestamp, helicon)
	if err != nil {
		return nil, err
	}
	chainConfig.GenesisPrecompiles = genesis
	chainConfig.PrecompileUpgrades = feemanager.ForceDisableAtHelicon(&chainConfig, helicon)

	if err := chainConfig.Verify(); err != nil {
		return nil, err
	}
	return &chainConfig, nil
}
