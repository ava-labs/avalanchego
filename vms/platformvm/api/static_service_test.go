// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
)

const testNetworkID = 10 // To be used in tests

func TestDEBUGGING(t *testing.T) {
	// var (
	// 	exactDelegationFee0 = json.Uint32(1000000)
	// 	exactDelegationFee1 = json.Uint32(500000)
	// 	exactDelegationFee3 = json.Uint32(250000)
	// 	exactDelegationFee4 = json.Uint32(125000)
	// 	exactDelegationFee5 = json.Uint32(62500)
	// )

	platformArgs := &BuildGenesisArgs{
		AvaxAssetID: ids.ID{0xdb, 0xcf, 0x89, 0xf, 0x77, 0xf4, 0x9b, 0x96, 0x85, 0x76, 0x48, 0xb7, 0x2b, 0x77, 0xf9, 0xf8, 0x29, 0x37, 0xf2, 0x8a, 0x68, 0x70, 0x4a, 0xf0, 0x5d, 0xa0, 0xdc, 0x12, 0xba, 0x53, 0xf2, 0xdb},
		NetworkID:   0x3039,
		UTXOs: []UTXO{
			{
				Locktime: 0x0,
				Amount:   0x470de4df820000,
				Address:  "local18jma8ppw3nhx5r4ap8clazz0dps7rv5u00z96u",
				Message:  "0xb3d82b1367d362de99ab59a658165aff520cbd4d4d5779cb",
			},
			{
				Locktime: 0x61622d00,
				Amount:   0x2386f26fc10000,
				Address:  "local18jma8ppw3nhx5r4ap8clazz0dps7rv5u00z96u",
				Message:  "0xb3d82b1367d362de99ab59a658165aff520cbd4d4d5779cb",
			},
			{
				Locktime: 0x61622d00,
				Amount:   0x2386f26fc10000,
				Address:  "local1ur873jhz9qnaqv5qthk5sn3e8nj3e0kmggalnu",
				Message:  "0xb3d82b1367d362de99ab59a658165aff520cbd4d4d5779cb",
			},
		},
		// Validators: []PermissionlessValidator{
		// 	{
		// 		Staker: Staker{
		// 			TxID:        ids.ID{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
		// 			StartTime:   0x62f9c4c0,
		// 			EndTime:     0x64daf840,
		// 			Weight:      0x0,
		// 			NodeID:      ids.NodeID{0x47, 0x9f, 0x66, 0xc8, 0xbe, 0x89, 0x58, 0x30, 0x54, 0x7e, 0x70, 0xb4, 0xb2, 0x98, 0xca, 0xfd, 0x43, 0x3d, 0xba, 0x6e},
		// 			StakeAmount: nil,
		// 		},
		// 		RewardOwner: &Owner{
		// 			Locktime:  0,
		// 			Threshold: 1,
		// 			Addresses: []string{"local18jma8ppw3nhx5r4ap8clazz0dps7rv5u00z96u"},
		// 		},
		// 		ValidationRewardOwner:  nil,
		// 		DelegationRewardOwner:  nil,
		// 		PotentialReward:        nil,
		// 		AccruedDelegateeReward: nil,
		// 		DelegationFee:          0,
		// 		ExactDelegationFee:     &exactDelegationFee0,
		// 		Uptime:                 nil,
		// 		Connected:              false,
		// 		Staked: []UTXO{
		// 			{
		// 				Locktime: 0x61622d00,
		// 				Amount:   0x71afd498d0000,
		// 				Address:  "local1g65uqn6t77p656w64023nh8nd9updzmxyymev2",
		// 				Message:  "0xb3d82b1367d362de99ab59a658165aff520cbd4d4d5779cb",
		// 			},
		// 		},
		// 		Signer:          nil,
		// 		DelegatorCount:  nil,
		// 		DelegatorWeight: nil,
		// 		Delegators:      nil,
		// 	},
		// 	{
		// 		Staker: Staker{
		// 			TxID:        ids.ID{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
		// 			StartTime:   0x62f9c4c0,
		// 			EndTime:     0x64dae328,
		// 			Weight:      0x0,
		// 			NodeID:      ids.NodeID{0xde, 0x31, 0xb4, 0xd8, 0xb2, 0x29, 0x91, 0xd5, 0x1a, 0xa6, 0xaa, 0x1f, 0xc7, 0x33, 0xf2, 0x3a, 0x85, 0x1a, 0x8c, 0x94},
		// 			StakeAmount: nil,
		// 		},
		// 		RewardOwner: &Owner{
		// 			Locktime:  0,
		// 			Threshold: 1,
		// 			Addresses: []string{"local18jma8ppw3nhx5r4ap8clazz0dps7rv5u00z96u"},
		// 		},
		// 		ValidationRewardOwner:  nil,
		// 		DelegationRewardOwner:  nil,
		// 		PotentialReward:        nil,
		// 		AccruedDelegateeReward: nil,
		// 		DelegationFee:          0,
		// 		ExactDelegationFee:     &exactDelegationFee1,
		// 		Uptime:                 nil,
		// 		Connected:              false,
		// 		Staked: []UTXO{
		// 			{
		// 				Locktime: 0x61622d00,
		// 				Amount:   0x71afd498d0000,
		// 				Address:  "local1g65uqn6t77p656w64023nh8nd9updzmxyymev2",
		// 				Message:  "0xb3d82b1367d362de99ab59a658165aff520cbd4d4d5779cb",
		// 			},
		// 		},
		// 		Signer:          nil,
		// 		DelegatorCount:  nil,
		// 		DelegatorWeight: nil,
		// 		Delegators:      nil,
		// 	},
		// 	{
		// 		Staker: Staker{
		// 			TxID:        ids.ID{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
		// 			StartTime:   0x62f9c4c0,
		// 			EndTime:     0x64dace10,
		// 			Weight:      0x0,
		// 			NodeID:      ids.NodeID{0xe9, 0x9, 0x4f, 0x73, 0x69, 0x80, 0x2, 0xfd, 0x52, 0xc9, 0x8, 0x19, 0xb4, 0x57, 0xb9, 0xfb, 0xc8, 0x66, 0xab, 0x80},
		// 			StakeAmount: nil,
		// 		},
		// 		RewardOwner: &Owner{
		// 			Locktime:  0,
		// 			Threshold: 1,
		// 			Addresses: []string{"local18jma8ppw3nhx5r4ap8clazz0dps7rv5u00z96u"},
		// 		},

		// 		ValidationRewardOwner:  nil,
		// 		DelegationRewardOwner:  nil,
		// 		PotentialReward:        nil,
		// 		AccruedDelegateeReward: nil,
		// 		DelegationFee:          0,
		// 		ExactDelegationFee:     &exactDelegationFee3,
		// 		Uptime:                 nil,
		// 		Connected:              false,
		// 		Staked: []UTXO{
		// 			{
		// 				Locktime: 0x61622d00,
		// 				Amount:   0x71afd498d0000,
		// 				Address:  "local1g65uqn6t77p656w64023nh8nd9updzmxyymev2",
		// 				Message:  "0xb3d82b1367d362de99ab59a658165aff520cbd4d4d5779cb",
		// 			},
		// 		},
		// 		Signer:          nil,
		// 		DelegatorCount:  nil,
		// 		DelegatorWeight: nil,
		// 		Delegators:      nil,
		// 	},
		// 	{
		// 		Staker: Staker{
		// 			TxID:        ids.ID{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
		// 			StartTime:   0x62f9c4c0,
		// 			EndTime:     0x64dab8f8,
		// 			Weight:      0x0,
		// 			NodeID:      ids.NodeID{0xaa, 0x18, 0xd3, 0x99, 0x1c, 0xf6, 0x37, 0xaa, 0x6c, 0x16, 0x2f, 0x5e, 0x95, 0xcf, 0x16, 0x3f, 0x69, 0xcd, 0x82, 0x91},
		// 			StakeAmount: nil,
		// 		},
		// 		RewardOwner: &Owner{
		// 			Locktime:  0,
		// 			Threshold: 1,
		// 			Addresses: []string{"local18jma8ppw3nhx5r4ap8clazz0dps7rv5u00z96u"},
		// 		},
		// 		ValidationRewardOwner:  nil,
		// 		DelegationRewardOwner:  nil,
		// 		PotentialReward:        nil,
		// 		AccruedDelegateeReward: nil,
		// 		DelegationFee:          0,
		// 		ExactDelegationFee:     &exactDelegationFee4,
		// 		Uptime:                 nil,
		// 		Connected:              false,
		// 		Staked: []UTXO{
		// 			{
		// 				Locktime: 0x61622d00,
		// 				Amount:   0x71afd498d0000,
		// 				Address:  "local1g65uqn6t77p656w64023nh8nd9updzmxyymev2",
		// 				Message:  "0xb3d82b1367d362de99ab59a658165aff520cbd4d4d5779cb",
		// 			},
		// 		},
		// 		Signer:          nil,
		// 		DelegatorCount:  nil,
		// 		DelegatorWeight: nil,
		// 		Delegators:      nil,
		// 	},
		// 	{
		// 		Staker: Staker{
		// 			TxID:        ids.ID{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
		// 			StartTime:   0x62f9c4c0,
		// 			EndTime:     0x64daa3e0,
		// 			Weight:      0x0,
		// 			NodeID:      ids.NodeID{0xf2, 0x9b, 0xce, 0x5f, 0x34, 0xa7, 0x43, 0x1, 0xeb, 0xd, 0xe7, 0x16, 0xd5, 0x19, 0x4e, 0x4a, 0x4a, 0xea, 0x5d, 0x7a},
		// 			StakeAmount: nil,
		// 		},
		// 		RewardOwner: &Owner{
		// 			Locktime:  0,
		// 			Threshold: 1,
		// 			Addresses: []string{"local18jma8ppw3nhx5r4ap8clazz0dps7rv5u00z96u"},
		// 		},
		// 		ValidationRewardOwner:  nil,
		// 		DelegationRewardOwner:  nil,
		// 		PotentialReward:        nil,
		// 		AccruedDelegateeReward: nil,
		// 		DelegationFee:          0,
		// 		ExactDelegationFee:     &exactDelegationFee5,
		// 		Uptime:                 nil,
		// 		Connected:              false,
		// 		Staked: []UTXO{
		// 			{
		// 				Locktime: 0x61622d00,
		// 				Amount:   0x71afd498d0000,
		// 				Address:  "local1g65uqn6t77p656w64023nh8nd9updzmxyymev2",
		// 				Message:  "0xb3d82b1367d362de99ab59a658165aff520cbd4d4d5779cb",
		// 			},
		// 		},
		// 		Signer:          nil,
		// 		DelegatorCount:  nil,
		// 		DelegatorWeight: nil,
		// 		Delegators:      nil,
		// 	},
		// },
		// Chains: []Chain{
		// 	{
		// 		GenesisData: "0x000000000001000441564158000030390000000000000000000000000000000000000000000000000000000000000000000000000000000000000028b3d82b1367d362de99ab59a658165aff520cbd4db3d82b1367d362de99ab59a658165aff520cbd4d00094176616c616e6368650004415641580900000001000000000000000200000007002386f26fc1000000000000000000000000000100000001e0cfe8cae22827d032805ded484e393ce51cbedb000000070429d069189e0000000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c25c5e815",
		// 		VMID:        ids.ID{0x61, 0x76, 0x6d, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
		// 		FxIDs: []ids.ID{
		// 			{0x73, 0x65, 0x63, 0x70, 0x32, 0x35, 0x36, 0x6b, 0x31, 0x66, 0x78, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
		// 			{0x6e, 0x66, 0x74, 0x66, 0x78, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
		// 			{0x70, 0x72, 0x6f, 0x70, 0x65, 0x72, 0x74, 0x79, 0x66, 0x78, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
		// 		},
		// 		Name:     "X-Chain",
		// 		SubnetID: ids.ID{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
		// 	},
		// 	{
		// 		GenesisData: "0x7b22636f6e666967223a7b22636861696e4964223a34333131322c22686f6d657374656164426c6f636b223a302c2264616f466f726b426c6f636b223a302c2264616f466f726b537570706f7274223a747275652c22656970313530426c6f636b223a302c2265697031353048617368223a22307832303836373939616565626561653133356332343663363530323163383262346531356132633435313334303939336161636664323735313838363531346630222c22656970313535426c6f636b223a302c22656970313538426c6f636b223a302c2262797a616e7469756d426c6f636b223a302c22636f6e7374616e74696e6f706c65426c6f636b223a302c2270657465727362757267426c6f636b223a302c22697374616e62756c426c6f636b223a302c226d756972476c6163696572426c6f636b223a302c2261707269636f74506861736531426c6f636b54696d657374616d70223a302c2261707269636f74506861736532426c6f636b54696d657374616d70223a307d2c226e6f6e6365223a22307830222c2274696d657374616d70223a22307830222c22657874726144617461223a2230783030222c226761734c696d6974223a22307835663565313030222c22646966666963756c7479223a22307830222c226d697848617368223a22307830303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030222c22636f696e62617365223a22307830303030303030303030303030303030303030303030303030303030303030303030303030303030222c22616c6c6f63223a7b2238646239374337634563453234396332623938624443303232364363344332413537424635324643223a7b2262616c616e6365223a22307832393542453936453634303636393732303030303030227d7d2c226e756d626572223a22307830222c2267617355736564223a22307830222c22706172656e7448617368223a22307830303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030227d8d7c4cd3",
		// 		VMID:        ids.ID{0x65, 0x76, 0x6d, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
		// 		FxIDs:       nil,
		// 		Name:        "C-Chain",
		// 		SubnetID:    ids.ID{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
		// 	},
		// },
		Time:          0x62f9c4c0,
		InitialSupply: 0x4fefa17b7240000,
		Message:       "{{ fun_quote }}",
		Encoding:      0x0,
	}

	platformvmReply := BuildGenesisReply{}
	platformvmSS := StaticService{}
	err := platformvmSS.BuildGenesis(nil, platformArgs, &platformvmReply)
	require.NoError(t, err)

	genesisBytes, err := formatting.Decode(platformvmReply.Encoding, platformvmReply.Bytes)
	require.NoError(t, err)
	require.Equal(t, genesisBytes, []byte{0x0, 0x0, 0x0, 0x0, 0x0, 0x3, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0xdb, 0xcf, 0x89, 0xf, 0x77, 0xf4, 0x9b, 0x96, 0x85, 0x76, 0x48, 0xb7, 0x2b, 0x77, 0xf9, 0xf8, 0x29, 0x37, 0xf2, 0x8a, 0x68, 0x70, 0x4a, 0xf0, 0x5d, 0xa0, 0xdc, 0x12, 0xba, 0x53, 0xf2, 0xdb, 0x0, 0x0, 0x0, 0x7, 0x0, 0x47, 0xd, 0xe4, 0xdf, 0x82, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, 0x0, 0x0, 0x0, 0x1, 0x3c, 0xb7, 0xd3, 0x84, 0x2e, 0x8c, 0xee, 0x6a, 0xe, 0xbd, 0x9, 0xf1, 0xfe, 0x88, 0x4f, 0x68, 0x61, 0xe1, 0xb2, 0x9c, 0x0, 0x0, 0x0, 0x14, 0xb3, 0xd8, 0x2b, 0x13, 0x67, 0xd3, 0x62, 0xde, 0x99, 0xab, 0x59, 0xa6, 0x58, 0x16, 0x5a, 0xff, 0x52, 0xc, 0xbd, 0x4d, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, 0xdb, 0xcf, 0x89, 0xf, 0x77, 0xf4, 0x9b, 0x96, 0x85, 0x76, 0x48, 0xb7, 0x2b, 0x77, 0xf9, 0xf8, 0x29, 0x37, 0xf2, 0x8a, 0x68, 0x70, 0x4a, 0xf0, 0x5d, 0xa0, 0xdc, 0x12, 0xba, 0x53, 0xf2, 0xdb, 0x0, 0x0, 0x0, 0x7, 0x0, 0x23, 0x86, 0xf2, 0x6f, 0xc1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, 0x0, 0x0, 0x0, 0x1, 0x3c, 0xb7, 0xd3, 0x84, 0x2e, 0x8c, 0xee, 0x6a, 0xe, 0xbd, 0x9, 0xf1, 0xfe, 0x88, 0x4f, 0x68, 0x61, 0xe1, 0xb2, 0x9c, 0x0, 0x0, 0x0, 0x14, 0xb3, 0xd8, 0x2b, 0x13, 0x67, 0xd3, 0x62, 0xde, 0x99, 0xab, 0x59, 0xa6, 0x58, 0x16, 0x5a, 0xff, 0x52, 0xc, 0xbd, 0x4d, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x2, 0xdb, 0xcf, 0x89, 0xf, 0x77, 0xf4, 0x9b, 0x96, 0x85, 0x76, 0x48, 0xb7, 0x2b, 0x77, 0xf9, 0xf8, 0x29, 0x37, 0xf2, 0x8a, 0x68, 0x70, 0x4a, 0xf0, 0x5d, 0xa0, 0xdc, 0x12, 0xba, 0x53, 0xf2, 0xdb, 0x0, 0x0, 0x0, 0x7, 0x0, 0x23, 0x86, 0xf2, 0x6f, 0xc1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, 0x0, 0x0, 0x0, 0x1, 0xe0, 0xcf, 0xe8, 0xca, 0xe2, 0x28, 0x27, 0xd0, 0x32, 0x80, 0x5d, 0xed, 0x48, 0x4e, 0x39, 0x3c, 0xe5, 0x1c, 0xbe, 0xdb, 0x0, 0x0, 0x0, 0x14, 0xb3, 0xd8, 0x2b, 0x13, 0x67, 0xd3, 0x62, 0xde, 0x99, 0xab, 0x59, 0xa6, 0x58, 0x16, 0x5a, 0xff, 0x52, 0xc, 0xbd, 0x4d, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x62, 0xf9, 0xc4, 0xc0, 0x4, 0xfe, 0xfa, 0x17, 0xb7, 0x24, 0x0, 0x0, 0x0, 0xf, 0x7b, 0x7b, 0x20, 0x66, 0x75, 0x6e, 0x5f, 0x71, 0x75, 0x6f, 0x74, 0x65, 0x20, 0x7d, 0x7d})
}

