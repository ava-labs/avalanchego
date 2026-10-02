// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	chainsatomic "github.com/ava-labs/avalanchego/chains/atomic"
)

// SUT bundles the system under test: a [State] plus both sides of the
// shared-memory pair.
type SUT struct {
	*State

	// db is used for both the chain state and shared memory
	db database.Database
	// sharedMemoryDB contains all the shared memory state.
	sharedMemoryDB *prefixdb.Database
}

func newSUT(tb testing.TB, opts ...sutOption) *SUT {
	tb.Helper()

	props := options.ApplyTo(&sutProperties{
		db:        memdb.New(),
		networkID: constants.UnitTestID,
	}, opts...)

	chainDB := prefixdb.New([]byte("chain"), props.db)
	smDB := prefixdb.New([]byte("shared memory"), props.db)
	mem := chainsatomic.NewMemory(smDB)

	ctx := snowtest.Context(tb, snowtest.CChainID)
	ctx.NetworkID = props.networkID
	ctx.Log = loggingtest.New(tb, logging.Debug)
	ctx.SharedMemory = mem.NewSharedMemory(snowtest.CChainID)

	state, err := New(ctx, chainDB)
	require.NoErrorf(tb, err, "New(%T, %T)", ctx, chainDB)
	tb.Cleanup(func() {
		require.NoErrorf(tb, state.Close(), "%T.Close()", state)
	})
	return &SUT{
		State:          state,
		db:             props.db,
		sharedMemoryDB: smDB,
	}
}

type (
	// A sutOption configures the default SUT properties used by [newSUT].
	sutOption = options.Option[sutProperties]

	sutProperties struct {
		// db is used for both the chain state and shared memory.
		db        database.Database
		networkID uint32
	}
)

// withDB configures the SUT to use the given database.
func withDB(db database.Database) sutOption {
	return options.Func[sutProperties](func(p *sutProperties) {
		p.db = db
	})
}

// withNetworkID configures the SUT's snow context to use the given network ID.
func withNetworkID(networkID uint32) sutOption {
	return options.Func[sutProperties](func(p *sutProperties) {
		p.networkID = networkID
	})
}

// block bundles a height and the txs accepted at it. Tests in this file pass
// blocks to State.Apply one at a time.
type block struct {
	height uint64
	txs    []*tx.Tx
}

// apply calls [SUT.Apply] for each block in order and fails if any call errors.
func (s *SUT) apply(tb testing.TB, blocks ...block) {
	tb.Helper()

	for _, b := range blocks {
		require.NoErrorf(tb, s.Apply(b.height, b.txs), "%T.Apply(%d)", s.State, b.height)
	}
}

func (s *SUT) assertEqual(tb testing.TB, want *SUT) {
	tb.Helper()

	currentHeight := s.CurrentHeight()
	require.Equalf(tb, want.CurrentHeight(), currentHeight, "%T.CurrentHeight()", s.State)

	for h := range currentHeight + 1 {
		wantRoot, err := want.GetRoot(h)
		require.NoErrorf(tb, err, "%T.GetRoot(%d)", want.State, h)

		gotRoot, err := s.GetRoot(h)
		require.NoErrorf(tb, err, "%T.GetRoot(%d)", s.State, h)
		assert.Equalf(tb, wantRoot, gotRoot, "%T.GetRoot(%d)", s.State, h)
	}

	saetest.AssertEqualDBs(tb, want.sharedMemoryDB, s.sharedMemoryDB, "shared memory")
}

func (s *SUT) assertHasTxs(tb testing.TB, blocks []block) {
	tb.Helper()

	for _, b := range blocks {
		for _, want := range b.txs {
			got, height, err := s.GetTx(want.ID())
			require.NoErrorf(tb, err, "%T.GetTx(%d)", s.State, b.height)
			assert.Equalf(tb, b.height, height, "%T.GetTx(%d).Height", s.State, b.height)
			if diff := cmp.Diff(want, got, txtest.CmpOpt()); diff != "" {
				tb.Errorf("%T.GetTx(%d).Tx diff (-want +got):\n%s", s.State, b.height, diff)
			}
		}
	}
}

// TestEmpty verifies the state behavior prior to applying any transactions.
func TestEmpty(t *testing.T) {
	s := newSUT(t)
	require.Zerof(t, s.CurrentHeight(), "%T.CurrentHeight()", s.State)

	_, _, err := s.GetTx(ids.GenerateTestID())
	require.ErrorIsf(t, err, database.ErrNotFound, "%T.GetTx(...)", s.State)

	tests := []struct {
		height  uint64
		want    common.Hash
		wantErr error
	}{
		{height: 0, want: types.EmptyRootHash},
		{height: 1, wantErr: database.ErrNotFound},
	}
	for _, test := range tests {
		root, err := s.GetRoot(test.height)
		require.ErrorIsf(t, err, test.wantErr, "%T.GetRoot(%d)", s.State, test.height)
		assert.Equalf(t, test.want, root, "%T.GetRoot(%d)", s.State, test.height)
	}
}

