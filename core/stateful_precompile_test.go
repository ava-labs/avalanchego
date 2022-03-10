// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"strings"
	"testing"

	"github.com/ava-labs/subnet-evm/core/rawdb"
	"github.com/ava-labs/subnet-evm/core/state"
	"github.com/ava-labs/subnet-evm/core/vm"
	"github.com/ava-labs/subnet-evm/precompile"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/stretchr/testify/assert"
)

type mockAccessibleState struct {
	state *state.StateDB
}

func (m *mockAccessibleState) GetStateDB() precompile.StateDB { return m.state }

// This test is added within the core package so that it can import all of the required code
// without creating any import cycles
func TestContractDeployerAllowListRun(t *testing.T) {
	type test struct {
		caller         common.Address
		precompileAddr common.Address
		input          func() []byte
		suppliedGas    uint64
		readOnly       bool

		setupState  func(state *state.StateDB)
		expectedRes []byte
		expectedErr string

		assertState func(t *testing.T, state *state.StateDB)
	}

	adminAddr := common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC")
	noRoleAddr := common.HexToAddress("0xF60C45c607D0f41687c94C314d300f483661E13a")

	for name, test := range map[string]test{
		"set admin": {
			caller:         adminAddr,
			precompileAddr: precompile.ContractDeployerAllowListAddress,
			input: func() []byte {
				input, err := precompile.PackModifyAllowList(noRoleAddr, precompile.AllowListAdmin)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.ModifyAllowListGasCost,
			readOnly:    false,
			setupState: func(state *state.StateDB) {
				precompile.SetContractDeployerAllowListStatus(state, adminAddr, precompile.AllowListAdmin)
			},
			expectedRes: []byte{},
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetContractDeployerAllowListStatus(state, adminAddr)
				assert.Equal(t, precompile.AllowListAdmin, res)

				res = precompile.GetContractDeployerAllowListStatus(state, noRoleAddr)
				assert.Equal(t, precompile.AllowListAdmin, res)
			},
		},
		"set deployer": {
			caller:         adminAddr,
			precompileAddr: precompile.ContractDeployerAllowListAddress,
			input: func() []byte {
				input, err := precompile.PackModifyAllowList(noRoleAddr, precompile.AllowListEnabled)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.ModifyAllowListGasCost,
			readOnly:    false,
			setupState: func(state *state.StateDB) {
				precompile.SetContractDeployerAllowListStatus(state, adminAddr, precompile.AllowListAdmin)
			},
			expectedRes: []byte{},
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetContractDeployerAllowListStatus(state, adminAddr)
				assert.Equal(t, precompile.AllowListAdmin, res)

				res = precompile.GetContractDeployerAllowListStatus(state, noRoleAddr)
				assert.Equal(t, precompile.AllowListEnabled, res)
			},
		},
		"set no role": {
			caller:         adminAddr,
			precompileAddr: precompile.ContractDeployerAllowListAddress,
			input: func() []byte {
				input, err := precompile.PackModifyAllowList(adminAddr, precompile.AllowListNoRole)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.ModifyAllowListGasCost,
			readOnly:    false,
			setupState: func(state *state.StateDB) {
				precompile.SetContractDeployerAllowListStatus(state, adminAddr, precompile.AllowListAdmin)
			},
			expectedRes: []byte{},
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetContractDeployerAllowListStatus(state, adminAddr)
				assert.Equal(t, precompile.AllowListNoRole, res)
			},
		},
		"set no role from non-admin": {
			caller:         noRoleAddr,
			precompileAddr: precompile.ContractDeployerAllowListAddress,
			input: func() []byte {
				input, err := precompile.PackModifyAllowList(adminAddr, precompile.AllowListNoRole)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.ModifyAllowListGasCost,
			readOnly:    false,
			setupState:  func(state *state.StateDB) {},
			expectedErr: precompile.ErrCannotModifyAllowList.Error(),
		},
		"set deployer from non-admin": {
			caller:         noRoleAddr,
			precompileAddr: precompile.ContractDeployerAllowListAddress,
			input: func() []byte {
				input, err := precompile.PackModifyAllowList(adminAddr, precompile.AllowListEnabled)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.ModifyAllowListGasCost,
			readOnly:    false,
			setupState:  func(state *state.StateDB) {},
			expectedErr: precompile.ErrCannotModifyAllowList.Error(),
		},
		"set admin from non-admin": {
			caller:         noRoleAddr,
			precompileAddr: precompile.ContractDeployerAllowListAddress,
			input: func() []byte {
				input, err := precompile.PackModifyAllowList(adminAddr, precompile.AllowListAdmin)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.ModifyAllowListGasCost,
			readOnly:    false,
			setupState:  func(state *state.StateDB) {},
			expectedErr: precompile.ErrCannotModifyAllowList.Error(),
		},
		"set no role with readOnly enabled": {
			caller:         adminAddr,
			precompileAddr: precompile.ContractDeployerAllowListAddress,
			input: func() []byte {
				input, err := precompile.PackModifyAllowList(adminAddr, precompile.AllowListNoRole)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.ModifyAllowListGasCost,
			readOnly:    true,
			setupState: func(state *state.StateDB) {
				precompile.SetContractDeployerAllowListStatus(state, adminAddr, precompile.AllowListAdmin)
			},
			expectedErr: precompile.ErrWriteProtection.Error(),
		},
		"set no role insufficient gas": {
			caller:         adminAddr,
			precompileAddr: precompile.ContractDeployerAllowListAddress,
			input: func() []byte {
				input, err := precompile.PackModifyAllowList(adminAddr, precompile.AllowListNoRole)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.ModifyAllowListGasCost - 1,
			readOnly:    false,
			setupState: func(state *state.StateDB) {
				precompile.SetContractDeployerAllowListStatus(state, adminAddr, precompile.AllowListAdmin)
			},
			expectedErr: vm.ErrOutOfGas.Error(),
		},
		"read allow list no role": {
			caller:         adminAddr,
			precompileAddr: precompile.ContractDeployerAllowListAddress,
			input: func() []byte {
				return precompile.PackReadAllowList(noRoleAddr)
			},
			suppliedGas: precompile.ReadAllowListGasCost,
			readOnly:    false,
			setupState:  func(state *state.StateDB) {},
			expectedRes: common.Hash(precompile.AllowListNoRole).Bytes(),
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetContractDeployerAllowListStatus(state, adminAddr)
				assert.Equal(t, precompile.AllowListNoRole, res)
			},
		},
		"read allow list admin role": {
			caller:         adminAddr,
			precompileAddr: precompile.ContractDeployerAllowListAddress,
			input: func() []byte {
				return precompile.PackReadAllowList(noRoleAddr)
			},
			suppliedGas: precompile.ReadAllowListGasCost,
			readOnly:    false,
			setupState: func(state *state.StateDB) {
				precompile.SetContractDeployerAllowListStatus(state, adminAddr, precompile.AllowListAdmin)
			},
			expectedRes: common.Hash(precompile.AllowListNoRole).Bytes(),
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetContractDeployerAllowListStatus(state, adminAddr)
				assert.Equal(t, precompile.AllowListAdmin, res)
			},
		},
		"read allow list with readOnly enabled": {
			caller:         adminAddr,
			precompileAddr: precompile.ContractDeployerAllowListAddress,
			input: func() []byte {
				return precompile.PackReadAllowList(noRoleAddr)
			},
			suppliedGas: precompile.ReadAllowListGasCost,
			readOnly:    true,
			setupState: func(state *state.StateDB) {
				precompile.SetContractDeployerAllowListStatus(state, adminAddr, precompile.AllowListAdmin)
			},
			expectedRes: common.Hash(precompile.AllowListNoRole).Bytes(),
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetContractDeployerAllowListStatus(state, adminAddr)
				assert.Equal(t, precompile.AllowListAdmin, res)
			},
		},
		"read allow list out of gas": {
			caller:         adminAddr,
			precompileAddr: precompile.ContractDeployerAllowListAddress,
			input: func() []byte {
				return precompile.PackReadAllowList(noRoleAddr)
			},
			suppliedGas: precompile.ReadAllowListGasCost - 1,
			readOnly:    true,
			setupState: func(state *state.StateDB) {
				precompile.SetContractDeployerAllowListStatus(state, adminAddr, precompile.AllowListAdmin)
			},
			expectedErr: vm.ErrOutOfGas.Error(),
		},
	} {
		t.Run(name, func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()
			state, err := state.New(common.Hash{}, state.NewDatabase(db), nil)
			if err != nil {
				t.Fatal(err)
			}
			test.setupState(state)

			ret, remainingGas, err := precompile.ContractDeployerAllowListPrecompile.Run(&mockAccessibleState{state: state}, test.caller, test.precompileAddr, test.input(), test.suppliedGas, test.readOnly)
			if len(test.expectedErr) != 0 {
				if err == nil {
					assert.Failf(t, "run expectedly passed without error", "expected error %q", test.expectedErr)
				} else {
					assert.True(t, strings.Contains(err.Error(), test.expectedErr), "expected error (%s) to contain substring (%s)", err, test.expectedErr)
				}
				return
			}

			if err != nil {
				t.Fatal(err)
			}

			assert.Equal(t, uint64(0), remainingGas)
			assert.Equal(t, test.expectedRes, ret)

			test.assertState(t, state)
		})
	}
}

func TestContractNativeMinterRun(t *testing.T) {
	type test struct {
		caller         common.Address
		precompileAddr common.Address
		input          func() []byte
		suppliedGas    uint64
		readOnly       bool

		setupState  func(state *state.StateDB)
		expectedRes []byte
		expectedErr string

		assertState func(t *testing.T, state *state.StateDB)
	}

	adminAddr := common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC")
	noRoleAddr := common.HexToAddress("0xF60C45c607D0f41687c94C314d300f483661E13a")

	for name, test := range map[string]test{
		"noRole tries minting": {
			caller:         noRoleAddr,
			precompileAddr: precompile.ContractNativeMinterAddress,
			input: func() []byte {
				input, err := precompile.PackMintInput(noRoleAddr, common.Big1)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.MintGasCost,
			readOnly:    false,
			setupState: func(state *state.StateDB) {
				precompile.SetContractNativeMinterStatus(state, adminAddr, precompile.AllowListAdmin)
			},
			expectedErr: precompile.ErrCannotMint.Error(),
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetContractNativeMinterStatus(state, adminAddr)
				assert.Equal(t, precompile.AllowListAdmin, res)

				res = precompile.GetContractNativeMinterStatus(state, noRoleAddr)
				assert.Equal(t, precompile.AllowListAdmin, res)
			},
		},
		"admin tries minting": {
			caller:         adminAddr,
			precompileAddr: precompile.ContractNativeMinterAddress,
			input: func() []byte {
				input, err := precompile.PackMintInput(adminAddr, common.Big1)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.MintGasCost,
			readOnly:    false,
			setupState: func(state *state.StateDB) {
				precompile.SetContractNativeMinterStatus(state, adminAddr, precompile.AllowListAdmin)
			},
			expectedErr: precompile.ErrCannotMint.Error(),
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetContractNativeMinterStatus(state, adminAddr)
				assert.Equal(t, precompile.AllowListAdmin, res)

				res = precompile.GetContractNativeMinterStatus(state, noRoleAddr)
				assert.Equal(t, precompile.AllowListAdmin, res)
			},
		},
		"mint with readOnly enabled": {
			caller:         noRoleAddr,
			precompileAddr: precompile.ContractNativeMinterAddress,
			input: func() []byte {
				input, err := precompile.PackModifyAllowList(noRoleAddr, precompile.AllowListEnabled)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.ModifyAllowListGasCost,
			readOnly:    true,
			setupState: func(state *state.StateDB) {
				precompile.SetContractNativeMinterStatus(state, adminAddr, precompile.AllowListAdmin)
				precompile.SetContractNativeMinterStatus(state, noRoleAddr, precompile.AllowListEnabled)
			},
			expectedErr: precompile.ErrWriteProtection.Error(),
		},
		"mint with insufficient gas": {
			caller:         noRoleAddr,
			precompileAddr: precompile.ContractNativeMinterAddress,
			input: func() []byte {
				input, err := precompile.PackMintInput(adminAddr, common.Big1)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.MintGasCost - 1,
			readOnly:    false,
			setupState: func(state *state.StateDB) {
				precompile.SetContractDeployerAllowListStatus(state, adminAddr, precompile.AllowListAdmin)
				precompile.SetContractNativeMinterStatus(state, noRoleAddr, precompile.AllowListEnabled)
			},
			expectedErr: vm.ErrOutOfGas.Error(),
		},
		"add role with insufficient gas": {
			caller:         adminAddr,
			precompileAddr: precompile.ContractNativeMinterAddress,
			input: func() []byte {
				input, err := precompile.PackModifyAllowList(noRoleAddr, precompile.AllowListEnabled)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.ModifyAllowListGasCost - 1,
			readOnly:    false,
			setupState: func(state *state.StateDB) {
				precompile.SetContractDeployerAllowListStatus(state, adminAddr, precompile.AllowListAdmin)
			},
			expectedErr: vm.ErrOutOfGas.Error(),
		},

		"read role": {
			caller:         adminAddr,
			precompileAddr: precompile.ContractNativeMinterAddress,
			input: func() []byte {
				return precompile.PackReadAllowList(noRoleAddr)
			},
			suppliedGas: precompile.ReadAllowListGasCost,
			readOnly:    false,
			setupState: func(state *state.StateDB) {
				precompile.SetContractNativeMinterStatus(state, adminAddr, precompile.AllowListAdmin)
			},
			expectedRes: common.Hash(precompile.AllowListNoRole).Bytes(),
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetContractNativeMinterStatus(state, adminAddr)
				assert.Equal(t, precompile.AllowListAdmin, res)
			},
		},
		"set enabled": {
			caller:         adminAddr,
			precompileAddr: precompile.ContractNativeMinterAddress,
			input: func() []byte {
				input, err := precompile.PackModifyAllowList(noRoleAddr, precompile.AllowListEnabled)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.ModifyAllowListGasCost,
			readOnly:    false,
			setupState: func(state *state.StateDB) {
				precompile.SetContractNativeMinterStatus(state, adminAddr, precompile.AllowListAdmin)
			},
			expectedRes: []byte{},
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetContractNativeMinterStatus(state, adminAddr)
				assert.Equal(t, precompile.AllowListAdmin, res)

				res = precompile.GetContractNativeMinterStatus(state, noRoleAddr)
				assert.Equal(t, precompile.AllowListEnabled, res)
			},
		},
		"mint": {
			caller:         noRoleAddr,
			precompileAddr: precompile.ContractNativeMinterAddress,
			input: func() []byte {
				input, err := precompile.PackMintInput(noRoleAddr, common.Big1)
				if err != nil {
					panic(err)
				}
				to, amount, err := precompile.UnpackMintInput(input[4:])
				if err != nil {
					panic(err)
				}
				assert.Equal(t, noRoleAddr, to)
				assert.Equal(t, common.Big1, amount)
				return input
			},
			suppliedGas: precompile.MintGasCost,
			readOnly:    false,
			setupState: func(state *state.StateDB) {
				precompile.SetContractNativeMinterStatus(state, noRoleAddr, precompile.AllowListEnabled)
				initialBalance := state.GetBalance(noRoleAddr)
				assert.Equal(t, common.Big0, initialBalance)
			},
			expectedRes: []byte{},
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetContractNativeMinterStatus(state, noRoleAddr)
				assert.Equal(t, precompile.AllowListEnabled, res)

				finalBalance := state.GetBalance(noRoleAddr)
				assert.Equal(t, common.Big1, finalBalance)
			},
		},
		"mint big": {
			caller:         noRoleAddr,
			precompileAddr: precompile.ContractNativeMinterAddress,
			input: func() []byte {
				input, err := precompile.PackMintInput(noRoleAddr, math.MaxBig256)
				if err != nil {
					panic(err)
				}
				to, amount, err := precompile.UnpackMintInput(input[4:])
				if err != nil {
					panic(err)
				}
				assert.Equal(t, noRoleAddr, to)
				assert.Equal(t, math.MaxBig256, amount)
				return input
			},
			suppliedGas: precompile.MintGasCost,
			readOnly:    false,
			setupState: func(state *state.StateDB) {
				precompile.SetContractNativeMinterStatus(state, noRoleAddr, precompile.AllowListEnabled)
				initialBalance := state.GetBalance(noRoleAddr)
				assert.Equal(t, common.Big0, initialBalance)
			},
			expectedRes: []byte{},
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetContractNativeMinterStatus(state, noRoleAddr)
				assert.Equal(t, precompile.AllowListEnabled, res)

				finalBalance := state.GetBalance(noRoleAddr)
				assert.Equal(t, math.MaxBig256, finalBalance)
			},
		},
		"mint for non-enabled": {
			caller:         adminAddr,
			precompileAddr: precompile.ContractNativeMinterAddress,
			input: func() []byte {
				input, err := precompile.PackMintInput(adminAddr, common.Big1)
				if err != nil {
					panic(err)
				}
				to, amount, err := precompile.UnpackMintInput(input[4:])
				if err != nil {
					panic(err)
				}
				assert.Equal(t, adminAddr, to)
				assert.Equal(t, common.Big1, amount)
				return input
			},
			suppliedGas: precompile.MintGasCost,
			readOnly:    false,
			setupState: func(state *state.StateDB) {
				precompile.SetContractNativeMinterStatus(state, adminAddr, precompile.AllowListEnabled)
				initialBalance := state.GetBalance(adminAddr)
				assert.Equal(t, common.Big0, initialBalance)
				initialBalance2 := state.GetBalance(noRoleAddr)
				assert.Equal(t, common.Big0, initialBalance2)
			},
			expectedRes: []byte{},
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetContractNativeMinterStatus(state, noRoleAddr)
				assert.Equal(t, precompile.AllowListNoRole, res)

				res = precompile.GetContractNativeMinterStatus(state, adminAddr)
				assert.Equal(t, precompile.AllowListEnabled, res)

				finalBalance := state.GetBalance(noRoleAddr)
				assert.Equal(t, common.Big0, finalBalance)

				finalBalance2 := state.GetBalance(adminAddr)
				assert.Equal(t, common.Big1, finalBalance2)
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()
			state, err := state.New(common.Hash{}, state.NewDatabase(db), nil)
			if err != nil {
				t.Fatal(err)
			}
			test.setupState(state)

			ret, remainingGas, err := precompile.ContractNativeMinterPrecompile.Run(&mockAccessibleState{state: state}, test.caller, test.precompileAddr, test.input(), test.suppliedGas, test.readOnly)
			if len(test.expectedErr) != 0 {
				if err == nil {
					assert.Failf(t, "run expectedly passed without error", "expected error %q", test.expectedErr)
				} else {
					assert.True(t, strings.Contains(err.Error(), test.expectedErr), "expected error (%s) to contain substring (%s)", err, test.expectedErr)
				}
				return
			}

			if err != nil {
				t.Fatal(err)
			}

			assert.Equal(t, uint64(0), remainingGas)
			assert.Equal(t, test.expectedRes, ret)

			test.assertState(t, state)
		})
	}
}
