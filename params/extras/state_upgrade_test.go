// (c) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extras

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/math"
	"github.com/ava-labs/subnet-evm/utils"
	"github.com/ava-labs/subnet-evm/utils/utilstest"
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
				{BlockTimestamp: utils.NewUint64(1), StateUpgradeAccounts: modifiedAccounts},
				{BlockTimestamp: utils.NewUint64(2), StateUpgradeAccounts: modifiedAccounts},
			},
		},
		{
			name: "upgrade block timestamp is not strictly increasing",
			upgrades: []StateUpgrade{
				{BlockTimestamp: utils.NewUint64(1), StateUpgradeAccounts: modifiedAccounts},
				{BlockTimestamp: utils.NewUint64(1), StateUpgradeAccounts: modifiedAccounts},
			},
			expectedError: "config block timestamp (1) <= previous timestamp (1)",
		},
		{
			name: "upgrade block timestamp decreases",
			upgrades: []StateUpgrade{
				{BlockTimestamp: utils.NewUint64(2), StateUpgradeAccounts: modifiedAccounts},
				{BlockTimestamp: utils.NewUint64(1), StateUpgradeAccounts: modifiedAccounts},
			},
			expectedError: "config block timestamp (1) <= previous timestamp (2)",
		},
		{
			name: "upgrade block timestamp is zero",
			upgrades: []StateUpgrade{
				{BlockTimestamp: utils.NewUint64(0), StateUpgradeAccounts: modifiedAccounts},
			},
			expectedError: "config block timestamp (0) must be greater than 0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			copy := *TestChainConfig
			config := &copy
			config.SnowCtx = utilstest.NewTestSnowContext(t)
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

func TestCheckCompatibleStateUpgrades(t *testing.T) {
	chainConfig := *TestChainConfig
	stateUpgrade := map[common.Address]StateUpgradeAccount{
		{1}: {BalanceChange: (*math.HexOrDecimal256)(common.Big1)},
	}
	differentStateUpgrade := map[common.Address]StateUpgradeAccount{
		{2}: {BalanceChange: (*math.HexOrDecimal256)(common.Big1)},
	}

	tests := map[string]upgradeCompatibilityTest{
		"reschedule upgrade before it happens": {
			startTimestamps: []uint64{5, 6},
			configs: []*UpgradeConfig{
				{
					StateUpgrades: []StateUpgrade{
						{BlockTimestamp: utils.NewUint64(6), StateUpgradeAccounts: stateUpgrade},
					},
				},
				{
					StateUpgrades: []StateUpgrade{
						{BlockTimestamp: utils.NewUint64(6), StateUpgradeAccounts: stateUpgrade},
					},
				},
			},
		},
		"modify upgrade after it happens not allowed": {
			expectedErrorString: "mismatching StateUpgrade",
			startTimestamps:     []uint64{5, 8},
			configs: []*UpgradeConfig{
				{
					StateUpgrades: []StateUpgrade{
						{BlockTimestamp: utils.NewUint64(6), StateUpgradeAccounts: stateUpgrade},
						{BlockTimestamp: utils.NewUint64(7), StateUpgradeAccounts: stateUpgrade},
					},
				},
				{
					StateUpgrades: []StateUpgrade{
						{BlockTimestamp: utils.NewUint64(6), StateUpgradeAccounts: stateUpgrade},
						{BlockTimestamp: utils.NewUint64(7), StateUpgradeAccounts: differentStateUpgrade},
					},
				},
			},
		},
		"cancel upgrade before it happens": {
			startTimestamps: []uint64{5, 6},
			configs: []*UpgradeConfig{
				{
					StateUpgrades: []StateUpgrade{
						{BlockTimestamp: utils.NewUint64(6), StateUpgradeAccounts: stateUpgrade},
						{BlockTimestamp: utils.NewUint64(7), StateUpgradeAccounts: stateUpgrade},
					},
				},
				{
					StateUpgrades: []StateUpgrade{
						{BlockTimestamp: utils.NewUint64(6), StateUpgradeAccounts: stateUpgrade},
					},
				},
			},
		},
		"retroactively enabling upgrades is not allowed": {
			expectedErrorString: "cannot retroactively enable StateUpgrade[0] in database (have timestamp nil, want timestamp 5, rewindto timestamp 4)",
			startTimestamps:     []uint64{6},
			configs: []*UpgradeConfig{
				{
					StateUpgrades: []StateUpgrade{
						{BlockTimestamp: utils.NewUint64(5), StateUpgradeAccounts: stateUpgrade},
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
				BlockTimestamp: utils.NewUint64(1677608400),
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
