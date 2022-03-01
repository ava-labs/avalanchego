// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"strings"
	"testing"

	"github.com/ava-labs/subnet-evm/core/rawdb"
	"github.com/ava-labs/subnet-evm/core/state"
	"github.com/ava-labs/subnet-evm/precompile"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
)

type mockAccessibleState struct {
	state *state.StateDB
}

func (m *mockAccessibleState) GetStateDB() precompile.StateDB { return m.state }

// This test is added within the core package so that it can import all of the required code
// without creating any import cycles
func TestAllowListConfigure(t *testing.T) {
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
			precompileAddr: precompile.AllowListAddress,
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
				precompile.SetAllowListRole(state, adminAddr, precompile.AllowListAdmin)
			},
			expectedRes: []byte{},
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetAllowListStatus(state, adminAddr)
				assert.Equal(t, precompile.AllowListAdmin, res)

				res = precompile.GetAllowListStatus(state, noRoleAddr)
				assert.Equal(t, precompile.AllowListAdmin, res)
			},
		},
		"set deployer": {
			caller:         adminAddr,
			precompileAddr: precompile.AllowListAddress,
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
				precompile.SetAllowListRole(state, adminAddr, precompile.AllowListAdmin)
			},
			expectedRes: []byte{},
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetAllowListStatus(state, adminAddr)
				assert.Equal(t, precompile.AllowListAdmin, res)

				res = precompile.GetAllowListStatus(state, noRoleAddr)
				assert.Equal(t, precompile.AllowListEnabled, res)
			},
		},
		"set no role": {
			caller:         adminAddr,
			precompileAddr: precompile.AllowListAddress,
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
				precompile.SetAllowListRole(state, adminAddr, precompile.AllowListAdmin)
			},
			expectedRes: []byte{},
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetAllowListStatus(state, adminAddr)
				assert.Equal(t, precompile.AllowListNoRole, res)
			},
		},
		"read allow list no role": {
			caller:         adminAddr,
			precompileAddr: precompile.AllowListAddress,
			input: func() []byte {
				return precompile.PackReadAllowList(noRoleAddr)
			},
			suppliedGas: precompile.ModifyAllowListGasCost,
			readOnly:    false,
			setupState:  func(state *state.StateDB) {},
			expectedRes: common.Hash(precompile.AllowListNoRole).Bytes(),
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetAllowListStatus(state, adminAddr)
				assert.Equal(t, precompile.AllowListNoRole, res)
			},
		},
		"read allow list admin role": {
			caller:         adminAddr,
			precompileAddr: precompile.AllowListAddress,
			input: func() []byte {
				return precompile.PackReadAllowList(noRoleAddr)
			},
			suppliedGas: precompile.ModifyAllowListGasCost,
			readOnly:    false,
			setupState: func(state *state.StateDB) {
				precompile.SetAllowListRole(state, adminAddr, precompile.AllowListAdmin)
			},
			expectedRes: common.Hash(precompile.AllowListNoRole).Bytes(),
			assertState: func(t *testing.T, state *state.StateDB) {
				res := precompile.GetAllowListStatus(state, adminAddr)
				assert.Equal(t, precompile.AllowListAdmin, res)
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

			ret, _, err := precompile.AllowListPrecompile.Run(&mockAccessibleState{state: state}, test.caller, test.precompileAddr, test.input(), test.suppliedGas, test.readOnly)
			if len(test.expectedErr) != 0 {
				assert.True(t, strings.Contains(err.Error(), test.expectedErr), "expected error (%s) to contain substring (%s)", err, test.expectedErr)
				return
			}

			if err != nil {
				t.Fatal(err)
			}

			assert.Equal(t, test.expectedRes, ret)

			test.assertState(t, state)
		})
	}
}
