// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package extstate

import (
	"testing"

	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/precompile/contract"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/stretchr/testify/require"
)

func NewTestStateDB(t testing.TB) contract.StateDB {
	db := rawdb.NewMemoryDatabase()
	statedb, err := state.New(common.Hash{}, state.NewDatabase(db), nil)
	require.NoError(t, err)
	return &StateDB{VmStateDB: statedb}
}
