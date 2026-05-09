// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Differential tests asserting byte-for-byte equivalence between the new
// vms/saevm/cchain/state package and the old
// graft/coreth/plugin/evm/atomic/state package. The migration to the new
// code does not run a database rewrite, so the indices retained by the new
// code (atomicTxDB, atomicHeightTxDB, atomicTrieDB, atomicTrieMetaDB) must
// produce the same on-disk bytes as the old code given the same inputs.

package state_test

import (
	"bytes"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/state"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	chainsatomic "github.com/ava-labs/avalanchego/chains/atomic"
	oldatomic "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	oldstate "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/state"
)

// commitInterval is small so the test exercises the catch-up commit loop and
// at least one disk commit without needing thousands of blocks.
const testCommitInterval uint64 = 4

// TestStateDifferentialMatchesOldCoreth drives the same sequence of
// (height, txs) blocks through both the new state package and the old coreth
// atomic state package, then asserts every byte under each retained prefix
// is equal between the two databases.
func TestStateDifferentialMatchesOldCoreth(t *testing.T) {
	type block struct {
		height uint64
		txs    []*tx.Tx
	}

	// Sequence is designed to hit:
	//   - empty blocks
	//   - single-tx blocks (Import, Export)
	//   - multi-tx block
	//   - exactly the commit boundary (height == 4)
	//   - an off-boundary height (height == 5)
	//   - a boundary at height == 8
	blocks := []block{
		{1, nil},
		{2, []*tx.Tx{newImportTx(t, 0xAA, 1)}},
		{3, []*tx.Tx{newExportTx(t, 0xBB, 2)}},
		{4, nil},
		{5, []*tx.Tx{newImportTx(t, 0xCC, 3), newExportTx(t, 0xDD, 4)}},
		{6, nil},
		{7, []*tx.Tx{newImportTx(t, 0xEE, 5)}},
		{8, []*tx.Tx{newExportTx(t, 0xFF, 6)}},
		// Run past a second commit boundary to verify roots accumulate
		// correctly across multiple commits.
		{9, nil},
		{10, []*tx.Tx{newImportTx(t, 0x11, 7), newImportTx(t, 0x22, 8)}},
		{11, nil},
		{12, []*tx.Tx{newExportTx(t, 0x33, 9)}},
	}

	newDB := memdb.New()
	oldUnderlying := memdb.New()
	oldVDB := versiondb.New(oldUnderlying)

	newSt, err := state.New(newDB, 0, testCommitInterval)
	require.NoError(t, err)

	oldRepo, err := oldstate.NewAtomicTxRepository(oldVDB, oldatomic.Codec, 0)
	require.NoError(t, err)
	oldBackend, err := oldstate.NewAtomicBackend(nil, nil, oldRepo, 0, common.Hash{}, testCommitInterval)
	require.NoError(t, err)

	var lastBlockHash common.Hash
	for _, b := range blocks {
		oldTxs := convertNewToOld(t, b.txs)

		// New side: write tx index + apply ops to trie atomically.
		ops := mergeAtomicRequestsForNew(t, b.txs)
		newBatch := newDB.NewBatch()
		require.NoError(t, newSt.WriteTxs(newBatch, b.height, b.txs))
		_, err := newSt.Apply(newBatch, b.height, ops)
		require.NoError(t, err)
		require.NoError(t, newBatch.Write())

		// Old side: insert into trie via backend, write tx index, accept
		// at the same height to flush at commit boundaries.
		blockHash := common.BigToHash(common.Big1.Lsh(common.Big1, uint(b.height%64))) // unique
		blockHash[31] = byte(b.height)
		root, err := oldBackend.InsertTxs(blockHash, b.height, lastBlockHash, oldTxs)
		require.NoError(t, err)

		require.NoError(t, oldRepo.Write(b.height, oldTxs))
		_, err = oldBackend.AtomicTrie().AcceptTrie(b.height, root)
		require.NoError(t, err)
		oldBackend.SetLastAccepted(blockHash)
		lastBlockHash = blockHash

		require.NoError(t, oldVDB.Commit())

		assertPrefixesEqual(t, newDB, oldUnderlying, b.height)
	}
}

// retainedPrefixes are the database prefixes whose contents must remain
// byte-identical between the old and new implementations. They correspond
// to the four prefixdb roots defined in old code at
// graft/coreth/plugin/evm/atomic/state/atomic_repository.go:30-34.
//
// Not retained:
//   - "atomicRepoMetadataDB" — the new code drops it.
var retainedPrefixes = []string{
	"atomicTxDB",
	"atomicHeightTxDB",
	"atomicTrieDB",
}

