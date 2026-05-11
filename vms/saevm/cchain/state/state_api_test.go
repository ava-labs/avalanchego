// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// API-level tests for the State type. These exercise the public methods
// directly, independent of the legacy coreth comparison.

package state

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func newState(t *testing.T) *State {
	t.Helper()
	s, err := New(memdb.New())
	require.NoError(t, err)
	return s
}

// requireTxBytesEqual compares two txs via their canonical bytes. Direct
// reflect.DeepEqual is fragile for txs because [tx.Tx] carries
// implementation-defined unexported state.
func requireTxBytesEqual(t *testing.T, want, got *tx.Tx) {
	t.Helper()
	wantBytes, err := want.Bytes()
	require.NoError(t, err)
	gotBytes, err := got.Bytes()
	require.NoError(t, err)
	require.Equal(t, wantBytes, gotBytes)
}

// sameChainTxs returns n Import txs that all target the same source chain so
// the per-chain merge in [applyTrie] is exercised (different orders produce
// different merged Requests slices).
func sameChainTxs(n int) []*tx.Tx {
	const (
		sourceChainTag byte = 0xA1
		assetTag       byte = 0xA2
		blockchainTag  byte = 0xA3
		addrTag        byte = 0xA4
	)
	sourceChain := ids.ID{sourceChainTag}
	assetID := ids.ID{assetTag}
	blockchainID := ids.ID{blockchainTag}
	addr := common.Address{addrTag}

	txs := make([]*tx.Tx, n)
	for i := range txs {
		// Tag-byte 0 is reserved for the shared addr; use i+1 to keep UTXO IDs
		// (and therefore tx IDs) distinct across the slice.
		utxoTxID := ids.ID{byte(i + 1)}
		txs[i] = &tx.Tx{
			Unsigned: &tx.Import{
				NetworkID:    1,
				BlockchainID: blockchainID,
				SourceChain:  sourceChain,
				ImportedInputs: []*avax.TransferableInput{{
					UTXOID: avax.UTXOID{TxID: utxoTxID},
					Asset:  avax.Asset{ID: assetID},
					In: &secp256k1fx.TransferInput{
						Amt:   1_000_000,
						Input: secp256k1fx.Input{SigIndices: []uint32{0}},
					},
				}},
				Outs: []tx.Output{{Address: addr, Amount: 1_000_000, AssetID: assetID}},
			},
			Creds: []tx.Credential{&secp256k1fx.Credential{Sigs: [][65]byte{{}}}},
		}
	}
	return txs
}

func TestNew_EmptyDB(t *testing.T) {
	require := require.New(t)
	s := newState(t)

	require.Equal(uint64(0), s.LastCommitted())

	root, err := s.GetRoot(0)
	require.NoError(err)
	require.Equal(types.EmptyRootHash, root)
}

func TestApply_EmptyTxs(t *testing.T) {
	require := require.New(t)
	s := newState(t)

	require.NoError(s.Apply(1, nil))
	require.NoError(s.Apply(2, []*tx.Tx{}))

	_, _, err := s.GetTx(ids.ID{0xAA})
	require.ErrorIs(err, database.ErrNotFound)

	_, err = s.GetRoot(1)
	require.ErrorIs(err, database.ErrNotFound)
	_, err = s.GetRoot(2)
	require.ErrorIs(err, database.ErrNotFound)

	require.Equal(uint64(0), s.LastCommitted())
}

func TestApply_GetTxRoundTrip(t *testing.T) {
	require := require.New(t)
	s := newState(t)

	importTx := newImportTx(0xAA, 1)
	exportTx := newExportTx(0xBB, 2)
	require.NoError(s.Apply(5, []*tx.Tx{importTx, exportTx}))

	for _, want := range []*tx.Tx{importTx, exportTx} {
		got, height, err := s.GetTx(want.ID())
		require.NoError(err)
		require.Equal(uint64(5), height)
		requireTxBytesEqual(t, want, got)
	}
}

func TestApply_MultipleHeights(t *testing.T) {
	require := require.New(t)
	s := newState(t)

	// Each Apply pins txs to its height; GetTx must return the original
	// height even after subsequent Applys at higher heights.
	heightOf := map[ids.ID]uint64{}
	for _, b := range []block{
		{2, []*tx.Tx{newImportTx(0x01, 1)}},
		{3, []*tx.Tx{newExportTx(0x02, 2), newImportTx(0x03, 3)}},
		{5, []*tx.Tx{newImportTx(0x04, 4)}},
		{11, []*tx.Tx{newExportTx(0x05, 5)}},
	} {
		require.NoError(s.Apply(b.height, b.txs))
		for _, tx := range b.txs {
			heightOf[tx.ID()] = b.height
		}
	}

	for id, wantHeight := range heightOf {
		_, gotHeight, err := s.GetTx(id)
		require.NoError(err)
		require.Equal(wantHeight, gotHeight)
	}
}

func TestGetTx_NotFound(t *testing.T) {
	require := require.New(t)
	s := newState(t)

	require.NoError(s.Apply(1, []*tx.Tx{newImportTx(0xAA, 1)}))

	_, _, err := s.GetTx(ids.ID{0xFF})
	require.ErrorIs(err, database.ErrNotFound)
}

