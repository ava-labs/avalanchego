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

func TestGenesisBytes(t *testing.T) {
	require := require.New(t)
	addr, err := ids.ShortFromString("A9bTQjfYGBFK3JPRJqF2eh3JYL7cHocvy")
	require.NoError(err)
	addrStr, err := address.FormatBech32(constants.UnitTestHRP, addr[:])
	require.NoError(err)
	genesis, err := NewGenesis(
		constants.UnitTestID,
		map[string]AssetDefinition{
			"asset": {
				Name:   "testAsset",
				Symbol: "TST",
				InitialState: AssetInitialState{
					FixedCap: []Holder{{
						Amount:  42,
						Address: addrStr,
					}},
				},
			},
		},
	)
	require.NoError(err)
	bytes, err := genesis.Bytes()
	require.NoError(err)
	require.NotEmpty(bytes)
}

func assertGenesisAssetsMatch(
	t *testing.T,
	got []*GenesisAsset,
	expected map[string]AssetDefinition,
) {
	require := require.New(t)
	gotMap := make(map[string]*GenesisAsset)
	for _, asset := range got {
		gotMap[asset.Alias] = asset
	}
	require.Len(gotMap, len(expected))
	for alias, def := range expected {
		asset, ok := gotMap[alias]
		require.True(ok)
		require.Equal(def.Name, asset.Name)
		require.Equal(def.Symbol, asset.Symbol)
		require.Equal(def.Denomination, asset.Denomination)

		// Check fixed cap holders count
		if len(def.InitialState.FixedCap) > 0 {
			require.NotEmpty(asset.States)
			fixedCapState := asset.States[0]
			outs := fixedCapState.Outs
			require.Len(outs, len(def.InitialState.FixedCap))
		}

		// Check variable cap owners count
		if len(def.InitialState.VariableCap) > 0 {
			require.NotEmpty(asset.States)
			variableCapStates := asset.States[0]
			require.Len(variableCapStates.Outs, len(def.InitialState.VariableCap))
		}
	}
}

func TestNewGenesis(t *testing.T) {
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
	expected := map[string]AssetDefinition{
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
	}
	genesis, err := NewGenesis(
		constants.UnitTestID,
		expected,
	)
	require.NoError(err)
	require.NotNil(genesis)
	assertGenesisAssetsMatch(t, genesis.Txs, expected)
}
