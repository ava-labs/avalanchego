// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func createTestGenesis(t *testing.T) *Genesis {
	require := require.New(t)

	nodeID := ids.BuildTestNodeID([]byte{1})
	addr, err := address.FormatBech32(constants.UnitTestHRP, nodeID.Bytes())
	require.NoError(err)

	validator := PermissionlessValidator{
		Validator: Validator{
			StartTime: 0,
			EndTime:   20,
			NodeID:    nodeID,
		},
		RewardOwner: &Owner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []Allocation{{
			Amount:  987654321,
			Address: addr,
		}},
	}

	genesis, err := New(
		ids.ID{'d', 'u', 'm', 'm', 'y', ' ', 'I', 'D'},
		constants.UnitTestID,
		[]Allocation{
			{
				Address: addr,
				Amount:  123456789,
			},
		},
		[]PermissionlessValidator{validator},
		nil,
		5,
		0,
		"Test Genesis",
	)
	require.NoError(err)

	return genesis
}

func TestNewInvalidUTXOBalance(t *testing.T) {
	require := require.New(t)
	nodeID := ids.BuildTestNodeID([]byte{1, 2, 3})
	addr, err := address.FormatBech32(constants.UnitTestHRP, nodeID.Bytes())
	require.NoError(err)

	utxo := Allocation{
		Address: addr,
		Amount:  0,
	}
	weight := uint64(987654321)
	validator := PermissionlessValidator{
		Validator: Validator{
			EndTime: 15,
			Weight:  weight,
			NodeID:  nodeID,
		},
		RewardOwner: &Owner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []Allocation{{
			Amount:  weight,
			Address: addr,
		}},
	}

	genesis, err := New(
		ids.Empty,
		constants.UnitTestID,
		[]Allocation{utxo},
		[]PermissionlessValidator{validator},
		nil,
		5,
		0,
		"",
	)
	require.ErrorIs(err, errUTXOHasNoValue)
	require.Nil(genesis)
}

func TestNewInvalidStakeWeight(t *testing.T) {
	require := require.New(t)
	nodeID := ids.BuildTestNodeID([]byte{1, 2, 3})
	addr, err := address.FormatBech32(constants.UnitTestHRP, nodeID.Bytes())
	require.NoError(err)

	utxo := Allocation{
		Address: addr,
		Amount:  123456789,
	}

	validator := PermissionlessValidator{
		Validator: Validator{
			StartTime: 0,
			EndTime:   15,
			NodeID:    nodeID,
		},
		RewardOwner: &Owner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []Allocation{{
			Amount:  0,
			Address: addr,
		}},
	}

	genesis, err := New(
		ids.Empty,
		0,
		[]Allocation{utxo},
		[]PermissionlessValidator{validator},
		nil,
		5,
		0,
		"",
	)
	require.ErrorIs(err, errValidatorHasNoWeight)
	require.Nil(genesis)
}

func TestNewInvalidEndtime(t *testing.T) {
	require := require.New(t)
	nodeID := ids.BuildTestNodeID([]byte{1, 2, 3})
	addr, err := address.FormatBech32(constants.UnitTestHRP, nodeID.Bytes())
	require.NoError(err)

	utxo := Allocation{
		Address: addr,
		Amount:  123456789,
	}

	weight := uint64(987654321)
	validator := PermissionlessValidator{
		Validator: Validator{
			StartTime: 0,
			EndTime:   5,
			NodeID:    nodeID,
		},
		RewardOwner: &Owner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []Allocation{{
			Amount:  weight,
			Address: addr,
		}},
	}

	genesis, err := New(
		ids.Empty,
		constants.UnitTestID,
		[]Allocation{utxo},
		[]PermissionlessValidator{validator},
		nil,
		5,
		0,
		"",
	)
	require.ErrorIs(err, errValidatorAlreadyExited)
	require.Nil(genesis)
}

func TestGenesisBytes(t *testing.T) {
	require := require.New(t)
	genesis := createTestGenesis(t)
	bytes, err := genesis.Bytes()
	require.NoError(err)
	require.NotEmpty(bytes)
}

func TestGenesis(t *testing.T) {
	require := require.New(t)
	genesis := createTestGenesis(t)

	avaxAssetID := ids.ID{'d', 'u', 'm', 'm', 'y', ' ', 'I', 'D'}
	nodeID := ids.BuildTestNodeID([]byte{1})
	require.Equal("Test Genesis", genesis.Message)

	// Validate allocations
	require.Len(genesis.UTXOs, 1)
	utxo := genesis.UTXOs[0]
	require.Equal(avaxAssetID, utxo.Asset.ID)
	output, ok := utxo.Out.(*secp256k1fx.TransferOutput)
	require.True(ok)
	require.Equal(uint64(123456789), output.Amt)
	require.Len(output.OutputOwners.Addrs, 1)

	// Validate validator
	require.Len(genesis.Validators, 1)
	validator := genesis.Validators[0]
	txValidator, ok := validator.Unsigned.(*txs.AddValidatorTx)
	require.True(ok)
	require.Equal(nodeID, txValidator.Validator.NodeID)
	require.Equal(uint64(20), txValidator.Validator.End)
	require.Len(txValidator.StakeOuts, 1)
	stakeOut := txValidator.StakeOuts[0]
	stakeOutput, ok := stakeOut.Out.(*secp256k1fx.TransferOutput)
	require.True(ok)
	require.Equal(uint64(987654321), stakeOutput.Amt)

	require.Empty(genesis.Chains)
	require.Equal(uint64(5), genesis.Timestamp)
	require.Equal(uint64(0), genesis.InitialSupply)
}

func TestNewReturnsSortedValidators(t *testing.T) {
	require := require.New(t)
	nodeID := ids.BuildTestNodeID([]byte{1})
	addr, err := address.FormatBech32(constants.UnitTestHRP, nodeID.Bytes())
	require.NoError(err)

	allocation := Allocation{
		Address: addr,
		Amount:  123456789,
	}

	weight := uint64(987654321)
	validator1 := PermissionlessValidator{
		Validator: Validator{
			StartTime: 0,
			EndTime:   20,
			NodeID:    nodeID,
		},
		RewardOwner: &Owner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []Allocation{{
			Amount:  weight,
			Address: addr,
		}},
	}

	validator2 := PermissionlessValidator{
		Validator: Validator{
			StartTime: 3,
			EndTime:   15,
			NodeID:    nodeID,
		},
		RewardOwner: &Owner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []Allocation{{
			Amount:  weight,
			Address: addr,
		}},
	}

	validator3 := PermissionlessValidator{
		Validator: Validator{
			StartTime: 1,
			EndTime:   10,
			NodeID:    nodeID,
		},
		RewardOwner: &Owner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []Allocation{{
			Amount:  weight,
			Address: addr,
		}},
	}

	avaxAssetID := ids.ID{'d', 'u', 'm', 'm', 'y', ' ', 'I', 'D'}
	genesis, err := New(
		avaxAssetID,
		constants.UnitTestID,
		[]Allocation{allocation},
		[]PermissionlessValidator{
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
	genesisBytes, err := genesis.Bytes()
	require.NoError(err)
	require.NotEmpty(genesisBytes)
	require.Len(genesis.Validators, 3)
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
