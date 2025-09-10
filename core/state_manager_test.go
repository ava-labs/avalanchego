// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/assert"
)

// Default state history size
const tipBufferSize = 32

type MockTrieDB struct {
	LastDereference common.Hash
	LastCommit      common.Hash
}

func (t *MockTrieDB) Dereference(root common.Hash) error {
	t.LastDereference = root
	return nil
}

func (t *MockTrieDB) Commit(root common.Hash, _ bool) error {
	t.LastCommit = root
	return nil
}

func (*MockTrieDB) Size() (common.StorageSize, common.StorageSize, common.StorageSize) {
	return 0, 0, 0
}

func (*MockTrieDB) Cap(common.StorageSize) error {
	return nil
}

func TestCappedMemoryTrieWriter(t *testing.T) {
	m := &MockTrieDB{}
	cacheConfig := &CacheConfig{Pruning: true, CommitInterval: 4096, StateHistory: uint64(tipBufferSize)}
	w := NewTrieWriter(m, cacheConfig)
	assert := assert.New(t)
	for i := 0; i < int(cacheConfig.CommitInterval)+1; i++ {
		bigI := big.NewInt(int64(i))
		block := types.NewBlock(
			&types.Header{
				Root:   common.BigToHash(bigI),
				Number: bigI,
			},
			nil, nil, nil, nil,
		)

		assert.NoError(w.InsertTrie(block))
		assert.Equal(common.Hash{}, m.LastDereference, "should not have dereferenced block on insert")
		assert.Equal(common.Hash{}, m.LastCommit, "should not have committed block on insert")
		assert.NoError(w.AcceptTrie(block))
		if i <= tipBufferSize {
			assert.Equal(common.Hash{}, m.LastDereference, "should not have dereferenced block on accept")
		} else {
			assert.Equal(common.BigToHash(big.NewInt(int64(i-tipBufferSize))), m.LastDereference, "should have dereferenced old block on last accept")
			m.LastDereference = common.Hash{}
		}
		if i < int(cacheConfig.CommitInterval) {
			assert.Equal(common.Hash{}, m.LastCommit, "should not have committed block on accept")
		} else {
			assert.Equal(block.Root(), m.LastCommit, "should have committed block after CommitInterval")
			m.LastCommit = common.Hash{}
		}

		assert.NoError(w.RejectTrie(block))
		assert.Equal(block.Root(), m.LastDereference, "should have dereferenced block on reject")
		assert.Equal(common.Hash{}, m.LastCommit, "should not have committed block on reject")
		m.LastDereference = common.Hash{}
	}
}

func TestNoPruningTrieWriter(t *testing.T) {
	m := &MockTrieDB{}
	w := NewTrieWriter(m, &CacheConfig{})
	assert := assert.New(t)
	for i := 0; i < tipBufferSize+1; i++ {
		bigI := big.NewInt(int64(i))
		block := types.NewBlock(
			&types.Header{
				Root:   common.BigToHash(bigI),
				Number: bigI,
			},
			nil, nil, nil, nil,
		)

		assert.NoError(w.InsertTrie(block))
		assert.Equal(common.Hash{}, m.LastDereference, "should not have dereferenced block on insert")
		assert.Equal(common.Hash{}, m.LastCommit, "should not have committed block on insert")

		assert.NoError(w.AcceptTrie(block))
		assert.Equal(common.Hash{}, m.LastDereference, "should not have dereferenced block on accept")
		assert.Equal(block.Root(), m.LastCommit, "should have committed block on accept")
		m.LastCommit = common.Hash{}

		assert.NoError(w.RejectTrie(block))
		assert.Equal(block.Root(), m.LastDereference, "should have dereferenced block on reject")
		assert.Equal(common.Hash{}, m.LastCommit, "should not have committed block on reject")
		m.LastDereference = common.Hash{}
	}
}