func TestGetRoot_Genesis(t *testing.T) {
	require := require.New(t)
	s := newState(t)

	root, err := s.GetRoot(0)
	require.NoError(err)
	require.Equal(types.EmptyRootHash, root)
}

func TestGetRoot_NonBoundary(t *testing.T) {
	require := require.New(t)
	s := newState(t)

	require.NoError(s.Apply(5, []*tx.Tx{newImportTx(0xAA, 1)}))

	_, err := s.GetRoot(5)
	require.ErrorIs(err, database.ErrNotFound)
}

func TestGetRoot_CommitBoundary(t *testing.T) {
	require := require.New(t)
	s := newState(t)

	require.NoError(s.Apply(commitInterval, []*tx.Tx{newImportTx(0xAA, 1)}))

	root, err := s.GetRoot(commitInterval)
	require.NoError(err)
	require.NotEqual(types.EmptyRootHash, root)
	require.NotEqual(common.Hash{}, root)
}

func TestLastCommitted_Initial(t *testing.T) {
	s := newState(t)
	require.Equal(t, uint64(0), s.LastCommitted())
}

func TestLastCommitted_NonBoundaryApply(t *testing.T) {
	require := require.New(t)
	s := newState(t)

	require.NoError(s.Apply(1, []*tx.Tx{newImportTx(0xAA, 1)}))
	require.NoError(s.Apply(5, []*tx.Tx{newImportTx(0xBB, 2)}))
	require.Equal(uint64(0), s.LastCommitted())
}

func TestLastCommitted_BoundaryApply(t *testing.T) {
	require := require.New(t)
	s := newState(t)

	require.NoError(s.Apply(commitInterval, []*tx.Tx{newImportTx(0xAA, 1)}))
	require.Equal(uint64(commitInterval), s.LastCommitted())

	// Applying past the boundary does not roll back the marker.
	require.NoError(s.Apply(commitInterval+1, []*tx.Tx{newImportTx(0xBB, 2)}))
	require.Equal(uint64(commitInterval), s.LastCommitted())

	// Crossing the next boundary advances the marker.
	require.NoError(s.Apply(2*commitInterval, []*tx.Tx{newImportTx(0xCC, 3)}))
	require.Equal(uint64(2*commitInterval), s.LastCommitted())
}

func TestApply_DoesNotMutateInput(t *testing.T) {
	require := require.New(t)
	s := newState(t)

	// Build in reverse-sorted-ID order so Apply's internal sort must reorder.
	txs := []*tx.Tx{
		newImportTx(0xFF, 1),
		newImportTx(0x88, 2),
		newImportTx(0x11, 3),
	}
	snapshot := make([]*tx.Tx, len(txs))
	copy(snapshot, txs)

	require.NoError(s.Apply(1, txs))
	require.Equal(snapshot, txs)
}

func TestApply_SortInvariant(t *testing.T) {
	require := require.New(t)

	txs := sameChainTxs(3)

	apply := func(in []*tx.Tx) common.Hash {
		s := newState(t)
		require.NoError(s.Apply(commitInterval, in))
		root, err := s.GetRoot(commitInterval)
		require.NoError(err)
		return root
	}

	forward := apply(txs)
	reversed := apply([]*tx.Tx{txs[2], txs[1], txs[0]})
	rotated := apply([]*tx.Tx{txs[1], txs[2], txs[0]})

	require.Equal(forward, reversed)
	require.Equal(forward, rotated)
	require.NotEqual(types.EmptyRootHash, forward)
}

func TestNew_ReplayPreservesState(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	s, err := New(db)
	require.NoError(err)

	seq := blockSequence(t)
	for _, b := range seq {
		require.NoError(s.Apply(b.height, b.txs))
	}
	wantLastCommitted := s.LastCommitted()
	wantRoot := s.currentRoot

	// Discard the in-memory state and rebuild from disk.
	reopened, err := New(db)
	require.NoError(err)

	require.Equal(wantLastCommitted, reopened.LastCommitted())
	require.Equal(wantRoot, reopened.currentRoot)

	// Every previously-applied tx is retrievable with its original height.
	for _, b := range seq {
		for _, want := range b.txs {
			got, height, err := reopened.GetTx(want.ID())
			require.NoError(err)
			require.Equal(b.height, height)
			requireTxBytesEqual(t, want, got)
		}
	}

	// Subsequent Apply on the reopened state must extend the same trie tip:
	// applying at the next commit boundary should advance the last-committed
	// marker and produce a retrievable tx.
	extra := newImportTx(0x99, 999)
	nextBoundary := (wantLastCommitted/commitInterval + 1) * commitInterval
	require.NoError(reopened.Apply(nextBoundary, []*tx.Tx{extra}))
	require.Equal(nextBoundary, reopened.LastCommitted())

	gotExtra, h, err := reopened.GetTx(extra.ID())
	require.NoError(err)
	require.Equal(nextBoundary, h)
	requireTxBytesEqual(t, extra, gotExtra)
}
