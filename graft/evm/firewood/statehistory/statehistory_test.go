// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statehistory

import (
	"testing"

	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/rlp"

	"github.com/ava-labs/avalanchego/database/memdb"
)

var (
	testAddr       = common.HexToAddress("0x000000000000000000000000000000000000dEaD")
	testHashedAddr = crypto.Keccak256Hash(testAddr.Bytes())
	testSlot       = common.HexToHash("0x01")
	testHashedSlot = crypto.Keccak256Hash(testSlot.Bytes())
)

func storageOpKey(hashedAddr, hashedSlot common.Hash) []byte {
	return append(append([]byte(nil), hashedAddr[:]...), hashedSlot[:]...)
}

func putStorageOp(value []byte) Op {
	return Op{Kind: OpPut, Key: storageOpKey(testHashedAddr, testHashedSlot), Value: value}
}

func encodeAccount(t *testing.T, acc *types.StateAccount) []byte {
	t.Helper()
	enc, err := rlp.EncodeToBytes(acc)
	require.NoError(t, err)
	return enc
}

// flushThrough flushes empty blocks to advance head contiguously, applying
// ops at their designated block.
func flushThrough(t *testing.T, s *Store, opsByBlock map[uint64][]Op, through uint64) {
	t.Helper()
	head := uint64(0)
	if h, ok, err := s.Head(); err == nil && ok {
		head = h + 1
	}
	for b := head; b <= through; b++ {
		require.NoError(t, s.Flush(b, opsByBlock[b]))
	}
}

func TestStorageReverseSeek(t *testing.T) {
	require := require.New(t)
	s := New(memdb.New())

	flushThrough(t, s, map[uint64][]Op{
		5:   {putStorageOp([]byte{0x05})},
		20:  {putStorageOp([]byte{0x20})},
		100: {putStorageOp([]byte{0x99})},
	}, 100)

	cases := []struct {
		target uint64
		want   []byte
	}{
		{3, nil}, // before first write -> zero
		{5, []byte{0x05}},
		{19, []byte{0x05}},
		{20, []byte{0x20}},
		{50, []byte{0x20}},
		{100, []byte{0x99}},
		{1000, []byte{0x99}},
	}
	for _, c := range cases {
		got, err := s.StorageAt(testHashedAddr, testHashedSlot, c.target)
		require.NoError(err)
		require.Equal(c.want, got, "target=%d", c.target)
	}
}

func TestStorageDeleteTombstone(t *testing.T) {
	require := require.New(t)
	s := New(memdb.New())

	flushThrough(t, s, map[uint64][]Op{
		2: {putStorageOp([]byte{0x02})},
		4: {{Kind: OpDelete, Key: storageOpKey(testHashedAddr, testHashedSlot)}},
	}, 4)

	got, err := s.StorageAt(testHashedAddr, testHashedSlot, 3)
	require.NoError(err)
	require.Equal([]byte{0x02}, got)

	got, err = s.StorageAt(testHashedAddr, testHashedSlot, 4)
	require.NoError(err)
	require.Nil(got)
}

func TestStorageDestructClearsSlot(t *testing.T) {
	require := require.New(t)
	s := New(memdb.New())

	flushThrough(t, s, map[uint64][]Op{
		5:  {putStorageOp([]byte{0x05})},
		10: {{Kind: OpDestruct, Key: testHashedAddr[:]}},
		15: {putStorageOp([]byte{0x15})},
	}, 15)

	// Before destruct: original value. At/after destruct: cleared to zero.
	got, err := s.StorageAt(testHashedAddr, testHashedSlot, 7)
	require.NoError(err)
	require.Equal([]byte{0x05}, got)

	got, err = s.StorageAt(testHashedAddr, testHashedSlot, 12)
	require.NoError(err)
	require.Nil(got)

	// Re-created and re-written at block 15: visible again afterwards.
	got, err = s.StorageAt(testHashedAddr, testHashedSlot, 16)
	require.NoError(err)
	require.Equal([]byte{0x15}, got)
}

func TestStorageDestructAndRewriteSameBlock(t *testing.T) {
	require := require.New(t)
	s := New(memdb.New())

	// Destruct precedes the re-creation writes within the same block (the
	// same order Firewood batch ops carry them): the new value wins.
	flushThrough(t, s, map[uint64][]Op{
		3: {putStorageOp([]byte{0x03})},
		7: {
			{Kind: OpDestruct, Key: testHashedAddr[:]},
			putStorageOp([]byte{0x77}),
		},
	}, 7)

	got, err := s.StorageAt(testHashedAddr, testHashedSlot, 7)
	require.NoError(err)
	require.Equal([]byte{0x77}, got)
}

