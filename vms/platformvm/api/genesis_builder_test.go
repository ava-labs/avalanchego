// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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

func TestBuildGenesisInvalidUTXOBalance(t *testing.T) {
	require := require.New(t)
	nodeID := ids.BuildTestNodeID([]byte{1, 2, 3})
	addr, err := address.FormatBech32(constants.UnitTestHRP, nodeID.Bytes())
	require.NoError(err)

	utxo := UTXO{
		Address: addr,
		Amount:  0,
	}
	weight := json.Uint64(987654321)
	validator := GenesisPermissionlessValidator{
		GenesisValidator: GenesisValidator{
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

	params := BuildGenesisParams{
		AvaxAssetID: ids.Empty,
		NetworkID:   0,
		UTXOs:       []UTXO{utxo},
		Validators:  []GenesisPermissionlessValidator{validator},
		Chains:      nil,
		Time:        5,
		Encoding:    formatting.Hex,
	}

	result, err := BuildGenesis(params)
	require.ErrorIs(err, errUTXOHasNoValue)
	require.Empty(result)
}

func TestBuildGenesisInvalidStakeWeight(t *testing.T) {
	require := require.New(t)
	nodeID := ids.BuildTestNodeID([]byte{1, 2, 3})
	addr, err := address.FormatBech32(constants.UnitTestHRP, nodeID.Bytes())
	require.NoError(err)

	utxo := UTXO{
		Address: addr,
		Amount:  123456789,
	}
	weight := json.Uint64(0)
	validator := GenesisPermissionlessValidator{
		GenesisValidator: GenesisValidator{
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

	params := BuildGenesisParams{
		AvaxAssetID: ids.Empty,
		NetworkID:   0,
		UTXOs:       []UTXO{utxo},
		Validators:  []GenesisPermissionlessValidator{validator},
		Chains:      nil,
		Time:        5,
		Encoding:    formatting.Hex,
	}

	result, err := BuildGenesis(params)
	require.ErrorIs(err, errValidatorHasNoWeight)
	require.Empty(result)
}

func TestBuildGenesisInvalidEndtime(t *testing.T) {
	require := require.New(t)
	nodeID := ids.BuildTestNodeID([]byte{1, 2, 3})
	addr, err := address.FormatBech32(constants.UnitTestHRP, nodeID.Bytes())
	require.NoError(err)

	utxo := UTXO{
		Address: addr,
		Amount:  123456789,
	}

	weight := json.Uint64(987654321)
	validator := GenesisPermissionlessValidator{
		GenesisValidator: GenesisValidator{
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

	params := BuildGenesisParams{
		AvaxAssetID: ids.Empty,
		NetworkID:   0,
		UTXOs:       []UTXO{utxo},
		Validators:  []GenesisPermissionlessValidator{validator},
		Chains:      nil,
		Time:        5,
		Encoding:    formatting.Hex,
	}

	result, err := BuildGenesis(params)
	require.ErrorIs(err, errValidatorAlreadyExited)
	require.Empty(result)
}

func TestBuildGenesisReturnsSortedValidators(t *testing.T) {
	require := require.New(t)
	nodeID := ids.BuildTestNodeID([]byte{1})
	addr, err := address.FormatBech32(constants.UnitTestHRP, nodeID.Bytes())
	require.NoError(err)

	utxo := UTXO{
		Address: addr,
		Amount:  123456789,
	}

	weight := json.Uint64(987654321)
	validator1 := GenesisPermissionlessValidator{
		GenesisValidator: GenesisValidator{
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

	validator2 := GenesisPermissionlessValidator{
		GenesisValidator: GenesisValidator{
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

	validator3 := GenesisPermissionlessValidator{
		GenesisValidator: GenesisValidator{
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

	avaxAssetID := ids.ID{'d', 'u', 'm', 'm', 'y', ' ', 'I', 'D'}
	params := BuildGenesisParams{
		AvaxAssetID: avaxAssetID,
		NetworkID:   0,
		UTXOs:       []UTXO{utxo},
		Validators: []GenesisPermissionlessValidator{
			validator1,
			validator2,
			validator3,
		},
		Chains:   nil,
		Time:     5,
		Encoding: formatting.Hex,
	}

	result, err := BuildGenesis(params)
	require.NoError(err)
	require.NotEmpty(result)

	genesisBytes, err := formatting.Decode(params.Encoding, result)
	require.NoError(err)

	genesis, err := genesis.Parse(genesisBytes)
	require.NoError(err)

	validators := genesis.Validators
	require.Len(validators, 3)
}

func TestUTXOCompare(t *testing.T) {
	var (
		smallerAddr = ids.ShortID{}
		largerAddr  = ids.ShortID{1}
	)
	smallerAddrStr, err := address.FormatBech32("avax", smallerAddr[:])
	require.NoError(t, err)
	largerAddrStr, err := address.FormatBech32("avax", largerAddr[:])
	require.NoError(t, err)

	type test struct {
		name     string
		utxo1    UTXO
		utxo2    UTXO
		expected int
	}
	tests := []test{
		{
			name:     "both empty",
			utxo1:    UTXO{},
			utxo2:    UTXO{},
			expected: 0,
		},
		{
			name:  "locktime smaller",
			utxo1: UTXO{},
			utxo2: UTXO{
				Locktime: 1,
			},
			expected: -1,
		},
		{
			name:  "amount smaller",
			utxo1: UTXO{},
			utxo2: UTXO{
				Amount: 1,
			},
			expected: -1,
		},
		{
			name: "address smaller",
			utxo1: UTXO{
				Address: smallerAddrStr,
			},
			utxo2: UTXO{
				Address: largerAddrStr,
			},
			expected: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			require.Equal(tt.expected, tt.utxo1.Compare(tt.utxo2))
			require.Equal(-tt.expected, tt.utxo2.Compare(tt.utxo1))
		})
	}
}
