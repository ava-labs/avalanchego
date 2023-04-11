// (c) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package feemanager

import (
	"math/big"
	"testing"

	"github.com/ava-labs/subnet-evm/commontype"
	"github.com/ava-labs/subnet-evm/core/state"
	"github.com/ava-labs/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/subnet-evm/precompile/contract"
	"github.com/ava-labs/subnet-evm/precompile/testutils"
	"github.com/ava-labs/subnet-evm/vmerrs"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

var (
	testFeeConfig = commontype.FeeConfig{
		GasLimit:        big.NewInt(8_000_000),
		TargetBlockRate: 2, // in seconds

		MinBaseFee:               big.NewInt(25_000_000_000),
		TargetGas:                big.NewInt(15_000_000),
		BaseFeeChangeDenominator: big.NewInt(36),

		MinBlockGasCost:  big.NewInt(0),
		MaxBlockGasCost:  big.NewInt(1_000_000),
		BlockGasCostStep: big.NewInt(200_000),
	}
	testBlockNumber = big.NewInt(7)
	tests           = map[string]testutils.PrecompileTest{
		"set config from no role fails": {
			Caller:     allowlist.TestNoRoleAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackSetFeeConfig(testFeeConfig)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: SetFeeConfigGasCost,
			ReadOnly:    false,
			ExpectedErr: ErrCannotChangeFee.Error(),
		},
		"set config from enabled address": {
			Caller:     allowlist.TestEnabledAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackSetFeeConfig(testFeeConfig)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: SetFeeConfigGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state contract.StateDB) {
				feeConfig := GetStoredFeeConfig(state)
				require.Equal(t, testFeeConfig, feeConfig)
			},
		},
		"set invalid config from enabled address": {
			Caller:     allowlist.TestEnabledAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				feeConfig := testFeeConfig
				feeConfig.MinBlockGasCost = new(big.Int).Mul(feeConfig.MaxBlockGasCost, common.Big2)
				input, err := PackSetFeeConfig(feeConfig)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: SetFeeConfigGasCost,
			ReadOnly:    false,
			Config: &Config{
				InitialFeeConfig: &testFeeConfig,
			},
			ExpectedErr: "cannot be greater than maxBlockGasCost",
			AfterHook: func(t testing.TB, state contract.StateDB) {
				feeConfig := GetStoredFeeConfig(state)
				require.Equal(t, testFeeConfig, feeConfig)
			},
		},
		"set config from admin address": {
			Caller:     allowlist.TestAdminAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackSetFeeConfig(testFeeConfig)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: SetFeeConfigGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			BlockNumber: testBlockNumber.Int64(),
			AfterHook: func(t testing.TB, state contract.StateDB) {
				feeConfig := GetStoredFeeConfig(state)
				require.Equal(t, testFeeConfig, feeConfig)
				lastChangedAt := GetFeeConfigLastChangedAt(state)
				require.EqualValues(t, testBlockNumber, lastChangedAt)
			},
		},
		"get fee config from non-enabled address": {
			Caller: allowlist.TestNoRoleAddr,
			BeforeHook: func(t testing.TB, state contract.StateDB) {
				allowlist.SetDefaultRoles(Module.Address)(t, state)
				err := StoreFeeConfig(state, testFeeConfig, contract.NewMockBlockContext(big.NewInt(6), 0))
				require.NoError(t, err)
			},
			Input:       PackGetFeeConfigInput(),
			SuppliedGas: GetFeeConfigGasCost,
			ReadOnly:    true,
			ExpectedRes: func() []byte {
				res, err := PackFeeConfig(testFeeConfig)
				if err != nil {
					panic(err)
				}
				return res
			}(),
			AfterHook: func(t testing.TB, state contract.StateDB) {
				feeConfig := GetStoredFeeConfig(state)
				lastChangedAt := GetFeeConfigLastChangedAt(state)
				require.Equal(t, testFeeConfig, feeConfig)
				require.EqualValues(t, big.NewInt(6), lastChangedAt)
			},
		},
		"get initial fee config": {
			Caller:      allowlist.TestNoRoleAddr,
			BeforeHook:  allowlist.SetDefaultRoles(Module.Address),
			Input:       PackGetFeeConfigInput(),
			SuppliedGas: GetFeeConfigGasCost,
			Config: &Config{
				InitialFeeConfig: &testFeeConfig,
			},
			ReadOnly: true,
			ExpectedRes: func() []byte {
				res, err := PackFeeConfig(testFeeConfig)
				if err != nil {
					panic(err)
				}
				return res
			}(),
			BlockNumber: testBlockNumber.Int64(),
			AfterHook: func(t testing.TB, state contract.StateDB) {
				feeConfig := GetStoredFeeConfig(state)
				lastChangedAt := GetFeeConfigLastChangedAt(state)
				require.Equal(t, testFeeConfig, feeConfig)
				require.EqualValues(t, testBlockNumber, lastChangedAt)
			},
		},
		"get last changed at from non-enabled address": {
			Caller: allowlist.TestNoRoleAddr,
			BeforeHook: func(t testing.TB, state contract.StateDB) {
				allowlist.SetDefaultRoles(Module.Address)(t, state)
				err := StoreFeeConfig(state, testFeeConfig, contract.NewMockBlockContext(testBlockNumber, 0))
				require.NoError(t, err)
			},
			Input:       PackGetLastChangedAtInput(),
			SuppliedGas: GetLastChangedAtGasCost,
			ReadOnly:    true,
			ExpectedRes: common.BigToHash(testBlockNumber).Bytes(),
			AfterHook: func(t testing.TB, state contract.StateDB) {
				feeConfig := GetStoredFeeConfig(state)
				lastChangedAt := GetFeeConfigLastChangedAt(state)
				require.Equal(t, testFeeConfig, feeConfig)
				require.Equal(t, testBlockNumber, lastChangedAt)
			},
		},
		"readOnly setFeeConfig with noRole fails": {
			Caller:     allowlist.TestNoRoleAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackSetFeeConfig(testFeeConfig)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: SetFeeConfigGasCost,
			ReadOnly:    true,
			ExpectedErr: vmerrs.ErrWriteProtection.Error(),
		},
		"readOnly setFeeConfig with allow role fails": {
			Caller:     allowlist.TestEnabledAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackSetFeeConfig(testFeeConfig)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: SetFeeConfigGasCost,
			ReadOnly:    true,
			ExpectedErr: vmerrs.ErrWriteProtection.Error(),
		},
		"readOnly setFeeConfig with admin role fails": {
			Caller:     allowlist.TestAdminAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackSetFeeConfig(testFeeConfig)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: SetFeeConfigGasCost,
			ReadOnly:    true,
			ExpectedErr: vmerrs.ErrWriteProtection.Error(),
		},
		"insufficient gas setFeeConfig from admin": {
			Caller:     allowlist.TestAdminAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackSetFeeConfig(testFeeConfig)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: SetFeeConfigGasCost - 1,
			ReadOnly:    false,
			ExpectedErr: vmerrs.ErrOutOfGas.Error(),
		},
	}
)

func TestFeeManager(t *testing.T) {
	allowlist.RunPrecompileWithAllowListTests(t, Module, state.NewTestStateDB, tests)
}

func BenchmarkFeeManager(b *testing.B) {
	allowlist.BenchPrecompileWithAllowList(b, Module, state.NewTestStateDB, tests)
}