// builder is a helper for constructing transactions.
type builder struct {
	count uint32
}

// newImport returns a minimal [tx.Import] that consumes a unique input from
// shared memory.
func (b *builder) newImport() *tx.Tx {
	b.count++
	return &tx.Tx{
		Unsigned: &tx.Import{
			SourceChain: snowtest.XChainID,
			ImportedInputs: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					OutputIndex: b.count,
				},
				In: &secp256k1fx.TransferInput{},
			}},
		},
	}
}

// newExport returns a minimal [tx.Export] that produces a unique output into
// shared memory.
func (b *builder) newExport() *tx.Tx {
	b.count++
	return &tx.Tx{
		Unsigned: &tx.Export{
			DestinationChain: snowtest.XChainID,
			ExportedOutputs: []*avax.TransferableOutput{{
				Out: &secp256k1fx.TransferOutput{
					Amt: uint64(b.count),
				},
			}},
		},
	}
}

// TestApply verifies that applying blocks indexes the txs and that the
// resulting state is correctly flushed to disk and can be reloaded from it.
func TestApply(t *testing.T) {
	var build builder
	tests := []struct {
		name   string
		blocks []block
	}{
		{
			name: "empty",
			blocks: []block{
				{height: 1, txs: nil},
				{height: 2, txs: []*tx.Tx{}},
			},
		},
		{
			name: "single_import",
			blocks: []block{
				{height: 1, txs: []*tx.Tx{build.newImport()}},
			},
		},
		{
			name: "single_export",
			blocks: []block{
				{height: 1, txs: []*tx.Tx{build.newExport()}},
			},
		},
		{
			name: "multi_tx_single_height",
			blocks: []block{
				{
					height: 1,
					txs: []*tx.Tx{
						build.newImport(),
						build.newExport(),
						build.newImport(),
					},
				},
			},
		},
		{
			name: "mixed",
			blocks: []block{
				{height: 1, txs: []*tx.Tx{build.newImport()}},
				{height: 2, txs: nil},
				{height: 3, txs: []*tx.Tx{build.newExport(), build.newImport()}},
				{height: 4, txs: nil},
				{height: 5, txs: []*tx.Tx{build.newImport()}},
				{height: 6, txs: []*tx.Tx{build.newExport()}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			s := newSUT(t)
			for i, b := range test.blocks {
				s.apply(t, b)

				// Reopening the SUT verifies that the state is correctly
				// flushed to disk and can be reloaded from it.
				reopened := newSUT(t, withDB(s.db))
				reopened.assertEqual(t, s)
				for _, sut := range []*SUT{s, reopened} {
					sut.assertHasTxs(t, test.blocks[:i+1])
				}
			}
		})
	}
}

// TestApply_BonusBlock verifies that a block's atomic operations are skipped for
// shared memory (but still written to the trie) only when the network is mainnet
// AND the height is a known bonus block.
func TestApply_BonusBlock(t *testing.T) {
	const (
		bonusHeight    uint64 = 102972
		nonBonusHeight uint64 = 102971
	)
	require.Containsf(t, bonusBlocks, bonusHeight, "bonusHeight=%d must be a known bonus block", bonusHeight)
	require.NotContainsf(t, bonusBlocks, nonBonusHeight, "nonBonusHeight=%d must not be a known bonus block", nonBonusHeight)

	tests := []struct {
		name               string
		networkID          uint32
		height             uint64
		wantInSharedMemory bool
	}{
		{
			name:               "mainnet_bonus_height",
			networkID:          constants.MainnetID,
			height:             bonusHeight,
			wantInSharedMemory: false,
		},
		{
			name:               "mainnet_non_bonus_height",
			networkID:          constants.MainnetID,
			height:             nonBonusHeight,
			wantInSharedMemory: true,
		},
		{
			name:               "non_mainnet_bonus_height",
			networkID:          constants.FujiID,
			height:             bonusHeight,
			wantInSharedMemory: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var build builder
			s := newSUT(t, withNetworkID(test.networkID))
			s.apply(t, block{height: test.height, txs: []*tx.Tx{build.newExport()}})

			// The tx is always written to the trie regardless of bonus status.
			root, err := s.GetRoot(test.height)
			require.NoErrorf(t, err, "%T.GetRoot(%d)", s.State, test.height)
			require.NotEqualf(t, types.EmptyRootHash, root, "%T.GetRoot(%d) should be updated", s.State, test.height)

			// Shared memory is only skipped for mainnet bonus blocks.
			it := s.sharedMemoryDB.NewIterator()
			defer it.Release()
			hasSharedMem := it.Next()
			require.NoError(t, it.Error())
			require.Equal(t, test.wantInSharedMemory, hasSharedMem, "shared memory written")
		})
	}
}