func TestBuildGenesisInvalidUTXOBalance(t *testing.T) {
	require := require.New(t)
	nodeID := ids.NodeID{1, 2, 3}
	hrp := constants.NetworkIDToHRP[testNetworkID]
	addr, err := address.FormatBech32(hrp, nodeID.Bytes())
	require.NoError(err)

	utxo := UTXO{
		Address: addr,
		Amount:  0,
	}
	weight := json.Uint64(987654321)
	validator := PermissionlessValidator{
		Staker: Staker{
			EndTime: 15,
			Weight:  weight,
			NodeID:  nodeID,
		},
		RewardOwner: &Owner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []UTXO{{
			Amount:  weight,
			Address: addr,
		}},
	}

	args := BuildGenesisArgs{
		UTXOs: []UTXO{
			utxo,
		},
		Validators: []PermissionlessValidator{
			validator,
		},
		Time:     5,
		Encoding: formatting.Hex,
	}
	reply := BuildGenesisReply{}

	ss := StaticService{}
	require.Error(ss.BuildGenesis(nil, &args, &reply), "should have errored due to an invalid balance")
}

func TestBuildGenesisInvalidAmount(t *testing.T) {
	require := require.New(t)
	nodeID := ids.NodeID{1, 2, 3}
	hrp := constants.NetworkIDToHRP[testNetworkID]
	addr, err := address.FormatBech32(hrp, nodeID.Bytes())
	require.NoError(err)

	utxo := UTXO{
		Address: addr,
		Amount:  123456789,
	}
	weight := json.Uint64(0)
	validator := PermissionlessValidator{
		Staker: Staker{
			StartTime: 0,
			EndTime:   15,
			NodeID:    nodeID,
		},
		RewardOwner: &Owner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []UTXO{{
			Amount:  weight,
			Address: addr,
		}},
	}

	args := BuildGenesisArgs{
		UTXOs: []UTXO{
			utxo,
		},
		Validators: []PermissionlessValidator{
			validator,
		},
		Time:     5,
		Encoding: formatting.Hex,
	}
	reply := BuildGenesisReply{}

	ss := StaticService{}
	require.Error(ss.BuildGenesis(nil, &args, &reply), "should have errored due to an invalid amount")
}

