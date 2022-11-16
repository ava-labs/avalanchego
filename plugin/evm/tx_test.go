// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"math/big"
	"strings"
	"testing"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/coreth/params"
	"github.com/stretchr/testify/require"
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
			baseFee:       new(big.Int).Set(x2cRate),
			expectedValue: 1,
		},
		{
			gas:           21000,
			baseFee:       big.NewInt(25 * params.GWei),
			expectedValue: 525000,
		},
	}

	for _, test := range tests {
		cost, err := calculateDynamicFee(test.gas, test.baseFee)
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
	generate    func(t *testing.T) UnsignedAtomicTx
	rules       params.Rules
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
	setup func(t *testing.T, vm *VM, sharedMemory *atomic.Memory) *Tx
	// define a string that should be contained in the error message if the tx fails verification
	// at some point. If the strings are empty, then the tx should pass verification at the
	// respective step.
	semanticVerifyErr, evmStateTransferErr, acceptErr string
	// checkState is called iff building and verifying a block containing the transaction is successful. Verifies
	// the state of the VM following the block's acceptance.
	checkState func(t *testing.T, vm *VM)

	// Whether or not the VM should be considered to still be bootstrapping
	bootstrapping bool
	// genesisJSON to use for the VM genesis (also defines the rule set that will be used in verification)
	// If this is left empty, [genesisJSONApricotPhase0], will be used
	genesisJSON string

	// passed directly into GenesisVM
	configJSON, upgradeJSON string
}

func executeTxTest(t *testing.T, test atomicTxTest) {
	genesisJSON := test.genesisJSON
	if len(genesisJSON) == 0 {
		genesisJSON = genesisJSONApricotPhase0
	}
	issuer, vm, _, sharedMemory, _ := GenesisVM(t, !test.bootstrapping, genesisJSON, test.configJSON, test.upgradeJSON)
	rules := vm.currentRules()

	tx := test.setup(t, vm, sharedMemory)

	var baseFee *big.Int
	// If ApricotPhase3 is active, use the initial base fee for the atomic transaction
	switch {
	case rules.IsApricotPhase3:
		baseFee = initialBaseFee
	}

	lastAcceptedBlock := vm.LastAcceptedBlockInternal().(*Block)
	if err := tx.UnsignedAtomicTx.SemanticVerify(vm, tx, lastAcceptedBlock, baseFee, rules); len(test.semanticVerifyErr) == 0 && err != nil {
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
	sdb, err := vm.blockChain.StateAt(lastAcceptedBlock.ethBlock.Root())
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.UnsignedAtomicTx.EVMStateTransfer(vm.ctx, sdb); len(test.evmStateTransferErr) == 0 && err != nil {
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
		vm.initBlockBuilding()
	}

	if err := vm.issueTx(tx, true /*=local*/); err != nil {
		t.Fatal(err)
	}
	<-issuer

	// If we've reached this point, we expect to be able to build and verify the block without any errors
	blk, err := vm.BuildBlock(context.Background())
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
		test.checkState(t, vm)
	}
}
