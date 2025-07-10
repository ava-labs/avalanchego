// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package listener

import (
	"context"
	"fmt"
	"math/big"
	"sync"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethclient"

	"github.com/ava-labs/avalanchego/tests/load"
)

// Listener listens for transaction confirmations from a node.
type Listener struct {
	// Injected parameters
	client  *ethclient.Client
	tracker *load.Tracker[common.Hash]
	address common.Address

	// Internal state
	mutex       sync.Mutex
	allIssued   bool
	nonce       uint64
	inFlightTxs []common.Hash
}

func New(
	client *ethclient.Client,
	tracker *load.Tracker[common.Hash],
	address common.Address,
	nonce uint64,
) *Listener {
	return &Listener{
		client:  client,
		tracker: tracker,
		address: address,
		nonce:   nonce,
	}
}

func (l *Listener) Listen(ctx context.Context) error {
	headNotifier := newHeadNotifier(l.client)
	newHeadCh, notifierErrCh, err := headNotifier.start()
	if err != nil {
		return fmt.Errorf("starting new head notifier: %w", err)
	}
	defer headNotifier.stop()
	defer l.markRemainingAsFailed()

	for {
		blockNumber := (*big.Int)(nil)
		nonce, err := l.client.NonceAt(ctx, l.address, blockNumber)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("checking last block account nonce for address %s: %w", l.address, err)
		}

		l.mutex.Lock()
		confirmed := uint64(len(l.inFlightTxs))
		if nonce < l.nonce { // lagging behind last issued nonce
			lag := l.nonce - nonce
			confirmed -= lag
		}
		for index := range confirmed {
			txHash := l.inFlightTxs[index]
			l.tracker.ObserveConfirmed(txHash)
		}
		l.inFlightTxs = l.inFlightTxs[confirmed:]
		if l.allIssued && len(l.inFlightTxs) == 0 {
			l.mutex.Unlock()
			return nil
		}
		l.mutex.Unlock()

		select {
		case <-ctx.Done():
			return nil
		case err := <-notifierErrCh:
			return fmt.Errorf("new head notifier failed: %w", err)
		case <-newHeadCh:
		}
	}
}

func (l *Listener) RegisterIssued(txHash common.Hash) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.nonce++
	l.inFlightTxs = append(l.inFlightTxs, txHash)
}

func (l *Listener) IssuingDone() {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.allIssued = true
}

func (l *Listener) markRemainingAsFailed() {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	for _, txHash := range l.inFlightTxs {
		l.tracker.ObserveFailed(txHash)
	}
	l.inFlightTxs = nil
}
