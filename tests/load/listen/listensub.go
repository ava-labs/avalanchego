// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package listen

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common"
)

type EthClientSubscriber interface {
	NonceAt(ctx context.Context, addr common.Address, blockNumber *big.Int) (uint64, error)
	NewHeadSubscriber
}

func (l *Listener) listenSub(ctx context.Context) error {
	headNotifier := newHeadNotifier(l.client)
	newHeadCh, notifierErrCh, err := headNotifier.start(ctx)
	if err != nil {
		return fmt.Errorf("starting new head notifier: %w", err)
	}
	defer headNotifier.stop()

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
		case blockNumber := <-newHeadCh:
			l.tracker.ObserveBlock(blockNumber)
		}
	}
}
