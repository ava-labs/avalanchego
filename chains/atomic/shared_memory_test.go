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
	assert := assert.New(t)

	m := Memory{}
	err := m.Initialize(logging.NoLog{}, memdb.New())
	assert.NoError(err)

	chainID0 := ids.GenerateTestID()
	chainID1 := ids.GenerateTestID()

	sm0 := m.NewSharedMemory(chainID0)
	sm1 := m.NewSharedMemory(chainID1)

	err = sm0.Put(chainID1, []*Element{{
		Key:   []byte{0},
		Value: []byte{1},
		Traits: [][]byte{
			{2},
			{3},
		},
	}})
	assert.NoError(err)

	err = sm0.Put(chainID1, []*Element{{
		Key:   []byte{4},
		Value: []byte{5},
		Traits: [][]byte{
			{2},
			{3},
		},
	}})
	assert.NoError(err)

	err = sm0.Put(chainID1, []*Element{{
		Key:   []byte{6},
		Value: []byte{7},
		Traits: [][]byte{
			{8},
		},
	}})
	assert.NoError(err)

	err = sm0.Put(chainID1, []*Element{{
		Key:    []byte{9},
		Value:  []byte{10},
		Traits: nil,
	}})
	assert.NoError(err)

	values, _, _, err := sm0.Indexed(chainID1, [][]byte{{2}}, nil, nil, 1)
	assert.NoError(err)
	assert.Empty(values, "wrong indexed values returned")

	values, _, _, err = sm1.Indexed(chainID0, [][]byte{{2}}, nil, nil, 0)
	assert.NoError(err)
	assert.Empty(values, "wrong indexed values returned")

	values, _, _, err = sm1.Indexed(chainID0, [][]byte{{2}}, nil, nil, 1)
	assert.NoError(err)
	assert.Len(values, 1)
	assert.Contains([][]byte{{1}, {5}}, values[0], "wrong indexed values returned")

	values, _, _, err = sm1.Indexed(chainID0, [][]byte{{2}}, nil, nil, 2)
	assert.NoError(err)
	assert.ElementsMatch([][]byte{{1}, {5}}, values, "wrong indexed values returned")

	values, _, _, err = sm1.Indexed(chainID0, [][]byte{{2}}, nil, nil, 3)
	assert.NoError(err)
	assert.ElementsMatch([][]byte{{1}, {5}}, values, "wrong indexed values returned")

	values, _, _, err = sm1.Indexed(chainID0, [][]byte{{3}}, nil, nil, 3)
	assert.NoError(err)
	assert.ElementsMatch([][]byte{{1}, {5}}, values, "wrong indexed values returned")

	values, _, _, err = sm1.Indexed(chainID0, [][]byte{{2}, {3}, {8}}, nil, nil, 3)
	assert.NoError(err)
	assert.ElementsMatch([][]byte{{1}, {5}, {7}}, values, "wrong indexed values returned")
}

func TestSharedMemoryCantDuplicatePut(t *testing.T) {
	assert := assert.New(t)

	m := Memory{}
	err := m.Initialize(logging.NoLog{}, memdb.New())
	assert.NoError(err)

	chainID0 := ids.GenerateTestID()
	chainID1 := ids.GenerateTestID()

	sm := m.NewSharedMemory(chainID0)

	err = sm.Put(chainID1, []*Element{
		{
			Key:   []byte{0},
			Value: []byte{1},
		},
		{
			Key:   []byte{0},
			Value: []byte{2},
		},
	})
	assert.Error(err, "shouldn't be able to write duplicated keys")

	err = sm.Put(chainID1, []*Element{{
		Key:   []byte{0},
		Value: []byte{1},
	}})
	assert.NoError(err)

	err = sm.Put(chainID1, []*Element{{
		Key:   []byte{0},
		Value: []byte{1},
	}})
	assert.Error(err, "shouldn't be able to write duplicated keys")
}

func TestSharedMemoryCantDuplicateRemove(t *testing.T) {
	assert := assert.New(t)

	m := Memory{}
	err := m.Initialize(logging.NoLog{}, memdb.New())
	assert.NoError(err)

	chainID0 := ids.GenerateTestID()
	chainID1 := ids.GenerateTestID()

	sm := m.NewSharedMemory(chainID0)

	err = sm.Remove(chainID1, [][]byte{{0}})
	assert.NoError(err)

	err = sm.Remove(chainID1, [][]byte{{0}})
	assert.Error(err, "shouldn't be able to remove duplicated keys")
}