func TestAccountHistory(t *testing.T) {
	require := require.New(t)
	s := New(memdb.New())

	acc := &types.StateAccount{
		Nonce:    7,
		Balance:  uint256.NewInt(1000),
		Root:     types.EmptyRootHash,
		CodeHash: types.EmptyCodeHash[:],
	}

	flushThrough(t, s, map[uint64][]Op{
		5:  {{Kind: OpPut, Key: testHashedAddr[:], Value: encodeAccount(t, acc)}},
		10: {{Kind: OpDestruct, Key: testHashedAddr[:]}},
	}, 10)

	got, err := s.AccountAt(testHashedAddr, 7)
	require.NoError(err)
	require.NotNil(got)
	require.Equal(uint64(7), got.Nonce)
	require.Equal(uint256.NewInt(1000), got.Balance)

	got, err = s.AccountAt(testHashedAddr, 3)
	require.NoError(err)
	require.Nil(got) // before first account write

	got, err = s.AccountAt(testHashedAddr, 12)
	require.NoError(err)
	require.Nil(got) // destroyed
}

func TestWatermarksAndIdempotentReflush(t *testing.T) {
	require := require.New(t)
	s := New(memdb.New())

	_, ok, err := s.FirstBlock()
	require.NoError(err)
	require.False(ok)

	flushThrough(t, s, map[uint64][]Op{
		2: {putStorageOp([]byte{0x02})},
	}, 4)

	first, ok, err := s.FirstBlock()
	require.NoError(err)
	require.True(ok)
	require.Zero(first)

	head, ok, err := s.Head()
	require.NoError(err)
	require.True(ok)
	require.Equal(uint64(4), head)

	// Crash replay rewrites already-captured blocks idempotently and never
	// moves head backward.
	require.NoError(s.Flush(2, []Op{putStorageOp([]byte{0x02})}))
	head, _, err = s.Head()
	require.NoError(err)
	require.Equal(uint64(4), head)

	got, err := s.StorageAt(testHashedAddr, testHashedSlot, 3)
	require.NoError(err)
	require.Equal([]byte{0x02}, got)
}

func TestFlushRejectsMalformedOps(t *testing.T) {
	require := require.New(t)
	s := New(memdb.New())

	require.Error(s.Flush(0, []Op{{Kind: OpDestruct, Key: storageOpKey(testHashedAddr, testHashedSlot)}}))
	require.Error(s.Flush(0, []Op{{Kind: OpKind(0xff), Key: testHashedAddr[:]}}))
}

func TestOverlayReadOnlyAndLoudErrors(t *testing.T) {
	require := require.New(t)
	s := New(memdb.New())

	targetRoot := common.HexToHash("0xabc")
	overlay := NewOverlay(s, nil, 5, targetRoot)
	tr, err := overlay.OpenTrie(targetRoot)
	require.NoError(err)

	require.Equal(targetRoot, tr.Hash())
	require.Nil(tr.GetKey([]byte{1}))

	require.ErrorIs(tr.UpdateAccount(testAddr, &types.StateAccount{}), errReadOnly)
	require.ErrorIs(tr.UpdateStorage(testAddr, testSlot[:], nil), errReadOnly)
	require.ErrorIs(tr.DeleteAccount(testAddr), errReadOnly)
	require.ErrorIs(tr.DeleteStorage(testAddr, testSlot[:]), errReadOnly)
	_, _, err = tr.Commit(false)
	require.ErrorIs(err, errReadOnly)

	_, err = tr.NodeIterator(nil)
	require.ErrorIs(err, errNoHistoryView)
	require.ErrorIs(tr.Prove(nil, nil), errNoHistoryView)
}

func TestOverlayReads(t *testing.T) {
	require := require.New(t)
	s := New(memdb.New())

	acc := &types.StateAccount{
		Nonce:    1,
		Balance:  uint256.NewInt(42),
		Root:     types.EmptyRootHash,
		CodeHash: types.EmptyCodeHash[:],
	}
	storageValue := []byte{0x77}
	encValue, err := rlp.EncodeToBytes(storageValue)
	require.NoError(err)

	flushThrough(t, s, map[uint64][]Op{
		5: {
			{Kind: OpPut, Key: testHashedAddr[:], Value: encodeAccount(t, acc)},
			{Kind: OpPut, Key: storageOpKey(testHashedAddr, testHashedSlot), Value: encValue},
		},
	}, 5)

	overlay := NewOverlay(s, nil, 5, common.HexToHash("0xabc"))
	tr, err := overlay.OpenTrie(common.HexToHash("0xabc"))
	require.NoError(err)

	gotAcc, err := tr.GetAccount(testAddr)
	require.NoError(err)
	require.NotNil(gotAcc)
	require.Equal(uint256.NewInt(42), gotAcc.Balance)

	gotStorage, err := tr.GetStorage(testAddr, testSlot[:])
	require.NoError(err)
	require.Equal(storageValue, gotStorage)

	// A view below the write sees nothing.
	overlayBefore := NewOverlay(s, nil, 4, common.HexToHash("0xdef"))
	trBefore, err := overlayBefore.OpenTrie(common.HexToHash("0xdef"))
	require.NoError(err)

	gotAcc, err = trBefore.GetAccount(testAddr)
	require.NoError(err)
	require.Nil(gotAcc)

	gotStorage, err = trBefore.GetStorage(testAddr, testSlot[:])
	require.NoError(err)
	require.Nil(gotStorage)
}
