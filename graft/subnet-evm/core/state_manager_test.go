// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"
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
	for i := 0; i < int(cacheConfig.CommitInterval)+1; i++ {
		bigI := big.NewInt(int64(i))
		block := types.NewBlock(
			&types.Header{
				Root:   common.BigToHash(bigI),
				Number: bigI,
			},
			nil, nil, nil, nil,
		)

		require.NoError(t, w.InsertTrie(block))
		require.Zero(t, m.LastDereference, "should not have dereferenced block on insert")
		require.Zero(t, m.LastCommit, "should not have committed block on insert")
		require.NoError(t, w.AcceptTrie(block))

		if i <= tipBufferSize {
			require.Zero(t, m.LastDereference, "should not have dereferenced block on accept")
		} else {
			require.Equal(t, common.BigToHash(big.NewInt(int64(i-tipBufferSize))), m.LastDereference, "should have dereferenced old block on last accept")
			m.LastDereference = common.Hash{}
		}
		if i < int(cacheConfig.CommitInterval) {
			require.Zero(t, m.LastCommit, "should not have committed block on accept")
		} else {
			require.Equal(t, block.Root(), m.LastCommit, "should have committed block after CommitInterval")
			m.LastCommit = common.Hash{}
		}

		require.NoError(t, w.RejectTrie(block))
		require.Equal(t, block.Root(), m.LastDereference, "should have dereferenced block on reject")
		require.Zero(t, m.LastCommit, "should not have committed block on reject")
		m.LastDereference = common.Hash{}
	}
}

func TestNoPruningTrieWriter(t *testing.T) {
	m := &MockTrieDB{}
	w := NewTrieWriter(m, &CacheConfig{})
	for i := 0; i < tipBufferSize+1; i++ {
		bigI := big.NewInt(int64(i))
		block := types.NewBlock(
			&types.Header{
				Root:   common.BigToHash(bigI),
				Number: bigI,
			},
			nil, nil, nil, nil,
		)

		require.NoError(t, w.InsertTrie(block))
		require.Zero(t, m.LastDereference, "should not have dereferenced block on insert")
		require.Zero(t, m.LastCommit, "should not have committed block on insert")

		require.NoError(t, w.AcceptTrie(block))
		require.Zero(t, m.LastDereference, "should not have dereferenced block on accept")
		require.Equal(t, block.Root(), m.LastCommit, "should have committed block on accept")
		m.LastCommit = common.Hash{}

		require.NoError(t, w.RejectTrie(block))
		require.Equal(t, block.Root(), m.LastDereference, "should have dereferenced block on reject")
		require.Zero(t, m.LastCommit, "should not have committed block on reject")
		m.LastDereference = common.Hash{}
	}
}
