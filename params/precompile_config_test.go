// (c) 2022 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"math/big"
	"testing"

	"github.com/ava-labs/subnet-evm/precompile"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
)

func TestValidateWithChainConfig(t *testing.T) {
	admins := []common.Address{{1}}
	config := &ChainConfig{
		PrecompileUpgrade: PrecompileUpgrade{
			TxAllowListConfig: &precompile.TxAllowListConfig{
				UpgradeableConfig: precompile.UpgradeableConfig{
					BlockTimestamp: big.NewInt(2),
				},
			},
		},
	}
	config.PrecompileUpgrades = []PrecompileUpgrade{
		{
			// disable TxAllowList at timestamp 4
			TxAllowListConfig: precompile.NewDisableTxAllowListConfig(big.NewInt(4)),
		},
		{
			// re-enable TxAllowList at timestamp 5
			TxAllowListConfig: precompile.NewTxAllowListConfig(big.NewInt(5), admins),
		},
	}

	// check this config is valid
	err := config.VerifyPrecompileUpgrades()
	assert.NoError(t, err)

	// same precompile cannot be configured twice for the same timestamp
	badConfig := *config
	badConfig.PrecompileUpgrades = append(
		badConfig.PrecompileUpgrades,
		PrecompileUpgrade{
			TxAllowListConfig: precompile.NewDisableTxAllowListConfig(big.NewInt(5)),
		},
	)
	err = badConfig.VerifyPrecompileUpgrades()
	assert.ErrorContains(t, err, "config timestamp (5) <= previous timestamp (5)")

	// cannot enable a precompile without disabling it first.
	badConfig = *config
	badConfig.PrecompileUpgrades = append(
		badConfig.PrecompileUpgrades,
		PrecompileUpgrade{
			TxAllowListConfig: precompile.NewTxAllowListConfig(big.NewInt(5), admins),
		},
	)
	err = badConfig.VerifyPrecompileUpgrades()
	assert.ErrorContains(t, err, "disable should be [true]")
}

func TestValidate(t *testing.T) {
	admins := []common.Address{{1}}
	config := &ChainConfig{}
	config.PrecompileUpgrades = []PrecompileUpgrade{
		{
			TxAllowListConfig: precompile.NewTxAllowListConfig(big.NewInt(1), admins),
		},
		{
			TxAllowListConfig: precompile.NewDisableTxAllowListConfig(big.NewInt(2)),
		},
	}

	// check this config is valid
	err := config.VerifyPrecompileUpgrades()
	assert.NoError(t, err)
}

func TestValidateRequiresSortedTimestamps(t *testing.T) {
	admins := []common.Address{{1}}
	config := &ChainConfig{}
	config.PrecompileUpgrades = []PrecompileUpgrade{
		{
			TxAllowListConfig: precompile.NewTxAllowListConfig(big.NewInt(2), admins),
		},
		{
			ContractDeployerAllowListConfig: precompile.NewContractDeployerAllowListConfig(big.NewInt(1), admins),
		},
	}

	// block timestamps must be monotonically increasing, so this config is invalid
	err := config.VerifyPrecompileUpgrades()
	assert.ErrorContains(t, err, "config timestamp (1) < previous timestamp (2)")
}

func TestGetPrecompileConfig(t *testing.T) {
	assert := assert.New(t)
	baseConfig := *SubnetEVMDefaultChainConfig
	config := &baseConfig
	config.PrecompileUpgrade = PrecompileUpgrade{
		ContractDeployerAllowListConfig: precompile.NewContractDeployerAllowListConfig(big.NewInt(10), []common.Address{}),
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
