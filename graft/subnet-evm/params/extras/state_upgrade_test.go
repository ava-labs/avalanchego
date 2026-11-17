// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extras

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/math"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/graft/evm/utils/utilstest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/stateupgrade"
)

func TestVerifyStateUpgrades(t *testing.T) {
	modifiedAccounts := map[common.Address]stateupgrade.Account{
		{1}: {
			BalanceChange: (*math.HexOrDecimal256)(common.Big1),
		},
	}
	tests := []struct {
		name          string
		upgrades      []stateupgrade.Block
		expectedError error
	}{
		{
			name: "valid upgrade",
			upgrades: []stateupgrade.Block{
				{Timestamp: utils.PointerTo[uint64](1), Accounts: modifiedAccounts},
				{Timestamp: utils.PointerTo[uint64](2), Accounts: modifiedAccounts},
			},
			expectedError: nil,
		},
		{
			name: "upgrade block timestamp is not strictly increasing",
			upgrades: []stateupgrade.Block{
				{Timestamp: utils.PointerTo[uint64](1), Accounts: modifiedAccounts},
				{Timestamp: utils.PointerTo[uint64](1), Accounts: modifiedAccounts},
			},
			expectedError: errStateUpgradeTimestampNotMonotonic,
		},
		{
			name: "upgrade block timestamp decreases",
			upgrades: []stateupgrade.Block{
				{Timestamp: utils.PointerTo[uint64](2), Accounts: modifiedAccounts},
				{Timestamp: utils.PointerTo[uint64](1), Accounts: modifiedAccounts},
			},
			expectedError: errStateUpgradeTimestampNotMonotonic,
		},
		{
			name: "upgrade block timestamp is zero",
			upgrades: []stateupgrade.Block{
				{Timestamp: utils.PointerTo[uint64](0), Accounts: modifiedAccounts},
			},
			expectedError: errStateUpgradeTimestampZero,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			c := *TestChainConfig
			config := &c
			config.SnowCtx = utilstest.NewTestSnowContext(t, utilstest.SubnetEVMTestChainID)
			config.StateUpgrades = tt.upgrades

			err := config.Verify()
			require.ErrorIs(err, tt.expectedError)
		})
	}
}

func TestCheckCompatibleStateUpgrades(t *testing.T) {
	chainConfig := *TestChainConfig
	stateUpgrade := map[common.Address]stateupgrade.Account{
		{1}: {BalanceChange: (*math.HexOrDecimal256)(common.Big1)},
	}
	differentStateUpgrade := map[common.Address]stateupgrade.Account{
		{2}: {BalanceChange: (*math.HexOrDecimal256)(common.Big1)},
	}

	tests := map[string]upgradeCompatibilityTest{
		"reschedule upgrade before it happens": {
			startTimestamps: []uint64{5, 6},
			configs: []*UpgradeConfig{
				{
					StateUpgrades: []stateupgrade.Block{
						{Timestamp: utils.PointerTo[uint64](6), Accounts: stateUpgrade},
					},
				},
				{
					StateUpgrades: []stateupgrade.Block{
						{Timestamp: utils.PointerTo[uint64](6), Accounts: stateUpgrade},
					},
				},
			},
		},
		"modify upgrade after it happens not allowed": {
			expectedErrorString: "mismatching StateUpgrade",
			startTimestamps:     []uint64{5, 8},
			configs: []*UpgradeConfig{
				{
					StateUpgrades: []stateupgrade.Block{
						{Timestamp: utils.PointerTo[uint64](6), Accounts: stateUpgrade},
						{Timestamp: utils.PointerTo[uint64](7), Accounts: stateUpgrade},
					},
				},
				{
					StateUpgrades: []stateupgrade.Block{
						{Timestamp: utils.PointerTo[uint64](6), Accounts: stateUpgrade},
						{Timestamp: utils.PointerTo[uint64](7), Accounts: differentStateUpgrade},
					},
				},
			},
		},
		"cancel upgrade before it happens": {
			startTimestamps: []uint64{5, 6},
			configs: []*UpgradeConfig{
				{
					StateUpgrades: []stateupgrade.Block{
						{Timestamp: utils.PointerTo[uint64](6), Accounts: stateUpgrade},
						{Timestamp: utils.PointerTo[uint64](7), Accounts: stateUpgrade},
					},
				},
				{
					StateUpgrades: []stateupgrade.Block{
						{Timestamp: utils.PointerTo[uint64](6), Accounts: stateUpgrade},
					},
				},
			},
		},
		"retroactively enabling upgrades is not allowed": {
			expectedErrorString: "cannot retroactively enable StateUpgrade[0] in database (have timestamp nil, want timestamp 5, rewindto timestamp 4)",
			startTimestamps:     []uint64{6},
			configs: []*UpgradeConfig{
				{
					StateUpgrades: []stateupgrade.Block{
						{Timestamp: utils.PointerTo[uint64](5), Accounts: stateUpgrade},
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
		StateUpgrades: []stateupgrade.Block{
			{
				Timestamp: utils.PointerTo[uint64](1677608400),
				Accounts: map[common.Address]stateupgrade.Account{
					common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"): {
						BalanceChange: (*math.HexOrDecimal256)(big.NewInt(100)),
					},
				},
			},
		},
	}
	var unmarshaledConfig UpgradeConfig
	require.NoError(t, json.Unmarshal(jsonBytes, &unmarshaledConfig))
	require.Equal(t, upgradeConfig, unmarshaledConfig)
}
