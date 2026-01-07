// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extras

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/graft/evm/utils/utilstest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/deployerallowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/feemanager"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/nativeminter"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/rewardmanager"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/txallowlist"
)

func TestVerifyWithChainConfig(t *testing.T) {
	admins := []common.Address{{1}}
	c := *TestChainConfig
	config := &c
	config.SnowCtx = utilstest.NewTestSnowContext(t, utilstest.SubnetEVMTestChainID)
	config.GenesisPrecompiles = Precompiles{
		txallowlist.ConfigKey: txallowlist.NewConfig(utils.NewUint64(2), nil, nil, nil),
	}
	config.PrecompileUpgrades = []PrecompileUpgrade{
		{
			// disable TxAllowList at timestamp 4
			txallowlist.NewDisableConfig(utils.NewUint64(4)),
		},
		{
			// re-enable TxAllowList at timestamp 5
			txallowlist.NewConfig(utils.NewUint64(5), admins, nil, nil),
		},
	}

	// check this config is valid
	err := config.Verify()
	require.NoError(t, err)

	// same precompile cannot be configured twice for the same timestamp
	badConfig := *config
	badConfig.PrecompileUpgrades = append(
		badConfig.PrecompileUpgrades,
		PrecompileUpgrade{
			Config: txallowlist.NewDisableConfig(utils.NewUint64(5)),
		},
	)
	err = badConfig.Verify()
	require.ErrorIs(t, err, errPrecompileUpgradeSameKeyTimestampNotStrictly)

	// cannot enable a precompile without disabling it first.
	badConfig = *config
	badConfig.PrecompileUpgrades = append(
		badConfig.PrecompileUpgrades,
		PrecompileUpgrade{
			Config: txallowlist.NewConfig(utils.NewUint64(5), admins, nil, nil),
		},
	)
	err = badConfig.Verify()
	require.ErrorIs(t, err, errPrecompileUpgradeInvalidDisable)
}

func TestVerifyWithChainConfigAtNilTimestamp(t *testing.T) {
	admins := []common.Address{{0}}
	c := *TestChainConfig
	config := &c
	config.SnowCtx = utilstest.NewTestSnowContext(t, utilstest.SubnetEVMTestChainID)
	config.PrecompileUpgrades = []PrecompileUpgrade{
		// this does NOT enable the precompile, so it should be upgradeable.
		{Config: txallowlist.NewConfig(nil, nil, nil, nil)},
	}
	require.False(t, config.IsPrecompileEnabled(txallowlist.ContractAddress, 0)) // check the precompile is not enabled.
	config.PrecompileUpgrades = []PrecompileUpgrade{
		{
			// enable TxAllowList at timestamp 5
			Config: txallowlist.NewConfig(utils.NewUint64(5), admins, nil, nil),
		},
	}

	// check this config is valid
	require.NoError(t, config.Verify())
}

func TestVerifyPrecompileUpgrades(t *testing.T) {
	admins := []common.Address{{1}}
	tests := []struct {
		name          string
		upgrades      []PrecompileUpgrade
		expectedError error
	}{
		{
			name: "enable and disable tx allow list",
			upgrades: []PrecompileUpgrade{
				{
					Config: txallowlist.NewConfig(utils.NewUint64(1), admins, nil, nil),
				},
				{
					Config: txallowlist.NewDisableConfig(utils.NewUint64(2)),
				},
			},
			expectedError: nil,
		},
		{
			name: "invalid allow list config in tx allowlist",
			upgrades: []PrecompileUpgrade{
				{
					Config: txallowlist.NewConfig(utils.NewUint64(1), admins, nil, nil),
				},
				{
					Config: txallowlist.NewDisableConfig(utils.NewUint64(2)),
				},
				{
					Config: txallowlist.NewConfig(utils.NewUint64(3), admins, admins, admins),
				},
			},
			expectedError: allowlist.ErrAdminAndEnabledAddress,
		},
		{
			name: "invalid initial fee manager config",
			upgrades: []PrecompileUpgrade{
				{
					Config: feemanager.NewConfig(utils.NewUint64(3), admins, nil, nil,
						func() *commontype.FeeConfig {
							feeConfig := DefaultFeeConfig
							feeConfig.GasLimit = big.NewInt(-1)
							return &feeConfig
						}()),
				},
			},
			expectedError: commontype.ErrGasLimitTooLow,
		},
		{
			name: "invalid initial fee manager config gas limit 0",
			upgrades: []PrecompileUpgrade{
				{
					Config: feemanager.NewConfig(utils.NewUint64(3), admins, nil, nil,
						func() *commontype.FeeConfig {
							feeConfig := DefaultFeeConfig
							feeConfig.GasLimit = common.Big0
							return &feeConfig
						}()),
				},
			},
			expectedError: commontype.ErrGasLimitTooLow,
		},
		{
			name: "different upgrades are allowed to configure same timestamp for different precompiles",
			upgrades: []PrecompileUpgrade{
				{
					Config: txallowlist.NewConfig(utils.NewUint64(1), admins, nil, nil),
				},
				{
					Config: feemanager.NewConfig(utils.NewUint64(1), admins, nil, nil, nil),
				},
			},
			expectedError: nil,
		},
		{
			name: "different upgrades must be monotonically increasing",
			upgrades: []PrecompileUpgrade{
				{
					Config: txallowlist.NewConfig(utils.NewUint64(2), admins, nil, nil),
				},
				{
					Config: feemanager.NewConfig(utils.NewUint64(1), admins, nil, nil, nil),
				},
			},
			expectedError: errPrecompileUpgradeTimestampNotMonotonic,
		},
		{
			name: "upgrades with same keys are not allowed to configure same timestamp for same precompiles",
			upgrades: []PrecompileUpgrade{
				{
					Config: txallowlist.NewConfig(utils.NewUint64(1), admins, nil, nil),
				},
				{
					Config: txallowlist.NewDisableConfig(utils.NewUint64(1)),
				},
			},
			expectedError: errPrecompileUpgradeSameKeyTimestampNotStrictly,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			c := *TestChainConfig
			config := &c
			config.SnowCtx = utilstest.NewTestSnowContext(t, utilstest.SubnetEVMTestChainID)
			config.PrecompileUpgrades = tt.upgrades

			err := config.Verify()
			require.ErrorIs(err, tt.expectedError)
		})
	}
}

