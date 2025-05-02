// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package listen

import (
	"context"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/load/agent"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethclient"
	"go.uber.org/zap"
)

var _ agent.Listener[*types.Transaction] = (*Listener)(nil)

// Listener listens for transaction confirmations from a node.
type Listener struct {
	// Injected parameters
	client   *ethclient.Client
	tracker  agent.Tracker[common.Hash]
	txTarget uint64

	// Internal state
	mutex            *sync.Mutex
	issued           uint64
	inFlightTxHashes set.Set[common.Hash]
}

func New(
	client *ethclient.Client,
	tracker agent.Tracker[common.Hash],
	txTarget uint64,
	address common.Address,
) *Listener {
	return &Listener{
		client:           client,
		tracker:          tracker,
		txTarget:         txTarget,
		mutex:            &sync.Mutex{},
		issued:           0,
		inFlightTxHashes: set.NewSet[common.Hash](int(txTarget)),
	}
}

// Listen creates a listener for new accepted blocks, and tracks the
// confirmed transactions in the blocks that were sent by the listener's address.
// It will mark the transactions as confirmed in the tracker.
// It will stop when the target number of transactions is reached or when the context is done.
// It will also mark any remaining transactions as failed in the tracker.
// It will return an error if the listener fails to start or if it fails to track the transactions.
func (l *Listener) Listen(tc tests.TestContext, ctx context.Context) error {
	headers := make(chan *types.Header)
	sub, err := l.client.SubscribeNewHead(ctx, headers)
	if err != nil {
		return fmt.Errorf("subscribing to new headers: %w", err)
	}
	defer sub.Unsubscribe()
	defer l.markRemainingAsFailed()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-sub.Err():
			tc.Log().Info("Stopping listening for new blocks due to context being done")
			return fmt.Errorf("subscription error: %w", err)
		case header := <-headers:
			tc.Log().Info("New block header received",
				zap.String("blockNumber", header.Number.String()))

			// Get the full block to check for transactions confirmations.
			block, err := l.client.BlockByHash(ctx, header.Hash())
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("getting block by hash %s: %w", header.Hash().Hex(), err)
			}

			// Check if the block contains any transactions from the listener's address.
			l.mutex.Lock()
			for _, tx := range block.Transactions() {
				if l.inFlightTxHashes.Contains(tx.Hash()) {
					tc.Log().Info("Transaction confirmed",
						zap.String("txHash", tx.Hash().Hex()))

					// Get the receipt for the transaction to check if it was successful.
					receipt, err := l.client.TransactionReceipt(ctx, tx.Hash())
					if err != nil {
						return fmt.Errorf("getting transaction receipt %s: %w", tx.Hash().Hex(), err)
					}
					if receipt.Status == 0 {
						tc.Log().Info("Transaction failed",
							zap.String("txHash", tx.Hash().Hex()))
						return fmt.Errorf("transaction %s failed", tx.Hash().Hex())
					}

					// Mark the transaction as confirmed in the tracker.
					l.tracker.ObserveConfirmed(tx.Hash())
					l.inFlightTxHashes.Remove(tx.Hash())
				}

			}
			finished := l.issued == l.txTarget && l.inFlightTxHashes.Len() == 0
			l.mutex.Unlock()

			if finished {
				tc.Log().Info("All transactions confirmed. Stopping listener.")
				return nil
			}
		}
	}
}

func (l *Listener) RegisterIssued(tx *types.Transaction) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.issued++
	l.inFlightTxHashes.Add(tx.Hash())
}

func (l *Listener) markRemainingAsFailed() {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	for txHash := range l.inFlightTxHashes {
		l.tracker.ObserveFailed(txHash)
	}
	l.inFlightTxHashes = nil
}
