// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/ava-labs/subnet-evm/ethdb/memorydb"
	"github.com/ava-labs/subnet-evm/precompile/contract"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func NewTestStateDB(t testing.TB) contract.StateDB {
	db := memorydb.New()
	stateDB, err := New(common.Hash{}, NewDatabase(db), nil)
	require.NoError(t, err)
	return stateDB
}
