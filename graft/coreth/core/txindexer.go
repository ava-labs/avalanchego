// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2024 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>

package core

import (
	"fmt"
	"time"

	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
)

// TxIndexProgress is the struct describing the progress for transaction indexing.
type TxIndexProgress struct {
	Indexed   uint64 // number of blocks whose transactions are indexed
	Remaining uint64 // number of blocks whose transactions are not indexed yet
}

// Done returns an indicator if the transaction indexing is finished.
func (progress TxIndexProgress) Done() bool {
	return progress.Remaining == 0
}

// txIndexer is the module responsible for maintaining transaction indexes
// according to the configured indexing range by users.
type txIndexer struct {
	// limit is the maximum number of blocks from head whose tx indexes
	// are reserved:
	//  * 0: means the entire chain should be indexed
	//  * N: means the latest N blocks [HEAD-N+1, HEAD] should be indexed
	//       and all others shouldn't.
	limit    uint64
	db       ethdb.Database
	progress chan chan TxIndexProgress
	term     chan chan struct{}
	closed   chan struct{}

	chain *BlockChain
}

// newTxIndexer initializes the transaction indexer.
func newTxIndexer(limit uint64, chain *BlockChain) *txIndexer {
	indexer := &txIndexer{
		limit:    limit,
		db:       chain.db,
		progress: make(chan chan TxIndexProgress),
		term:     make(chan chan struct{}),
		closed:   make(chan struct{}),
		chain:    chain,
	}
	chain.wg.Add(1)
	go func() {
		defer chain.wg.Done()
		indexer.loop(chain)
	}()

	var msg string
	if limit == 0 {
		msg = "entire chain"
	} else {
		msg = fmt.Sprintf("last %d blocks", limit)
	}
	log.Info("Initialized transaction indexer", "range", msg)

	return indexer
}

// run executes the scheduled indexing/unindexing task in a separate thread.
// If the stop channel is closed, the task should be terminated as soon as
// possible, the done channel will be closed once the task is finished.
func (indexer *txIndexer) run(tail *uint64, head uint64, stop chan struct{}, done chan struct{}) {
	start := time.Now()
	defer func() {
		txUnindexTimer.Inc(time.Since(start).Milliseconds())
		close(done)
	}()

	// Short circuit if chain is empty and nothing to index.
	if head == 0 {
		return
	}

	// Defensively ensure tail is not nil.
	tailValue := uint64(0)
	if tail != nil {
		// use intermediate variable to avoid modifying the pointer
		tailValue = *tail
	}

	if head-indexer.limit+1 >= tailValue {
		// Unindex a part of stale indices and forward index tail to HEAD-limit
		rawdb.UnindexTransactions(indexer.db, tailValue, head-indexer.limit+1, stop, false)
	}
}

// loop is the scheduler of the indexer, assigning indexing/unindexing tasks depending
// on the received chain event.
func (indexer *txIndexer) loop(chain *BlockChain) {
	defer close(indexer.closed)
	// Listening to chain events and manipulate the transaction indexes.
	var (
		stop     chan struct{} // Non-nil if background routine is active.
		done     chan struct{} // Non-nil if background routine is active.
		lastHead uint64        // The latest announced chain head (whose tx indexes are assumed created)

		headCh = make(chan ChainEvent)
		sub    = chain.SubscribeChainAcceptedEvent(headCh)
	)
	if sub == nil {
		log.Warn("could not create chain accepted subscription to unindex txs")
		return
	}
	defer sub.Unsubscribe()

	log.Info("Initialized transaction unindexer", "limit", indexer.limit)

	// Launch the initial processing if chain is not empty (head != genesis).
	// This step is useful in these scenarios that chain has no progress.
	if head := indexer.chain.CurrentBlock(); head != nil && head.Number.Uint64() > indexer.limit {
		stop = make(chan struct{})
		done = make(chan struct{})
		lastHead = head.Number.Uint64()
		indexer.chain.wg.Add(1)
		go func() {
			indexer.lockedRun(head.Number.Uint64(), stop, done)
		}()
	}
	for {
		select {
		case head := <-headCh:
			headNum := head.Block.NumberU64()
			if headNum < indexer.limit {
				break
			}

			if done == nil {
				stop = make(chan struct{})
				done = make(chan struct{})
				indexer.chain.wg.Add(1)
				go func() {
					indexer.lockedRun(headNum, stop, done)
				}()
			}
			lastHead = head.Block.NumberU64()
		case <-done:
			stop = nil
			done = nil
		case ch := <-indexer.progress:
			ch <- indexer.report(lastHead, rawdb.ReadTxIndexTail(indexer.db))
		case ch := <-indexer.term:
			if stop != nil {
				close(stop)
			}
			if done != nil {
				log.Info("Waiting background transaction unindexer to exit")
				<-done
			}
			close(ch)
			return
		}
	}
}

// report returns the tx indexing progress.
func (indexer *txIndexer) report(head uint64, tail *uint64) TxIndexProgress {
	total := indexer.limit
	if indexer.limit == 0 || total > head {
		total = head + 1 // genesis included
	}
	var indexed uint64
	if tail != nil {
		indexed = head - *tail + 1
	}
	// The value of indexed might be larger than total if some blocks need
	// to be unindexed, avoiding a negative remaining.
	var remaining uint64
	if indexed < total {
		remaining = total - indexed
	}
	return TxIndexProgress{
		Indexed:   indexed,
		Remaining: remaining,
	}
}

// close shutdown the indexer. Safe to be called for multiple times.
func (indexer *txIndexer) close() {
	ch := make(chan struct{})
	select {
	case indexer.term <- ch:
		<-ch
	case <-indexer.closed:
	}
}

// lockedRun runs the indexing/unindexing task in a locked manner. It reads
// the current tail index from the database.
func (indexer *txIndexer) lockedRun(head uint64, stop chan struct{}, done chan struct{}) {
	indexer.chain.txIndexTailLock.Lock()
	indexer.run(rawdb.ReadTxIndexTail(indexer.db), head, stop, done)
	indexer.chain.txIndexTailLock.Unlock()
	indexer.chain.wg.Done()
}
