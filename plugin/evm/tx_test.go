// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"math/big"
	"strings"
	"testing"

	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/coreth/core/extstate"
	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	atomicvm "github.com/ava-labs/coreth/plugin/evm/atomic/vm"
	"github.com/ava-labs/coreth/utils"

	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
)

func TestCalculateDynamicFee(t *testing.T) {
	type test struct {
		gas           uint64
		baseFee       *big.Int
		expectedErr   error
		expectedValue uint64
	}
	var tests []test = []test{
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
		if test.expectedErr == nil {
			if err != nil {
				t.Fatalf("Unexpectedly failed to calculate dynamic fee: %s", err)
			}
			if cost != test.expectedValue {
				t.Fatalf("Expected value: %d, found: %d", test.expectedValue, cost)
			}
		} else {
			if err != test.expectedErr {
				t.Fatalf("Expected error: %s, found error: %s", test.expectedErr, err)
			}
		}
	}
}

type atomicTxVerifyTest struct {
	ctx         *snow.Context
	generate    func(t *testing.T) atomic.UnsignedAtomicTx
	rules       extras.Rules
	expectedErr string
}

// executeTxVerifyTest tests
func executeTxVerifyTest(t *testing.T, test atomicTxVerifyTest) {
	require := require.New(t)
	atomicTx := test.generate(t)
	err := atomicTx.Verify(test.ctx, test.rules)
	if len(test.expectedErr) == 0 {
		require.NoError(err)
	} else {
		require.ErrorContains(err, test.expectedErr, "expected tx verify to fail with specified error")
	}
}

type atomicTxTest struct {
	// setup returns the atomic transaction for the test
	setup func(t *testing.T, vm *atomicvm.VM, sharedMemory *avalancheatomic.Memory) *atomic.Tx
	// define a string that should be contained in the error message if the tx fails verification
	// at some point. If the strings are empty, then the tx should pass verification at the
	// respective step.
	semanticVerifyErr, evmStateTransferErr, acceptErr string
	// checkState is called iff building and verifying a block containing the transaction is successful. Verifies
	// the state of the VM following the block's acceptance.
	checkState func(t *testing.T, vm *atomicvm.VM)

	// Whether or not the VM should be considered to still be bootstrapping
	bootstrapping bool
	// fork to use for the VM rules and genesis
	// If this is left empty, [upgradetest.NoUpgrades], will be used
	fork upgradetest.Fork
}

func executeTxTest(t *testing.T, test atomicTxTest) {
	tvm := newVM(t, testVMConfig{
		isSyncing: test.bootstrapping,
		fork:      &test.fork,
	})
	rules := tvm.vm.currentRules()

	tx := test.setup(t, tvm.atomicVM, tvm.atomicMemory)

	var baseFee *big.Int
	// If ApricotPhase3 is active, use the initial base fee for the atomic transaction
	switch {
	case rules.IsApricotPhase3:
		baseFee = initialBaseFee
	}

	lastAcceptedBlock := tvm.vm.LastAcceptedExtendedBlock()
	backend := atomicvm.NewVerifierBackend(tvm.atomicVM, rules)
	if err := backend.SemanticVerify(tx, lastAcceptedBlock, baseFee); len(test.semanticVerifyErr) == 0 && err != nil {
		t.Fatalf("SemanticVerify failed unexpectedly due to: %s", err)
	} else if len(test.semanticVerifyErr) != 0 {
		if err == nil {
			t.Fatalf("SemanticVerify unexpectedly returned a nil error. Expected err: %s", test.semanticVerifyErr)
		}
		if !strings.Contains(err.Error(), test.semanticVerifyErr) {
			t.Fatalf("Expected SemanticVerify to fail due to %s, but failed with: %s", test.semanticVerifyErr, err)
		}
		// If SemanticVerify failed for the expected reason, return early
		return
	}

	// Retrieve dummy state to test that EVMStateTransfer works correctly
	statedb, err := tvm.vm.blockChain.StateAt(lastAcceptedBlock.GetEthBlock().Root())
	if err != nil {
		t.Fatal(err)
	}
	wrappedStateDB := extstate.New(statedb)
	if err := tx.UnsignedAtomicTx.EVMStateTransfer(tvm.vm.ctx, wrappedStateDB); len(test.evmStateTransferErr) == 0 && err != nil {
		t.Fatalf("EVMStateTransfer failed unexpectedly due to: %s", err)
	} else if len(test.evmStateTransferErr) != 0 {
		if err == nil {
			t.Fatalf("EVMStateTransfer unexpectedly returned a nil error. Expected err: %s", test.evmStateTransferErr)
		}
		if !strings.Contains(err.Error(), test.evmStateTransferErr) {
			t.Fatalf("Expected SemanticVerify to fail due to %s, but failed with: %s", test.evmStateTransferErr, err)
		}
		// If EVMStateTransfer failed for the expected reason, return early
		return
	}

	if test.bootstrapping {
		// If this test simulates processing txs during bootstrapping (where some verification is skipped),
		// initialize the block building goroutines normally initialized in SetState(snow.NormalOps).
		// This ensures that the VM can build a block correctly during the test.
		if err := tvm.vm.initBlockBuilding(); err != nil {
			t.Fatal(err)
		}
	}

	if err := tvm.atomicVM.AtomicMempool.AddLocalTx(tx); err != nil {
		t.Fatal(err)
	}

	require.Equal(t, commonEng.PendingTxs, tvm.WaitForEvent(context.Background()))

	// If we've reached this point, we expect to be able to build and verify the block without any errors
	blk, err := tvm.vm.BuildBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(context.Background()); len(test.acceptErr) == 0 && err != nil {
		t.Fatalf("Accept failed unexpectedly due to: %s", err)
	} else if len(test.acceptErr) != 0 {
		if err == nil {
			t.Fatalf("Accept unexpectedly returned a nil error. Expected err: %s", test.acceptErr)
		}
		if !strings.Contains(err.Error(), test.acceptErr) {
			t.Fatalf("Expected Accept to fail due to %s, but failed with: %s", test.acceptErr, err)
		}
		// If Accept failed for the expected reason, return early
		return
	}

	if test.checkState != nil {
		test.checkState(t, tvm.atomicVM)
	}
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
