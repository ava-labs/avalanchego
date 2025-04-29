// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package issuer

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/coreth/ethclient"
	"github.com/ava-labs/libevm/common"
	"go.uber.org/zap"
)

func (i *Issuer) listenPoll(tc tests.TestContext, ctx context.Context) error {
	const period = 100 * time.Millisecond
	ticker := time.NewTicker(period)
	defer ticker.Stop()

	for {
		tc.Log().Info("polling for tx acceptance...")
		blockNumber, nonce, err := getBlockNumberAndNonce(tc, ctx, i.client, i.address)
		if err != nil {
			tc.Log().Error("polling for tx acceptance failed",
				zap.Error(err),
			)
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("polling node: %w", err)
		}
		tc.Log().Info("polled for block number and nonce",
			zap.Uint64("blockNumber", blockNumber),
			zap.Uint64("nonce", nonce),
		)

		i.tracker.ObserveBlock(blockNumber)

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
		tc.Log().Info("polled for tx acceptance",
			zap.Uint64("blockNumber", blockNumber),
			zap.Uint64("nonce", nonce),
			zap.Uint64("lastIssuedNonce", i.lastIssuedNonce),
			zap.Uint64("issuedTxs", i.issuedTxs),
			zap.Uint64("confirmed", confirmed),
			zap.Bool("allIssued", i.allIssued),
			zap.Bool("finished", finished),
			zap.Int("remainingInFlightTxs", len(i.inFlightTxHashes)),
		)
		if finished {
			i.mutex.Unlock()
			return nil
		}

		i.mutex.Unlock()

		select {
		case <-ctx.Done():
			tc.Log().Info("listenPoll stopped due to context being done")
			return nil
		case <-ticker.C:
		}
	}
}

func getBlockNumberAndNonce(
	tc tests.TestContext,
	ctx context.Context,
	client ethclient.Client,
	address common.Address) (blockNumber, nonce uint64, err error) {
	number, err := client.BlockNumber(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("error getting block number: %w", err)
	}
	tc.Log().Info("got block number", zap.Uint64("blockNumber", number))

	nonce, err = client.NonceAt(ctx, address, new(big.Int).SetUint64(number))
	if err != nil {
		return 0, 0, fmt.Errorf("error checking last block account nonce for address %s: %w", address, err)
	}
	tc.Log().Info("got nonce", zap.Uint64("nonce", nonce))

	return blockNumber, nonce, err
}
