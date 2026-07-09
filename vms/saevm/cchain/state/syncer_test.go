// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
)

func sync(t *testing.T, srcSUT, dstSUT *SUT) error {
	src := srcSUT.stateImpl.(*State)
	dst := dstSUT.stateImpl.(*State)

	net, tracker := synctest.NewSelfNetwork(t, t.Context(), src.snowCtx.NodeID)
	require.NoError(t, RegisterSyncHandler(net, src), "RegisterSyncHandler()")

	syncer := NewSyncer(net, tracker, dst, src.currentRoot, src.CurrentHeight())
	return syncer.Sync(t.Context())
}

func checkStatesMatch(t *testing.T, wantSUT, gotSUT *SUT, blocks ...block) {
	t.Helper()

	var (
		want = wantSUT.stateImpl.(*State)
		got  = gotSUT.stateImpl.(*State)
	)

	require.Equal(t, want.CurrentHeight(), got.CurrentHeight(), "CurrentHeight()")
	saetest.RequireEqualDBs(t, wantSUT.sharedMemoryDB, gotSUT.sharedMemoryDB, "shared memory")
	require.Equal(t, want.currentRoot, got.currentRoot, "current merkle root")

	for _, b := range blocks {
		if len(b.txs) == 0 {
			// If the block has no transactions, the root is constant.
			continue
		}
		wantRoot, err := want.GetRoot(b.height)
		require.NoError(t, err, "want.GetRoot(%d)", b.height)
		gotRoot, err := got.GetRoot(b.height)
		require.NoError(t, err, "got.GetRoot(%d)", b.height)
		require.Equal(t, wantRoot, gotRoot, "root at height %d", b.height)
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
		name      string
		networkID uint32
	}{
		{
			name:      "mainnet_skips_bonus",
			networkID: constants.MainnetID,
		},
		{
			name:      "non_mainnet_applies_bonus",
			networkID: constants.FujiID,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			src := newSUT(t, withNetworkID(test.networkID))
			src.apply(t, blocks...)

			dst := newSUT(t, withNetworkID(test.networkID))
			require.NoError(t, sync(t, src, dst), "sync()")
			checkStatesMatch(t, src, dst, blocks...)
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

// blocksFromBytes decodes a fuzzer byte stream into blocks.
func blocksFromBytes(data []byte) []block {
	const (
		maxBlocks    = 8
		maxTxsPerBlk = 4
	)

	var (
		build  builder
		height uint64
	)
	r := &byteReader{data: data}
	numBlocks := int(r.next()%maxBlocks) + 1
	blocks := make([]block, numBlocks)

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
	f.Add([]byte{0x00, 0x00, 0x01, 0x00})                   // one block, one import
	f.Add([]byte{0x00, 0x00, 0x01, 0x01})                   // one block, one export
	f.Add([]byte{0x01, 0x00, 0x01, 0x00, 0x00, 0x01, 0x01}) // two blocks, an import then an export

	f.Fuzz(func(t *testing.T, data []byte) {
		blocks := blocksFromBytes(data)

		src := newSUT(t)
		src.apply(t, blocks...)

		dst := newSUT(t)
		require.NoError(t, sync(t, src, dst), "sync()")
		checkStatesMatch(t, src, dst, blocks...)
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

	want := newSUT(t)
	want.apply(t, blocks...)

	wantDB := saetest.NewFlakyDB(memdb.New(), math.MaxInt)
	require.NoError(t, sync(t, want, newSUT(t, withDB(wantDB))), "sync()")

	for failAfter := range wantDB.Calls() {
		t.Run(fmt.Sprintf("failAfter_%d", failAfter), func(t *testing.T) {
			db := memdb.New()

			preCrash := newSUT(t, withDB(saetest.NewFlakyDB(db, failAfter)))
			err := sync(t, want, preCrash)
			require.ErrorIs(t, err, saetest.ErrInjected, "sync()")

			// Clean re-run on the same DB must complete and match the source.
			got := newSUT(t, withDB(db))
			require.NoError(t, sync(t, want, got), "re-run sync()")
			checkStatesMatch(t, want, got, blocks...)
		})
	}
}

// TestSyncer_Stale tries to state sync to an older state, and verifies no
// changes to the [State] are made.
func TestSyncer_Stale(t *testing.T) {
	var build builder
	blocks := []block{
		{height: 1, txs: []*tx.Tx{build.newImport()}},
		{height: 2, txs: []*tx.Tx{build.newExport(), build.newImport()}},
		{height: 4, txs: []*tx.Tx{build.newExport()}},
		{height: 5, txs: []*tx.Tx{build.newImport(), build.newExport()}},
	}

	// stale is a block behind for [sync] to use an old height.
	stale := newSUT(t)
	staleHeight := len(blocks) - 2
	stale.apply(t, blocks[:staleHeight]...)

	want := newSUT(t)
	want.apply(t, blocks...)

	got := newSUT(t)
	got.apply(t, blocks...)
	require.NoError(t, sync(t, stale, got), "sync()")

	// Syncing to earlier state shouldn't corrupt
	checkStatesMatch(t, want, got, blocks...)
}
