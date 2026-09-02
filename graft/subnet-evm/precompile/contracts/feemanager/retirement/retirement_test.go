// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package retirement_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/feemanager"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/feemanager/retirement"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/nativeminter"
	"github.com/ava-labs/avalanchego/utils"
)

func TestReconcileForHelicon(t *testing.T) {
	const (
		helicon     = uint64(100)
		preHelicon  = helicon - 5
		postHelicon = helicon + 5
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
			name:         "genesis activation is disabled at Helicon",
			genesis:      fmGenesis(utils.PointerTo[uint64](0)),
			wantGenesis:  fmGenesis(utils.PointerTo[uint64](0)),
			wantUpgrades: []extras.PrecompileUpgrade{fmDisable(helicon)},
		},
		{
			name:         "upgrade activation is disabled at Helicon",
			upgrades:     []extras.PrecompileUpgrade{fmEnable(preHelicon)},
			wantUpgrades: []extras.PrecompileUpgrade{fmEnable(preHelicon), fmDisable(helicon)},
		},
		{
			name:         "existing disable is preserved",
			genesis:      fmGenesis(utils.PointerTo[uint64](0)),
			upgrades:     []extras.PrecompileUpgrade{fmDisable(preHelicon)},
			wantGenesis:  fmGenesis(utils.PointerTo[uint64](0)),
			wantUpgrades: []extras.PrecompileUpgrade{fmDisable(preHelicon)},
		},
		{
			name:     "upgrade at Helicon is rejected",
			upgrades: []extras.PrecompileUpgrade{fmEnable(helicon)},
			wantErr:  retirement.ErrFeeManagerEnabledAfterHelicon,
		},
		{
			name:             "stale genesis activation is removed",
			genesisTimestamp: preHelicon,
			genesis: extras.Precompiles{
				feemanager.ConfigKey:   feemanager.NewConfig(utils.PointerTo(postHelicon), nil, nil, nil, nil),
				nativeminter.ConfigKey: mintGenesis,
			},
			wantGenesis: extras.Precompiles{nativeminter.ConfigKey: mintGenesis},
		},
		{
			name:        "synthetic disable preserves global upgrade order",
			genesis:     fmGenesis(utils.PointerTo[uint64](0)),
			upgrades:    []extras.PrecompileUpgrade{mintEnable(preHelicon), mintDisable(postHelicon)},
			wantGenesis: fmGenesis(utils.PointerTo[uint64](0)),
			wantUpgrades: []extras.PrecompileUpgrade{
				mintEnable(preHelicon),
				fmDisable(helicon),
				mintDisable(postHelicon),
			},
		},
		{
			name:             "post-Helicon genesis is rejected",
			genesisTimestamp: helicon,
			genesis:          fmGenesis(nil),
			wantErr:          retirement.ErrFeeManagerEnabledAfterHelicon,
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
			err := retirement.ReconcileForHelicon(&got, test.genesisTimestamp, helicon)
			require.ErrorIs(t, err, test.wantErr, "ReconcileForHelicon()")
			if err != nil {
				return
			}
			require.Equal(t, test.wantGenesis, got.GenesisPrecompiles, "ReconcileForHelicon() genesis precompiles")
			require.Equal(t, test.wantUpgrades, got.PrecompileUpgrades, "ReconcileForHelicon() precompile upgrades")
		})
	}
}
