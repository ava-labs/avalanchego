// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"

	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
)

func TestCalculateDynamicFee(t *testing.T) {
	type test struct {
		gas           uint64
		baseFee       *big.Int
		expectedErr   error
		expectedValue uint64
	}
	tests := []test{
		{
			gas:           1,
			baseFee:       new(big.Int).Set(atomic.X2CRate.ToBig()),
			expectedValue: 1,
		},
		{
			gas:           21000,
			baseFee:       big.NewInt(25 * utils.GWei),
			expectedValue: 525000,
		},
	}

	for _, test := range tests {
		cost, err := atomic.CalculateDynamicFee(test.gas, test.baseFee)
		require.ErrorIs(t, err, test.expectedErr)
		if test.expectedErr == nil {
			require.Equal(t, test.expectedValue, cost)
		}
	}
}

type atomicTxVerifyTest struct {
	ctx         *snow.Context
	generate    func() atomic.UnsignedAtomicTx
	rules       *extras.Rules
	expectedErr error
}

// executeTxVerifyTest tests
func executeTxVerifyTest(t *testing.T, test atomicTxVerifyTest) {
	require := require.New(t)
	atomicTx := test.generate()
	err := atomicTx.Verify(test.ctx, *test.rules)
	require.ErrorIs(err, test.expectedErr)
}

type atomicTxTest struct {
	// setup returns the atomic transaction for the test
	setup func(t *testing.T, vm *VM, sharedMemory *avalancheatomic.Memory) *atomic.Tx
	// define a string that should be contained in the error message if the tx fails verification
	// at some point. If the strings are empty, then the tx should pass verification at the
	// respective step.
	semanticVerifyErr, evmStateTransferErr, acceptErr error
	// checkState is called iff building and verifying a block containing the transaction is successful. Verifies
	// the state of the VM following the block's acceptance.
	checkState func(t *testing.T, vm *VM)

	// Whether or not the VM should be considered to still be bootstrapping
	bootstrapping bool
	// fork to use for the VM rules and genesis
	// If this is left empty, [upgradetest.NoUpgrades], will be used
	fork upgradetest.Fork
}

func TestEVMOutputCompare(t *testing.T) {
	type test struct {
		name     string
		a, b     atomic.EVMOutput
		expected int
	}

	tests := []test{
		{
			name: "address less",
			a: atomic.EVMOutput{
				Address: common.BytesToAddress([]byte{0x01}),
				AssetID: ids.ID{1},
			},
			b: atomic.EVMOutput{
				Address: common.BytesToAddress([]byte{0x02}),
				AssetID: ids.ID{0},
			},
			expected: -1,
		},
		{
			name: "address greater; assetIDs equal",
			a: atomic.EVMOutput{
				Address: common.BytesToAddress([]byte{0x02}),
				AssetID: ids.ID{},
			},
			b: atomic.EVMOutput{
				Address: common.BytesToAddress([]byte{0x01}),
				AssetID: ids.ID{},
			},
			expected: 1,
		},
		{
			name: "addresses equal; assetID less",
			a: atomic.EVMOutput{
				Address: common.BytesToAddress([]byte{0x01}),
				AssetID: ids.ID{0},
			},
			b: atomic.EVMOutput{
				Address: common.BytesToAddress([]byte{0x01}),
				AssetID: ids.ID{1},
			},
			expected: -1,
		},
		{
			name:     "equal",
			a:        atomic.EVMOutput{},
			b:        atomic.EVMOutput{},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			require.Equal(tt.expected, tt.a.Compare(tt.b))
			require.Equal(-tt.expected, tt.b.Compare(tt.a))
		})
	}
}

func TestEVMInputCompare(t *testing.T) {
	type test struct {
		name     string
		a, b     atomic.EVMInput
		expected int
	}

	tests := []test{
		{
			name: "address less",
			a: atomic.EVMInput{
				Address: common.BytesToAddress([]byte{0x01}),
				AssetID: ids.ID{1},
			},
			b: atomic.EVMInput{
				Address: common.BytesToAddress([]byte{0x02}),
				AssetID: ids.ID{0},
			},
			expected: -1,
		},
		{
			name: "address greater; assetIDs equal",
			a: atomic.EVMInput{
				Address: common.BytesToAddress([]byte{0x02}),
				AssetID: ids.ID{},
			},
			b: atomic.EVMInput{
				Address: common.BytesToAddress([]byte{0x01}),
				AssetID: ids.ID{},
			},
			expected: 1,
		},
		{
			name: "addresses equal; assetID less",
			a: atomic.EVMInput{
				Address: common.BytesToAddress([]byte{0x01}),
				AssetID: ids.ID{0},
			},
			b: atomic.EVMInput{
				Address: common.BytesToAddress([]byte{0x01}),
				AssetID: ids.ID{1},
			},
			expected: -1,
		},
		{
			name:     "equal",
			a:        atomic.EVMInput{},
			b:        atomic.EVMInput{},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			require.Equal(tt.expected, tt.a.Compare(tt.b))
			require.Equal(-tt.expected, tt.b.Compare(tt.a))
		})
	}
}