// assertPrefixesEqual checks that every key/value under each retained
// prefix matches between newDB and oldDB. The atomicTrieMetaDB prefix is
// checked separately because the new code adds atomicTrieLastAppliedBlock,
// which the old code does not write.
func assertPrefixesEqual(t *testing.T, newDB, oldDB database.Database, height uint64) {
	t.Helper()

	for _, name := range retainedPrefixes {
		newPDB := prefixdb.New([]byte(name), newDB)
		oldPDB := prefixdb.New([]byte(name), oldDB)
		assertDBsEqual(t, newPDB, oldPDB, name, height)
	}

	// atomicTrieMetaDB: new code adds atomicTrieLastAppliedBlock under this
	// prefix, but the old code does not. Filter it out before comparing.
	newMeta := prefixdb.New([]byte("atomicTrieMetaDB"), newDB)
	oldMeta := prefixdb.New([]byte("atomicTrieMetaDB"), oldDB)
	assertMetaEqualSkippingApplied(t, newMeta, oldMeta, height)
}

func assertDBsEqual(t *testing.T, a, b database.Iteratee, prefix string, height uint64) {
	t.Helper()

	itA := a.NewIterator()
	defer itA.Release()
	itB := b.NewIterator()
	defer itB.Release()

	for {
		hasA := itA.Next()
		hasB := itB.Next()
		if !hasA && !hasB {
			break
		}

		require.Equalf(t, hasA, hasB, "prefix=%s height=%d: iterator length mismatch", prefix, height)
		require.Equalf(t, itA.Key(), itB.Key(), "prefix=%s height=%d: key mismatch", prefix, height)
		require.Equalf(t, itA.Value(), itB.Value(), "prefix=%s height=%d key=%x: value mismatch", prefix, height, itA.Key())
	}
	require.NoError(t, itA.Error())
	require.NoError(t, itB.Error())
}

func assertMetaEqualSkippingApplied(t *testing.T, newMeta, oldMeta database.Iteratee, height uint64) {
	t.Helper()

	itNew := newMeta.NewIterator()
	defer itNew.Release()
	itOld := oldMeta.NewIterator()
	defer itOld.Release()

	appliedKey := []byte("atomicTrieLastAppliedBlock")

	advanceNew := func() bool {
		for itNew.Next() {
			if !bytes.Equal(itNew.Key(), appliedKey) {
				return true
			}
		}
		return false
	}

	for {
		hasNew := advanceNew()
		hasOld := itOld.Next()
		if !hasNew && !hasOld {
			break
		}

		require.Equalf(t, hasNew, hasOld, "atomicTrieMetaDB height=%d: iterator length mismatch", height)
		require.Equalf(t, itOld.Key(), itNew.Key(), "atomicTrieMetaDB height=%d: key mismatch", height)
		require.Equalf(t, itOld.Value(), itNew.Value(), "atomicTrieMetaDB height=%d key=%x: value mismatch", height, itOld.Key())
	}
	require.NoError(t, itNew.Error())
	require.NoError(t, itOld.Error())
}

// convertNewToOld serializes each new tx and parses the bytes through the old
// codec to produce an equivalent old atomic.Tx. The codec registration order
// matches between the two packages, so this round-trip preserves bytes.
func convertNewToOld(t *testing.T, newTxs []*tx.Tx) []*oldatomic.Tx {
	t.Helper()
	out := make([]*oldatomic.Tx, len(newTxs))
	for i, n := range newTxs {
		b, err := n.Bytes()
		require.NoError(t, err)
		oldTx, err := oldatomic.ExtractAtomicTx(b, oldatomic.Codec)
		require.NoError(t, err)
		out[i] = oldTx
	}
	return out
}

// mergeAtomicRequestsForNew sorts txs by ID (matching old mergeAtomicOps in
// atomic_backend.go:408) and merges per-tx requests by chainID.
func mergeAtomicRequestsForNew(t *testing.T, txs []*tx.Tx) map[ids.ID]*chainsatomic.Requests {
	t.Helper()
	if len(txs) == 0 {
		return nil
	}

	// Old mergeAtomicOps (atomic_backend.go:408) sorts by tx ID for
	// deterministic merge order; do the same here.
	sorted := make([]*tx.Tx, len(txs))
	copy(sorted, txs)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].ID().Compare(sorted[i].ID()) < 0 {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	out := make(map[ids.ID]*chainsatomic.Requests)
	for _, tx := range sorted {
		chainID, req, err := tx.AtomicRequests()
		require.NoError(t, err)
		if existing, ok := out[chainID]; ok {
			existing.PutRequests = append(existing.PutRequests, req.PutRequests...)
			existing.RemoveRequests = append(existing.RemoveRequests, req.RemoveRequests...)
		} else {
			out[chainID] = req
		}
	}
	return out
}

// newImportTx returns a deterministic Import tx parameterized by tag (which
// makes IDs unique across calls) and nonce.
func newImportTx(t *testing.T, tag byte, nonce uint64) *tx.Tx {
	t.Helper()
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
func newExportTx(t *testing.T, tag byte, nonce uint64) *tx.Tx {
	t.Helper()
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
