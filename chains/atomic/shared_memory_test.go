// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestSharedMemory(t *testing.T) {
	m := Memory{}
	m.Initialize(logging.NoLog{}, memdb.New())

	chainID0 := ids.GenerateTestID()
	chainID1 := ids.GenerateTestID()

	sm0 := m.NewSharedMemory(chainID0)
	sm1 := m.NewSharedMemory(chainID1)

	err := sm0.Put(chainID1, []*Element{{
		Key:   []byte{0},
		Value: []byte{1},
		Traits: [][]byte{
			{2},
			{3},
		},
	}})
	assert.NoError(t, err)

	err = sm0.Put(chainID1, []*Element{{
		Key:   []byte{4},
		Value: []byte{5},
		Traits: [][]byte{
			{2},
			{3},
		},
	}})
	assert.NoError(t, err)

	values, _, _, err := sm0.Indexed(chainID1, [][]byte{{2}}, nil, nil, 1)
	assert.NoError(t, err)
	assert.Empty(t, values, "wrong indexed values returned")

	values, _, _, err = sm1.Indexed(chainID0, [][]byte{{2}}, nil, nil, 0)
	assert.NoError(t, err)
	assert.Empty(t, values, "wrong indexed values returned")

	values, _, _, err = sm1.Indexed(chainID0, [][]byte{{2}}, nil, nil, 1)
	assert.NoError(t, err)
	assert.Equal(t, [][]byte{{1}}, values, "wrong indexed values returned")

	values, _, _, err = sm1.Indexed(chainID0, [][]byte{{2}}, nil, nil, 2)
	assert.NoError(t, err)
	assert.Equal(t, [][]byte{{1}, {5}}, values, "wrong indexed values returned")

	values, _, _, err = sm1.Indexed(chainID0, [][]byte{{2}}, nil, nil, 3)
	assert.NoError(t, err)
	assert.Equal(t, [][]byte{{1}, {5}}, values, "wrong indexed values returned")

	values, _, _, err = sm1.Indexed(chainID0, [][]byte{{3}}, nil, nil, 3)
	assert.NoError(t, err)
	assert.Equal(t, [][]byte{{1}, {5}}, values, "wrong indexed values returned")

	values, _, _, err = sm1.Indexed(chainID0, [][]byte{{2}, {3}}, nil, nil, 3)
	assert.NoError(t, err)
	assert.Equal(t, [][]byte{{1}, {5}}, values, "wrong indexed values returned")
}
