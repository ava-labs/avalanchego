// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/ethclient"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

type Worker struct {
	client  ethclient.Client
	address common.Address
	txs     []*types.Transaction
}

// NewWorker creates a new worker that will issue the sequence of transactions from the given address
//
// Assumes that all transactions are from the same address, ordered by nonce, and this worker has exclusive access
// to issuance of transactions from the underlying private key.
func NewWorker(client ethclient.Client, address common.Address, txs []*types.Transaction) *Worker {
	return &Worker{
		client:  client,
		address: address,
		txs:     txs,
	}
}

func (w *Worker) ExecuteTxsFromAddress(ctx context.Context) error {
	log.Info("Executing txs", "numTxs", len(w.txs))
	for i, tx := range w.txs {
		start := time.Now()
		err := w.client.SendTransaction(ctx, tx)
		if err != nil {
			return fmt.Errorf("failed to issue tx %d: %w", i, err)
		}
		log.Info("execute tx", "tx", tx.Hash(), "nonce", tx.Nonce(), "duration", time.Since(start))
	}
	return nil
}

// AwaitTxs awaits for the nonce of the last transaction issued by the worker to be confirmed or
// rejected by the network.
//
// Assumes that a non-zero number of transactions were already generated and that they were issued
// by this worker.
func (w *Worker) AwaitTxs(ctx context.Context) error {
	nonce := w.txs[len(w.txs)-1].Nonce()

	newHeads := make(chan *types.Header)
	defer close(newHeads)

	sub, err := w.client.SubscribeNewHead(ctx, newHeads)
	if err != nil {
		log.Debug("failed to subscribe new heads, falling back to polling", "err", err)
	} else {
		defer sub.Unsubscribe()
	}

	for {
		select {
		case <-newHeads:
		case <-time.After(time.Second):
		case <-ctx.Done():
			return fmt.Errorf("failed to await nonce: %w", ctx.Err())
		}

		currentNonce, err := w.client.NonceAt(ctx, w.address, nil)
		if err != nil {
			log.Warn("failed to get nonce", "err", err)
		}
		if currentNonce >= nonce {
			return nil
		} else {
			log.Info("fetched nonce", "awaiting", nonce, "currentNonce", currentNonce)
		}
	}
}

// ConfirmAllTransactions iterates over every transaction of this worker and confirms it
// via eth_getTransactionByHash
func (w *Worker) ConfirmAllTransactions(ctx context.Context) error {
	for i, tx := range w.txs {
		_, isPending, err := w.client.TransactionByHash(ctx, tx.Hash())
		if err != nil {
			return fmt.Errorf("failed to confirm tx at index %d: %s", i, tx.Hash())
		}
		if isPending {
			return fmt.Errorf("failed to confirm tx at index %d: pending", i)
		}
	}
	log.Info("Confirmed all transactions")
	return nil
}

// Execute issues and confirms all transactions for the worker.
func (w *Worker) Execute(ctx context.Context) error {
	if err := w.ExecuteTxsFromAddress(ctx); err != nil {
		return err
	}
	if err := w.AwaitTxs(ctx); err != nil {
		return err
	}
	return w.ConfirmAllTransactions(ctx)
}
