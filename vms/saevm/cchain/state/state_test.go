// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"encoding/binary"
	"errors"
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
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	chainsatomic "github.com/ava-labs/avalanchego/chains/atomic"
	oldstate "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/state"
)

// SUT bundles the system under test: a state implementation plus both sides
// of the shared-memory pair.
type SUT struct {
	stateImpl

	// db is used for both the chain state and shared memory
	db database.Database
	// sharedMemoryDB contains all the shared memory state.
	sharedMemoryDB *prefixdb.Database
}

func newSUT(tb testing.TB, opts ...sutOption) *SUT {
	tb.Helper()

	props := options.ApplyTo(&sutProperties{
		db:  memdb.New(),
		new: newState,
	}, opts...)

	chainDB := prefixdb.New([]byte("chain"), props.db)
	smDB := prefixdb.New([]byte("shared memory"), props.db)
	mem := chainsatomic.NewMemory(smDB)
	self := mem.NewSharedMemory(snowtest.CChainID)

	state := props.new(tb, chainDB, self)
	tb.Cleanup(func() {
		require.NoErrorf(tb, state.Close(), "%T.Close()", state)
	})
	return &SUT{
		stateImpl:      state,
		db:             props.db,
		sharedMemoryDB: smDB,
	}
}

// stateImpl is the surface common to [State] and [oldState]. It's used by the
// [SUT] so the same test helpers can drive either backend.
type stateImpl interface {
	Apply(height uint64, txs []*tx.Tx) error
	GetTx(txID ids.ID) (*tx.Tx, uint64, error)
	GetRoot(height uint64) (common.Hash, error)
	CurrentHeight() uint64
	Close() error
}

// A sutOption configures the default SUT properties used by [newSUT].
type sutOption = options.Option[sutProperties]

type sutProperties struct {
	// db is used for both the chain state and shared memory.
	db  database.Database
	new func(testing.TB, *prefixdb.Database, chainsatomic.SharedMemory) stateImpl
}

// withDB configures the SUT to use the given database.
func withDB(db database.Database) sutOption {
	return options.Func[sutProperties](func(p *sutProperties) {
		p.db = db
	})
}

// withLegacyBackend configures the SUT to use [oldState] rather than [State].
func withLegacyBackend() sutOption {
	return options.Func[sutProperties](func(p *sutProperties) {
		p.new = newOldState
	})
}

func newState(tb testing.TB, db *prefixdb.Database, sm chainsatomic.SharedMemory) stateImpl {
	ctx := snowtest.Context(tb, snowtest.CChainID)
	ctx.Log = saetest.NewTBLogger(tb, logging.Debug)
	ctx.SharedMemory = sm

	s, err := New(ctx, db)
	require.NoErrorf(tb, err, "New(%T, %T)", ctx, db)
	return s
}

// oldState drives the legacy [oldstate] package behind a surface that mirrors
// [State].
type oldState struct {
	tb      testing.TB
	db      database.Database
	repo    *oldstate.AtomicRepository
	backend *oldstate.AtomicBackend
	parent  common.Hash
}

func newOldState(tb testing.TB, db *prefixdb.Database, sm chainsatomic.SharedMemory) stateImpl {
	repo, err := oldstate.NewAtomicTxRepository(versiondb.New(db), atomic.Codec, 0)
	require.NoErrorf(tb, err, "state.NewAtomicTxRepository(%T, %T, ...)", db, atomic.Codec)

	// Making the legacy backend commit at every height matches the new State's
	// every-height-is-committed semantics.
	const commitInterval = 1
	backend, err := oldstate.NewAtomicBackend(sm, nil, repo, 0, common.Hash{}, commitInterval)
	require.NoErrorf(tb, err, "state.NewAtomicBackend(%T, %T)", sm, repo)
	return &oldState{
		tb:      tb,
		db:      db,
		repo:    repo,
		backend: backend,
	}
}

