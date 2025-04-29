// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package listen

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ava-labs/libevm/common"
)

type EthClientPoll interface {
	BlockNumber(ctx context.Context) (uint64, error)
	NonceAt(ctx context.Context, addr common.Address, blockNumber *big.Int) (uint64, error)
}

func (l *Listener) listenPoll(ctx context.Context) error {
	const period = 50 * time.Millisecond
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	defer l.markRemainingAsFailed()

	for {
		blockNumber, nonce, err := pollParallel(ctx, l.client, l.address)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("polling node: %w", err)
		}

		l.tracker.ObserveBlock(blockNumber)

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
		case <-ticker.C:
		}
	}
}

type blockNumResult struct {
	blockNumber uint64
	err         error
}

type nonceResult struct {
	nonce uint64
	err   error
}

func pollParallel(ctx context.Context, client EthClientPoll, address common.Address) (
	blockNumber, nonce uint64, err error,
) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	blockNumberCh := make(chan blockNumResult)
	go func() {
		number, err := client.BlockNumber(ctx)
		if err != nil {
			err = fmt.Errorf("getting block number: %w", err)
		}
		blockNumberCh <- blockNumResult{blockNumber: number, err: err}
	}()

	nonceCh := make(chan nonceResult)
	go func() {
		nonceQueryBlock := (*big.Int)(nil)
		nonce, err = client.NonceAt(ctx, address, nonceQueryBlock)
		if err != nil {
			err = fmt.Errorf("checking last block account nonce for address %s: %w", address, err)
		}
		nonceCh <- nonceResult{nonce: nonce, err: err}
	}()

	const numGoroutines = 2
	for range numGoroutines {
		select {
		case result := <-blockNumberCh:
			if err == nil && result.err != nil {
				err = result.err
				cancel()
				continue
			}
			blockNumber = result.blockNumber
		case result := <-nonceCh:
			if err == nil && result.err != nil {
				err = result.err
				cancel()
				continue
			}
			nonce = result.nonce
		}
	}

	return blockNumber, nonce, err
}
