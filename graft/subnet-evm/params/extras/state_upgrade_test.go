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
				{BlockTimestamp: utils.PointerTo[uint64](1), Accounts: modifiedAccounts},
				{BlockTimestamp: utils.PointerTo[uint64](2), Accounts: modifiedAccounts},
			},
			expectedError: nil,
		},
		{
			name: "upgrade block timestamp is not strictly increasing",
			upgrades: []stateupgrade.Block{
				{BlockTimestamp: utils.PointerTo[uint64](1), Accounts: modifiedAccounts},
				{BlockTimestamp: utils.PointerTo[uint64](1), Accounts: modifiedAccounts},
			},
			expectedError: errStateUpgradeTimestampNotMonotonic,
		},
		{
			name: "upgrade block timestamp decreases",
			upgrades: []stateupgrade.Block{
				{BlockTimestamp: utils.PointerTo[uint64](2), Accounts: modifiedAccounts},
				{BlockTimestamp: utils.PointerTo[uint64](1), Accounts: modifiedAccounts},
			},
			expectedError: errStateUpgradeTimestampNotMonotonic,
		},
		{
			name: "upgrade block timestamp is zero",
			upgrades: []stateupgrade.Block{
				{BlockTimestamp: utils.PointerTo[uint64](0), Accounts: modifiedAccounts},
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
						{BlockTimestamp: utils.PointerTo[uint64](6), Accounts: stateUpgrade},
					},
				},
				{
					StateUpgrades: []stateupgrade.Block{
						{BlockTimestamp: utils.PointerTo[uint64](6), Accounts: stateUpgrade},
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
						{BlockTimestamp: utils.PointerTo[uint64](6), Accounts: stateUpgrade},
						{BlockTimestamp: utils.PointerTo[uint64](7), Accounts: stateUpgrade},
					},
				},
				{
					StateUpgrades: []stateupgrade.Block{
						{BlockTimestamp: utils.PointerTo[uint64](6), Accounts: stateUpgrade},
						{BlockTimestamp: utils.PointerTo[uint64](7), Accounts: differentStateUpgrade},
					},
				},
			},
		},
		"cancel upgrade before it happens": {
			startTimestamps: []uint64{5, 6},
			configs: []*UpgradeConfig{
				{
					StateUpgrades: []stateupgrade.Block{
						{BlockTimestamp: utils.PointerTo[uint64](6), Accounts: stateUpgrade},
						{BlockTimestamp: utils.PointerTo[uint64](7), Accounts: stateUpgrade},
					},
				},
				{
					StateUpgrades: []stateupgrade.Block{
						{BlockTimestamp: utils.PointerTo[uint64](6), Accounts: stateUpgrade},
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
						{BlockTimestamp: utils.PointerTo[uint64](5), Accounts: stateUpgrade},
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

func TestStateUpgradeEqual(t *testing.T) {
	tests := []struct {
		name    string
		upgrade string
		other   string // if empty, round-trips upgrade
		want    bool
	}{
		{
			name:    "round-trip full entry",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x1234","storage":{"0x0000000000000000000000000000000000000000000000000000000000000001":"0x0000000000000000000000000000000000000000000000000000000000000002"},"balanceChange":"100"}}}`,
			want:    true,
		},
		{
			name:    "round-trip empty storage dropped by omitempty",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x1234","storage":{}}}}`,
			want:    true,
		},
		{
			name:    "round-trip empty code dropped by omitempty",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x","balanceChange":"0x1"}}}`,
			want:    true,
		},
		{
			name:    "round-trip decimal zero balance change re-marshaled as hex",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"balanceChange":"0"}}}`,
			want:    true,
		},
		{
			name:    "hex and decimal balance change compare by value",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"balanceChange":"100"}}}`,
			other:   `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"balanceChange":"0x64"}}}`,
			want:    true,
		},
		{
			name:    "different timestamp",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x1234"}}}`,
			other:   `{"blockTimestamp":2,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x1234"}}}`,
			want:    false,
		},
		{
			name:    "different code",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x1234"}}}`,
			other:   `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x125678"}}}`,
			want:    false,
		},
		{
			name:    "different storage value",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"storage":{"0x0000000000000000000000000000000000000000000000000000000000000001":"0x0000000000000000000000000000000000000000000000000000000000000002"}}}}`,
			other:   `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"storage":{"0x0000000000000000000000000000000000000000000000000000000000000001":"0x0000000000000000000000000000000000000000000000000000000000000003"}}}}`,
			want:    false,
		},
		{
			name:    "different balance change",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"balanceChange":"1"}}}`,
			other:   `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"balanceChange":"2"}}}`,
			want:    false,
		},
		{
			name:    "removed balance change",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x1234","balanceChange":"0"}}}`,
			other:   `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x1234"}}}`,
			want:    false,
		},
		{
			name:    "extra account",
			upgrade: `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x1234"}}}`,
			other:   `{"blockTimestamp":1,"accounts":{"0x0100000000000000000000000000000000000000":{"code":"0x1234"},"0x0200000000000000000000000000000000000000":{"code":"0xff"}}}`,
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			var a stateupgrade.Block
			require.NoError(json.Unmarshal([]byte(tt.upgrade), &a))
			var b stateupgrade.Block
			other := []byte(tt.other)
			if len(other) == 0 {
				var err error
				other, err = json.Marshal(a)
				require.NoError(err)
			}
			require.NoError(json.Unmarshal(other, &b))
			require.Equal(tt.want, a.Equal(&b))
			require.Equal(tt.want, b.Equal(&a))
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
				BlockTimestamp: utils.PointerTo[uint64](1677608400),
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
