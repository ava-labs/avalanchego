// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"bytes"
	"context"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"
)

type fakeCodeQueue struct {
	hashes []common.Hash
}

func (f *fakeCodeQueue) AddCode(_ context.Context, hashes []common.Hash) error {
	f.hashes = append(f.hashes, hashes...)
	return nil
}

type registered struct {
	root, account common.Hash
}

type fakeRegistry struct {
	tries []registered
}

func (f *fakeRegistry) RegisterStorageTrie(root, account common.Hash) error {
	f.tries = append(f.tries, registered{root, account})
	return nil
}

func accountLeaf(t *testing.T, storageRoot, codeHash common.Hash) []byte {
	t.Helper()
	acc := types.StateAccount{
		Nonce:    1,
		Balance:  uint256.NewInt(1000),
		Root:     storageRoot,
		CodeHash: codeHash.Bytes(),
	}
	val, err := rlp.EncodeToBytes(&acc)
	require.NoError(t, err)
	return val
}

// TestAccountLeaves_DecodesAndDiscovers checks a snapshot is written per leaf and only non-empty storage roots and code hashes are discovered.
func TestAccountLeaves_DecodesAndDiscovers(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	queue := &fakeCodeQueue{}
	reg := &fakeRegistry{}
	leaves := newAccountLeaves(db, queue, reg)

	storageRoot := common.HexToHash("0xaa")
	codeHash := common.HexToHash("0xbb")

	keys := [][]byte{synctest.AccountKey(1), synctest.AccountKey(2), synctest.AccountKey(3)}
	vals := [][]byte{
		accountLeaf(t, types.EmptyRootHash, types.EmptyCodeHash), // plain account
		accountLeaf(t, storageRoot, types.EmptyCodeHash),         // storage only
		accountLeaf(t, types.EmptyRootHash, codeHash),            // code only
	}

	batch := db.NewBatch()
	require.NoError(t, leaves.writeLeaves(t.Context(), batch, keys, vals))

	// Only the non-empty storage root is registered, keyed to its account.
	require.Len(t, reg.tries, 1)
	require.Equal(t, storageRoot, reg.tries[0].root)
	require.Equal(t, common.BytesToHash(keys[1]), reg.tries[0].account)
	// Only the non-empty code hash is enqueued.
	require.Equal(t, []common.Hash{codeHash}, queue.hashes)

	// Snapshots are buffered in the segment batch until it is written.
	require.Nil(t, rawdb.ReadAccountSnapshot(db, common.BytesToHash(keys[0])))
	require.NoError(t, batch.Write())
	for _, k := range keys {
		require.NotNil(t, rawdb.ReadAccountSnapshot(db, common.BytesToHash(k)))
	}
}

// TestAccountLeaves_RejectsMalformedAccount checks an undecodable account leaf errors and discovers nothing.
func TestAccountLeaves_RejectsMalformedAccount(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	queue := &fakeCodeQueue{}
	reg := &fakeRegistry{}
	leaves := newAccountLeaves(db, queue, reg)

	err := leaves.writeLeaves(t.Context(), db.NewBatch(), [][]byte{synctest.AccountKey(1)}, [][]byte{[]byte("not-a-valid-rlp-account")})
	require.ErrorIs(t, err, errDecodeAccount)
	require.Empty(t, reg.tries, "a malformed account must register no storage trie")
	require.Empty(t, queue.hashes, "a malformed account must enqueue no code")
}

func TestAccountLeafIterator(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	hashes := []common.Hash{common.HexToHash("0x11"), common.HexToHash("0x33"), common.HexToHash("0x55")}
	accounts := make(map[common.Hash]types.StateAccount, len(hashes))
	for i, h := range hashes {
		acc := types.StateAccount{
			Nonce:    uint64(i + 1),
			Balance:  uint256.NewInt(uint64(i+1) * 100),
			Root:     types.EmptyRootHash,
			CodeHash: types.EmptyCodeHash.Bytes(),
		}
		accounts[h] = acc
		rawdb.WriteAccountSnapshot(db, h, types.SlimAccountRLP(acc))
	}

	// Full walk yields sorted keys and full-RLP values that decode back.
	it := newAccountLeafIterator(db, common.Hash{})
	defer it.Release()
	var got []common.Hash
	for it.Next() {
		key := common.BytesToHash(it.Key())
		got = append(got, key)

		var decoded types.StateAccount
		require.NoError(t, rlp.DecodeBytes(it.Value(), &decoded))
		require.Equal(t, accounts[key].Nonce, decoded.Nonce)
		require.Equal(t, accounts[key].Balance, decoded.Balance)
	}
	require.NoError(t, it.Error())
	require.Equal(t, hashes, got, "keys must come back sorted ascending")

	// Seek skips everything strictly before the seek key.
	seeked := newAccountLeafIterator(db, hashes[1])
	defer seeked.Release()
	require.True(t, seeked.Next())
	require.Equal(t, hashes[1], common.BytesToHash(seeked.Key()))
}

func TestStorageLeafIterator(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	account := common.HexToHash("0xaa")
	other := common.HexToHash("0xbb")

	slots := []common.Hash{common.HexToHash("0x01"), common.HexToHash("0x02"), common.HexToHash("0x03")}
	vals := map[common.Hash][]byte{}
	for i, s := range slots {
		v := bytes.Repeat([]byte{byte(i + 1)}, 8)
		vals[s] = v
		rawdb.WriteStorageSnapshot(db, account, s, v)
	}
	// A different account's storage must not leak into the iteration.
	rawdb.WriteStorageSnapshot(db, other, common.HexToHash("0x09"), []byte("other"))

	it := newStorageLeafIterator(db, account, common.Hash{})
	defer it.Release()
	var got []common.Hash
	for it.Next() {
		key := common.BytesToHash(it.Key())
		got = append(got, key)
		require.Equal(t, vals[key], it.Value())
	}
	require.NoError(t, it.Error())
	require.Equal(t, slots, got, "only this account's slots, sorted")

	seeked := newStorageLeafIterator(db, account, slots[2])
	defer seeked.Release()
	require.True(t, seeked.Next())
	require.Equal(t, slots[2], common.BytesToHash(seeked.Key()))
	require.False(t, seeked.Next(), "no slots past the last")
}