func (o *oldState) Apply(height uint64, txs []*tx.Tx) error {
	var blockHash common.Hash
	binary.BigEndian.PutUint64(blockHash[:], height)

	oldTxs := txtest.ToOlds(o.tb, txs)
	if _, err := o.backend.InsertTxs(blockHash, height, o.parent, oldTxs); err != nil {
		return err
	}
	as, err := o.backend.GetVerifiedAtomicState(blockHash)
	if err != nil {
		return err
	}
	// The batch is needed to satisfy the API; it carries no extra writes.
	if err := as.Accept(o.db.NewBatch()); err != nil {
		return err
	}
	o.parent = blockHash
	return nil
}

func (o *oldState) GetTx(txID ids.ID) (*tx.Tx, uint64, error) {
	oldTx, height, err := o.repo.GetByTxID(txID)
	if err != nil {
		return nil, 0, err
	}
	return txtest.ToNew(o.tb, oldTx), height, nil
}

func (o *oldState) GetRoot(height uint64) (common.Hash, error) {
	return o.backend.AtomicTrie().Root(height)
}

func (o *oldState) CurrentHeight() uint64 {
	_, h := o.backend.AtomicTrie().LastCommitted()
	return h
}

func (*oldState) Close() error { return nil }

// block bundles a height and the txs accepted at it. Tests in this file pass
// blocks to State.Apply (and to the oldState shim) one at a time.
type block struct {
	height uint64
	txs    []*tx.Tx
}

// apply calls [SUT.Apply] for each block in order and fails if any call errors.
func (s *SUT) apply(tb testing.TB, blocks ...block) {
	tb.Helper()

	for _, b := range blocks {
		require.NoErrorf(tb, s.Apply(b.height, b.txs), "%T.Apply(%d)", s.stateImpl, b.height)
	}
}

func (s *SUT) assertEqual(tb testing.TB, want *SUT) {
	tb.Helper()

	currentHeight := s.CurrentHeight()
	require.Equalf(tb, want.CurrentHeight(), currentHeight, "%T.CurrentHeight()", s.stateImpl)

	for h := range currentHeight + 1 {
		wantRoot, err := want.GetRoot(h)
		require.NoErrorf(tb, err, "%T.GetRoot(%d)", want.stateImpl, h)

		gotRoot, err := s.GetRoot(h)
		require.NoErrorf(tb, err, "%T.GetRoot(%d)", s.stateImpl, h)
		assert.Equalf(tb, wantRoot, gotRoot, "%T.GetRoot(%d)", s.stateImpl, h)
	}

	type entry struct {
		Key   []byte
		Value []byte
	}
	dbEntries := func(db database.Database) []entry {
		tb.Helper()

		it := db.NewIterator()
		defer it.Release()

		var out []entry
		for it.Next() {
			out = append(out, entry{
				Key:   slices.Clone(it.Key()),
				Value: slices.Clone(it.Value()),
			})
		}
		require.NoErrorf(tb, it.Error(), "%T.Error()", it)
		return out
	}

	wantEntries := dbEntries(want.sharedMemoryDB)
	gotEntries := dbEntries(s.sharedMemoryDB)
	assert.Equalf(tb, wantEntries, gotEntries, "shared memory")
}

func (s *SUT) assertHasTxs(tb testing.TB, blocks []block) {
	tb.Helper()

	for _, b := range blocks {
		for _, want := range b.txs {
			got, height, err := s.GetTx(want.ID())
			require.NoErrorf(tb, err, "%T.GetTx(%d)", s.stateImpl, b.height)
			assert.Equalf(tb, b.height, height, "%T.GetTx(%d).Height", s.stateImpl, b.height)
			if diff := cmp.Diff(want, got, txtest.CmpOpt()); diff != "" {
				tb.Errorf("%T.GetTx(%d).Tx diff (-want +got):\n%s", s.stateImpl, b.height, diff)
			}
		}
	}
}