func TestVerifyPrecompiles(t *testing.T) {
	admins := []common.Address{{1}}
	tests := []struct {
		name          string
		precompiles   Precompiles
		expectedError error
	}{
		{
			name: "invalid allow list config in tx allowlist",
			precompiles: Precompiles{
				txallowlist.ConfigKey: txallowlist.NewConfig(utils.NewUint64(3), admins, admins, admins),
			},
			expectedError: allowlist.ErrAdminAndEnabledAddress,
		},
		{
			name: "invalid initial fee manager config",
			precompiles: Precompiles{
				feemanager.ConfigKey: feemanager.NewConfig(utils.NewUint64(3), admins, nil, nil,
					func() *commontype.FeeConfig {
						feeConfig := DefaultFeeConfig
						feeConfig.GasLimit = big.NewInt(-1)
						return &feeConfig
					}()),
			},
			expectedError: commontype.ErrGasLimitTooLow,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			c := *TestChainConfig
			config := &c
			config.SnowCtx = utilstest.NewTestSnowContext(t, utilstest.SubnetEVMTestChainID)
			config.GenesisPrecompiles = tt.precompiles

			err := config.Verify()
			require.ErrorIs(err, tt.expectedError)
		})
	}
}

func TestVerifyRequiresSortedTimestamps(t *testing.T) {
	admins := []common.Address{{1}}
	config := &ChainConfig{
		FeeConfig: DefaultFeeConfig,
		AvalancheContext: AvalancheContext{
			SnowCtx: utilstest.NewTestSnowContext(t, utilstest.SubnetEVMTestChainID),
		},
	}
	config.PrecompileUpgrades = []PrecompileUpgrade{
		{
			Config: txallowlist.NewConfig(utils.NewUint64(2), admins, nil, nil),
		},
		{
			Config: deployerallowlist.NewConfig(utils.NewUint64(1), admins, nil, nil),
		},
	}

	// block timestamps must be monotonically increasing, so this config is invalid
	err := config.Verify()
	require.ErrorIs(t, err, errPrecompileUpgradeTimestampNotMonotonic)
}

func TestGetPrecompileConfig(t *testing.T) {
	require := require.New(t)
	config := &ChainConfig{}
	config.GenesisPrecompiles = Precompiles{
		deployerallowlist.ConfigKey: deployerallowlist.NewConfig(utils.NewUint64(10), nil, nil, nil),
	}

	deployerConfig := config.GetActivePrecompileConfig(deployerallowlist.ContractAddress, 0)
	require.Nil(deployerConfig)

	deployerConfig = config.GetActivePrecompileConfig(deployerallowlist.ContractAddress, 10)
	require.NotNil(deployerConfig)

	deployerConfig = config.GetActivePrecompileConfig(deployerallowlist.ContractAddress, 11)
	require.NotNil(deployerConfig)

	txAllowListConfig := config.GetActivePrecompileConfig(txallowlist.ContractAddress, 0)
	require.Nil(txAllowListConfig)
}

func TestPrecompileUpgradeUnmarshalJSON(t *testing.T) {
	require := require.New(t)

	upgradeBytes := []byte(`
			{
				"precompileUpgrades": [
					{
						"rewardManagerConfig": {
							"blockTimestamp": 1671542573,
							"adminAddresses": [
								"0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"
							],
							"initialRewardConfig": {
								"allowFeeRecipients": true
							}
						}
					},
					{
						"contractNativeMinterConfig": {
							"blockTimestamp": 1671543172,
							"disable": false
						}
					}
				]
			}
	`)

	var upgradeConfig UpgradeConfig
	require.NoError(json.Unmarshal(upgradeBytes, &upgradeConfig))

	require.Len(upgradeConfig.PrecompileUpgrades, 2)

	rewardManagerConf := upgradeConfig.PrecompileUpgrades[0]
	require.Equal(rewardmanager.ConfigKey, rewardManagerConf.Key())
	testRewardManagerConfig := rewardmanager.NewConfig(
		utils.NewUint64(1671542573),
		[]common.Address{common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC")},
		nil,
		nil,
		&rewardmanager.InitialRewardConfig{
			AllowFeeRecipients: true,
		})
	require.True(rewardManagerConf.Equal(testRewardManagerConfig))

	nativeMinterConfig := upgradeConfig.PrecompileUpgrades[1]
	require.Equal(nativeminter.ConfigKey, nativeMinterConfig.Key())
	expectedNativeMinterConfig := nativeminter.NewConfig(utils.NewUint64(1671543172), nil, nil, nil, nil)
	require.True(nativeMinterConfig.Equal(expectedNativeMinterConfig))

	// Marshal and unmarshal again and check that the result is the same
	upgradeBytes2, err := json.Marshal(upgradeConfig)
	require.NoError(err)
	var upgradeConfig2 UpgradeConfig
	require.NoError(json.Unmarshal(upgradeBytes2, &upgradeConfig2))
	require.Equal(upgradeConfig, upgradeConfig2)
}
