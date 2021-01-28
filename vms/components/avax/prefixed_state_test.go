// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
)

func TestStateIDs(t *testing.T) {
	state := NewState(memdb.New(), nil, nil, 0, 0, 10)

	keyA := []byte{'a'}
	keyB := []byte{'b'}

	IDA1 := ids.GenerateTestID()
	IDA2 := ids.GenerateTestID()

	err := state.AddID(keyA, IDA1)
	assert.NoError(t, err)

	err = state.AddID(keyA, IDA2)
	assert.NoError(t, err)

	// Test State returns all of the added IDs
	IDs, err := state.IDs(keyA, nil, 5)
	assert.NoError(t, err)

	assert.Len(t, IDs, 2)
	set := ids.Set{}
	set.Add(IDs...)

	assert.True(t, set.Contains(IDA1))
	assert.True(t, set.Contains(IDA2))

	// Test State only returns up to [limit] IDs
	IDs, err = state.IDs(keyA, nil, 1)
	assert.NoError(t, err)

	assert.Len(t, IDs, 1)
	set = ids.Set{}
	set.Add(IDs[0])

	assert.True(t, set.Contains(IDA1) || set.Contains(IDA2))

	// Test State RemoveID removes the ID from the added IDs
	err = state.RemoveID(keyA, IDA1)
	assert.NoError(t, err)

	IDs, err = state.IDs(keyA, nil, 1)
	assert.NoError(t, err)

	assert.Len(t, IDs, 1)

	assert.True(t, IDs[0] == IDA2)

	// Test State does not return any IDs added for a different key
	IDs, err = state.IDs(keyB, nil, 5)
	assert.NoError(t, err)
	assert.Len(t, IDs, 0)
}
