// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
)

func TestGenesisAssetCompare(t *testing.T) {
	tests := []struct {
		a        *GenesisAsset
		b        *GenesisAsset
		expected int
	}{
		{
			a:        &GenesisAsset{},
			b:        &GenesisAsset{},
			expected: 0,
		},
		{
			a: &GenesisAsset{
				Alias: "a",
			},
			b: &GenesisAsset{
				Alias: "aa",
			},
			expected: -1,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%s_%s_%d", test.a.Alias, test.b.Alias, test.expected), func(t *testing.T) {
			require := require.New(t)

			require.Equal(test.expected, test.a.Compare(test.b))
			require.Equal(-test.expected, test.b.Compare(test.a))
		})
	}
}

func TestNewGenesisBytes(t *testing.T) {
	require := require.New(t)
	addrStrArray := []string{
		"A9bTQjfYGBFK3JPRJqF2eh3JYL7cHocvy",
		"6mxBGnjGDCKgkVe7yfrmvMA7xE7qCv3vv",
		"6ncQ19Q2U4MamkCYzshhD8XFjfwAWFzTa",
		"Jz9ayEDt7dx9hDx45aXALujWmL9ZUuqe7",
	}

	addrMap := map[string]string{}
	for _, addrStr := range addrStrArray {
		addr, err := ids.ShortFromString(addrStr)
		require.NoError(err)
		addrMap[addrStr], err = address.FormatBech32(constants.UnitTestHRP, addr[:])
		require.NoError(err)
	}
	result, err := NewGenesisBytes(
		constants.UnitTestID,
		map[string]AssetDefinition{
			"asset1": {
				Name:         "myFixedCapAsset",
				Symbol:       "MFCA",
				Denomination: 8,
				InitialState: AssetInitialState{
					FixedCap: []Holder{
						{
							Amount:  100000,
							Address: addrMap["A9bTQjfYGBFK3JPRJqF2eh3JYL7cHocvy"],
						},
						{
							Amount:  100000,
							Address: addrMap["6mxBGnjGDCKgkVe7yfrmvMA7xE7qCv3vv"],
						},
						{
							Amount:  startBalance,
							Address: addrMap["6ncQ19Q2U4MamkCYzshhD8XFjfwAWFzTa"],
						},
						{
							Amount:  startBalance,
							Address: addrMap["Jz9ayEDt7dx9hDx45aXALujWmL9ZUuqe7"],
						},
					},
				},
			},
			"asset2": {
				Name:   "myVarCapAsset",
				Symbol: "MVCA",
				InitialState: AssetInitialState{
					VariableCap: []Owners{
						{
							Threshold: 1,
							Minters: []string{
								addrMap["A9bTQjfYGBFK3JPRJqF2eh3JYL7cHocvy"],
								addrMap["6mxBGnjGDCKgkVe7yfrmvMA7xE7qCv3vv"],
							},
						},
						{
							Threshold: 2,
							Minters: []string{
								addrMap["6ncQ19Q2U4MamkCYzshhD8XFjfwAWFzTa"],
								addrMap["Jz9ayEDt7dx9hDx45aXALujWmL9ZUuqe7"],
							},
						},
					},
				},
			},
			"asset3": {
				Name: "myOtherVarCapAsset",
				InitialState: AssetInitialState{
					VariableCap: []Owners{
						{
							Threshold: 1,
							Minters: []string{
								addrMap["A9bTQjfYGBFK3JPRJqF2eh3JYL7cHocvy"],
							},
						},
					},
				},
			},
		},
	)
	require.NoError(err)
	require.NotEmpty(result)
}
