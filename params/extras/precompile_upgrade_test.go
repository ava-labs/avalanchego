// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extras

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/subnet-evm/precompile/contracts/deployerallowlist"
	"github.com/ava-labs/subnet-evm/precompile/contracts/txallowlist"
	"github.com/ava-labs/subnet-evm/utils"
	"github.com/stretchr/testify/require"
)

func TestVerifyUpgradeConfig(t *testing.T) {
	admins := []common.Address{{1}}
	chainConfig := &ChainConfig{
		FeeConfig: DefaultFeeConfig,
	}
	chainConfig.GenesisPrecompiles = Precompiles{
		txallowlist.ConfigKey: txallowlist.NewConfig(utils.NewUint64(1), admins, nil, nil),
	}

	type test struct {
		upgrades            []PrecompileUpgrade
		expectedErrorString string
	}

	tests := map[string]test{
		"upgrade bytes conflicts with genesis (re-enable without disable)": {
			expectedErrorString: "disable should be [true]",
			upgrades: []PrecompileUpgrade{
				{
					Config: txallowlist.NewConfig(utils.NewUint64(2), admins, nil, nil),
				},
			},
		},
		"upgrade bytes conflicts with genesis (disable before enable)": {
			expectedErrorString: "config block timestamp (0) <= previous timestamp (1) of same key",
			upgrades: []PrecompileUpgrade{
				{
					Config: txallowlist.NewDisableConfig(utils.NewUint64(0)),
				},
			},
		},
		"upgrade bytes conflicts with genesis (disable same time as enable)": {
			expectedErrorString: "config block timestamp (1) <= previous timestamp (1) of same key",
			upgrades: []PrecompileUpgrade{
				{
					Config: txallowlist.NewDisableConfig(utils.NewUint64(1)),
				},
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			// make a local copy of the chainConfig
			chainConfig := chainConfig

			// verify with the upgrades from the test
			chainConfig.UpgradeConfig.PrecompileUpgrades = tt.upgrades
			err := chainConfig.Verify()

			if tt.expectedErrorString != "" {
				require.ErrorContains(t, err, tt.expectedErrorString)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCheckCompatibleUpgradeConfigs(t *testing.T) {
	admins := []common.Address{{1}}
	chainConfig := &ChainConfig{}
	chainConfig.GenesisPrecompiles = Precompiles{
		txallowlist.ConfigKey:       txallowlist.NewConfig(utils.NewUint64(1), admins, nil, nil),
		deployerallowlist.ConfigKey: deployerallowlist.NewConfig(utils.NewUint64(10), admins, nil, nil),
	}

	tests := map[string]upgradeCompatibilityTest{
		"disable and re-enable": {
			startTimestamps: []uint64{5},
			configs: []*UpgradeConfig{
				{
					PrecompileUpgrades: []PrecompileUpgrade{
						{
							Config: txallowlist.NewDisableConfig(utils.NewUint64(6)),
						},
						{
							Config: txallowlist.NewConfig(utils.NewUint64(7), admins, nil, nil),
						},
					},
				},
			},
		},
		"disable and re-enable, reschedule upgrade before it happens": {
			startTimestamps: []uint64{5, 6},
			configs: []*UpgradeConfig{
				{
					PrecompileUpgrades: []PrecompileUpgrade{
						{
							Config: txallowlist.NewDisableConfig(utils.NewUint64(6)),
						},
						{
							Config: txallowlist.NewConfig(utils.NewUint64(7), admins, nil, nil),
						},
					},
				},
				{
					PrecompileUpgrades: []PrecompileUpgrade{
						{
							Config: txallowlist.NewDisableConfig(utils.NewUint64(6)),
						},
						{
							Config: txallowlist.NewConfig(utils.NewUint64(8), admins, nil, nil),
						},
					},
				},
			},
		},
		"disable and re-enable, reschedule upgrade after it happens": {
			expectedErrorString: "mismatching PrecompileUpgrade",
			startTimestamps:     []uint64{5, 8},
			configs: []*UpgradeConfig{
				{
					PrecompileUpgrades: []PrecompileUpgrade{
						{
							Config: txallowlist.NewDisableConfig(utils.NewUint64(6)),
						},
						{
							Config: txallowlist.NewConfig(utils.NewUint64(7), admins, nil, nil),
						},
					},
				},
				{
					PrecompileUpgrades: []PrecompileUpgrade{
						{
							Config: txallowlist.NewDisableConfig(utils.NewUint64(6)),
						},
						{
							Config: txallowlist.NewConfig(utils.NewUint64(8), admins, nil, nil),
						},
					},
				},
			},
		},
		"disable and re-enable, cancel upgrade before it happens": {
			startTimestamps: []uint64{5, 6},
			configs: []*UpgradeConfig{
				{
					PrecompileUpgrades: []PrecompileUpgrade{
						{
							Config: txallowlist.NewDisableConfig(utils.NewUint64(6)),
						},
						{
							Config: txallowlist.NewConfig(utils.NewUint64(7), admins, nil, nil),
						},
					},
				},
				{
					PrecompileUpgrades: []PrecompileUpgrade{
						{
							Config: txallowlist.NewDisableConfig(utils.NewUint64(6)),
						},
					},
				},
			},
		},
		"disable and re-enable, cancel upgrade after it happens": {
			expectedErrorString: "mismatching missing PrecompileUpgrade",
			startTimestamps:     []uint64{5, 8},
			configs: []*UpgradeConfig{
				{
					PrecompileUpgrades: []PrecompileUpgrade{
						{
							Config: txallowlist.NewDisableConfig(utils.NewUint64(6)),
						},
						{
							Config: txallowlist.NewConfig(utils.NewUint64(7), admins, nil, nil),
						},
					},
				},
				{
					PrecompileUpgrades: []PrecompileUpgrade{
						{
							Config: txallowlist.NewDisableConfig(utils.NewUint64(6)),
						},
					},
				},
			},
		},
		"disable and re-enable, change upgrade config after upgrade not allowed": {
			expectedErrorString: "mismatching PrecompileUpgrade",
			startTimestamps:     []uint64{5, 8},
			configs: []*UpgradeConfig{
				{
					PrecompileUpgrades: []PrecompileUpgrade{
						{
							Config: txallowlist.NewDisableConfig(utils.NewUint64(6)),
						},
						{
							Config: txallowlist.NewConfig(utils.NewUint64(7), admins, nil, nil),
						},
					},
				},
				{
					PrecompileUpgrades: []PrecompileUpgrade{
						{
							Config: txallowlist.NewDisableConfig(utils.NewUint64(6)),
						},
						{
							// uses a different (empty) admin list, not allowed
							Config: txallowlist.NewConfig(utils.NewUint64(7), []common.Address{}, nil, nil),
						},
					},
				},
			},
		},
		"disable and re-enable, identical upgrade config should be accepted": {
			startTimestamps: []uint64{5, 8},
			configs: []*UpgradeConfig{
				{
					PrecompileUpgrades: []PrecompileUpgrade{
						{
							Config: txallowlist.NewDisableConfig(utils.NewUint64(6)),
						},
						{
							Config: txallowlist.NewConfig(utils.NewUint64(7), admins, nil, nil),
						},
					},
				},
				{
					PrecompileUpgrades: []PrecompileUpgrade{
						{
							Config: txallowlist.NewDisableConfig(utils.NewUint64(6)),
						},
						{
							Config: txallowlist.NewConfig(utils.NewUint64(7), admins, nil, nil),
						},
					},
				},
			},
		},
		"retroactively enabling upgrades is not allowed": {
			expectedErrorString: "cannot retroactively enable PrecompileUpgrade[1] in database (have timestamp nil, want timestamp 5, rewindto timestamp 4)",
			startTimestamps:     []uint64{6},
			configs: []*UpgradeConfig{
				{
					PrecompileUpgrades: []PrecompileUpgrade{
						{
							Config: txallowlist.NewDisableConfig(utils.NewUint64(5)),
						},
						{
							Config: txallowlist.NewConfig(utils.NewUint64(6), admins, nil, nil),
						},
					},
				},
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tt.run(t, *chainConfig)
		})
	}
}

type upgradeCompatibilityTest struct {
	configs             []*UpgradeConfig
	startTimestamps     []uint64
	expectedErrorString string
}

func (tt *upgradeCompatibilityTest) run(t *testing.T, chainConfig ChainConfig) {
	// apply all the upgrade bytes specified in order
	for i, upgrade := range tt.configs {
		newCfg := chainConfig
		newCfg.UpgradeConfig = *upgrade

		err := chainConfig.checkConfigCompatible(&newCfg, new(big.Int), tt.startTimestamps[i])

		// if this is not the final upgradeBytes, continue applying
		// the next upgradeBytes. (only check the result on the last apply)
		if i != len(tt.configs)-1 {
			if err != nil {
				t.Fatalf("expecting checkConfigCompatible call %d to return nil, got %s", i+1, err)
			}
			chainConfig = newCfg
			continue
		}

		if tt.expectedErrorString != "" {
			require.ErrorContains(t, err, tt.expectedErrorString)
		} else {
			require.Nil(t, err)
		}
	}
}
