// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"sync/atomic"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/params"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/proxytime"
	"github.com/ava-labs/avalanchego/vms/saevm/saexec"
	"github.com/ava-labs/avalanchego/vms/saevm/types"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
)

type recovery struct {
	db          ethdb.Database
	xdb         types.ExecutionResults
	chainConfig *params.ChainConfig
	snowCtx     *snow.Context
	hooks       hook.Points
	config      Config
}

func (rec *recovery) newCanonicalBlock(num uint64, parent *blocks.Block) (*blocks.Block, error) {
	ethB, err := canonicalBlock(rec.db, num)
	if err != nil {
		return nil, err
	}
	return blocks.New(ethB, parent, nil, rec.snowCtx.Log)
}

// lastCommittedBlock returns the highest settled block whose post-execution
// state is available on disk. This is required because its post-execution state
// is the basis for the worst-case checks needed for block verifications.
func (rec *recovery) lastCommittedBlock() (_ *blocks.Block, retErr error) {
	cache := state.NewDatabaseWithConfig(rec.db, rec.config.DBConfig.TrieDBConfig(rec.snowCtx.ChainDataDir, rec.snowCtx.Log))
	defer func() {
		retErr = errors.Join(retErr, cache.TrieDB().Close())
	}()

	lastSettledHash := rawdb.ReadFinalizedBlockHash(rec.db)
	if lastSettledHash == (common.Hash{}) {
		return nil, errors.New("no finalized block recorded")
	}
	lastSettledHeight := rawdb.ReadHeaderNumber(rec.db, lastSettledHash)
	if lastSettledHeight == nil {
		return nil, fmt.Errorf("no height for finalized block %s", lastSettledHash)
	}

	// Search for highest settled post-execution state
	// Invariant: The state is written to disk AFTER the block is written to
	// disk. Therefore, the state can only lag behind the block read.
	// Additionally, we assume any block has been written atomically, so
	// if the last settled height was found, the underlying block is present.
	// At minimum, [NewVM] requires a genesis block to be written (which is
	// synchronous by definition).
	//
	// There's no reasonable cap on how far back to search, since the distance
	// between the settler and settled block is unbounded, and node crashes
	// must be accounted for.
	for height := *lastSettledHeight; ; height-- {
		ethB, err := canonicalBlock(rec.db, height)
		if err != nil {
			return nil, err
		}

		b, err := blocks.RestoreSettledBlock(ethB, rec.hooks, rec.snowCtx.Log, rec.db, rec.xdb, rec.chainConfig)
		if err != nil {
			return nil, err
		}

		if _, err := state.New(b.PostExecutionStateRoot(), cache, nil); err == nil { // if NO error
			return b, nil
		}

		if b.Synchronous() {
			return nil, fmt.Errorf("last synchronous block %d has no available post-execution state", height)
		}
	}
}

func (rec *recovery) canonicalAfter(parent *blocks.Block) iter.Seq2[*blocks.Block, error] {
	return func(yield func(*blocks.Block, error) bool) {
		lastAcceptedHash := rawdb.ReadHeadFastBlockHash(rec.db)
		if lastAcceptedHash == (common.Hash{}) {
			// SAE writes this hash on [VM.AcceptBlock], so the set of accepted,
			// asynchronous blocks MUST be empty.
			return
		}

		for curr := parent; curr.Hash() != lastAcceptedHash; {
			b, err := rec.newCanonicalBlock(curr.Height()+1, curr)
			if !yield(b, err) || err != nil {
				return
			}
			curr = b
		}
	}
}

func (rec *recovery) executeAllAccepted(ctx context.Context, exec *saexec.Executor) error {
	after := exec.LastExecuted()
	last := after
	for b, err := range rec.canonicalAfter(after) {
		if err != nil {
			return err
		}
		if err := exec.Enqueue(ctx, b); err != nil {
			return err
		}
		last = b
	}
	if err := last.WaitUntilExecuted(ctx); err != nil {
		return err
	}

	// Consensus only requires post-execution state after and including the
	// last-settled block.
	keepFrom := rec.hooks.SettledBy(last.Header()).Height
	for b := last; b.NumberU64() > after.NumberU64(); b = b.ParentBlock() {
		if b.NumberU64() < keepFrom {
			exec.Tracker.Untrack(b.PostExecutionStateRoot())
		}
	}
	return nil
}

// lastOf returns the lastOf element in a slice, which MUST NOT be empty.
func lastOf[E any](s []E) E {
	return s[len(s)-1]
}

// consensusCriticalBlocks returns a block-hash-keyed map of all blocks from the
// last executed back to, and including, the block that it settled. Said settled
// block is returned separately, for convenience.
func (rec *recovery) consensusCriticalBlocks(exec *saexec.Executor) (_ *syncMap[common.Hash, *blocks.Block], lastSettled *blocks.Block, _ error) {
	chain := []*blocks.Block{exec.LastExecuted()} // reverse height order
	blackhole := new(atomic.Pointer[blocks.Block])

	// extend appends to the chain all the blocks in settler's ancestry up to
	// and including the block that it settled.
	extend := func(settler *blocks.Block) error {
		settleAt := rec.hooks.BlockTime(settler.Header()).Add(-saeparams.Tau)
		tm := proxytime.Of[gas.Gas](settleAt)

		for {
			switch b := lastOf(chain); {
			case b.Synchronous():
				return nil

			case b.ExecutedByGasTime().Compare(tm) <= 0:
				if b.Settled() {
					return nil
				}
				return b.MarkSettled(blackhole)

			default:
				parent, err := rec.newCanonicalBlock(b.Height()-1, nil)
				if err != nil {
					return err
				}
				if err := parent.RestoreExecutionArtefacts(rec.hooks, rec.db, rec.xdb, rec.chainConfig); err != nil {
					return err
				}
				chain = append(chain, parent)

				if !b.Settled() && !parent.Synchronous() {
					continue
				}
				if err := parent.MarkSettled(blackhole); err != nil {
					return err
				}
			}
		}
	}

	if err := extend(exec.LastExecuted()); err != nil {
		return nil, nil, err
	}
	lastSettled = lastOf(chain)
	tr := exec.Tracker
	bMap := newSyncMap[common.Hash, *blocks.Block](
		func(b *blocks.Block) {
			tr.Track(b.SettledStateRoot())
			// The post-execution root is tracked by the [saexec.Executor] as
			// soon as it's known. In the case of database recovery, this
			// occurred in [recovery.executeAllAccepted].
		},
		func(b *blocks.Block) {
			tr.Untrack(b.SettledStateRoot())
			if b.Executed() { // i.e. deleted due to settlement not rejection
				tr.Untrack(b.PostExecutionStateRoot())
			}
		},
	)
	for _, b := range chain {
		bMap.Store(b.Hash(), b)
	}

	for i, b := range chain[:len(chain)-1] {
		if err := extend(b); err != nil {
			return nil, nil, err
		}
		if err := b.SetAncestors(chain[i+1], lastOf(chain)); err != nil {
			return nil, nil, err
		}
	}
	for _, b := range bMap.m {
		stage := blocks.Executed
		if b.Hash() == lastSettled.Hash() {
			stage = blocks.Settled
		}
		if err := b.CheckInvariants(stage); err != nil {
			return nil, nil, err
		}
	}
	return bMap, lastSettled, nil
}
