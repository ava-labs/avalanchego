// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package issuer

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

func (i *Issuer) listenSub(ctx context.Context) error {
	headNotifier := newHeadNotifier(i.client)
	newHeadCh, notifierErrCh, err := headNotifier.start(ctx)
	if err != nil {
		return fmt.Errorf("starting new head notifier: %w", err)
	}
	defer headNotifier.stop()

	for {
		blockNumber := (*big.Int)(nil)
		nonce, err := i.client.NonceAt(ctx, i.address, blockNumber)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("checking last block account nonce for address %s: %w", i.address, err)
		}

		i.mutex.Lock()
		confirmed := uint64(len(i.inFlightTxHashes))
		if nonce < i.lastIssuedNonce { // lagging behind last issued nonce
			lag := i.lastIssuedNonce - nonce
			confirmed -= lag
		}
		for index := range confirmed {
			txHash := i.inFlightTxHashes[index]
			i.tracker.ObserveConfirmed(txHash)
		}
		i.inFlightTxHashes = i.inFlightTxHashes[confirmed:]
		finished := i.allIssued && len(i.inFlightTxHashes) == 0
		if finished {
			i.mutex.Unlock()
			return nil
		}
		i.mutex.Unlock()

		select {
		case <-ctx.Done():
			return nil
		case err := <-notifierErrCh:
			return fmt.Errorf("new head notifier failed: %w", err)
		case blockNumber := <-newHeadCh:
			i.tracker.ObserveBlock(blockNumber)
		}
	}
}
