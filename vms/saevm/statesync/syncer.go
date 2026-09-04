// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/params"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/graft/evm/sync/evmstate"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/evm/sync/code"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
	"github.com/ava-labs/avalanchego/vms/evm/sync/hashdb"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/network"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"

	graftsnap "github.com/ava-labs/avalanchego/graft/evm/core/state/snapshot"
	syncblock "github.com/ava-labs/avalanchego/vms/evm/sync/block"
)

// A Syncer can fully state-sync an EVM database, sufficient for execution with
// an [sae.VM].
type Syncer struct {
	cfg   Config
	hooks hook.Points

	snowCtx     *snow.Context
	network     *network.Network
	db          ethdb.Database
	blockParser syncblock.Parser
}

// Syncer returns a [Syncer] using the same data as the [Handler].
func NewSyncer(cfg Config, hooks hook.Points, snowCtx *snow.Context, network *network.Network, db ethdb.Database) *Syncer {
	return &Syncer{
		cfg:         cfg,
		hooks:       hooks,
		snowCtx:     snowCtx,
		network:     network,
		db:          db,
		blockParser: parser(hooks),
	}
}

// ShouldAcceptSummary returns true if the summary should be state synced to,
// given the current disk state.
func (s *Syncer) ShouldAcceptSummary(summary *Summary) bool {
	if !s.cfg.Enabled {
		return false
	}

	if s.cfg.DBConfig.Scheme == customrawdb.FirewoodScheme {
		s.snowCtx.Log.Warn("State sync is not supported with Firewood scheme")
		return false
	}

	if summary.AcceptedHeight == 0 {
		// The genesis block is already accepted, so we don't need to do anything.
		return false
	}

	// If any blocks have been accepted, don't state sync.
	//
	// TransitionVM assumes that a node will state-sync if state-sync is enabled
	// and the node is at the genesis block. Until transitionvm is removed, this
	// check MUST NOT change.
	hash := rawdb.ReadHeadFastBlockHash(s.db)
	if hash == (common.Hash{}) {
		s.snowCtx.Log.Warn("no last accepted hash")
		return false
	}
	height := rawdb.ReadHeaderNumber(s.db, hash)
	return height == nil || *height == 0
}

var errSynchronousBlock = errors.New("cannot state sync to synchronous block")

// Sync fetches all state associated with [Summary] and applies it to disk.
// Any error returned MUST be treated as fatal. After this method returns
// without error, one MUST call [Syncer.WriteSynced] to finalize the state
// sync.
func (s *Syncer) Sync(ctx context.Context, summary *Summary) error {
	const (
		// TODO(alarso16): Need 256 blocks for the BLOCKHASH op code from
		// the last settled. We should find a way to guarantee sufficient
		// blocks, but this overestimate will work for now.
		numBlocksToFetch   = 512
		maxLeafRequestSize = 1024
	)

	s.snowCtx.Log.Info("syncing blocks",
		zap.Stringer("acceptedHash", summary.AcceptedHash),
		zap.Uint64("acceptedHeight", summary.AcceptedHeight),
		zap.Uint64("numToFetch", numBlocksToFetch),
	)
	blockSyncer := syncblock.NewSyncer(
		s.snowCtx.Log,
		syncblock.NewClient(s.network.Network, s.network.PeerTracker),
		s.db,
		s.blockParser,
		summary.AcceptedHash,
		summary.AcceptedHeight,
		numBlocksToFetch,
	)
	if err := blockSyncer.Sync(ctx); err != nil {
		return err
	}
	s.snowCtx.Log.Info("finished syncing blocks")

	// With blocks now on disk, we can find the state root
	hdr := rawdb.ReadHeader(s.db, summary.AcceptedHash, summary.AcceptedHeight)
	if hdr == nil {
		return fmt.Errorf("couldn't find header %s at height %d", summary.AcceptedHash, summary.AcceptedHeight)
	}

	if hook.Synchronous(s.hooks, hdr) {
		// This requires malicious summary providers, but would corrupt database.
		return fmt.Errorf("%w: %s at height %d", errSynchronousBlock, summary.AcceptedHash, summary.AcceptedHeight)
	}

	codeSyncer, err := code.NewSyncer(
		s.snowCtx.Log,
		code.NewClient(s.network.Network, s.network.PeerTracker),
		s.db,
	)
	if err != nil {
		return fmt.Errorf("creating code syncer: %w", err)
	}

	// The snapshot MUST either be empty or match the requested root.
	// It will be regenerated anyway, so we can always wipe it.
	// TODO(powerslider): Push into EVM syncer.
	s.snowCtx.Log.Info("wiping snapshot before syncing state")
	if err := graftsnap.WipeSnapshotSync(ctx, s.db); err != nil {
		return fmt.Errorf("wiping snapshot: %w", err)
	}
	s.snowCtx.Log.Info("finished wiping snapshot")

	// TODO(powerslider): Remove dependency on graft.
	s.snowCtx.Log.Info("syncing state",
		zap.Stringer("settledRoot", hdr.Root),
		zap.Stringer("acceptedHash", summary.AcceptedHash),
		zap.Uint64("acceptedHeight", summary.AcceptedHeight),
	)
	evmSyncer, err := evmstate.NewSyncer(
		s.snowCtx.Log,
		hashdb.NewClient(
			s.snowCtx.Log,
			s.network.Network,
			p2p.EVMLeafRequestHandlerID,
			common.HashLength,
			s.network.PeerTracker,
		),
		s.db,
		hdr.Root,
		codeSyncer,
		maxLeafRequestSize,
	)
	if err != nil {
		return fmt.Errorf("creating evm state syncer: %w", err)
	}

	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return codeSyncer.Sync(egCtx)
	})
	eg.Go(func() error {
		return evmSyncer.Sync(egCtx)
	})
	if err := eg.Wait(); err != nil {
		// Finalize allows the EVM syncer to resume its progress on restart.
		// This is not required for correctness.
		return errors.Join(err, evmSyncer.Finalize())
	}
	s.snowCtx.Log.Info("finished syncing state")
	return nil
}

