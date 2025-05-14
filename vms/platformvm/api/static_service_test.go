// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
)

func TestNewGenesisBytesInvalidUTXOBalance(t *testing.T) {
	require := require.New(t)
	nodeID := ids.BuildTestNodeID([]byte{1, 2, 3})
	addr, err := address.FormatBech32(constants.UnitTestHRP, nodeID.Bytes())
	require.NoError(err)

	utxo := Allocation{
		Address: addr,
		Amount:  0,
	}
	weight := uint64(987654321)
	validator := GenesisPermissionlessValidator{
		GenesisValidator: GenesisValidator{
			EndTime: 15,
			Weight:  weight,
			NodeID:  nodeID,
		},
		RewardOwner: &GenesisOwner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []Allocation{{
			Amount:  weight,
			Address: addr,
		}},
	}

	result, err := NewGenesisBytes(
		ids.Empty,
		constants.UnitTestID,
		[]Allocation{utxo},
		[]GenesisPermissionlessValidator{validator},
		nil,
		5,
		0,
		"",
	)
	require.ErrorIs(err, errUTXOHasNoValue)
	require.Empty(result)
}

func TestNewGenesisBytesInvalidStakeWeight(t *testing.T) {
	require := require.New(t)
	nodeID := ids.BuildTestNodeID([]byte{1, 2, 3})
	addr, err := address.FormatBech32(constants.UnitTestHRP, nodeID.Bytes())
	require.NoError(err)

	utxo := Allocation{
		Address: addr,
		Amount:  123456789,
	}

	validator := GenesisPermissionlessValidator{
		GenesisValidator: GenesisValidator{
			StartTime: 0,
			EndTime:   15,
			NodeID:    nodeID,
		},
		RewardOwner: &GenesisOwner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []Allocation{{
			Amount:  0,
			Address: addr,
		}},
	}

	result, err := NewGenesisBytes(
		ids.Empty,
		0,
		[]Allocation{utxo},
		[]GenesisPermissionlessValidator{validator},
		nil,
		5,
		0,
		"",
	)
	require.ErrorIs(err, errValidatorHasNoWeight)
	require.Empty(result)
}

func TestNewGenesisBytesInvalidEndtime(t *testing.T) {
	require := require.New(t)
	nodeID := ids.BuildTestNodeID([]byte{1, 2, 3})
	addr, err := address.FormatBech32(constants.UnitTestHRP, nodeID.Bytes())
	require.NoError(err)

	utxo := Allocation{
		Address: addr,
		Amount:  123456789,
	}

	weight := uint64(987654321)
	validator := GenesisPermissionlessValidator{
		GenesisValidator: GenesisValidator{
			StartTime: 0,
			EndTime:   5,
			NodeID:    nodeID,
		},
		RewardOwner: &GenesisOwner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []Allocation{{
			Amount:  weight,
			Address: addr,
		}},
	}

	result, err := NewGenesisBytes(
		ids.Empty,
		constants.UnitTestID,
		[]Allocation{utxo},
		[]GenesisPermissionlessValidator{validator},
		nil,
		5,
		0,
		"",
	)
	require.ErrorIs(err, errValidatorAlreadyExited)
	require.Empty(result)
}

func TestNewGenesisBytesReturnsSortedValidators(t *testing.T) {
	require := require.New(t)
	nodeID := ids.BuildTestNodeID([]byte{1})
	addr, err := address.FormatBech32(constants.UnitTestHRP, nodeID.Bytes())
	require.NoError(err)

	allocation := Allocation{
		Address: addr,
		Amount:  123456789,
	}

	weight := uint64(987654321)
	validator1 := GenesisPermissionlessValidator{
		GenesisValidator: GenesisValidator{
			StartTime: 0,
			EndTime:   20,
			NodeID:    nodeID,
		},
		RewardOwner: &GenesisOwner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []Allocation{{
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
		RewardOwner: &GenesisOwner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []Allocation{{
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
		RewardOwner: &GenesisOwner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []Allocation{{
			Amount:  weight,
			Address: addr,
		}},
	}

	avaxAssetID := ids.ID{'d', 'u', 'm', 'm', 'y', ' ', 'I', 'D'}
	genesisBytes, err := NewGenesisBytes(
		avaxAssetID,
		constants.UnitTestID,
		[]Allocation{allocation},
		[]GenesisPermissionlessValidator{
			validator1,
			validator2,
			validator3,
		},
		nil,
		5,
		0,
		"",
	)
	require.NoError(err)
	require.NotEmpty(genesisBytes)
	genesis, err := genesis.Parse(genesisBytes)
	require.NoError(err)

	validators := genesis.Validators
	require.Len(validators, 3)
}

func TestAllocationCompare(t *testing.T) {
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
		alloc1   Allocation
		alloc2   Allocation
		expected int
	}
	tests := []test{
		{
			name:     "both empty",
			alloc1:   Allocation{},
			alloc2:   Allocation{},
			expected: 0,
		},
		{
			name:   "locktime smaller",
			alloc1: Allocation{},
			alloc2: Allocation{
				Locktime: 1,
			},
			expected: -1,
		},
		{
			name:   "amount smaller",
			alloc1: Allocation{},
			alloc2: Allocation{
				Amount: 1,
			},
			expected: -1,
		},
		{
			name: "address smaller",
			alloc1: Allocation{
				Address: smallerAddrStr,
			},
			alloc2: Allocation{
				Address: largerAddrStr,
			},
			expected: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			require.Equal(tt.expected, tt.alloc1.Compare(tt.alloc2))
			require.Equal(-tt.expected, tt.alloc2.Compare(tt.alloc1))
		})
	}
}
