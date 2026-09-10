// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
)

// TestSnapshotRebuildAfterRecoveryNeverCompletes demonstrates, through an
// ordinary VM lifecycle, that a crash-recovered pruning node's state snapshot
// is rebuilt from scratch and that the rebuild can never complete:
//
//  1. Rebuild: recovery boots from the last root whose trie is committed on
//     disk, while the persisted snapshot disk layer sits wherever
//     [state.StateDB.Commit]'s hardcoded snaps.Cap(root, 128) last flattened
//     it. The roots differ, libevm's loadSnapshot treats that as fatal
//     (saedb sets no Recovery flag), and snapshot.New silently discards the
//     snapshot and regenerates from key zero.
//  2. Wedge: generation proves each range against the trie at the disk
//     layer's root. Once recovery has re-executed more than 128 blocks, the
//     flattens pin that root ~128 blocks behind the head — inside the range
//     SAE's settlement-based pruning (Tau = 5s ≈ a handful of blocks) has
//     already dereferenced. The generator dies on a missing trie, is
//     restarted against the next (equally pruned) root by the next flatten,
//     and never completes; the tree refuses to construct iterators
//     ([snapshot.ErrNotConstructed]) for as long as the node runs.
//
// The wedge is even tighter than the flatten depth suggests: while generation
// is in progress, Cap caps the diff layers at 8 instead of 128
// (libevm snapshot.go's cap), so the flattens — each of which advances the
// generation target — begin 9 blocks after the rebuild.
//
// In production the generator loses the race to the flattens because healing
// a real state takes hours; at test scale it would win, so the copied
// database's iterators — which only snapshot generation uses on hot paths —
// are slowed to a bounded delay per step until recovery has finished,
// modeling a state too large to heal in time. The delay must be bounded, not
// a hard gate: diffToDisk's stopGeneration blocks until the generator
// acknowledges the abort, which it can only do between iterator steps.
// Everything else about the lifecycle is the VM's own behavior.
func TestSnapshotRebuildAfterRecoveryNeverCompletes(t *testing.T) {
	t.Parallel()

	const (
		blockTime = 850 * time.Millisecond
		// snapshotFlattenDepth is hardcoded in libevm's StateDB.Commit as
		// snaps.Cap(root, 128).
		snapshotFlattenDepth = 128
		// Build past the flatten depth so the snapshot disk layer advances
		// beyond genesis, guaranteeing recovery's boot root (genesis, since
		// the commit interval is never reached) differs from it.
		numBlocks = snapshotFlattenDepth + 7
		// Never commit a post-genesis trie: every post-genesis root is pruned
		// once settled, so no future disk-layer position is ever healable.
		commitInterval = 4096
	)

	sutOpt, vmTime := withVMTime(t, time.Unix(saeparams.TauSeconds, 0))
	withSnapshots := options.Func[sutConfig](func(c *sutConfig) {
		c.logLevel = logging.Warn
		c.vmConfig.DBConfig.SnapshotCacheMiB = 16
		// Bulk accounts give post-crash healing a mandatory amount of flat
		// iteration (one gated step per account), so it cannot complete
		// before recovery's re-execution has moved the generation target
		// into pruned territory: 500 gated steps is seconds of mandatory
		// healing, against the milliseconds the first nine re-executed
		// blocks take to trigger the first flattens.
		for i := range 500 {
			addr := common.BigToAddress(new(big.Int).SetUint64(1<<32 + uint64(i))) //#nosec G115 -- test loop index
			c.genesis.Alloc[addr] = types.Account{Balance: big.NewInt(1)}
		}
	})

	var srcDB database.Database
	srcHDB := saetest.NewHeightIndexDB()
	ctx, src := newSUT(t, 1, sutOpt, withExecResultsDB(srcHDB), withCommitInterval(commitInterval), withSnapshots, options.Func[sutConfig](func(c *sutConfig) {
		srcDB = c.db
	}))

	// Let the fresh node's generation finish before producing blocks, so the
	// source enters the run with a healthy, complete snapshot.
	require.Eventually(t, func() bool {
		gen, ok := readGenerator(src.db)
		return ok && gen.Done
	}, 30*time.Second, 20*time.Millisecond, "snapshot generation completion on the fresh source VM")

	transfer := func(sut *SUT) *blocks.Block {
		vmTime.Advance(blockTime)
		return sut.runConsensusLoop(t, src.wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
			To:        &common.Address{},
			Gas:       params.TxGas,
			GasFeeCap: big.NewInt(1),
		}))
	}

	for range numBlocks {
		b := transfer(src)
		require.NoErrorf(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)
	}

	// The healthy baseline: the source's snapshot is still fully generated,
	// and its disk layer was flattened past the genesis root.
	gen, ok := readGenerator(src.db)
	require.True(t, ok, "snapshot generator entry on the source VM")
	require.True(t, gen.Done, "snapshot generation Done on the source VM after %d blocks", numBlocks)
	genesisRoot := src.genesis.PostExecutionStateRoot()
	require.NotEqual(t, genesisRoot, rawdb.ReadSnapshotRoot(src.db), "snapshot disk root after %d blocks", numBlocks)

	// Crash: copy the database out from under the running VM — no shutdown,
	// no snapshot journal.
	gate := newIteratorGate()
	t.Cleanup(gate.open)
	sutCtx, sut := newSUT(t, 1, sutOpt, withExecResultsDB(srcHDB.Clone()), withCommitInterval(commitInterval), withSnapshots, options.Func[sutConfig](func(c *sutConfig) {
		c.db = gate.wrap(saetest.CopyDB(t, srcDB))
	}))

	// Rebuild (1): the pre-crash snapshot was complete; recovery discarded it
	// and started generating from key zero.
	gen, ok = readGenerator(sut.db)
	require.True(t, ok, "snapshot generator entry after recovery")
	require.False(t, gen.Done, "snapshot generation restarted from scratch by recovery")

	// Wedge precondition (2): recovery re-executed numBlocks > 128 blocks, so
	// the flattens moved the disk layer's root into the settled-and-pruned
	// range; its state is no longer resolvable — there is nothing for the
	// generator to verify its ranges against. The disk root is read from disk
	// because diffToDisk persists it in the same batch as the flattened data.
	diskRoot := rawdb.ReadSnapshotRoot(sut.db)
	require.NotEqual(t, genesisRoot, diskRoot, "snapshot disk root after recovery re-execution")
	_, err := sut.rawVM.exec.StateDB(diskRoot)
	var missingNode *trie.MissingNodeError
	require.ErrorAs(t, err, &missingNode, "state at the snapshot disk root %s: pruning must have removed its trie", diskRoot)

	// Release the generator. From here on nothing is artificial: it runs
	// freely, dies on the missing trie, and every block's flatten restarts it
	// against the next pruned root.
	gate.open()

	for range 8 {
		b := transfer(sut)
		require.NoErrorf(t, b.WaitUntilExecuted(sutCtx), "%T.WaitUntilExecuted() after recovery", b)
	}

	// The wedge: the marker is still set, and no number of further blocks
	// clears it — each flatten simply restarts generation against the next,
	// equally pruned, root.
	gen, ok = readGenerator(sut.db)
	require.True(t, ok, "snapshot generator entry after post-recovery blocks")
	require.False(t, gen.Done, "snapshot generation completed despite pruned disk-root tries")

	// The externally visible consequence, not asserted here because this
	// package exposes no accessor for the tree: while the marker is set,
	// [snapshot.Tree.AccountIterator] and StorageIterator return
	// [snapshot.ErrNotConstructed] unconditionally — the calls the state sync
	// leaf handler's snapshot fast path makes, and the reason a source node
	// serves every leaf request from trie iteration instead.
}