func TestApply_BonusBlock_Index(t *testing.T) {
	const (
		bonusHeight    uint64 = 102972
		nonBonusHeight uint64 = 102971
	)
	require.Containsf(t, bonusBlocks, bonusHeight, "bonusHeight=%d must be a known bonus block", bonusHeight)
	require.NotContainsf(t, bonusBlocks, nonBonusHeight, "nonBonusHeight=%d must not be a known bonus block", nonBonusHeight)

	s := newSUT(t, withNetworkID(constants.MainnetID))

	var build builder
	export := build.newExport()
	id := export.ID()

	s.apply(t, block{height: nonBonusHeight, txs: []*tx.Tx{export}})

	got, height, err := s.GetTx(id)
	require.NoErrorf(t, err, "%T.GetTx(%s) non-bonus", s.State, id)
	require.Equalf(t, nonBonusHeight, height, "%T.GetTx(%s) non-bonus height", s.State, id)
	if diff := cmp.Diff(export, got, txtest.CmpOpt()); diff != "" {
		t.Errorf("%T.GetTx(%d) non-bonus Tx diff (-want +got):\n%s", s.State, nonBonusHeight, diff)
	}

	// Apply same tx at bonus height and verify it is retrievable.
	s.apply(t, block{height: bonusHeight, txs: []*tx.Tx{export}})
	got, height, err = s.GetTx(id)
	require.NoErrorf(t, err, "%T.GetTx(%s) bonus", s.State, id)
	require.Equalf(t, nonBonusHeight, height, "%T.GetTx(%s) bonus height", s.State, id) // see NOT bonus
	if diff := cmp.Diff(export, got, txtest.CmpOpt()); diff != "" {
		t.Errorf("%T.GetTx(%d) bonus Tx diff (-want +got):\n%s", s.State, bonusHeight, diff)
	}
}

// TestApply_SortInvariant verifies that the order of txs passed to Apply does
// not affect the resulting state.
func TestApply_SortInvariant(t *testing.T) {
	getRoot := func(txs []*tx.Tx) common.Hash {
		t.Helper()

		snapshot := slices.Clone(txs)
		defer func() {
			require.Equal(t, snapshot, txs, "Apply must not mutate the caller's slice")
		}()

		s := newSUT(t)

		const height = 1
		require.NoErrorf(t, s.Apply(height, txs), "%T.Apply(%d)", s.State, height)

		root, err := s.GetRoot(height)
		require.NoErrorf(t, err, "%T.GetRoot(%d)", s.State, height)
		return root
	}

	var build builder
	forward := []*tx.Tx{
		build.newImport(),
		build.newImport(),
		build.newImport(),
	}
	backward := slices.Clone(forward)
	slices.Reverse(backward)

	forwardRoot := getRoot(forward)
	backwardRoot := getRoot(backward)
	require.Equal(t, forwardRoot, backwardRoot, "Apply must be invariant to the order of txs")
}

// TestCrash verifies that crashes while applying are gracefully handled.
func TestCrash(t *testing.T) {
	var build builder
	blocks := []block{
		{height: 1, txs: nil},
		{height: 2, txs: []*tx.Tx{build.newImport()}},
		{height: 3, txs: []*tx.Tx{build.newExport(), build.newImport()}},
		{height: 4, txs: nil},
		{height: 5, txs: []*tx.Tx{build.newImport()}},
		{height: 6, txs: []*tx.Tx{build.newExport()}},
		{height: 7, txs: []*tx.Tx{build.newImport(), build.newExport()}},
	}

	wantDB := saetest.NewFlakyDB(memdb.New(), math.MaxInt)
	want := newSUT(t, withDB(wantDB))
	want.apply(t, blocks...)

	// Iterating over all of the calls made to the db allows us to crash at
	// every possible point during Apply.
	for failAfter := range wantDB.Calls() {
		t.Run(fmt.Sprintf("failAfter_%d", failAfter), func(t *testing.T) {
			t.Parallel()

			db := memdb.New()
			preCrash := newSUT(t, withDB(saetest.NewFlakyDB(db, failAfter)))
			remainingBlocks := blocks
			for i, b := range blocks {
				if err := preCrash.Apply(b.height, b.txs); err != nil {
					require.ErrorIsf(t, err, saetest.ErrInjected, "%T.Apply(%d)", preCrash.State, b.height)
					break
				}
				remainingBlocks = blocks[i+1:]
			}

			got := newSUT(t, withDB(db))
			got.apply(t, remainingBlocks...)

			got.assertHasTxs(t, blocks)
			got.assertEqual(t, want)
		})
	}
}
