// (c) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"math/big"
	"testing"

	"github.com/ava-labs/subnet-evm/precompile"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
)

func TestVerifyUpgradeConfig(t *testing.T) {
	admins := []common.Address{{1}}
	chainConfig := *TestChainConfig
	chainConfig.TxAllowListConfig = precompile.NewTxAllowListConfig(big.NewInt(1), admins)

	type test struct {
		upgrades            []PrecompileUpgrade
		expectedErrorString string
	}

	tests := map[string]test{
		"upgrade bytes conflicts with genesis (re-enable without disable)": {
			expectedErrorString: "disable should be [true]",
			upgrades: []PrecompileUpgrade{
				{
					TxAllowListConfig: precompile.NewTxAllowListConfig(big.NewInt(2), admins),
				},
			},
		},
		"upgrade bytes conflicts with genesis (disable before enable)": {
			expectedErrorString: "config timestamp (0) <= previous timestamp (1)",
			upgrades: []PrecompileUpgrade{
				{
					TxAllowListConfig: precompile.NewDisableTxAllowListConfig(big.NewInt(0)),
				},
			},
		},
		"upgrade bytes conflicts with genesis (disable same time as enable)": {
			expectedErrorString: "config timestamp (1) <= previous timestamp (1)",
			upgrades: []PrecompileUpgrade{
				{
					TxAllowListConfig: precompile.NewDisableTxAllowListConfig(big.NewInt(1)),
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
				assert.ErrorContains(t, err, tt.expectedErrorString)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCheckCompatibleUpgradeConfigs(t *testing.T) {
	admins := []common.Address{{1}}
	chainConfig := *TestChainConfig
	chainConfig.TxAllowListConfig = precompile.NewTxAllowListConfig(big.NewInt(1), admins)
	chainConfig.ContractDeployerAllowListConfig = precompile.NewContractDeployerAllowListConfig(big.NewInt(10), admins)

	type test struct {
		configs             []*UpgradeConfig
		startTimestamps     []*big.Int
		expectedErrorString string
	}

	tests := map[string]test{
		"disable and re-enable": {
			startTimestamps: []*big.Int{big.NewInt(5)},
			configs: []*UpgradeConfig{
				{
					PrecompileUpgrades: []PrecompileUpgrade{
						{
							TxAllowListConfig: precompile.NewDisableTxAllowListConfig(big.NewInt(6)),
						},
						{
							TxAllowListConfig: precompile.NewTxAllowListConfig(big.NewInt(7), admins),
						},
					},
				},
			},
		},
		"disable and re-enable, reschedule upgrade before it happens": {
			startTimestamps: []*big.Int{big.NewInt(5), big.NewInt(6)},
			configs: []*UpgradeConfig{
				{
					PrecompileUpgrades: []PrecompileUpgrade{
						{
							TxAllowListConfig: precompile.NewDisableTxAllowListConfig(big.NewInt(6)),
						},
						{
							TxAllowListConfig: precompile.NewTxAllowListConfig(big.NewInt(7), admins),
						},
					},
				},
				{
					PrecompileUpgrades: []PrecompileUpgrade{
						{
							TxAllowListConfig: precompile.NewDisableTxAllowListConfig(big.NewInt(6)),
						},
						{
							TxAllowListConfig: precompile.NewTxAllowListConfig(big.NewInt(8), admins),
						},
					},
				},
			},
		},
		"disable and re-enable, reschedule upgrade after it happens": {
			expectedErrorString: "mismatching PrecompileUpgrade",
			startTimestamps:     []*big.Int{big.NewInt(5), big.NewInt(8)},
			configs: []*UpgradeConfig{
				{
					PrecompileUpgrades: []PrecompileUpgrade{
						{
							TxAllowListConfig: precompile.NewDisableTxAllowListConfig(big.NewInt(6)),
						},
						{
							TxAllowListConfig: precompile.NewTxAllowListConfig(big.NewInt(7), admins),
						},
					},
				},
				{
					PrecompileUpgrades: []PrecompileUpgrade{
						{
							TxAllowListConfig: precompile.NewDisableTxAllowListConfig(big.NewInt(6)),
						},
						{
							TxAllowListConfig: precompile.NewTxAllowListConfig(big.NewInt(8), admins),
						},
					},
				},
			},
		},
		"disable and re-enable, cancel upgrade before it happens": {
			startTimestamps: []*big.Int{big.NewInt(5), big.NewInt(6)},
			configs: []*UpgradeConfig{
				{
					PrecompileUpgrades: []PrecompileUpgrade{
						{
							TxAllowListConfig: precompile.NewDisableTxAllowListConfig(big.NewInt(6)),
						},
						{
							TxAllowListConfig: precompile.NewTxAllowListConfig(big.NewInt(7), admins),
						},
					},
				},
				{
					PrecompileUpgrades: []PrecompileUpgrade{
						{
							TxAllowListConfig: precompile.NewDisableTxAllowListConfig(big.NewInt(6)),
						},
					},
				},
			},
		},
		"disable and re-enable, cancel upgrade after it happens": {
			expectedErrorString: "mismatching missing PrecompileUpgrade",
			startTimestamps:     []*big.Int{big.NewInt(5), big.NewInt(8)},
			configs: []*UpgradeConfig{
				{
					PrecompileUpgrades: []PrecompileUpgrade{
						{
							TxAllowListConfig: precompile.NewDisableTxAllowListConfig(big.NewInt(6)),
						},
						{
							TxAllowListConfig: precompile.NewTxAllowListConfig(big.NewInt(7), admins),
						},
					},
				},
				{
					PrecompileUpgrades: []PrecompileUpgrade{
						{
							TxAllowListConfig: precompile.NewDisableTxAllowListConfig(big.NewInt(6)),
						},
					},
				},
			},
		},
		"disable and re-enable, change upgrade config after upgrade not allowed": {
			expectedErrorString: "mismatching PrecompileUpgrade",
			startTimestamps:     []*big.Int{big.NewInt(5), big.NewInt(8)},
			configs: []*UpgradeConfig{
				{
					PrecompileUpgrades: []PrecompileUpgrade{
						{
							TxAllowListConfig: precompile.NewDisableTxAllowListConfig(big.NewInt(6)),
						},
						{
							TxAllowListConfig: precompile.NewTxAllowListConfig(big.NewInt(7), admins),
						},
					},
				},
				{
					PrecompileUpgrades: []PrecompileUpgrade{
						{
							TxAllowListConfig: precompile.NewDisableTxAllowListConfig(big.NewInt(6)),
						},
						{
							// uses a different (empty) admin list, not allowed
							TxAllowListConfig: precompile.NewTxAllowListConfig(big.NewInt(7), []common.Address{}),
						},
					},
				},
			},
		},
		"disable and re-enable, identical upgrade config should be accepted": {
			startTimestamps: []*big.Int{big.NewInt(5), big.NewInt(8)},
			configs: []*UpgradeConfig{
				{
					PrecompileUpgrades: []PrecompileUpgrade{
						{
							TxAllowListConfig: precompile.NewDisableTxAllowListConfig(big.NewInt(6)),
						},
						{
							TxAllowListConfig: precompile.NewTxAllowListConfig(big.NewInt(7), admins),
						},
					},
				},
				{
					PrecompileUpgrades: []PrecompileUpgrade{
						{
							TxAllowListConfig: precompile.NewDisableTxAllowListConfig(big.NewInt(6)),
						},
						{
							TxAllowListConfig: precompile.NewTxAllowListConfig(big.NewInt(7), admins),
						},
					},
				},
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			// make a local copy of the chainConfig
			chainConfig := chainConfig

			// apply all the upgrade bytes specified in order
			for i, upgrade := range tt.configs {
				newCfg := chainConfig
				newCfg.UpgradeConfig = *upgrade

				err := chainConfig.checkCompatible(&newCfg, nil, tt.startTimestamps[i])

				// if this is not the final upgradeBytes, continue applying
				// the next upgradeBytes. (only check the result on the last apply)
				if i != len(tt.configs)-1 {
					if err != nil {
						t.Fatalf("expecting ApplyUpgradeBytes call %d to return nil, got %s", i+1, err)
					}
					chainConfig = newCfg
					continue
				}

				if tt.expectedErrorString != "" {
					assert.ErrorContains(t, err, tt.expectedErrorString)
				} else {
					if err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}
