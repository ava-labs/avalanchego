// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"math/big"
	"strings"
	"testing"

	"github.com/ava-labs/subnet-evm/commontype"
	"github.com/ava-labs/subnet-evm/core/rawdb"
	"github.com/ava-labs/subnet-evm/core/state"
	"github.com/ava-labs/subnet-evm/precompile"
	"github.com/ava-labs/subnet-evm/vmerrs"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/stretchr/testify/assert"
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
)

type mockBlockContext struct {
	blockNumber *big.Int
	timestamp   uint64
}

func (mb *mockBlockContext) Number() *big.Int    { return mb.blockNumber }
func (mb *mockBlockContext) Timestamp() *big.Int { return new(big.Int).SetUint64(mb.timestamp) }

type mockAccessibleState struct {
	state        *state.StateDB
	blockContext *mockBlockContext
}

func (m *mockAccessibleState) GetStateDB() precompile.StateDB { return m.state }

func (m *mockAccessibleState) GetBlockContext() precompile.BlockContext { return m.blockContext }

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
			blockContext := &mockBlockContext{blockNumber: common.Big0}
			ret, remainingGas, err := precompile.ContractDeployerAllowListPrecompile.Run(&mockAccessibleState{state: state, blockContext: blockContext}, test.caller, test.precompileAddr, test.input(), test.suppliedGas, test.readOnly)
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
			blockContext := &mockBlockContext{blockNumber: common.Big0}
			ret, remainingGas, err := precompile.TxAllowListPrecompile.Run(&mockAccessibleState{state: state, blockContext: blockContext}, test.caller, test.precompileAddr, test.input(), test.suppliedGas, test.readOnly)
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
			blockContext := &mockBlockContext{blockNumber: common.Big0}
			ret, remainingGas, err := precompile.ContractNativeMinterPrecompile.Run(&mockAccessibleState{state: state, blockContext: blockContext}, test.caller, test.precompileAddr, test.input(), test.suppliedGas, test.readOnly)
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

