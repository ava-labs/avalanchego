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
	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
)

// TestSnapshotRegenerationAfterRecoveryCompletes exercises the crash-recovery
// path of a pruning node's state snapshot:
//
//  1. Discard: a boot-root/disk-root mismatch alone no longer discards the
//     snapshot — [saedb.Config.snapConfig] opens the tree in libevm's
//     Recovery mode, which loads a disk layer sitting ahead of the boot root
//     (see [TestSnapshotLoadedAfterCleanShutdown]). To force the discard and
//     regenerate path under test here, the persisted snapshot root is
//     deleted from the crash copy, modeling a snapshot corrupted by the
//     crash; libevm's loadSnapshot then fails and snapshot.New regenerates
//     from key zero.
//  2. Completion (the regression): the rebuilt generation used to wedge, and
//     this test reproduces that end-to-end. See "Wedge mechanics" below for
//     why the timing constants are shaped the way they are.
//
// The copied database's iterators are slowed to a bounded delay per step
// until the gate opens, modeling a state too large to heal instantly; this
// makes the "generation still in progress right after recovery" assertion
// deterministic. The delay must be bounded, not a hard gate: diffToDisk's
// stopGeneration blocks until the generator acknowledges the abort, which it
// can only do between iterator steps. The gate is held closed for the entire
// postRecoveryBlocks loop, not just past the initial post-recovery
// assertions: the wedge mechanism below only engages while a generator is
// actively running (gen.Done == false) against the disk layer, so releasing
// the gate early would let generation race the flattens to completion and
// hide the regression.
//
// # Wedge mechanics
//
// libevm's snapshot.Tree.Cap contains a special case (see
// core/state/snapshot/snapshot.go, "If the generator is still running, use a
// more aggressive cap"): whenever a generator is actively running against the
// disk layer, ANY configured layer count above 8 is silently forced down to
// 8, and the resulting flatten is pushed to disk unconditionally (bypassing
// the usual in-memory size threshold), because the trie is about to move out
// from under the generator regardless of memory pressure. This means that
// once recovery's freshly-started generator is running, the pre-fix
// configuration (no [saedb.Tracker.StateDBCommitOptions], so libevm's default
// of 128) does NOT actually let 128 diff layers accumulate before flattening
// — it collapses to the same effective "8 blocks behind head" as any other
// configured value above 8. The fix's [saedb.SnapshotCapLayers] = 1 is below
// that 8-block floor, so it isn't affected by the special case and the disk
// root genuinely trails by only 1 block.
//
// The wedge therefore forms iff that trailing distance exceeds the real
// settlement→untrack lag: the number of blocks a post-execution root stays
// referenced (via the VM's consensus-critical bookkeeping in consensus.go)
// before [saedb.Tracker.Untrack] dereferences it. That lag is driven by
// simulated time — [saeparams.TauSeconds] of settlement lag, advanced via
// vmTime.Advance(blockTime) once per block — so it shrinks as blockTime
// grows. Empirically (using this test's own instrumentation, checking
// trie.New resolvability of each pre-crash block's post-execution root
// against the source VM's TrieDB right after producing it): at the
// originally-committed blockTime of 850ms the lag was ~13 blocks, safely
// above libevm's 8-block floor, so no pre-fix mutation could ever be made to
// wedge here no matter how many post-recovery blocks were produced (this was
// verified up to 1000). blockTime = 2500ms was chosen because it drives that
// lag down under libevm's 8-block floor, so the pre-fix ~8-block trailing
// distance reliably outruns real dereferencing. postRecoveryBlocks = 30 gives
// several times the handful of blocks needed to reach the steady-state 8-deep
// trail and hit a since-dereferenced trie, with margin.
//
// A mutation matrix run against this configuration confirmed: dropping
// [saedb.Tracker.StateDBCommitOptions] from the [state.StateDB.Commit] call in
// saexec.Executor.afterExecution — with or without also dropping
// [saedb.Tracker.PinSnapshotDiskRoot] — makes this test fail deterministically
// (3/3 runs each): the disk root lands on a pruned trie and the trailing
// completion require.Eventually times out. Dropping only the pin (commit
// options, and therefore the 1-layer cap, kept) still passes at this scale:
// with the trailing distance genuinely at 1 block, it stays inside the
// settlement lag regardless of the pin. Precise, unit-level coverage of the
// pin's own contribution (that even a 1-block-stale disk root can lose its
// trie without the pin, e.g. under different timing) lives in vms/saevm/saedb's
// generation wedge tests, which construct the wedged and healthy steady
// states directly rather than relying on real settlement-based pruning to get
// there. What this test verifies end-to-end, through a real crash and real
// block execution/settlement/pruning, is that recovery correctly discards on
// a genuine boot-root/disk-root mismatch and that the resulting regeneration
// actually completes and serves iterators under sustained post-recovery load,
// rather than hanging or erroring, across real restarts.
func TestSnapshotRegenerationAfterRecoveryCompletes(t *testing.T) {
	t.Parallel()

	const (
		// blockTime is sized to drive the real settlement→untrack lag below
		// libevm's 8-block aggressive-cap floor during active generation, so
		// the pre-fix ~8-block trailing disk root reliably outruns real
		// dereferencing. See "Wedge mechanics" in the doc comment above.
		blockTime = 2500 * time.Millisecond
		numBlocks = 14
		// A small commit interval guarantees a post-genesis boot root for
		// recovery, which differs from the persisted snapshot disk root
		// (fixed by libevm's single post-generation flush; see the doc
		// comment above) and so deterministically triggers the snapshot
		// discard-and-rebuild path under regression test here.
		commitInterval = 4
		// postRecoveryBlocks gives several times the handful of blocks needed
		// to reach the steady-state 8-deep trail and hit a since-dereferenced
		// trie (see "Wedge mechanics" above), with margin. This is
		// deliberately NOT ~129 (128 retained layers + 1): libevm forces the
		// effective cap down to 8 while a generator is active regardless of
		// the configured layer count, so the naive "128 layers must
		// accumulate" arithmetic never applies here.
		postRecoveryBlocks = 30
	)

	sutOpt, vmTime := withVMTime(t, time.Unix(saeparams.TauSeconds, 0))
	withSnapshots := options.Func[sutConfig](func(c *sutConfig) {
		c.logLevel = logging.Warn
		c.vmConfig.DBConfig.SnapshotCacheMiB = 16
		// Bulk accounts give post-crash regeneration a mandatory amount of
		// flat iteration (one gated step per account), so it cannot complete
		// before the post-recovery assertions run.
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

	// The healthy baseline: generation stayed complete under the block flow.
	gen, ok := readGenerator(src.db)
	require.True(t, ok, "snapshot generator entry on the source VM")
	require.True(t, gen.Done, "snapshot generation Done on the source VM after %d blocks", numBlocks)

	// Crash: copy the database out from under the running VM — no shutdown,
	// no snapshot journal — and delete the persisted snapshot root, modeling
	// a snapshot corrupted by the crash. Without the deletion, recovery loads
	// the persisted disk layer as-is (see the doc comment above and
	// [TestSnapshotLoadedAfterCleanShutdown]) and never regenerates.
	gate := newIteratorGate()
	t.Cleanup(gate.open)
	sutCtx, sut := newSUT(t, 1, sutOpt, withExecResultsDB(srcHDB.Clone()), withCommitInterval(commitInterval), withSnapshots, options.Func[sutConfig](func(c *sutConfig) {
		crashed := saetest.CopyDB(t, srcDB)
		rawdb.DeleteSnapshotRoot(saetypes.NewEthDB(crashed))
		c.db = gate.wrap(crashed)
	}))

	// Discard (1): the persisted snapshot was unloadable, so recovery
	// discarded it and started regenerating from key zero. The gate
	// guarantees it is still in progress here.
	gen, ok = readGenerator(sut.db)
	require.True(t, ok, "snapshot generator entry after recovery")
	require.False(t, gen.Done, "snapshot generation restarted from scratch by recovery")

	// The wedge is gone (2): the disk layer's root now trails the head by at
	// most one block, and its trie is pinned — resolvable, unlike before,
	// where it sat in settled-and-pruned territory.
	snaps := sut.rawVM.exec.Snapshot()
	diskRoot := snaps.DiskRoot()
	_, err := trie.New(trie.TrieID(diskRoot), sut.rawVM.exec.TrieDB())
	require.NoError(t, err, "trie.New() at the snapshot disk root %s after recovery: the generation target must be resolvable", diskRoot)

	for range postRecoveryBlocks {
		b := transfer(sut)
		require.NoErrorf(t, b.WaitUntilExecuted(sutCtx), "%T.WaitUntilExecuted() after recovery", b)
	}

	gate.open()

	require.Eventually(t, func() bool {
		gen, ok := readGenerator(sut.db)
		return ok && gen.Done
	}, 30*time.Second, 20*time.Millisecond, "snapshot regeneration completion on the recovered VM")

	// The externally visible recovery: the tree hands out iterators — the
	// call the state sync leaf handler's snapshot fast path makes.
	it, err := snaps.AccountIterator(snaps.DiskRoot(), common.Hash{})
	require.NoError(t, err, "AccountIterator on the recovered VM after regeneration")
	it.Release()
	sit, err := snaps.StorageIterator(snaps.DiskRoot(), common.Hash{}, common.Hash{})
	require.NoError(t, err, "StorageIterator on the recovered VM after regeneration")
	sit.Release()
}

// TestSnapshotLoadedAfterCleanShutdown cleanly restarts a VM. The snapshot
// persisted at shutdown MUST be loaded as-is, even though the persisted disk
// root (the last-executed root, per the lockstep flatten of
// [saedb.SnapshotCapLayers] and the final flatten in [saedb.Tracker.Close])
// differs from the older, settled root recovery boots from — recovery's
// re-execution catches the chain back up to the disk layer. A snapshot that
// fails to load is regenerated, which may take hours on a mainnet-sized
// state; clean restarts are routine and must not pay that.
func TestSnapshotLoadedAfterCleanShutdown(t *testing.T) {
	t.Parallel()

	const (
		blockTime      = time.Second
		commitInterval = 4
		numBlocks      = 6
	)

	sutOpt, vmTime := withVMTime(t, time.Unix(saeparams.TauSeconds, 0))
	withSnapshots := options.Func[sutConfig](func(c *sutConfig) {
		c.vmConfig.DBConfig.SnapshotCacheMiB = 16
	})

	var srcDB database.Database
	srcHDB := saetest.NewHeightIndexDB()
	ctx, src := newSUT(t, 1, sutOpt, withExecResultsDB(srcHDB), withCommitInterval(commitInterval), withSnapshots, options.Func[sutConfig](func(c *sutConfig) {
		srcDB = c.db
	}))

	// Let the fresh node's generation finish so a Done generator after the
	// restart can only mean the snapshot was loaded, not regenerated.
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
	lastExecutedRoot := src.rawVM.exec.LastExecuted().PostExecutionStateRoot()
	src.close()

	// Closing flattens the snapshot onto the last-executed root, so the
	// persisted disk root MUST be read after the shutdown.
	persistedRoot := rawdb.ReadSnapshotRoot(src.db)
	require.Equal(t, lastExecutedRoot, persistedRoot, "rawdb.ReadSnapshotRoot() after shutdown: [saedb.Tracker.Close] flattens onto the last-executed root")
	gen, ok := readGenerator(src.db)
	require.True(t, ok, "snapshot generator entry after shutdown")
	require.True(t, gen.Done, "snapshot generation Done after shutdown")

	sutCtx, sut := newSUT(t, 1, sutOpt, withExecResultsDB(srcHDB.Clone()), withCommitInterval(commitInterval), withSnapshots, options.Func[sutConfig](func(c *sutConfig) {
		c.db = saetest.CopyDB(t, srcDB)
	}))

	gen, ok = readGenerator(sut.db)
	require.True(t, ok, "snapshot generator entry after restart")
	require.True(t, gen.Done, "snapshot generation Done after restart: the persisted snapshot MUST be loaded, not regenerated")

	snaps := sut.rawVM.exec.Snapshot()
	require.NotNilf(t, snaps, "%T.Snapshot()", sut.rawVM.exec)
	require.Equalf(t, persistedRoot, snaps.DiskRoot(), "%T.DiskRoot() after restart MUST be the root persisted at shutdown", snaps)
	require.NoErrorf(t, snaps.Verify(persistedRoot), "%T.Verify([persisted disk root]) after restart", snaps)

	// The loaded snapshot stays live: a post-restart block layers its diff on
	// the loaded disk layer and still reproduces the executed state.
	b := transfer(sut)
	require.NoErrorf(t, b.WaitUntilExecuted(sutCtx), "%T.WaitUntilExecuted() after restart", b)
	require.NoErrorf(t, snaps.Verify(b.PostExecutionStateRoot()), "%T.Verify([post-restart executed root])", snaps)
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
