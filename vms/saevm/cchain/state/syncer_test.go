// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
)

// syncBlocks applies blocks to a source SUT, then leaf-syncs the resulting
// atomic trie into a fresh destination SUT over an in-memory p2p network, and
// returns both.
func syncBlocks(t *testing.T, blocks []block) (src, dst *SUT) {
	t.Helper()

	src = newSUT(t)
	src.apply(t, blocks...)
	srcState := src.stateImpl.(*State)

	server := newSyncServer(t, srcState)

	dst = newSUT(t)
	require.NoError(t,
		server.syncInto(t.Context(), dst.stateImpl.(*State), srcState.currentRoot, srcState.CurrentHeight()),
		"Sync()",
	)
	return src, dst
}

// runSyncRoundTrip leaf-syncs blocks into a fresh state and asserts the
// destination trie and shared memory match at the target height.
func runSyncRoundTrip(t *testing.T, blocks []block) {
	t.Helper()

	src, dst := syncBlocks(t, blocks)
	target := src.stateImpl.(*State).currentRoot
	targetHeight := src.CurrentHeight()

	require.Equal(t, targetHeight, dst.CurrentHeight(), "CurrentHeight()")
	gotRoot, err := dst.GetRoot(targetHeight)
	require.NoErrorf(t, err, "GetRoot(%d)", targetHeight)
	require.Equal(t, target, gotRoot, "GetRoot(%d)", targetHeight)
	require.Equal(t, dbEntries(t, src.sharedMemoryDB), dbEntries(t, dst.sharedMemoryDB), "shared memory")
}

// syncServer serves a source state's atomic trie to an in-memory p2p peer.
type syncServer struct {
	net     *p2p.Network
	tracker *p2p.PeerTracker
}

func newSyncServer(t *testing.T, src *State) *syncServer {
	t.Helper()

	net, tracker := synctest.NewSelfNetwork(t, t.Context(), src.snowCtx.NodeID)
	require.NoError(t, RegisterSyncHandler(net, src), "RegisterSyncHandler()")
	return &syncServer{net: net, tracker: tracker}
}

// syncInto runs a syncer that pulls the served trie into dst.
func (s *syncServer) syncInto(ctx context.Context, dst *State, target common.Hash, targetHeight uint64) error {
	syncer := NewSyncer(s.net, s.tracker, dst, target, targetHeight)
	return syncer.Sync(ctx)
}

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

// TestSyncer_RootIndexIsSparse characterizes a known asymmetry between a
// leaf-synced node and one that executed every block. The synced node's atomic
// trie and shared memory are complete and identical to the executed node's — but
// its height->root index (rootKey) is written only for heights that carried
// atomic txs, plus the target height. State.Apply, by contrast, records a marker
// for every height (op-free ones included, where newRoot == oldRoot), so op-free
// interior heights have no marker on a synced node.
//
// This is not a state-correctness bug: nothing on the block verify/accept path
// reads GetRoot. Its only consumer is statesync.SummaryHandler, and the impact
// is a transient, self-healing degradation of a freshly-synced node's ability to
// SERVE summaries — exercised end to end by
// TestGetLastStateSummary_FreshlySyncedNodeServingWindow in the statesync
// package. This test pins the exact shape of the index so that changing it (e.g.
// writing markers densely) is a deliberate, visible decision.
//
// All scenarios apply every height in [1, tip] (some with no txs) so the
// executed source has a dense index; only the synced destination is sparse.
func TestSyncer_RootIndexIsSparse(t *testing.T) {
	var build builder
	tests := []struct {
		name        string
		blocks      []block
		wantMissing []uint64 // heights the synced node fails to serve, today
	}{
		{
			name: "single_gap",
			blocks: []block{
				{height: 1, txs: []*tx.Tx{build.newExport()}},
				{height: 2, txs: nil},
				{height: 3, txs: []*tx.Tx{build.newImport()}},
			},
			wantMissing: []uint64{2},
		},
		{
			name: "consecutive_gaps",
			blocks: []block{
				{height: 1, txs: []*tx.Tx{build.newImport()}},
				{height: 2, txs: nil},
				{height: 3, txs: nil},
				{height: 4, txs: nil},
				{height: 5, txs: []*tx.Tx{build.newExport()}},
			},
			wantMissing: []uint64{2, 3, 4},
		},
		{
			name: "interleaved_gaps",
			blocks: []block{
				{height: 1, txs: []*tx.Tx{build.newExport()}},
				{height: 2, txs: nil},
				{height: 3, txs: nil},
				{height: 4, txs: []*tx.Tx{build.newImport()}},
				{height: 5, txs: []*tx.Tx{build.newExport()}},
				{height: 6, txs: nil},
				{height: 7, txs: []*tx.Tx{build.newImport()}},
			},
			wantMissing: []uint64{2, 3, 6},
		},
		{
			// The tip is always covered by Sync's final marker write, even when
			// it is op-free, so only the interior gap is missing.
			name: "op_free_tip",
			blocks: []block{
				{height: 1, txs: []*tx.Tx{build.newExport()}},
				{height: 2, txs: nil},
				{height: 3, txs: []*tx.Tx{build.newImport()}},
				{height: 4, txs: nil},
			},
			wantMissing: []uint64{2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, dst := syncBlocks(t, tt.blocks)
			tip := src.CurrentHeight()

			// The load-bearing state is complete: same height, byte-identical
			// shared memory, and the target root is present and correct.
			require.Equal(t, tip, dst.CurrentHeight(), "CurrentHeight()")
			require.Equal(t, dbEntries(t, src.sharedMemoryDB), dbEntries(t, dst.sharedMemoryDB), "shared memory")
			srcTip, err := src.GetRoot(tip)
			require.NoErrorf(t, err, "executed node GetRoot(tip=%d)", tip)
			dstTip, err := dst.GetRoot(tip)
			require.NoErrorf(t, err, "synced node GetRoot(tip=%d)", tip)
			require.Equal(t, srcTip, dstTip, "GetRoot(tip)")

			// The only difference is the historical index. Collect the heights
			// the executed node serves but the synced node does not, and where a
			// marker is present assert it matches.
			var missing []uint64
			for h := uint64(0); h <= tip; h++ {
				wantRoot, err := src.GetRoot(h)
				require.NoErrorf(t, err, "executed node GetRoot(%d)", h)

				gotRoot, err := dst.GetRoot(h)
				if errors.Is(err, database.ErrNotFound) {
					missing = append(missing, h)
					continue
				}
				require.NoErrorf(t, err, "synced node GetRoot(%d)", h)
				require.Equalf(t, wantRoot, gotRoot, "GetRoot(%d) value where present", h)
			}

			// The gap is exactly the op-free interior heights; the tip is always
			// covered by Sync's final marker write.
			require.Equalf(t, tt.wantMissing, missing,
				"op-free interior heights absent from the synced index")
		})
	}
}