// TestEmpty verifies the state behavior prior to applying any transactions.
func TestEmpty(t *testing.T) {
	s := newSUT(t)
	require.Zerof(t, s.CurrentHeight(), "%T.CurrentHeight()", s.stateImpl)

	_, _, err := s.GetTx(ids.GenerateTestID())
	require.ErrorIsf(t, err, database.ErrNotFound, "%T.GetTx(...)", s.stateImpl)

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
		require.ErrorIsf(t, err, test.wantErr, "%T.GetRoot(%d)", s.stateImpl, test.height)
		assert.Equalf(t, test.want, root, "%T.GetRoot(%d)", s.stateImpl, test.height)
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

// TestApply verifies that applying blocks results in the same state as the
// prior coreth code regardless of when the state is migrated to the new
// implementation.
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

			legacy := newSUT(t, withLegacyBackend())
			fresh := newSUT(t)
			migrations := make([]*SUT, len(test.blocks))
			for i := range migrations {
				migrations[i] = newSUT(t, withLegacyBackend())
			}

			for i, b := range test.blocks {
				migrations[i] = newSUT(t, withDB(migrations[i].db))

				all := append([]*SUT{legacy, fresh}, migrations...)
				for _, sut := range all {
					sut.apply(t, b)
				}

				reopenedFresh := newSUT(t, withDB(fresh.db))
				all = append(all, reopenedFresh)
				for _, sut := range all {
					sut.assertEqual(t, legacy)
					sut.assertHasTxs(t, test.blocks[:i+1])
				}
			}
		})
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
		require.NoErrorf(t, s.Apply(height, txs), "%T.Apply(%d)", s.stateImpl, height)

		root, err := s.GetRoot(height)
		require.NoErrorf(t, err, "%T.GetRoot(%d)", s.stateImpl, height)
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

	wantDB := newFlakyDB(memdb.New(), math.MaxInt)
	want := newSUT(t, withDB(wantDB))
	want.apply(t, blocks...)

	for failAfter := range wantDB.calls {
		t.Run(fmt.Sprintf("failAfter_%d", failAfter), func(t *testing.T) {
			t.Parallel()

			db := memdb.New()
			preCrash := newSUT(t, withDB(newFlakyDB(db, failAfter)))
			remainingBlocks := blocks
			for i, b := range blocks {
				if err := preCrash.Apply(b.height, b.txs); err != nil {
					require.ErrorIsf(t, err, errInjected, "%T.Apply(%d)", preCrash.stateImpl, b.height)
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

var errInjected = errors.New("injected fault")

// flakyDB wraps a database and fails after a configured number of mutating
// operations. Each [flakyDB.Put], [flakyDB.Delete], and [flakyBatch.Write]
// counts as an op; reads and iteration are not counted and never fail.
type flakyDB struct {
	database.Database
	failAfter int
	calls     int
}

func newFlakyDB(db database.Database, failAfter int) *flakyDB {
	return &flakyDB{
		Database:  db,
		failAfter: failAfter,
	}
}

func (f *flakyDB) shouldFail() error {
	if f.calls >= f.failAfter {
		return errInjected
	}
	f.calls++
	return nil
}

func (f *flakyDB) Put(key, value []byte) error {
	if err := f.shouldFail(); err != nil {
		return err
	}
	return f.Database.Put(key, value)
}

func (f *flakyDB) Delete(key []byte) error {
	if err := f.shouldFail(); err != nil {
		return err
	}
	return f.Database.Delete(key)
}

func (f *flakyDB) NewBatch() database.Batch {
	return &flakyBatch{Batch: f.Database.NewBatch(), db: f}
}

type flakyBatch struct {
	database.Batch
	db *flakyDB
}

func (b *flakyBatch) Write() error {
	if err := b.db.shouldFail(); err != nil {
		return err
	}
	return b.Batch.Write()
}

// Inner returns the wrapper so that [chainsatomic.WriteAll] calls
// [flakyBatch.Write].
func (b *flakyBatch) Inner() database.Batch { return b }
