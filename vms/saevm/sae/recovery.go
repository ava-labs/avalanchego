// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
	"iter"
	"math"
	"sync/atomic"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/params"

	"github.com/ava-labs/strevm/blocks"
	"github.com/ava-labs/strevm/hook"
	saeparams "github.com/ava-labs/strevm/params"
	"github.com/ava-labs/strevm/proxytime"
	"github.com/ava-labs/strevm/saedb"
	"github.com/ava-labs/strevm/saexec"
	"github.com/ava-labs/strevm/types"
)

type recovery struct {
	db              ethdb.Database
	xdb             types.ExecutionResults
	chainConfig     *params.ChainConfig
	log             logging.Logger
	hooks           hook.Points
	config          Config
	lastSynchronous *blocks.Block
}

func (rec *recovery) newCanonicalBlock(num uint64, parent *blocks.Block) (*blocks.Block, error) {
	ethB, err := canonicalBlock(rec.db, num)
	if err != nil {
		return nil, err
	}
	return blocks.New(ethB, parent, nil, rec.log)
}

func (rec *recovery) lastCommittedBlock() (*blocks.Block, error) {
	num := saedb.LastHeightWithExecutionRootCommitted(
		rec.db,
		rec.config.DBConfig,
		rec.hooks,
		rec.lastSynchronous.Height(),
	)
	if ls := rec.lastSynchronous; num == ls.Height() {
		return ls, nil
	}

	b, err := rec.newCanonicalBlock(num, nil)
	if err != nil {
		return nil, err
	}
	if err := b.RestoreExecutionArtefacts(rec.db, rec.xdb, rec.chainConfig); err != nil {
		return nil, err
	}
	return b, nil
}

func (rec *recovery) canonicalAfter(parent *blocks.Block) iter.Seq2[*blocks.Block, error] {
	nums, _ := rawdb.ReadAllCanonicalHashes(rec.db, parent.NumberU64()+1, math.MaxUint64, math.MaxInt)

	return func(yield func(*blocks.Block, error) bool) {
		for _, num := range nums {
			b, err := rec.newCanonicalBlock(num, parent)
			if !yield(b, err) || err != nil {
				return
			}
			parent = b
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
	keepFrom := rec.hooks.SettledHeight(last.Header())
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
		settleAt := blocks.PreciseTime(rec.hooks, settler.Header()).Add(-saeparams.Tau)
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

			case b.Height() == rec.lastSynchronous.Height()+1:
				chain = append(chain, rec.lastSynchronous)

			default:
				parent, err := rec.newCanonicalBlock(b.Height()-1, nil)
				if err != nil {
					return err
				}
				if err := parent.RestoreExecutionArtefacts(rec.db, rec.xdb, rec.chainConfig); err != nil {
					return err
				}
				chain = append(chain, parent)

				if !b.Settled() {
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