func TestBuildGenesisInvalidEndtime(t *testing.T) {
	require := require.New(t)
	nodeID := ids.NodeID{1, 2, 3}
	hrp := constants.NetworkIDToHRP[testNetworkID]
	addr, err := address.FormatBech32(hrp, nodeID.Bytes())
	require.NoError(err)

	utxo := UTXO{
		Address: addr,
		Amount:  123456789,
	}

	weight := json.Uint64(987654321)
	validator := PermissionlessValidator{
		Staker: Staker{
			StartTime: 0,
			EndTime:   5,
			NodeID:    nodeID,
		},
		RewardOwner: &Owner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []UTXO{{
			Amount:  weight,
			Address: addr,
		}},
	}

	args := BuildGenesisArgs{
		UTXOs: []UTXO{
			utxo,
		},
		Validators: []PermissionlessValidator{
			validator,
		},
		Time:     5,
		Encoding: formatting.Hex,
	}
	reply := BuildGenesisReply{}

	ss := StaticService{}
	require.Error(ss.BuildGenesis(nil, &args, &reply), "should have errored due to an invalid end time")
}

func TestBuildGenesisReturnsSortedValidators(t *testing.T) {
	require := require.New(t)
	nodeID := ids.NodeID{1}
	hrp := constants.NetworkIDToHRP[testNetworkID]
	addr, err := address.FormatBech32(hrp, nodeID.Bytes())
	require.NoError(err)

	utxo := UTXO{
		Address: addr,
		Amount:  123456789,
	}

	weight := json.Uint64(987654321)
	validator1 := PermissionlessValidator{
		Staker: Staker{
			StartTime: 0,
			EndTime:   20,
			NodeID:    nodeID,
		},
		RewardOwner: &Owner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []UTXO{{
			Amount:  weight,
			Address: addr,
		}},
	}

	validator2 := PermissionlessValidator{
		Staker: Staker{
			StartTime: 3,
			EndTime:   15,
			NodeID:    nodeID,
		},
		RewardOwner: &Owner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []UTXO{{
			Amount:  weight,
			Address: addr,
		}},
	}

	validator3 := PermissionlessValidator{
		Staker: Staker{
			StartTime: 1,
			EndTime:   10,
			NodeID:    nodeID,
		},
		RewardOwner: &Owner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []UTXO{{
			Amount:  weight,
			Address: addr,
		}},
	}

	args := BuildGenesisArgs{
		AvaxAssetID: ids.ID{'d', 'u', 'm', 'm', 'y', ' ', 'I', 'D'},
		UTXOs: []UTXO{
			utxo,
		},
		Validators: []PermissionlessValidator{
			validator1,
			validator2,
			validator3,
		},
		Time:     5,
		Encoding: formatting.Hex,
	}
	reply := BuildGenesisReply{}

	ss := StaticService{}
	require.NoError(ss.BuildGenesis(nil, &args, &reply))

	genesisBytes, err := formatting.Decode(reply.Encoding, reply.Bytes)
	require.NoError(err)

	genesis, err := genesis.Parse(genesisBytes)
	require.NoError(err)

	validators := genesis.Validators
	require.Len(validators, 3)
}

