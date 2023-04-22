// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"time"

	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/ethclient"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"golang.org/x/sync/errgroup"
)

type WorkerGroup struct {
	workers []*Worker
}

func NewWorkerGroup(clients []ethclient.Client, senders []common.Address, txSequences [][]*types.Transaction) *WorkerGroup {
	workers := make([]*Worker, len(clients))
	for i, client := range clients {
		workers[i] = NewWorker(client, senders[i], txSequences[i])
	}

	return &WorkerGroup{
		workers: workers,
	}
}

func (wg *WorkerGroup) executeTask(ctx context.Context, f func(ctx context.Context, w *Worker) error) error {
	eg := errgroup.Group{}
	for _, worker := range wg.workers {
		worker := worker
		eg.Go(func() error {
			return f(ctx, worker)
		})
	}

	return eg.Wait()
}

// IssueTxs concurrently issues transactions from each worker
func (wg *WorkerGroup) IssueTxs(ctx context.Context) error {
	return wg.executeTask(ctx, func(ctx context.Context, w *Worker) error {
		return w.ExecuteTxsFromAddress(ctx)
	})
}

// AwaitTxs concurrently waits for each worker to confirm the nonce of its last issued transaction.
func (wg *WorkerGroup) AwaitTxs(ctx context.Context) error {
	return wg.executeTask(ctx, func(ctx context.Context, w *Worker) error {
		return w.AwaitTxs(ctx)
	})
}

// ConfirmAllTransactions concurrently waits for each worker to confirm each transaction by checking
// for it via eth_getTransactionByHash
func (wg *WorkerGroup) ConfirmAllTransactions(ctx context.Context) error {
	return wg.executeTask(ctx, func(ctx context.Context, w *Worker) error {
		return w.ConfirmAllTransactions(ctx)
	})
}

// Execute performs executes the following steps in order:
// - IssueTxs
// - AwaitTxs
// - ConfirmAllTransactions
func (wg *WorkerGroup) Execute(ctx context.Context) error {
	start := time.Now()
	defer func() {
		log.Info("Completed execution", "totalTime", time.Since(start))
	}()

	executionStart := time.Now()
	log.Info("Executing transactions", "startTime", executionStart)
	if err := wg.IssueTxs(ctx); err != nil {
		return err
	}

	awaitStart := time.Now()
	log.Info("Awaiting transactions", "startTime", awaitStart, "executionElapsed", awaitStart.Sub(executionStart))
	if err := wg.AwaitTxs(ctx); err != nil {
		return err
	}

	confirmationStart := time.Now()
	log.Info("Confirming transactions", "startTime", confirmationStart, "awaitElapsed", confirmationStart.Sub(awaitStart))
	if err := wg.ConfirmAllTransactions(ctx); err != nil {
		return err
	}

	endTime := time.Now()
	log.Info("Transaction confirmation completed", "confirmationElapsed", endTime.Sub(confirmationStart))
	totalTxs := 0
	for _, worker := range wg.workers {
		totalTxs += len(worker.txs)
	}

	// We exclude the time for ConfirmAllTransactions because we assume once the nonce is reported, the transaction has been finalized.
	// For Subnet-EVM, this assumes that unfinalized data is not exposed via API.
	issueAndConfirmDuration := confirmationStart.Sub(executionStart)
	log.Info("Simulator completed", "totalTxs", totalTxs, "TPS", float64(totalTxs)/issueAndConfirmDuration.Seconds())
	return nil
}
