// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/trie/trienode"
	"github.com/ava-labs/libevm/triedb"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// AccountDesc describes one account to build into a [StateFixture]. A StorageSize
// above zero attaches a storage trie of that many slots, and equal sizes share a root.
type AccountDesc struct {
	WithCode    bool
	StorageSize int
}

// StorageFixture is one reconstructed storage trie plus the accounts that
// reference it.
type StorageFixture struct {
	Root       common.Hash
	Keys, Vals [][]byte
	Accounts   []common.Hash
}

// StateFixture is a server-side EVM state: an account trie whose accounts may
// carry code and storage, all committed to one TrieDB, with code in CodeDB.
type StateFixture struct {
	TrieDB  *triedb.Database
	CodeDB  ethdb.Database
	Root    common.Hash
	AccKeys [][]byte
	AccVals [][]byte
	Codes   map[common.Hash][]byte
	Storage map[common.Hash]*StorageFixture
}

// NewStateFixture builds an account trie from descs, committing every account,
// storage trie, and code blob so the leaf and code handlers can serve a full sync.
func NewStateFixture(t *testing.T, descs []AccountDesc) *StateFixture {
	t.Helper()

	trieDB, disk := NewTrieDBWithDisk()
	f := &StateFixture{
		TrieDB:  trieDB,
		CodeDB:  disk,
		Codes:   make(map[common.Hash][]byte),
		Storage: make(map[common.Hash]*StorageFixture),
	}

	// Build or reuse storage tries by size so equal sizes share a root.
	bySize := make(map[int]*StorageFixture)

	tr, err := trie.New(trie.TrieID(types.EmptyRootHash), trieDB)
	require.NoError(t, err)

	for i, d := range descs {
		accountHash := AccountKey(uint64(i + 1))

		storageRoot := types.EmptyRootHash
		if d.StorageSize > 0 {
			st, ok := bySize[d.StorageSize]
			if !ok {
				root, keys, vals := FillTrie(t, trieDB, d.StorageSize)
				st = &StorageFixture{Root: root, Keys: keys, Vals: vals}
				bySize[d.StorageSize] = st
				f.Storage[root] = st
			}
			st.Accounts = append(st.Accounts, common.BytesToHash(accountHash))
			storageRoot = st.Root
		}

		codeHash := types.EmptyCodeHash
		if d.WithCode {
			blob := fmt.Appendf(nil, "contract-code-%d", i)
			codeHash = crypto.Keccak256Hash(blob)
			rawdb.WriteCode(disk, codeHash, blob)
			f.Codes[codeHash] = blob
		}

		acc := types.StateAccount{
			Nonce:    uint64(i + 1),
			Balance:  uint256.NewInt(uint64(i+1) * 1000),
			Root:     storageRoot,
			CodeHash: codeHash.Bytes(),
		}
		full, err := types.FullAccountRLP(types.SlimAccountRLP(acc))
		require.NoError(t, err)
		tr.MustUpdate(accountHash, full)
	}

	root, nodes, err := tr.Commit(false)
	require.NoError(t, err)
	require.NoError(t, trieDB.Update(root, types.EmptyRootHash, 0, trienode.NewWithNodeSet(nodes), nil))
	require.NoError(t, trieDB.Commit(root, false))
	f.Root = root

	// Collect the sorted account leaves for reconstruction assertions.
	accTrie, err := trie.New(trie.TrieID(root), trieDB)
	require.NoError(t, err)
	nodeIt, err := accTrie.NodeIterator(nil)
	require.NoError(t, err)
	it := trie.NewIterator(nodeIt)
	for it.Next() {
		f.AccKeys = append(f.AccKeys, common.CopyBytes(it.Key))
		f.AccVals = append(f.AccVals, common.CopyBytes(it.Value))
	}
	require.NoError(t, it.Err)

	return f
}

// AccountKey returns the 32-byte account trie key for the i-th account.
func AccountKey(i uint64) []byte {
	key := make([]byte, common.HashLength)
	binary.BigEndian.PutUint64(key, i)
	return key
}