func TestUTXOLess(t *testing.T) {
	var (
		smallerAddr = ids.ShortID{}
		largerAddr  = ids.ShortID{1}
	)
	smallerAddrStr, err := address.FormatBech32("avax", smallerAddr[:])
	if err != nil {
		panic(err)
	}
	largerAddrStr, err := address.FormatBech32("avax", largerAddr[:])
	if err != nil {
		panic(err)
	}
	type test struct {
		name     string
		utxo1    UTXO
		utxo2    UTXO
		expected bool
	}
	tests := []test{
		{
			name:     "both empty",
			utxo1:    UTXO{},
			utxo2:    UTXO{},
			expected: false,
		},
		{
			name:  "first locktime smaller",
			utxo1: UTXO{},
			utxo2: UTXO{
				Locktime: 1,
			},
			expected: true,
		},
		{
			name: "first locktime larger",
			utxo1: UTXO{
				Locktime: 1,
			},
			utxo2:    UTXO{},
			expected: false,
		},
		{
			name:  "first amount smaller",
			utxo1: UTXO{},
			utxo2: UTXO{
				Amount: 1,
			},
			expected: true,
		},
		{
			name: "first amount larger",
			utxo1: UTXO{
				Amount: 1,
			},
			utxo2:    UTXO{},
			expected: false,
		},
		{
			name: "first address smaller",
			utxo1: UTXO{
				Address: smallerAddrStr,
			},
			utxo2: UTXO{
				Address: largerAddrStr,
			},
			expected: true,
		},
		{
			name: "first address larger",
			utxo1: UTXO{
				Address: largerAddrStr,
			},
			utxo2: UTXO{
				Address: smallerAddrStr,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.utxo1.Less(tt.utxo2))
		})
	}
}
