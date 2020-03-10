// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"testing"

	"github.com/ava-labs/gecko/utils/formatting"
)

func TestBuildGenesis(t *testing.T) {
	ss := StaticService{}

	args := BuildGenesisArgs{GenesisData: map[string]AssetDefinition{
		"asset1": AssetDefinition{
			Name:         "myFixedCapAsset",
			Symbol:       "MFCA",
			Denomination: 8,
			InitialState: map[string][]interface{}{
				"fixedCap": []interface{}{
					Holder{
						Amount:  100000,
						Address: "A9bTQjfYGBFK3JPRJqF2eh3JYL7cHocvy",
					},
					Holder{
						Amount:  100000,
						Address: "6mxBGnjGDCKgkVe7yfrmvMA7xE7qCv3vv",
					},
					Holder{
						Amount:  50000,
						Address: "6ncQ19Q2U4MamkCYzshhD8XFjfwAWFzTa",
					},
					Holder{
						Amount:  50000,
						Address: "Jz9ayEDt7dx9hDx45aXALujWmL9ZUuqe7",
					},
				},
			},
		},
		"asset2": AssetDefinition{
			Name:   "myVarCapAsset",
			Symbol: "MVCA",
			InitialState: map[string][]interface{}{
				"variableCap": []interface{}{
					Owners{
						Threshold: 1,
						Minters: []string{
							"A9bTQjfYGBFK3JPRJqF2eh3JYL7cHocvy",
							"6mxBGnjGDCKgkVe7yfrmvMA7xE7qCv3vv",
						},
					},
					Owners{
						Threshold: 2,
						Minters: []string{
							"6ncQ19Q2U4MamkCYzshhD8XFjfwAWFzTa",
							"Jz9ayEDt7dx9hDx45aXALujWmL9ZUuqe7",
						},
					},
				},
			},
		},
		"asset3": AssetDefinition{
			Name: "myOtherVarCapAsset",
			InitialState: map[string][]interface{}{
				"variableCap": []interface{}{
					Owners{
						Threshold: 1,
						Minters: []string{
							"A9bTQjfYGBFK3JPRJqF2eh3JYL7cHocvy",
						},
					},
				},
			},
		},
	}}
	reply := BuildGenesisReply{}
	err := ss.BuildGenesis(nil, &args, &reply)
	if err != nil {
		t.Fatal(err)
	}

	expected := "1112YAVd1YsJ7JBDMQssciuuu9ySgebznWfmfT8JSw5vUKERtP4WGyitE7z38J8tExNmvK2kuwHsUP3erfcncXBWmJkdnd9nDJoj9tCiQHJmW1pstNQn3zXHdTnw6KJcG8Ro36ahknQkuy9ZSXgnZtpFhqUuwSd7mPj8vzZcqJMXLXorCBfvhwypTbZKogM9tUshyUfngfkg256ZsoU2ufMjhTG14PBBrgJkXD2F38uVSXWvYbubMVWDZbDnUzbyD3Azrs2Hydf8Paio6aNjwfwc1py61oXS5ehC55wiYbKpfzwE4px3bfYBu9yV6rvhivksB56vop9LEo8Pdo71tFAMkhR5toZmYcqRKyLXAnYqonUgmPsyxNwU22as8oscT5dj3Qxy1jsg6bEp6GwQepNqsWufGYx6Hiby2r5hyRZeYdk6xsXMPGBSBWUXhKX3ReTxBnjcrVE2Zc3G9eMvRho1tKzt7ppkutpcQemdDy2dxGryMqaFmPJaTaqcH2vB197KgVFbPgmHZY3ufUdfpVzzHax365pwCmzQD2PQh8hCqEP7rfV5e8uXKQiSynngoNDM4ak145zTpcUaX8htMGinfs45aKQvo5WHcD6ccRnHzc7dyXN8xJRnMznsuRN7D6k66DdbfDYhc2NbVUgXRAF4wSNTtsuZGxCGTEjQyYaoUoJowGXvnxmXAWHvLyMJswNizBeYgw1agRg5qB4AEKX96BFXhJq3MbsBRiypLR6nSuZgPFhCrLdBtstxEC2SPQNuUVWW9Qy68dDWQ3Fxx95n1pnjVru9wDJFoemg2imXRR"

	cb58 := formatting.CB58{}
	if err := cb58.FromString(expected); err != nil {
		t.Fatal(err)
	}
	expectedBytes := cb58.Bytes

	if result := reply.Bytes.String(); result != expected {
		t.Fatalf("Create genesis returned unexpected bytes:\n\n%s\n\n%s\n\n%s",
			reply.Bytes,
			formatting.DumpBytes{Bytes: reply.Bytes.Bytes},
			formatting.DumpBytes{Bytes: expectedBytes},
		)
	}
}
