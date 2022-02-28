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
		input          []byte
		suppliedGas    uint64
		readOnly       bool

		setupState  func(state *state.StateDB)
		expectedRes []byte
		expectedErr string

		assertState func(t *testing.T, state *state.StateDB)
	}

	for name, test := range map[string]test{} {
		t.Run(name, func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()
			state, err := state.New(common.Hash{}, state.NewDatabase(db), nil)
			if err != nil {
				t.Fatal(err)
			}
			test.setupState(state)

			ret, _, err := precompile.AllowListPrecompile.Run(&mockAccessibleState{state: state}, test.caller, test.precompileAddr, test.input, test.suppliedGas, test.readOnly)
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
