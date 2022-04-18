// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"strings"
	"testing"

	"github.com/ava-labs/subnet-evm/core/rawdb"
	"github.com/ava-labs/subnet-evm/core/state"
	"github.com/ava-labs/subnet-evm/precompile"
	"github.com/ava-labs/subnet-evm/vmerrs"
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
			expectedErr: vmerrs.ErrWriteProtection.Error(),
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
			expectedErr: vmerrs.ErrOutOfGas.Error(),
		},
		"read allow list no role": {
			caller:         noRoleAddr,
			precompileAddr: precompile.ContractDeployerAllowListAddress,
			input: func() []byte {
				return precompile.PackReadAllowList(noRoleAddr)
			},
			suppliedGas: precompile.ReadAllowListGasCost,
			readOnly:    false,
			expectedRes: common.Hash(precompile.AllowListNoRole).Bytes(),
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetContractDeployerAllowListStatus(state, noRoleAddr)
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
			expectedErr: vmerrs.ErrOutOfGas.Error(),
		},
	} {
		t.Run(name, func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()
			state, err := state.New(common.Hash{}, state.NewDatabase(db), nil)
			if err != nil {
				t.Fatal(err)
			}

			// Set up the state so that each address has the expected permissions at the start.
			precompile.SetContractDeployerAllowListStatus(state, adminAddr, precompile.AllowListAdmin)
			precompile.SetContractDeployerAllowListStatus(state, noRoleAddr, precompile.AllowListNoRole)

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

func TestTxAllowListRun(t *testing.T) {
	type test struct {
		caller         common.Address
		precompileAddr common.Address
		input          func() []byte
		suppliedGas    uint64
		readOnly       bool

		expectedRes []byte
		expectedErr string

		assertState func(t *testing.T, state *state.StateDB)
	}

	adminAddr := common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC")
	noRoleAddr := common.HexToAddress("0xF60C45c607D0f41687c94C314d300f483661E13a")

	for name, test := range map[string]test{
		"set admin": {
			caller:         adminAddr,
			precompileAddr: precompile.TxAllowListAddress,
			input: func() []byte {
				input, err := precompile.PackModifyAllowList(noRoleAddr, precompile.AllowListAdmin)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.ModifyAllowListGasCost,
			readOnly:    false,
			expectedRes: []byte{},
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetTxAllowListStatus(state, adminAddr)
				assert.Equal(t, precompile.AllowListAdmin, res)

				res = precompile.GetTxAllowListStatus(state, noRoleAddr)
				assert.Equal(t, precompile.AllowListAdmin, res)
			},
		},
		"set allowed": {
			caller:         adminAddr,
			precompileAddr: precompile.TxAllowListAddress,
			input: func() []byte {
				input, err := precompile.PackModifyAllowList(noRoleAddr, precompile.AllowListEnabled)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.ModifyAllowListGasCost,
			readOnly:    false,
			expectedRes: []byte{},
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetTxAllowListStatus(state, adminAddr)
				assert.Equal(t, precompile.AllowListAdmin, res)

				res = precompile.GetTxAllowListStatus(state, noRoleAddr)
				assert.Equal(t, precompile.AllowListEnabled, res)
			},
		},
		"set no role": {
			caller:         adminAddr,
			precompileAddr: precompile.TxAllowListAddress,
			input: func() []byte {
				input, err := precompile.PackModifyAllowList(adminAddr, precompile.AllowListNoRole)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.ModifyAllowListGasCost,
			readOnly:    false,
			expectedRes: []byte{},
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetTxAllowListStatus(state, adminAddr)
				assert.Equal(t, precompile.AllowListNoRole, res)
			},
		},
		"set no role from non-admin": {
			caller:         noRoleAddr,
			precompileAddr: precompile.TxAllowListAddress,
			input: func() []byte {
				input, err := precompile.PackModifyAllowList(adminAddr, precompile.AllowListNoRole)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.ModifyAllowListGasCost,
			readOnly:    false,
			expectedErr: precompile.ErrCannotModifyAllowList.Error(),
		},
		"set allowed from non-admin": {
			caller:         noRoleAddr,
			precompileAddr: precompile.TxAllowListAddress,
			input: func() []byte {
				input, err := precompile.PackModifyAllowList(adminAddr, precompile.AllowListEnabled)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.ModifyAllowListGasCost,
			readOnly:    false,
			expectedErr: precompile.ErrCannotModifyAllowList.Error(),
		},
		"set admin from non-admin": {
			caller:         noRoleAddr,
			precompileAddr: precompile.TxAllowListAddress,
			input: func() []byte {
				input, err := precompile.PackModifyAllowList(adminAddr, precompile.AllowListAdmin)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.ModifyAllowListGasCost,
			readOnly:    false,
			expectedErr: precompile.ErrCannotModifyAllowList.Error(),
		},
		"set no role with readOnly enabled": {
			caller:         adminAddr,
			precompileAddr: precompile.TxAllowListAddress,
			input: func() []byte {
				input, err := precompile.PackModifyAllowList(adminAddr, precompile.AllowListNoRole)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.ModifyAllowListGasCost,
			readOnly:    true,
			expectedErr: vmerrs.ErrWriteProtection.Error(),
		},
		"set no role insufficient gas": {
			caller:         adminAddr,
			precompileAddr: precompile.TxAllowListAddress,
			input: func() []byte {
				input, err := precompile.PackModifyAllowList(adminAddr, precompile.AllowListNoRole)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.ModifyAllowListGasCost - 1,
			readOnly:    false,
			expectedErr: vmerrs.ErrOutOfGas.Error(),
		},
		"read allow list no role": {
			caller:         noRoleAddr,
			precompileAddr: precompile.TxAllowListAddress,
			input: func() []byte {
				return precompile.PackReadAllowList(noRoleAddr)
			},
			suppliedGas: precompile.ReadAllowListGasCost,
			readOnly:    false,
			expectedRes: common.Hash(precompile.AllowListNoRole).Bytes(),
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetTxAllowListStatus(state, noRoleAddr)
				assert.Equal(t, precompile.AllowListNoRole, res)
			},
		},
		"read allow list admin role": {
			caller:         adminAddr,
			precompileAddr: precompile.TxAllowListAddress,
			input: func() []byte {
				return precompile.PackReadAllowList(noRoleAddr)
			},
			suppliedGas: precompile.ReadAllowListGasCost,
			readOnly:    false,
			expectedRes: common.Hash(precompile.AllowListNoRole).Bytes(),
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetTxAllowListStatus(state, adminAddr)
				assert.Equal(t, precompile.AllowListAdmin, res)
			},
		},
		"read allow list with readOnly enabled": {
			caller:         adminAddr,
			precompileAddr: precompile.TxAllowListAddress,
			input: func() []byte {
				return precompile.PackReadAllowList(noRoleAddr)
			},
			suppliedGas: precompile.ReadAllowListGasCost,
			readOnly:    true,
			expectedRes: common.Hash(precompile.AllowListNoRole).Bytes(),
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetTxAllowListStatus(state, adminAddr)
				assert.Equal(t, precompile.AllowListAdmin, res)
			},
		},
		"read allow list out of gas": {
			caller:         adminAddr,
			precompileAddr: precompile.TxAllowListAddress,
			input: func() []byte {
				return precompile.PackReadAllowList(noRoleAddr)
			},
			suppliedGas: precompile.ReadAllowListGasCost - 1,
			readOnly:    true,
			expectedErr: vmerrs.ErrOutOfGas.Error(),
		},
	} {
		t.Run(name, func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()
			state, err := state.New(common.Hash{}, state.NewDatabase(db), nil)
			if err != nil {
				t.Fatal(err)
			}

			// Set up the state so that each address has the expected permissions at the start.
			precompile.SetTxAllowListStatus(state, adminAddr, precompile.AllowListAdmin)

			ret, remainingGas, err := precompile.TxAllowListPrecompile.Run(&mockAccessibleState{state: state}, test.caller, test.precompileAddr, test.input(), test.suppliedGas, test.readOnly)
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

		expectedRes []byte
		expectedErr string

		assertState func(t *testing.T, state *state.StateDB)
	}

	adminAddr := common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC")
	allowAddr := common.HexToAddress("0xAb5801a7D398351b8bE11C439e05C5B3259aeC9B")
	noRoleAddr := common.HexToAddress("0xF60C45c607D0f41687c94C314d300f483661E13a")

	for name, test := range map[string]test{
		"mint funds from no role fails": {
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
			expectedErr: precompile.ErrCannotMint.Error(),
		},
		"mint funds from allow address": {
			caller:         allowAddr,
			precompileAddr: precompile.ContractNativeMinterAddress,
			input: func() []byte {
				input, err := precompile.PackMintInput(allowAddr, common.Big1)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.MintGasCost,
			readOnly:    false,
			expectedRes: []byte{},
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetContractNativeMinterStatus(state, allowAddr)
				assert.Equal(t, precompile.AllowListEnabled, res)

				assert.Equal(t, common.Big1, state.GetBalance(allowAddr), "expected minted funds")
			},
		},
		"mint funds from admin address": {
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
			expectedRes: []byte{},
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetContractNativeMinterStatus(state, adminAddr)
				assert.Equal(t, precompile.AllowListAdmin, res)

				assert.Equal(t, common.Big1, state.GetBalance(adminAddr), "expected minted funds")
			},
		},
		"mint max big funds": {
			caller:         adminAddr,
			precompileAddr: precompile.ContractNativeMinterAddress,
			input: func() []byte {
				input, err := precompile.PackMintInput(adminAddr, math.MaxBig256)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.MintGasCost,
			readOnly:    false,
			expectedRes: []byte{},
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetContractNativeMinterStatus(state, adminAddr)
				assert.Equal(t, precompile.AllowListAdmin, res)

				assert.Equal(t, math.MaxBig256, state.GetBalance(adminAddr), "expected minted funds")
			},
		},
		"readOnly mint with noRole fails": {
			caller:         noRoleAddr,
			precompileAddr: precompile.ContractNativeMinterAddress,
			input: func() []byte {
				input, err := precompile.PackMintInput(adminAddr, common.Big1)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.MintGasCost,
			readOnly:    true,
			expectedErr: vmerrs.ErrWriteProtection.Error(),
		},
		"readOnly mint with allow role fails": {
			caller:         allowAddr,
			precompileAddr: precompile.ContractNativeMinterAddress,
			input: func() []byte {
				input, err := precompile.PackMintInput(allowAddr, common.Big1)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.MintGasCost,
			readOnly:    true,
			expectedErr: vmerrs.ErrWriteProtection.Error(),
		},
		"readOnly mint with admin role fails": {
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
			readOnly:    true,
			expectedErr: vmerrs.ErrWriteProtection.Error(),
		},
		"insufficient gas mint from admin": {
			caller:         adminAddr,
			precompileAddr: precompile.ContractNativeMinterAddress,
			input: func() []byte {
				input, err := precompile.PackMintInput(allowAddr, common.Big1)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.MintGasCost - 1,
			readOnly:    false,
			expectedErr: vmerrs.ErrOutOfGas.Error(),
		},
		"read from noRole address": {
			caller:         noRoleAddr,
			precompileAddr: precompile.ContractNativeMinterAddress,
			input: func() []byte {
				return precompile.PackReadAllowList(noRoleAddr)
			},
			suppliedGas: precompile.ReadAllowListGasCost,
			readOnly:    false,
			expectedRes: common.Hash(precompile.AllowListNoRole).Bytes(),
			assertState: func(t *testing.T, state *state.StateDB) {},
		},
		"read from noRole address readOnly enabled": {
			caller:         noRoleAddr,
			precompileAddr: precompile.ContractNativeMinterAddress,
			input: func() []byte {
				return precompile.PackReadAllowList(noRoleAddr)
			},
			suppliedGas: precompile.ReadAllowListGasCost,
			readOnly:    true,
			expectedRes: common.Hash(precompile.AllowListNoRole).Bytes(),
			assertState: func(t *testing.T, state *state.StateDB) {},
		},
		"read from noRole address with insufficient gas": {
			caller:         noRoleAddr,
			precompileAddr: precompile.ContractNativeMinterAddress,
			input: func() []byte {
				return precompile.PackReadAllowList(noRoleAddr)
			},
			suppliedGas: precompile.ReadAllowListGasCost - 1,
			readOnly:    false,
			expectedErr: vmerrs.ErrOutOfGas.Error(),
		},
		"set allow role from admin": {
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
			expectedRes: []byte{},
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetContractNativeMinterStatus(state, adminAddr)
				assert.Equal(t, precompile.AllowListAdmin, res)

				res = precompile.GetContractNativeMinterStatus(state, noRoleAddr)
				assert.Equal(t, precompile.AllowListEnabled, res)
			},
		},
		"set allow role from non-admin fails": {
			caller:         allowAddr,
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
			expectedErr: precompile.ErrCannotModifyAllowList.Error(),
		},
	} {
		t.Run(name, func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()
			state, err := state.New(common.Hash{}, state.NewDatabase(db), nil)
			if err != nil {
				t.Fatal(err)
			}
			// Set up the state so that each address has the expected permissions at the start.
			precompile.SetContractNativeMinterStatus(state, adminAddr, precompile.AllowListAdmin)
			precompile.SetContractNativeMinterStatus(state, allowAddr, precompile.AllowListEnabled)
			precompile.SetContractNativeMinterStatus(state, noRoleAddr, precompile.AllowListNoRole)

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