func TestSyncer_BonusBlock(t *testing.T) {
	const (
		nonBonusHeight uint64 = 102971
		bonusHeight    uint64 = 102972
	)
	require.Containsf(t, bonusBlocks, bonusHeight, "bonusHeight=%d must be a known bonus block", bonusHeight)
	require.NotContainsf(t, bonusBlocks, nonBonusHeight, "nonBonusHeight=%d must not be a known bonus block", nonBonusHeight)

	var build builder
	blocks := []block{
		{height: nonBonusHeight, txs: []*tx.Tx{build.newExport()}},
		{height: bonusHeight, txs: []*tx.Tx{build.newExport()}},
	}

	tests := []struct {
		name       string
		networkID  uint32
		wantBlocks []block // expected in shared memory
	}{
		{
			name:       "mainnet_skips_bonus",
			networkID:  constants.MainnetID,
			wantBlocks: blocks[:1],
		},
		{
			name:       "non_mainnet_applies_bonus",
			networkID:  constants.FujiID,
			wantBlocks: blocks,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			srcSUT := newSUT(t, withNetworkID(test.networkID))
			srcSUT.apply(t, blocks...)
			src := srcSUT.stateImpl.(*State)

			server := newSyncServer(t, src)

			dstSUT := newSUT(t, withNetworkID(test.networkID))
			dst := dstSUT.stateImpl.(*State)
			require.NoError(t, server.syncInto(t.Context(), dst, src.currentRoot, src.CurrentHeight()), "Sync()")

			// The trie always includes the bonus height's leaves.
			require.Equal(t, src.CurrentHeight(), dst.CurrentHeight(), "CurrentHeight()")
			gotRoot, err := dst.GetRoot(bonusHeight)
			require.NoErrorf(t, err, "GetRoot(%d)", bonusHeight)
			require.Equalf(t, src.currentRoot, gotRoot, "GetRoot(%d)", bonusHeight)

			// The synced shared memory matches a node that applied only the
			// non-skipped blocks.
			want := newSUT(t, withNetworkID(test.networkID))
			want.apply(t, test.wantBlocks...)
			require.Equal(t, dbEntries(t, want.sharedMemoryDB), dbEntries(t, dstSUT.sharedMemoryDB), "shared memory")
		})
	}
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
func blocksFromBytes(data []byte, build *builder) []block {
	const (
		maxBlocks    = 8
		maxTxsPerBlk = 4
	)

	r := &byteReader{data: data}
	numBlocks := int(r.next()%maxBlocks) + 1
	blocks := make([]block, numBlocks)

	var height uint64
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
		blocks[i] = block{height: height, txs: txs}
	}

	return blocks
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

	wantDB := saetest.NewFlakyDB(memdb.New(), math.MaxInt)
	require.NoError(t, server.syncInto(t.Context(), newSUT(t, withDB(wantDB)).stateImpl.(*State), target, targetHeight))

	for failAfter := range wantDB.Calls() {
		t.Run(fmt.Sprintf("failAfter_%d", failAfter), func(t *testing.T) {
			db := memdb.New()

			preCrash := newSUT(t, withDB(saetest.NewFlakyDB(db, failAfter)))
			err := server.syncInto(t.Context(), preCrash.stateImpl.(*State), target, targetHeight)
			require.ErrorIs(t, err, saetest.ErrInjected, "Sync()")

			// Clean re-run on the same DB must complete and match the source.
			got := newSUT(t, withDB(db))
			require.NoError(t, server.syncInto(t.Context(), got.stateImpl.(*State), target, targetHeight), "re-run Sync()")

			require.Equal(t, targetHeight, got.CurrentHeight(), "CurrentHeight()")
			require.Equal(t, dbEntries(t, srcSUT.sharedMemoryDB), dbEntries(t, got.sharedMemoryDB), "shared memory")
		})
	}
}
