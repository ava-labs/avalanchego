// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
)

// covered maps each P-Chain tx type that the fuzzer issues to the op kind that
// builds it. The key is the [platform.TxVisitor] method name for the type.
var covered = map[string]Kind{
	"BaseTx":                          Transfer,
	"CreateSubnetTx":                  CreateSubnet,
	"TransferSubnetOwnershipTx":       TransferSubnetOwnership,
	"AddPermissionlessValidatorTx":    AddValidator,
	"AddPermissionlessDelegatorTx":    AddDelegator,
	"ConvertSubnetToL1Tx":             ConvertSubnetToL1,
	"IncreaseL1ValidatorBalanceTx":    IncreaseL1Balance,
	"DisableL1ValidatorTx":            DisableL1Validator,
	"AddAutoRenewedValidatorTx":       AddAutoRenewedValidator,
	"SetAutoRenewedValidatorConfigTx": SetAutoRenewedValidatorConfig,
	"CreateChainTx":                   CreateChain,
	"AddSubnetValidatorTx":            AddSubnetValidator,
	"RemoveSubnetValidatorTx":         RemoveSubnetValidator,
	"ExportTx":                        Export,
	"ImportTx":                        ImportToP,
}

// excluded lists the tx types that the fuzzer does not issue, and why. A user
// cannot issue any of these: disabled types are rejected, and proposer-only
// types are built by a block proposer, not issued, so they need the Byzantine
// proposer path, not an op.
var excluded = map[string]string{
	"AddValidatorTx":               "disabled post-Durango",
	"AddDelegatorTx":               "disabled post-Durango",
	"TransformSubnetTx":            "disabled post-Etna",
	"AdvanceTimeTx":                "proposer-only, not user-issuable",
	"RewardValidatorTx":            "proposer-only, not user-issuable",
	"RewardAutoRenewedValidatorTx": "proposer-only, not user-issuable",
}

// todo lists supported tx types that should become ops but are not yet.
var todo = map[string]string{
	"RegisterL1ValidatorTx":  "needs a signed Warp message",
	"SetL1ValidatorWeightTx": "needs a signed Warp message",
}

// TestTxCoverage fails if a tx type in [platform.TxVisitor] is neither an op,
// nor excluded, nor a known todo. A new tx type then fails this test until it
// is classified, so the fuzzer's coverage cannot silently fall behind the
// P-Chain. It logs the todo types as a standing reminder.
func TestTxCoverage(t *testing.T) {
	visitor := reflect.TypeOf((*platform.TxVisitor)(nil)).Elem()
	for i := range visitor.NumMethod() {
		name := visitor.Method(i).Name
		_, isCovered := covered[name]
		_, isExcluded := excluded[name]
		_, isTodo := todo[name]
		require.True(t, isCovered || isExcluded || isTodo,
			"tx type %s is not an op, not excluded, and not a known todo: classify it in coverage_test.go", name)
		if reason, ok := todo[name]; ok {
			t.Logf("TODO: no op for %s (%s)", name, reason)
		}
	}

	// The covered kinds must be real ops.
	for name, kind := range covered {
		require.True(t, kind.valid(), "covered tx type %s maps to an invalid kind", name)
	}
}
