// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/log"
)

// speculativeWarmup executes the block's transactions concurrently against
// throwaway copies of the parent state while the real, sequential execution
// runs in Process. The results are discarded: the only purpose is to trigger
// the state reads each transaction will perform so that the trie/node caches
// are warm by the time the sequential execution reaches it.
//
// tx0 is skipped because the sequential execution starts on it immediately,
// and any transaction the sequential execution has already reached (tracked
// through mainProgress) is skipped to avoid competing for the same reads.
func (p *StateProcessor) speculativeWarmup(done <-chan struct{}, block *types.Block, parent *types.Header, cfg vm.Config, mainProgress *atomic.Int64) {
	defer func() {
		if r := recover(); r != nil {
			log.Debug("Speculative warmup panic recovered", "block", block.NumberU64(), "err", r)
		}
	}()

	txs := block.Transactions()
	if len(txs) <= 1 {
		return
	}

	header := block.Header()
	signer := types.MakeSigner(p.config, header.Number, header.Time)

	numWorkers := min(runtime.NumCPU(), len(txs)-1)

	txCh := make(chan int, len(txs))
	for i := 1; i < len(txs); i++ {
		txCh <- i
	}
	close(txCh)

	start := time.Now()
	var wg sync.WaitGroup
	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Debug("Speculative warmup worker panic recovered", "block", block.NumberU64(), "err", r)
				}
			}()

			// Each worker executes against its own state so the workers never
			// contend on a StateDB; no prefetcher is started on it since the
			// execution itself is the warmup.
			warmupDB, err := state.New(parent.Root, p.bc.stateCache, nil)
			if err != nil {
				return
			}
			blockCtx := NewEVMBlockContext(header, p.bc, nil)
			warmupEVM := vm.NewEVM(blockCtx, vm.TxContext{}, warmupDB, p.config, cfg)

			for txIdx := range txCh {
				select {
				case <-done:
					return
				default:
				}
				if int64(txIdx) <= mainProgress.Load() {
					continue
				}
				warmTransaction(warmupEVM, warmupDB, txs[txIdx], txIdx, signer, header)
			}
		}()
	}
	wg.Wait()
	log.Debug("Speculative warmup complete", "block", block.NumberU64(), "txs", len(txs), "workers", numWorkers, "elapsed", common.PrettyDuration(time.Since(start)))
}

// warmTransaction executes tx against warmupDB purely for the side effect of
// populating caches. The gas pool is effectively unlimited so that the warmup
// never fails on block gas accounting, and errors are ignored.
func warmTransaction(evm *vm.EVM, warmupDB *state.StateDB, tx *types.Transaction, txIdx int, signer types.Signer, header *types.Header) {
	msg, err := TransactionToMessage(tx, signer, header.BaseFee)
	if err != nil {
		return
	}
	warmupDB.SetTxContext(tx.Hash(), txIdx)
	evm.Reset(NewEVMTxContext(msg), warmupDB)
	ApplyMessage(evm, msg, new(GasPool).AddGas(1<<53)) //nolint:errcheck
}
