// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"
	"math"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
)

// runSyncRoundTrip applies blocks to a source state, then leaf-syncs the
// resulting atomic trie into a fresh destination state over an in-memory p2p
// network and asserts the destination trie, shared memory, and synced tip all
// match the source.
func runSyncRoundTrip(t *testing.T, blocks []block) {
	t.Helper()

	// The default SUT is backed by *State, so we can reach the unexported
	// trieDB/currentRoot the syncer operates on.
	srcSUT := newSUT(t)
	srcSUT.apply(t, blocks...)
	src := srcSUT.stateImpl.(*State)

	target := src.currentRoot
	targetHeight := src.CurrentHeight()

	// Serve the source trie to a single in-memory peer at the leaf-request
	// handler ID that the syncer's getter dials.
	server := newSyncServer(t, src)

	dstSUT := newSUT(t)
	dst := dstSUT.stateImpl.(*State)
	require.NoError(t, server.syncInto(t, dst, target, targetHeight), "Sync()")

	// The synced tip matches the source.
	require.Equal(t, targetHeight, dst.CurrentHeight(), "CurrentHeight()")
	gotRoot, err := dst.GetRoot(targetHeight)
	require.NoErrorf(t, err, "GetRoot(%d)", targetHeight)
	require.Equal(t, target, gotRoot, "GetRoot(%d)", targetHeight)

	// Shared memory was applied identically to the source.
	require.Equal(t, dbEntries(t, srcSUT.sharedMemoryDB), dbEntries(t, dstSUT.sharedMemoryDB), "shared memory")

	// Each height that carried atomic ops committed the same historical root as
	// the source: because the trie is append-only and leaves stream in height
	// order, the syncer reproduces the per-height root that Apply produced.
	for _, b := range blocks {
		if len(b.txs) == 0 {
			continue
		}
		want, err := src.GetRoot(b.height)
		require.NoErrorf(t, err, "src.GetRoot(%d)", b.height)
		got, err := dst.GetRoot(b.height)
		require.NoErrorf(t, err, "dst.GetRoot(%d)", b.height)
		require.Equalf(t, want, got, "GetRoot(%d)", b.height)
	}
}

// syncServer serves a source state's atomic trie to an in-memory p2p peer.
type syncServer struct {
	net     *p2p.Network
	tracker *p2p.PeerTracker
	log     logging.Logger
}

func newSyncServer(t *testing.T, src *State) *syncServer {
	t.Helper()

	log := loggingtest.New(t, logging.Debug)
	net, tracker := synctest.NewLoopbackNetwork(
		t,
		p2p.EVMAtomicLeafRequestHandlerID,
		NewSyncHandler(src, log),
	)
	return &syncServer{net: net, tracker: tracker, log: log}
}

// syncInto runs a syncer that pulls the served trie into dst.
func (s *syncServer) syncInto(t *testing.T, dst *State, target common.Hash, targetHeight uint64) error {
	t.Helper()

	syncer, err := NewSyncer(s.net, s.tracker, dst, s.log, target, targetHeight)
	require.NoError(t, err, "NewSyncer()")
	return syncer.Sync(t.Context())
}

type byteReader struct {
	data []byte
	pos  int
}

func (r *byteReader) next() byte {
	if r.pos >= len(r.data) {
		return 0
	}
	b := r.data[r.pos]
	r.pos++
	return b
}

// blocksFromBytes decodes a fuzzer byte stream into blocks. The first byte
// picks the number of blocks; subsequent bytes pick, per block, the height gap,
// the number of txs, and whether each tx is an import (even) or export (odd).
// At least one tx is produced so the resulting root is non-empty.
func blocksFromBytes(data []byte, build *builder) []block {
	const (
		maxBlocks    = 8
		maxTxsPerBlk = 4
	)

	r := &byteReader{data: data}
	numBlocks := int(r.next()%maxBlocks) + 1
	blocks := make([]block, numBlocks)

	var height uint64
	total := 0
	for i := range numBlocks {
		height += uint64(r.next()) + 1 // strictly increasing, gaps allowed
		numTxs := int(r.next() % (maxTxsPerBlk + 1))

		txs := make([]*tx.Tx, numTxs)
		for j := range txs {
			if r.next()%2 == 0 {
				txs[j] = build.newImport()
			} else {
				txs[j] = build.newExport()
			}
		}
		total += numTxs
		blocks[i] = block{height: height, txs: txs}
	}

	return blocks
}

