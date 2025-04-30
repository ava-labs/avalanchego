// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package listen

import (
	"context"
	"fmt"
	"math/big"
	"sync"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
)

type EthClient interface {
	NonceAt(ctx context.Context, addr common.Address, blockNumber *big.Int) (uint64, error)
	NewHeadSubscriber
}

type Tracker interface {
	ObserveConfirmed(txHash common.Hash)
	ObserveFailed(txHash common.Hash)
}

// Listener listens for transaction confirmations from a node.
type Listener struct {
	// Injected parameters
	client   EthClient
	tracker  Tracker
	txTarget uint64
	address  common.Address

	// Internal state
	mutex            sync.Mutex
	issued           uint64
	lastIssuedNonce  uint64
	inFlightTxHashes []common.Hash
}

func New(client EthClient, tracker Tracker, txTarget uint64, address common.Address) *Listener {
	return &Listener{
		client:   client,
		tracker:  tracker,
		txTarget: txTarget,
		address:  address,
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
		confirmed := uint64(len(l.inFlightTxHashes))
		if nonce < l.lastIssuedNonce { // lagging behind last issued nonce
			lag := l.lastIssuedNonce - nonce
			confirmed -= lag
		}
		for index := range confirmed {
			txHash := l.inFlightTxHashes[index]
			l.tracker.ObserveConfirmed(txHash)
		}
		l.inFlightTxHashes = l.inFlightTxHashes[confirmed:]
		finished := l.issued == l.txTarget && len(l.inFlightTxHashes) == 0
		if finished {
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

func (l *Listener) RegisterIssued(tx *types.Transaction) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.issued++
	l.lastIssuedNonce = tx.Nonce()
	l.inFlightTxHashes = append(l.inFlightTxHashes, tx.Hash())
}

func (l *Listener) markRemainingAsFailed() {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	for _, txHash := range l.inFlightTxHashes {
		l.tracker.ObserveFailed(txHash)
	}
	l.inFlightTxHashes = nil
}
