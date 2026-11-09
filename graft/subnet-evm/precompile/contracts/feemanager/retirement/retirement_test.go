// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package retirement_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/feemanager"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/feemanager/retirement"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/nativeminter"
)

func TestReconcileForIgloo(t *testing.T) {
	const (
		igloo     = uint64(100)
		preIgloo  = igloo - 5
		postIgloo = igloo + 5
	)

	fmEnable := func(timestamp uint64) extras.PrecompileUpgrade {
		return extras.PrecompileUpgrade{Config: feemanager.NewConfig(utils.PointerTo(timestamp), nil, nil, nil, nil)}
	}
	fmDisable := func(timestamp uint64) extras.PrecompileUpgrade {
		return extras.PrecompileUpgrade{Config: feemanager.NewDisableConfig(utils.PointerTo(timestamp))}
	}
	mintEnable := func(timestamp uint64) extras.PrecompileUpgrade {
		return extras.PrecompileUpgrade{Config: nativeminter.NewConfig(utils.PointerTo(timestamp), nil, nil, nil, nil)}
	}
	mintDisable := func(timestamp uint64) extras.PrecompileUpgrade {
		return extras.PrecompileUpgrade{Config: nativeminter.NewDisableConfig(utils.PointerTo(timestamp))}
	}
	fmGenesis := func(timestamp *uint64) extras.Precompiles {
		return extras.Precompiles{
			feemanager.ConfigKey: feemanager.NewConfig(timestamp, nil, nil, nil, nil),
		}
	}
	mintGenesis := nativeminter.NewConfig(utils.PointerTo[uint64](0), nil, nil, nil, nil)

	tests := []struct {
		name             string
		genesisTimestamp uint64
		genesis          extras.Precompiles
		upgrades         []extras.PrecompileUpgrade
		wantGenesis      extras.Precompiles
		wantUpgrades     []extras.PrecompileUpgrade
		wantErr          error
	}{
		{name: "no fee manager"},
		{
			name:         "genesis activation is disabled at Igloo",
			genesis:      fmGenesis(utils.PointerTo[uint64](0)),
			wantGenesis:  fmGenesis(utils.PointerTo[uint64](0)),
			wantUpgrades: []extras.PrecompileUpgrade{fmDisable(igloo)},
		},
		{
			name:         "upgrade activation is disabled at Igloo",
			upgrades:     []extras.PrecompileUpgrade{fmEnable(preIgloo)},
			wantUpgrades: []extras.PrecompileUpgrade{fmEnable(preIgloo), fmDisable(igloo)},
		},
		{
			name:         "existing disable is preserved",
			genesis:      fmGenesis(utils.PointerTo[uint64](0)),
			upgrades:     []extras.PrecompileUpgrade{fmDisable(preIgloo)},
			wantGenesis:  fmGenesis(utils.PointerTo[uint64](0)),
			wantUpgrades: []extras.PrecompileUpgrade{fmDisable(preIgloo)},
		},
		{
			name:     "upgrade at Igloo is rejected",
			upgrades: []extras.PrecompileUpgrade{fmEnable(igloo)},
			wantErr:  retirement.ErrFeeManagerEnabledAfterIgloo,
		},
		{
			name:             "stale genesis activation is removed",
			genesisTimestamp: preIgloo,
			genesis: extras.Precompiles{
				feemanager.ConfigKey:   feemanager.NewConfig(utils.PointerTo(postIgloo), nil, nil, nil, nil),
				nativeminter.ConfigKey: mintGenesis,
			},
			wantGenesis: extras.Precompiles{nativeminter.ConfigKey: mintGenesis},
		},
		{
			name:        "synthetic disable preserves global upgrade order",
			genesis:     fmGenesis(utils.PointerTo[uint64](0)),
			upgrades:    []extras.PrecompileUpgrade{mintEnable(preIgloo), mintDisable(postIgloo)},
			wantGenesis: fmGenesis(utils.PointerTo[uint64](0)),
			wantUpgrades: []extras.PrecompileUpgrade{
				mintEnable(preIgloo),
				fmDisable(igloo),
				mintDisable(postIgloo),
			},
		},
		{
			name:             "post-Igloo genesis is rejected",
			genesisTimestamp: igloo,
			genesis:          fmGenesis(nil),
			wantErr:          retirement.ErrFeeManagerEnabledAfterIgloo,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := extras.ChainConfig{
				GenesisPrecompiles: test.genesis,
				UpgradeConfig: extras.UpgradeConfig{
					PrecompileUpgrades: test.upgrades,
				},
			}
			err := retirement.ReconcileForIgloo(&got, test.genesisTimestamp, igloo)
			require.ErrorIs(t, err, test.wantErr, "ReconcileForIgloo()")
			if err != nil {
				return
			}
			require.Equal(t, test.wantGenesis, got.GenesisPrecompiles, "ReconcileForIgloo() genesis precompiles")
			require.Equal(t, test.wantUpgrades, got.PrecompileUpgrades, "ReconcileForIgloo() precompile upgrades")
		})
	}
}
