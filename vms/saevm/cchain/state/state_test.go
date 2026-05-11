// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Differential tests asserting byte-for-byte equivalence between the new
// vms/saevm/cchain/state package and the old
// graft/coreth/plugin/evm/atomic/state package. The migration to the new
// code does not run a database rewrite, so the indices retained by the new
// code (atomicTxDB, atomicHeightTxDB, atomicTrieDB, atomicTrieMetaDB) must
// produce the same on-disk bytes as the old code given the same inputs.

package state

import (
	"bytes"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/state"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

type block struct {
	height uint64
	txs    []*tx.Tx
}

// blockSequence covers single-tx and multi-tx blocks at low heights, then
// hits each commit boundary directly so the trie commit and on-disk
// metadata fire without a catch-up. Non-boundary heights can skip; boundary
// heights themselves are part of the sequence.
func blockSequence(t *testing.T) []block {
	t.Helper()
	return []block{
		{1, nil}, // empty Apply: should be a no-op for both tx index and trie.
		{2, []*tx.Tx{newImportTx(0xAA, 1)}},
		{3, []*tx.Tx{newExportTx(0xBB, 2)}},
		{5, []*tx.Tx{newImportTx(0xCC, 3), newExportTx(0xDD, 4)}},
		{7, []*tx.Tx{newImportTx(0xEE, 5)}},
		{8, []*tx.Tx{newExportTx(0xFF, 6)}},
		{10, []*tx.Tx{newImportTx(0x11, 7), newImportTx(0x22, 8)}},
		{12, []*tx.Tx{newExportTx(0x33, 9)}},
		// First commit boundary (4096) is applied directly.
		{commitInterval, []*tx.Tx{newImportTx(0x44, 10)}},
		{commitInterval + 2, []*tx.Tx{newImportTx(0x55, 11)}},
		// Second commit boundary (8192) is applied directly.
		{2 * commitInterval, []*tx.Tx{newExportTx(0x66, 12)}},
		{2*commitInterval + 1, []*tx.Tx{newImportTx(0x77, 13)}},
	}
}

// TestStateDifferentialMatchesOldCoreth drives the same sequence of
// (height, txs) blocks through both the new state package and the old
// coreth atomic state package, then asserts every byte under each retained
// prefix is equal between the two databases.
func TestStateDifferentialMatchesOldCoreth(t *testing.T) {
	newDB, oldUnderlying, oldVDB, newSt, oldDriver := newPair(t)
	for _, b := range blockSequence(t) {
		applyToBoth(t, newSt, oldDriver, oldVDB, b)
		assertPrefixesEqual(t, newDB, oldUnderlying, b.height)
	}
}

// TestStateDifferentialReplayMatchesOldCoreth applies a sequence, drops
// the in-memory state on the new side (re-opens via New), then
// continues applying. Verifies that replay reconstructs the in-memory tip
// such that subsequent Apply calls remain byte-equivalent to the old code.
func TestStateDifferentialReplayMatchesOldCoreth(t *testing.T) {
	newDB, oldUnderlying, oldVDB, newSt, oldDriver := newPair(t)

	seq := blockSequence(t)
	splitAt := len(seq) / 2

	for _, b := range seq[:splitAt] {
		applyToBoth(t, newSt, oldDriver, oldVDB, b)
	}
	assertPrefixesEqual(t, newDB, oldUnderlying, seq[splitAt-1].height)

	// Re-open the new state. The old AtomicBackend is still in-memory; the
	// new one is reconstructed only from disk via replay.
	reopened, err := New(newDB)
	require.NoError(t, err)

	for _, b := range seq[splitAt:] {
		applyToBoth(t, reopened, oldDriver, oldVDB, b)
		assertPrefixesEqual(t, newDB, oldUnderlying, b.height)
	}
}

// oldDriver wraps the old AtomicBackend / AtomicRepository so the test can
// drive the equivalent operations of the new state.Apply.
type oldDriver struct {
	repo      *state.AtomicRepository
	backend   *state.AtomicBackend
	parent    common.Hash
	heightCnt uint64
}

func newPair(t *testing.T) (newDB database.Database, oldUnderlying database.Database, oldVDB *versiondb.Database, newSt *State, drv *oldDriver) {
	t.Helper()
	newDB = memdb.New()
	oldUnderlying = memdb.New()
	oldVDB = versiondb.New(oldUnderlying)

	newSt, err := New(newDB)
	require.NoError(t, err)

	repo, err := state.NewAtomicTxRepository(oldVDB, atomic.Codec, 0)
	require.NoError(t, err)
	backend, err := state.NewAtomicBackend(nil, nil, repo, 0, common.Hash{}, commitInterval)
	require.NoError(t, err)

	drv = &oldDriver{repo: repo, backend: backend}
	return
}

func applyToBoth(t *testing.T, newSt *State, drv *oldDriver, oldVDB *versiondb.Database, b block) {
	t.Helper()

	// New side: single Apply does everything.
	require.NoError(t, newSt.Apply(b.height, b.txs))

	// Old side: convert txs, drive InsertTxs + Repo.Write + AcceptTrie,
	// then commit the versiondb.
	oldTxs := convertNewToOld(t, b.txs)

	// Generate a unique block hash deterministically. drv.heightCnt is used
	// instead of b.height so non-sequential heights still get unique hashes
	// (height bytes alone could collide if a future test reuses heights).
	drv.heightCnt++
	var blockHash common.Hash
	blockHash[0] = byte(drv.heightCnt >> 8)
	blockHash[1] = byte(drv.heightCnt)
	blockHash[31] = byte(b.height)

	root, err := drv.backend.InsertTxs(blockHash, b.height, drv.parent, oldTxs)
	require.NoError(t, err)

	require.NoError(t, drv.repo.Write(b.height, oldTxs))
	_, err = drv.backend.AtomicTrie().AcceptTrie(b.height, root)
	require.NoError(t, err)
	drv.backend.SetLastAccepted(blockHash)
	drv.parent = blockHash

	require.NoError(t, oldVDB.Commit())
}

// retainedPrefixes are the prefixes whose contents must remain
// byte-identical between old and new. They correspond to the four prefixdb
// roots defined in old code at atomic_repository.go:30-34, minus
// atomicRepoMetadataDB which the new code drops.
var retainedPrefixes = []string{
	"atomicTxDB",
	"atomicHeightTxDB",
	"atomicTrieDB",
}

func assertPrefixesEqual(t *testing.T, newDB, oldDB database.Database, height uint64) {
	t.Helper()

	for _, name := range retainedPrefixes {
		newPDB := prefixdb.New([]byte(name), newDB)
		oldPDB := prefixdb.New([]byte(name), oldDB)
		assertDBsEqual(t, newPDB, oldPDB, name, height, nil)
	}

	// atomicTrieMetaDB: the new code adds atomicTrieLastAppliedBlock under
	// this prefix; the old code does not. Filter it out before comparing.
	appliedKey := []byte("atomicTrieLastAppliedBlock")
	newMeta := prefixdb.New([]byte("atomicTrieMetaDB"), newDB)
	oldMeta := prefixdb.New([]byte("atomicTrieMetaDB"), oldDB)
	assertDBsEqual(t, newMeta, oldMeta, "atomicTrieMetaDB", height, func(k []byte) bool {
		return bytes.Equal(k, appliedKey)
	})
}

// assertDBsEqual iterates a and b in lockstep and asserts every (key,value)
// matches. If skipA is non-nil, entries on the a side whose key satisfies
// the predicate are skipped before comparison.
func assertDBsEqual(t *testing.T, a, b database.Iteratee, prefix string, height uint64, skipA func([]byte) bool) {
	t.Helper()

	itA := a.NewIterator()
	defer itA.Release()
	itB := b.NewIterator()
	defer itB.Release()

	advanceA := func() bool {
		for itA.Next() {
			if skipA == nil || !skipA(itA.Key()) {
				return true
			}
		}
		return false
	}

	for {
		hasA := advanceA()
		hasB := itB.Next()
		if !hasA && !hasB {
			break
		}

		require.Equalf(t, hasA, hasB, "prefix=%s height=%d: iterator length mismatch", prefix, height)
		require.Equalf(t, itB.Key(), itA.Key(), "prefix=%s height=%d: key mismatch", prefix, height)
		require.Equalf(t, itB.Value(), itA.Value(), "prefix=%s height=%d key=%x: value mismatch", prefix, height, itA.Key())
	}
	require.NoError(t, itA.Error())
	require.NoError(t, itB.Error())
}

// convertNewToOld serializes each new tx and parses the bytes through the
// old codec to produce an equivalent old atomic.Tx. The codec registration
// order matches between the two packages, so this round-trip preserves
// bytes.
func convertNewToOld(t *testing.T, newTxs []*tx.Tx) []*atomic.Tx {
	t.Helper()
	out := make([]*atomic.Tx, len(newTxs))
	for i, n := range newTxs {
		b, err := n.Bytes()
		require.NoError(t, err)
		oldTx, err := atomic.ExtractAtomicTx(b, atomic.Codec)
		require.NoError(t, err)
		out[i] = oldTx
	}
	return out
}

// newImportTx returns a deterministic Import tx parameterized by a tag byte
// (which makes IDs unique across calls) and nonce.
func newImportTx(tag byte, nonce uint64) *tx.Tx {
	utxoTxID := ids.ID{tag, 0x01}
	assetID := ids.ID{tag, 0x02}
	sourceChain := ids.ID{tag, 0x03}
	addr := common.Address{tag, 0x04}

	return &tx.Tx{
		Unsigned: &tx.Import{
			NetworkID:    1,
			BlockchainID: ids.ID{tag, 0x05},
			SourceChain:  sourceChain,
			ImportedInputs: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{TxID: utxoTxID, OutputIndex: uint32(nonce)},
				Asset:  avax.Asset{ID: assetID},
				In: &secp256k1fx.TransferInput{
					Amt:   1_000_000,
					Input: secp256k1fx.Input{SigIndices: []uint32{0}},
				},
			}},
			Outs: []tx.Output{{Address: addr, Amount: 1_000_000, AssetID: assetID}},
		},
		Creds: []tx.Credential{
			&secp256k1fx.Credential{Sigs: [][65]byte{{}}},
		},
	}
}

// newExportTx returns a deterministic Export tx.
func newExportTx(tag byte, nonce uint64) *tx.Tx {
	assetID := ids.ID{tag, 0x02}
	destChain := ids.ID{tag, 0x06}
	addr := common.Address{tag, 0x07}

	return &tx.Tx{
		Unsigned: &tx.Export{
			NetworkID:        1,
			BlockchainID:     ids.ID{tag, 0x05},
			DestinationChain: destChain,
			Ins: []tx.Input{{
				Address: addr,
				Amount:  500_000,
				AssetID: assetID,
				Nonce:   nonce,
			}},
			ExportedOutputs: []*avax.TransferableOutput{{
				Asset: avax.Asset{ID: assetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 500_000,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{{tag, 0x08}},
					},
				},
			}},
		},
		Creds: []tx.Credential{
			&secp256k1fx.Credential{Sigs: [][65]byte{{}}},
		},
	}
}
