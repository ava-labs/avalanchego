// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/stretchr/testify/require"
)

var addrStrArray = []string{
	"A9bTQjfYGBFK3JPRJqF2eh3JYL7cHocvy",
	"6mxBGnjGDCKgkVe7yfrmvMA7xE7qCv3vv",
	"6ncQ19Q2U4MamkCYzshhD8XFjfwAWFzTa",
	"Jz9ayEDt7dx9hDx45aXALujWmL9ZUuqe7",
}

var testHRP = constants.NetworkIDToHRP[constants.UnitTestID]

func TestBuildGenesis(t *testing.T) {
	ss := CreateStaticService()
	addrMap := map[string]string{}
	for _, addrStr := range addrStrArray {
		addr, err := ids.ShortFromString(addrStr)
		if err != nil {
			t.Fatal(err)
		}
		addrMap[addrStr], err = address.FormatBech32(testHRP, addr[:])
		if err != nil {
			t.Fatal(err)
		}
	}
	args := BuildGenesisArgs{
		Encoding: formatting.Hex,
		GenesisData: map[string]AssetDefinition{
			"asset1": {
				Name:         "myFixedCapAsset",
				Symbol:       "MFCA",
				Denomination: 8,
				InitialState: map[string][]interface{}{
					"fixedCap": {
						Holder{
							Amount:  100000,
							Address: addrMap["A9bTQjfYGBFK3JPRJqF2eh3JYL7cHocvy"],
						},
						Holder{
							Amount:  100000,
							Address: addrMap["6mxBGnjGDCKgkVe7yfrmvMA7xE7qCv3vv"],
						},
						Holder{
							Amount:  json.Uint64(startBalance),
							Address: addrMap["6ncQ19Q2U4MamkCYzshhD8XFjfwAWFzTa"],
						},
						Holder{
							Amount:  json.Uint64(startBalance),
							Address: addrMap["Jz9ayEDt7dx9hDx45aXALujWmL9ZUuqe7"],
						},
					},
				},
			},
			"asset2": {
				Name:   "myVarCapAsset",
				Symbol: "MVCA",
				InitialState: map[string][]interface{}{
					"variableCap": {
						Owners{
							Threshold: 1,
							Minters: []string{
								addrMap["A9bTQjfYGBFK3JPRJqF2eh3JYL7cHocvy"],
								addrMap["6mxBGnjGDCKgkVe7yfrmvMA7xE7qCv3vv"],
							},
						},
						Owners{
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
				InitialState: map[string][]interface{}{
					"variableCap": {
						Owners{
							Threshold: 1,
							Minters: []string{
								addrMap["A9bTQjfYGBFK3JPRJqF2eh3JYL7cHocvy"],
							},
						},
					},
				},
			},
		},
	}
	reply := BuildGenesisReply{}
	err := ss.BuildGenesis(nil, &args, &reply)
	if err != nil {
		t.Fatal(err)
	}
	require := require.New(t)
	require.Equal(reply.Bytes, "0x0000000000030006617373657431000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000f6d794669786564436170417373657400044d4643410800000001000000000000000400000007000000000000c350000000000000000000000001000000013f78e510df62bc48b0829ec06d6a6b98062d695300000007000000000000c35000000000000000000000000100000001c54903de5177a16f7811771ef2f4659d9e8646710000000700000000000186a0000000000000000000000001000000013f58fda2e9ea8d9e4b181832a07b26dae286f2cb0000000700000000000186a000000000000000000000000100000001645938bb7ae2193270e6ffef009e3664d11e07c10006617373657432000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000d6d79566172436170417373657400044d5643410000000001000000000000000200000006000000000000000000000001000000023f58fda2e9ea8d9e4b181832a07b26dae286f2cb645938bb7ae2193270e6ffef009e3664d11e07c100000006000000000000000000000001000000023f78e510df62bc48b0829ec06d6a6b98062d6953c54903de5177a16f7811771ef2f4659d9e864671000661737365743300000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000126d794f7468657256617243617041737365740000000000000100000000000000010000000600000000000000000000000100000001645938bb7ae2193270e6ffef009e3664d11e07c1279fa028")
}
