// (c) 2022 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"math/big"
	"testing"

	"github.com/ava-labs/subnet-evm/commontype"
	"github.com/ava-labs/subnet-evm/precompile"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyWithChainConfig(t *testing.T) {
	admins := []common.Address{{1}}
	baseConfig := *SubnetEVMDefaultChainConfig
	config := &baseConfig
	config.PrecompileUpgrade = PrecompileUpgrade{
		TxAllowListConfig: precompile.NewTxAllowListConfig(big.NewInt(2), nil, nil),
	}
	config.PrecompileUpgrades = []PrecompileUpgrade{
		{
			// disable TxAllowList at timestamp 4
			TxAllowListConfig: precompile.NewDisableTxAllowListConfig(big.NewInt(4)),
		},
		{
			// re-enable TxAllowList at timestamp 5
			TxAllowListConfig: precompile.NewTxAllowListConfig(big.NewInt(5), admins, nil),
		},
	}

	// check this config is valid
	err := config.Verify()
	assert.NoError(t, err)

	// same precompile cannot be configured twice for the same timestamp
	badConfig := *config
	badConfig.PrecompileUpgrades = append(
		badConfig.PrecompileUpgrades,
		PrecompileUpgrade{
			TxAllowListConfig: precompile.NewDisableTxAllowListConfig(big.NewInt(5)),
		},
	)
	err = badConfig.Verify()
	assert.ErrorContains(t, err, "config timestamp (5) <= previous timestamp (5)")

	// cannot enable a precompile without disabling it first.
	badConfig = *config
	badConfig.PrecompileUpgrades = append(
		badConfig.PrecompileUpgrades,
		PrecompileUpgrade{
			TxAllowListConfig: precompile.NewTxAllowListConfig(big.NewInt(5), admins, nil),
		},
	)
	err = badConfig.Verify()
	assert.ErrorContains(t, err, "disable should be [true]")
}

func TestVerifyPrecompileUpgrades(t *testing.T) {
	admins := []common.Address{{1}}
	tests := []struct {
		name          string
		upgrades      []PrecompileUpgrade
		expectedError string
	}{
		{
			name: "enable and disable tx allow list",
			upgrades: []PrecompileUpgrade{
				{
					TxAllowListConfig: precompile.NewTxAllowListConfig(big.NewInt(1), admins, nil),
				},
				{
					TxAllowListConfig: precompile.NewDisableTxAllowListConfig(big.NewInt(2)),
				},
			},
			expectedError: "",
		},
		{
			name: "invalid allow list config in tx allowlist",
			upgrades: []PrecompileUpgrade{
				{
					TxAllowListConfig: precompile.NewTxAllowListConfig(big.NewInt(1), admins, nil),
				},
				{
					TxAllowListConfig: precompile.NewDisableTxAllowListConfig(big.NewInt(2)),
				},
				{
					TxAllowListConfig: precompile.NewTxAllowListConfig(big.NewInt(3), admins, admins),
				},
			},
			expectedError: "cannot set address",
		},
		{
			name: "invalid initial fee manager config",
			upgrades: []PrecompileUpgrade{
				{
					FeeManagerConfig: precompile.NewFeeManagerConfig(big.NewInt(3), admins, nil,
						&commontype.FeeConfig{
							GasLimit: big.NewInt(-1),
						}),
				},
			},
			expectedError: "gasLimit = -1 cannot be less than or equal to 0",
		},
		{
			name: "invalid initial fee manager config gas limit 0",
			upgrades: []PrecompileUpgrade{
				{
					FeeManagerConfig: precompile.NewFeeManagerConfig(big.NewInt(3), admins, nil,
						&commontype.FeeConfig{
							GasLimit: big.NewInt(0),
						}),
				},
			},
			expectedError: "gasLimit = 0 cannot be less than or equal to 0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			baseConfig := *SubnetEVMDefaultChainConfig
			config := &baseConfig
			config.PrecompileUpgrades = tt.upgrades

			err := config.Verify()
			if tt.expectedError == "" {
				require.NoError(err)
			} else {
				require.ErrorContains(err, tt.expectedError)
			}
		})
	}
}

func TestVerifyPrecompiles(t *testing.T) {
	admins := []common.Address{{1}}
	tests := []struct {
		name          string
		upgrade       PrecompileUpgrade
		expectedError string
	}{
		{
			name: "invalid allow list config in tx allowlist",
			upgrade: PrecompileUpgrade{
				TxAllowListConfig: precompile.NewTxAllowListConfig(big.NewInt(3), admins, admins),
			},
			expectedError: "cannot set address",
		},
		{
			name: "invalid initial fee manager config",
			upgrade: PrecompileUpgrade{
				FeeManagerConfig: precompile.NewFeeManagerConfig(big.NewInt(3), admins, nil,
					&commontype.FeeConfig{
						GasLimit: big.NewInt(-1),
					}),
			},
			expectedError: "gasLimit = -1 cannot be less than or equal to 0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			baseConfig := *SubnetEVMDefaultChainConfig
			config := &baseConfig
			config.PrecompileUpgrade = tt.upgrade

			err := config.Verify()
			if tt.expectedError == "" {
				require.NoError(err)
			} else {
				require.ErrorContains(err, tt.expectedError)
			}
		})
	}
}

func TestVerifyRequiresSortedTimestamps(t *testing.T) {
	admins := []common.Address{{1}}
	baseConfig := *SubnetEVMDefaultChainConfig
	config := &baseConfig
	config.PrecompileUpgrades = []PrecompileUpgrade{
		{
			TxAllowListConfig: precompile.NewTxAllowListConfig(big.NewInt(2), admins, nil),
		},
		{
			ContractDeployerAllowListConfig: precompile.NewContractDeployerAllowListConfig(big.NewInt(1), admins, nil),
		},
	}

	// block timestamps must be monotonically increasing, so this config is invalid
	err := config.Verify()
	assert.ErrorContains(t, err, "config timestamp (1) < previous timestamp (2)")
}

func TestGetPrecompileConfig(t *testing.T) {
	assert := assert.New(t)
	baseConfig := *SubnetEVMDefaultChainConfig
	config := &baseConfig
	config.PrecompileUpgrade = PrecompileUpgrade{
		ContractDeployerAllowListConfig: precompile.NewContractDeployerAllowListConfig(big.NewInt(10), nil, nil),
	}

	deployerConfig := config.GetContractDeployerAllowListConfig(big.NewInt(0))
	assert.Nil(deployerConfig)

	deployerConfig = config.GetContractDeployerAllowListConfig(big.NewInt(10))
	assert.NotNil(deployerConfig)

	deployerConfig = config.GetContractDeployerAllowListConfig(big.NewInt(11))
	assert.NotNil(deployerConfig)

	txAllowListConfig := config.GetTxAllowListConfig(big.NewInt(0))
	assert.Nil(txAllowListConfig)
}
