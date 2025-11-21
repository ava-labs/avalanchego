// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/firewood"
	"github.com/ava-labs/avalanchego/ids"
)

var atomicTxPrefix = []byte("atomic_tx")

type firewoodDB struct {
	db        *firewood.DB
	versionDB *versiondb.Database
}

var _ ChainDB = (*firewoodDB)(nil)

func (f *firewoodDB) AddAtomicTx(txID ids.ID) {
	f.db.Put(firewood.Prefix(atomicTxPrefix, txID[:]), []byte{})
}

func (f *firewoodDB) Repair(ctx context.Context, vm VM, s State) error {
	lastAcceptedBlk, err := s.GetBlock(s.GetLastAccepted())
	if err != nil {
		return fmt.Errorf("getting last accepted block: %w", err)
	}

	var replayStartHeight int
	if firewoodHeight, ok := f.db.Height(); !ok {
		// A height of zero means that we have not flushed any data to firewood yet,
		// so we need to replay all blocks including genesis.
		replayStartHeight = -1
	} else {
		replayStartHeight = int(firewoodHeight)
	}

	// Replay any blocks until the last accepted height to synchronize the chain
	// and local dbs.
	for i := replayStartHeight; i < int(lastAcceptedBlk.Height()); i++ {
		blkID, err := s.GetBlockIDAtHeight(uint64(i + 1))
		if err != nil {
			return fmt.Errorf("getting block id: %w", err)
		}

		blk, err := s.GetBlock(blkID)
		if err != nil {
			return fmt.Errorf("getting block: %w", err)
		}

		if err := vm.Replay(ctx, blk); err != nil {
			return fmt.Errorf("replaying block: %w", err)
		}
	}

	return nil
}

func (f *firewoodDB) Abort() {
	f.db.Abort()
}

func (f *firewoodDB) CommitBatch(height uint64) (
	database.Batch,
	error,
) {
	b, err := f.versionDB.CommitBatch()
	if err != nil {
		return nil, err
	}

	return &firewoodBatch{
		Batch:           b,
		nextStateHeight: height,
		firewoodDB:      f.db,
	}, nil
}

func (f *firewoodDB) Close(ctx context.Context) error {
	return f.db.Close(ctx)
}

// firewoodBatch wraps the underlying batch to also write to firewood after
// it is written to.
type firewoodBatch struct {
	database.Batch
	nextStateHeight uint64
	firewoodDB      *firewood.DB
}

func (f *firewoodBatch) Write() error {
	chainDBHeight, ok := f.firewoodDB.Height()
	if !ok && f.nextStateHeight > 1 {
		// This should always be initialized after the first (non-genesis) block is
		// accepted.
		return errors.New("chain db was not initialized")
	}

	if nextFirewoodHeight := chainDBHeight + 1; ok && nextFirewoodHeight != f.nextStateHeight {
		// Avoid writing state if the next revision height would be out-of-sync
		// because firewood should never be out-of-sync with versiondb after we have
		// finished repairing. We should only create one firewood revision per
		// block height.
		//
		// We cannot perform this check if the height is not initialized because
		// there is no height in firewood before the genesis block is written.
		return fmt.Errorf(
			"%w: next height is at %d but next revision would be %d",
			errDBsOutOfSync,
			f.nextStateHeight,
			nextFirewoodHeight,
		)
	}

	if err := f.Batch.Write(); err != nil {
		return fmt.Errorf("writing versiondb batch: %w", err)
	}

	if err := f.firewoodDB.Flush(); err != nil {
		return fmt.Errorf("flushing to firewood: %w", err)
	}

	return nil
}
