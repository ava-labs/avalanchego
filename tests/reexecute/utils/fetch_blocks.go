// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"context"
	"fmt"
	"math/big"

	ethclient "github.com/ava-labs/coreth/plugin/evm/customethclient"
	"github.com/ava-labs/libevm/rlp"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/utils/logging"
)

func FetchBlocksToBlockDB(ctx context.Context, log logging.Logger, dbDir string, startBlock, endBlock uint64, rpcURL string, concurrency int) error {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return fmt.Errorf("failed to connect to RPC URL %s: %w", rpcURL, err)
	}

	blockDB, err := NewBlockDB(dbDir)
	if err != nil {
		return err
	}
	defer blockDB.Close()

	log.Info("Fetching blocks",
		zap.Uint64("startBlock", startBlock),
		zap.Uint64("endBlock", endBlock),
		zap.String("rpcURL", rpcURL),
		zap.String("dbDir", dbDir),
	)

	blocksCh := make(chan uint64, concurrency)
	go func() {
		defer close(blocksCh)
		for i := startBlock; i <= endBlock; i++ {
			select {
			case blocksCh <- i:
			case <-ctx.Done():
				return
			}
		}
	}()

	eg, ctx := errgroup.WithContext(ctx)
	for i := 0; i < concurrency; i++ {
		eg.Go(func() error {
			for blockNum := range blocksCh {
				log.Debug("Fetching block", zap.Uint64("blockNumber", blockNum))
				block, err := client.BlockByNumber(ctx, new(big.Int).SetUint64(blockNum))
				if err != nil {
					return fmt.Errorf("failed to fetch block %d: %w", blockNum, err)
				}
				log.Debug("Fetched block",
					zap.Uint64("blockNumber", blockNum),
					zap.Stringer("blockHash", block.Hash()),
				)

				blockBytes, err := rlp.EncodeToBytes(block)
				if err != nil {
					return fmt.Errorf("failed to encode block %d: %w", blockNum, err)
				}
				if err := blockDB.WriteBlock(blockNum, blockBytes); err != nil {
					return err
				}
			}
			return nil
		})
	}
	return eg.Wait()
}
