// (c) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/stretchr/testify/require"
)

func TestVerifyStateUpgrades(t *testing.T) {
	modifiedAccounts := map[common.Address]StateUpgradeAccount{
		{1}: {
			BalanceChange: (*math.HexOrDecimal256)(common.Big1),
		},
	}
	tests := []struct {
		name          string
		upgrades      []StateUpgrade
		expectedError string
	}{
		{
			name: "valid upgrade",
			upgrades: []StateUpgrade{
				{BlockTimestamp: common.Big1, StateUpgradeAccounts: modifiedAccounts},
				{BlockTimestamp: common.Big2, StateUpgradeAccounts: modifiedAccounts},
			},
		},
		{
			name: "upgrade block timestamp is not strictly increasing",
			upgrades: []StateUpgrade{
				{BlockTimestamp: common.Big1, StateUpgradeAccounts: modifiedAccounts},
				{BlockTimestamp: common.Big1, StateUpgradeAccounts: modifiedAccounts},
			},
			expectedError: "config block timestamp (1) <= previous timestamp (1)",
		},
		{
			name: "upgrade block timestamp decreases",
			upgrades: []StateUpgrade{
				{BlockTimestamp: common.Big2, StateUpgradeAccounts: modifiedAccounts},
				{BlockTimestamp: common.Big1, StateUpgradeAccounts: modifiedAccounts},
			},
			expectedError: "config block timestamp (1) <= previous timestamp (2)",
		},
		{
			name: "upgrade block timestamp is zero",
			upgrades: []StateUpgrade{
				{BlockTimestamp: common.Big0, StateUpgradeAccounts: modifiedAccounts},
			},
			expectedError: "config block timestamp (0) must be greater than 0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			baseConfig := *SubnetEVMDefaultChainConfig
			config := &baseConfig
			config.StateUpgrades = tt.upgrades

			err := config.Verify()
			if tt.expectedError == "" {
				require.NoError(err)
			} else {
				require.ErrorContains(err, tt.expectedError)
			}
		})
	}
}

func TestCheckCompatibleStateUpgradeConfigs(t *testing.T) {
	chainConfig := *TestChainConfig
	stateUpgrade := map[common.Address]StateUpgradeAccount{
		{1}: {BalanceChange: (*math.HexOrDecimal256)(common.Big1)},
	}
	differentStateUpgrade := map[common.Address]StateUpgradeAccount{
		{2}: {BalanceChange: (*math.HexOrDecimal256)(common.Big1)},
	}

	tests := map[string]upgradeCompatibilityTest{
		"reschedule upgrade before it happens": {
			startTimestamps: []*big.Int{big.NewInt(5), big.NewInt(6)},
			configs: []*UpgradeConfig{
				{
					StateUpgrades: []StateUpgrade{
						{BlockTimestamp: big.NewInt(6), StateUpgradeAccounts: stateUpgrade},
					},
				},
				{
					StateUpgrades: []StateUpgrade{
						{BlockTimestamp: big.NewInt(6), StateUpgradeAccounts: stateUpgrade},
					},
				},
			},
		},
		"modify upgrade after it happens not allowed": {
			expectedErrorString: "mismatching StateUpgrade",
			startTimestamps:     []*big.Int{big.NewInt(5), big.NewInt(8)},
			configs: []*UpgradeConfig{
				{
					StateUpgrades: []StateUpgrade{
						{BlockTimestamp: big.NewInt(6), StateUpgradeAccounts: stateUpgrade},
						{BlockTimestamp: big.NewInt(7), StateUpgradeAccounts: stateUpgrade},
					},
				},
				{
					StateUpgrades: []StateUpgrade{
						{BlockTimestamp: big.NewInt(6), StateUpgradeAccounts: stateUpgrade},
						{BlockTimestamp: big.NewInt(7), StateUpgradeAccounts: differentStateUpgrade},
					},
				},
			},
		},
		"cancel upgrade before it happens": {
			startTimestamps: []*big.Int{big.NewInt(5), big.NewInt(6)},
			configs: []*UpgradeConfig{
				{
					StateUpgrades: []StateUpgrade{
						{BlockTimestamp: big.NewInt(6), StateUpgradeAccounts: stateUpgrade},
						{BlockTimestamp: big.NewInt(7), StateUpgradeAccounts: stateUpgrade},
					},
				},
				{
					StateUpgrades: []StateUpgrade{
						{BlockTimestamp: big.NewInt(6), StateUpgradeAccounts: stateUpgrade},
					},
				},
			},
		},
		"retroactively enabling upgrades is not allowed": {
			expectedErrorString: "cannot retroactively enable StateUpgrade",
			startTimestamps:     []*big.Int{big.NewInt(6)},
			configs: []*UpgradeConfig{
				{
					StateUpgrades: []StateUpgrade{
						{BlockTimestamp: big.NewInt(5), StateUpgradeAccounts: stateUpgrade},
					},
				},
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tt.run(t, chainConfig)
		})
	}
}

func TestUnmarshalStateUpgradeJSON(t *testing.T) {
	jsonBytes := []byte(
		`{
			"stateUpgrades": [
				{
					"blockTimestamp": 1677608400,
					"accounts": {
						"0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC": {
							"balanceChange": "100"
						}
					}
				}
			]
		}`,
	)

	upgradeConfig := UpgradeConfig{
		StateUpgrades: []StateUpgrade{
			{
				BlockTimestamp: big.NewInt(1677608400),
				StateUpgradeAccounts: map[common.Address]StateUpgradeAccount{
					common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"): {
						BalanceChange: (*math.HexOrDecimal256)(big.NewInt(100)),
					},
				},
			},
		},
	}
	var unmarshaledConfig UpgradeConfig
	err := json.Unmarshal(jsonBytes, &unmarshaledConfig)
	require.NoError(t, err)
	require.Equal(t, upgradeConfig, unmarshaledConfig)
}