// TestSyncer_RoundTrip verifies the synced trie matches its source across a
// handful of representative block layouts.
func TestSyncer_RoundTrip(t *testing.T) {
	var build builder
	tests := []struct {
		name   string
		blocks []block
	}{
		{
			name:   "single_import",
			blocks: []block{{height: 1, txs: []*tx.Tx{build.newImport()}}},
		},
		{
			name:   "single_export",
			blocks: []block{{height: 1, txs: []*tx.Tx{build.newExport()}}},
		},
		{
			name: "mixed",
			blocks: []block{
				{height: 1, txs: []*tx.Tx{build.newImport(), build.newExport()}},
				{height: 3, txs: nil},
				{height: 5, txs: []*tx.Tx{build.newExport(), build.newImport()}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runSyncRoundTrip(t, tt.blocks)
		})
	}
}

// FuzzSyncer fuzzes the number of blocks and the import/export layout within
// them, verifying each synced trie matches its source.
func FuzzSyncer(f *testing.F) {
	f.Add([]byte{0x00})                         // one block, one import
	f.Add([]byte{0x00, 0x01, 0x01, 0x00})       // one block, one export
	f.Add([]byte{0x02, 0x01, 0x02, 0x00, 0x01}) // two blocks, an import and an export

	f.Fuzz(func(t *testing.T, data []byte) {
		var build builder
		runSyncRoundTrip(t, blocksFromBytes(data, &build))
	})
}

// TestSyncer_Crash verifies that a crash at any point during a sync is safe: a
// clean re-run against the same DB (as the engine does after a restart)
// completes and yields exactly the source's shared memory, never double-applying
// the non-idempotent shared memory ops.
func TestSyncer_Crash(t *testing.T) {
	var build builder
	blocks := []block{
		{height: 1, txs: []*tx.Tx{build.newImport()}},
		{height: 2, txs: []*tx.Tx{build.newExport(), build.newImport()}},
		{height: 4, txs: []*tx.Tx{build.newExport()}},
		{height: 5, txs: []*tx.Tx{build.newImport(), build.newExport()}},
	}

	srcSUT := newSUT(t)
	srcSUT.apply(t, blocks...)
	src := srcSUT.stateImpl.(*State)
	target := src.currentRoot
	targetHeight := src.CurrentHeight()

	server := newSyncServer(t, src)

	wantDB := newFlakyDB(memdb.New(), math.MaxInt)
	require.NoError(t, server.syncInto(t, newSUT(t, withDB(wantDB)).stateImpl.(*State), target, targetHeight))

	for failAfter := range wantDB.calls {
		t.Run(fmt.Sprintf("failAfter_%d", failAfter), func(t *testing.T) {
			db := memdb.New()

			// Crash run: either hits the injected fault or (rarely) completes.
			preCrash := newSUT(t, withDB(newFlakyDB(db, failAfter)))
			err := server.syncInto(t, preCrash.stateImpl.(*State), target, targetHeight)
			require.ErrorIs(t, err, errInjected, "Sync()")

			// Clean re-run on the same DB must complete and match the source.
			got := newSUT(t, withDB(db))
			require.NoError(t, server.syncInto(t, got.stateImpl.(*State), target, targetHeight), "re-run Sync()")

			require.Equal(t, targetHeight, got.CurrentHeight(), "CurrentHeight()")
			require.Equal(t, dbEntries(t, srcSUT.sharedMemoryDB), dbEntries(t, got.sharedMemoryDB), "shared memory")
		})
	}
}
