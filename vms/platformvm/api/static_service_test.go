// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
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