func TestFeeConfigManagerRun(t *testing.T) {
	type test struct {
		caller         common.Address
		precompileAddr common.Address
		preCondition   func(t *testing.T, state *state.StateDB)
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
		"set config from no role fails": {
			caller:         noRoleAddr,
			precompileAddr: precompile.FeeConfigManagerAddress,
			input: func() []byte {
				input, err := precompile.PackSetFeeConfig(testFeeConfig)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.SetFeeConfigGasCost,
			readOnly:    false,
			expectedErr: precompile.ErrCannotChangeFee.Error(),
		},
		"set config from allow address": {
			caller:         allowAddr,
			precompileAddr: precompile.FeeConfigManagerAddress,
			input: func() []byte {
				input, err := precompile.PackSetFeeConfig(testFeeConfig)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.SetFeeConfigGasCost,
			readOnly:    false,
			expectedRes: []byte{},
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetFeeConfigManagerStatus(state, allowAddr)
				assert.Equal(t, precompile.AllowListEnabled, res)

				feeConfig := precompile.GetStoredFeeConfig(state)
				assert.Equal(t, testFeeConfig, feeConfig)
			},
		},
		"set invalid config from allow address": {
			caller:         allowAddr,
			precompileAddr: precompile.FeeConfigManagerAddress,
			input: func() []byte {
				feeConfig := testFeeConfig
				feeConfig.MinBlockGasCost = new(big.Int).Mul(feeConfig.MaxBlockGasCost, common.Big2)
				input, err := precompile.PackSetFeeConfig(feeConfig)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.SetFeeConfigGasCost,
			readOnly:    false,
			expectedRes: []byte{},
			expectedErr: "cannot be greater than maxBlockGasCost",
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetFeeConfigManagerStatus(state, allowAddr)
				assert.Equal(t, precompile.AllowListEnabled, res)

				feeConfig := precompile.GetStoredFeeConfig(state)
				assert.Equal(t, testFeeConfig, feeConfig)
			},
		},
		"set config from admin address": {
			caller:         adminAddr,
			precompileAddr: precompile.FeeConfigManagerAddress,
			input: func() []byte {
				input, err := precompile.PackSetFeeConfig(testFeeConfig)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.SetFeeConfigGasCost,
			readOnly:    false,
			expectedRes: []byte{},
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetFeeConfigManagerStatus(state, adminAddr)
				assert.Equal(t, precompile.AllowListAdmin, res)

				feeConfig := precompile.GetStoredFeeConfig(state)
				assert.Equal(t, testFeeConfig, feeConfig)
				lastChangedAt := precompile.GetFeeConfigLastChangedAt(state)
				assert.EqualValues(t, testBlockNumber, lastChangedAt)
			},
		},
		"get fee config from non-enabled address": {
			caller:         noRoleAddr,
			precompileAddr: precompile.FeeConfigManagerAddress,
			preCondition: func(t *testing.T, state *state.StateDB) {
				err := precompile.StoreFeeConfig(state, testFeeConfig, &mockBlockContext{blockNumber: big.NewInt(6)})
				if err != nil {
					panic(err)
				}
			},
			input: func() []byte {
				return precompile.PackGetFeeConfigInput()
			},
			suppliedGas: precompile.GetFeeConfigGasCost,
			readOnly:    true,
			expectedRes: func() []byte {
				res, err := precompile.PackFeeConfig(testFeeConfig)
				assert.NoError(t, err)
				return res
			}(),
			assertState: func(t *testing.T, state *state.StateDB) {
				feeConfig := precompile.GetStoredFeeConfig(state)
				lastChangedAt := precompile.GetFeeConfigLastChangedAt(state)
				assert.Equal(t, testFeeConfig, feeConfig)
				assert.EqualValues(t, big.NewInt(6), lastChangedAt)
			},
		},
		"get last changed at from non-enabled address": {
			caller:         noRoleAddr,
			precompileAddr: precompile.FeeConfigManagerAddress,
			preCondition: func(t *testing.T, state *state.StateDB) {
				err := precompile.StoreFeeConfig(state, testFeeConfig, &mockBlockContext{blockNumber: testBlockNumber})
				if err != nil {
					panic(err)
				}
			},
			input: func() []byte {
				return precompile.PackGetLastChangedAtInput()
			},
			suppliedGas: precompile.GetLastChangedAtGasCost,
			readOnly:    true,
			expectedRes: common.BigToHash(testBlockNumber).Bytes(),
			assertState: func(t *testing.T, state *state.StateDB) {
				feeConfig := precompile.GetStoredFeeConfig(state)
				lastChangedAt := precompile.GetFeeConfigLastChangedAt(state)
				assert.Equal(t, testFeeConfig, feeConfig)
				assert.Equal(t, testBlockNumber, lastChangedAt)
			},
		},
		"readOnly setFeeConfig with noRole fails": {
			caller:         noRoleAddr,
			precompileAddr: precompile.FeeConfigManagerAddress,
			input: func() []byte {
				input, err := precompile.PackSetFeeConfig(testFeeConfig)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.SetFeeConfigGasCost,
			readOnly:    true,
			expectedErr: vmerrs.ErrWriteProtection.Error(),
		},
		"readOnly setFeeConfig with allow role fails": {
			caller:         allowAddr,
			precompileAddr: precompile.FeeConfigManagerAddress,
			input: func() []byte {
				input, err := precompile.PackSetFeeConfig(testFeeConfig)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.SetFeeConfigGasCost,
			readOnly:    true,
			expectedErr: vmerrs.ErrWriteProtection.Error(),
		},
		"readOnly setFeeConfig with admin role fails": {
			caller:         adminAddr,
			precompileAddr: precompile.FeeConfigManagerAddress,
			input: func() []byte {
				input, err := precompile.PackSetFeeConfig(testFeeConfig)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.SetFeeConfigGasCost,
			readOnly:    true,
			expectedErr: vmerrs.ErrWriteProtection.Error(),
		},
		"insufficient gas setFeeConfig from admin": {
			caller:         adminAddr,
			precompileAddr: precompile.FeeConfigManagerAddress,
			input: func() []byte {
				input, err := precompile.PackSetFeeConfig(testFeeConfig)
				if err != nil {
					panic(err)
				}
				return input
			},
			suppliedGas: precompile.SetFeeConfigGasCost - 1,
			readOnly:    false,
			expectedErr: vmerrs.ErrOutOfGas.Error(),
		},
		"set allow role from admin": {
			caller:         adminAddr,
			precompileAddr: precompile.FeeConfigManagerAddress,
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
				res := precompile.GetFeeConfigManagerStatus(state, adminAddr)
				assert.Equal(t, precompile.AllowListAdmin, res)

				res = precompile.GetFeeConfigManagerStatus(state, noRoleAddr)
				assert.Equal(t, precompile.AllowListEnabled, res)
			},
		},
		"set allow role from non-admin fails": {
			caller:         allowAddr,
			precompileAddr: precompile.FeeConfigManagerAddress,
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
			precompile.SetFeeConfigManagerStatus(state, adminAddr, precompile.AllowListAdmin)
			precompile.SetFeeConfigManagerStatus(state, allowAddr, precompile.AllowListEnabled)
			precompile.SetFeeConfigManagerStatus(state, noRoleAddr, precompile.AllowListNoRole)

			if test.preCondition != nil {
				test.preCondition(t, state)
			}

			blockContext := &mockBlockContext{blockNumber: testBlockNumber}
			ret, remainingGas, err := precompile.FeeConfigManagerPrecompile.Run(&mockAccessibleState{state: state, blockContext: blockContext}, test.caller, test.precompileAddr, test.input(), test.suppliedGas, test.readOnly)
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