// generatorEntry mirrors the RLP encoding of libevm's unexported
// journalGenerator; see saedb's generatorState counterpart.
type generatorEntry struct {
	Wiping   bool
	Done     bool
	Marker   []byte
	Accounts uint64
	Slots    uint64
	Storage  uint64
}

func readGenerator(db ethdb.Database) (generatorEntry, bool) {
	blob := rawdb.ReadSnapshotGenerator(db)
	if len(blob) == 0 {
		return generatorEntry{}, false
	}
	var gen generatorEntry
	if err := rlp.DecodeBytes(blob, &gen); err != nil {
		return generatorEntry{}, false
	}
	return gen, true
}

// gateDelay is the per-step cost of iterating the wrapped database while the
// gate is closed. It MUST be finite: diffToDisk's stopGeneration blocks until
// the generator acknowledges the abort, which only happens between iterator
// steps — an unbounded block here deadlocks recovery's re-execution.
const gateDelay = 5 * time.Millisecond

// iteratorGate slows all iterators of the wrapped database until opened.
// Snapshot generation is the only hot-path iterator user in the VM, so a
// closed gate stalls generation — modeling a state too large to heal before
// the flattens move its target into pruned territory — without slowing block
// processing.
type iteratorGate struct {
	ch   chan struct{}
	once sync.Once
}

func newIteratorGate() *iteratorGate {
	return &iteratorGate{ch: make(chan struct{})}
}

func (g *iteratorGate) open() {
	g.once.Do(func() { close(g.ch) })
}

func (g *iteratorGate) wrap(db database.Database) database.Database {
	return gatedDB{Database: db, gate: g}
}

type gatedDB struct {
	database.Database
	gate *iteratorGate
}

func (db gatedDB) NewIterator() database.Iterator {
	return gatedIterator{Iterator: db.Database.NewIterator(), gate: db.gate}
}

func (db gatedDB) NewIteratorWithStart(start []byte) database.Iterator {
	return gatedIterator{Iterator: db.Database.NewIteratorWithStart(start), gate: db.gate}
}

func (db gatedDB) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	return gatedIterator{Iterator: db.Database.NewIteratorWithPrefix(prefix), gate: db.gate}
}

func (db gatedDB) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	return gatedIterator{Iterator: db.Database.NewIteratorWithStartAndPrefix(start, prefix), gate: db.gate}
}

type gatedIterator struct {
	database.Iterator
	gate *iteratorGate
}

func (it gatedIterator) Next() bool {
	select {
	case <-it.gate.ch:
	case <-time.After(gateDelay):
	}
	return it.Iterator.Next()
}