// WriteSynced marks the state sync as complete on disk, allowing an [sae.VM] to
// start up from [Summary.AcceptedHash] as the last accepted block. It MUST be
// called after a successful [Syncer.Sync]. Any non-EVM state sync
// behavior MUST have already completed.
func (s *Syncer) WriteSynced(summary *Summary) error {
	lastAccepted := rawdb.ReadHeader(s.db, summary.AcceptedHash, summary.AcceptedHeight)
	if lastAccepted == nil {
		return errors.New("couldn't find last accepted header")
	}
	settledHeight := s.hooks.SettledBy(lastAccepted).Height
	settledHash := rawdb.ReadCanonicalHash(s.db, settledHeight)
	if settledHash == (common.Hash{}) {
		return fmt.Errorf("no canonical hash for settled block at height %d", settledHeight)
	}
	lastSettled := rawdb.ReadBlock(s.db, settledHash, settledHeight)
	if lastSettled == nil {
		return fmt.Errorf("couldn't find last settled block at height %d", settledHeight)
	}

	if err := s.writeExecutionResults(lastSettled, lastAccepted); err != nil {
		return err
	}

	if err := writeBloomIndex(s.db, lastAccepted); err != nil {
		return fmt.Errorf("updating bloom indexer: %w", err)
	}

	// MUST be called last since rawdb markers signal success.
	if err := writeAcceptedMarkers(s.db, lastSettled.Hash(), lastAccepted); err != nil {
		return fmt.Errorf("writing rawdb invariants: %w", err)
	}

	return nil
}

func (s *Syncer) writeExecutionResults(lastSettled *types.Block, lastAccepted *types.Header) (retErr error) {
	// Synchronous blocks MUST NOT persist their execution results.
	if hook.Synchronous(s.hooks, lastSettled.Header()) {
		return nil
	}

	gt, err := hook.SettledGasTime(s.hooks, lastSettled.Header(), lastAccepted)
	if err != nil {
		return fmt.Errorf("couldn't calculate settled gas time: %w", err)
	}

	xdb, err := s.hooks.ExecutionResultsDB(sae.ExecutionResultsPath(s.snowCtx.ChainDataDir))
	if err != nil {
		return err
	}
	defer func() {
		retErr = errors.Join(retErr, xdb.Close())
	}()

	block, err := blocks.New(lastSettled, nil, nil, s.hooks, s.snowCtx.Log)
	if err != nil {
		return fmt.Errorf("creating block for last settled: %w", err)
	}

	return block.MarkExecuted(
		s.db,
		xdb,
		gt,
		time.Now(), // time only used for metrics
		nil,        // base fee is unknown
		nil,        // receipts are unknown
		lastAccepted.Root,
		new(atomic.Pointer[blocks.Block]), // last executed
	)
}

// writeBloomIndex adds a bloom indexer checkpoint to prevent the indexer from
// attempting to iterate through blocks earlier than those fetched during the
// sync and erroring.
//
// Assumes that settler.Number is a multiple of [params.BloomBitsBlocks]. The
// indexer can only add checkpoints on that interval, but if settler.Number is
// not a multiple, then we don't know the hash of the next block at that
// interval.
func writeBloomIndex(db ethdb.Database, settler *types.Header) error {
	const sectionSize = params.BloomBitsBlocks
	idx := core.NewBloomIndexer(db, sectionSize, 0)
	section := (settler.Number.Uint64() - 1) / sectionSize
	idx.AddCheckpoint(section, settler.ParentHash)
	return idx.Close()
}

// writeAcceptedMarkers persists the database markers to complete the state sync,
// allowing the [sae.VM] to start up from accepted by executing from settled.
func writeAcceptedMarkers(db ethdb.Database, settled common.Hash, accepted *types.Header) error {
	batch := db.NewBatch()

	rawdb.WriteHeadFastBlockHash(batch, accepted.Hash())
	rawdb.WriteHeadHeaderHash(batch, settled)
	rawdb.WriteHeadBlockHash(batch, settled)
	rawdb.WriteFinalizedBlockHash(batch, settled)

	// TODO(powerslider): Move to statesync code.
	rawdb.WriteSnapshotRoot(batch, accepted.Root)
	rawdb.WriteSnapshotGenerator(batch, graftsnap.GenerationDoneBlob)

	return batch.Write()
}
