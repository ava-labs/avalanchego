// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"testing"

	"github.com/chain4travel/caminogo/utils/constants"
	"github.com/chain4travel/caminogo/utils/formatting"
	"github.com/chain4travel/caminogo/utils/json"
)

var addrStrArray = []string{
	"A9bTQjfYGBFK3JPRJqF2eh3JYL7cHocvy",
	"6mxBGnjGDCKgkVe7yfrmvMA7xE7qCv3vv",
	"6ncQ19Q2U4MamkCYzshhD8XFjfwAWFzTa",
	"Jz9ayEDt7dx9hDx45aXALujWmL9ZUuqe7",
}

var testHRP = constants.NetworkIDToHRP[networkID]

func TestBuildGenesis(t *testing.T) {
	ss := CreateStaticService()
	addrMap := map[string]string{}
	for _, addrStr := range addrStrArray {
		b, err := formatting.Decode(formatting.CB58, addrStr)
		if err != nil {
			t.Fatal(err)
		}
		addrMap[addrStr], err = formatting.FormatBech32(testHRP, b)
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
}
