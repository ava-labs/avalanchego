// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cmd

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ava-labs/avalanchego/tests/reexecute/blockdb"
	ethclient "github.com/ava-labs/coreth/plugin/evm/customethclient"
	_ "github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/libevm/rlp"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

const (
	dbDirKey       = "db-dir"
	startBlockKey  = "start-block"
	endBlockKey    = "end-block"
	rpcURLKey      = "rpc-url"
	concurrencyKey = "concurrency"
)

// fetchBlocksCmd represents the fetchBlocks command
var fetchBlocksCmd = &cobra.Command{
	Use:   "fetchBlocks",
	Short: "Fetch blocks from the network and write to the specified database",
	RunE:  runFetchBlocks,
}

func init() {
	rootCmd.AddCommand(fetchBlocksCmd)

	fetchBlocksCmd.Flags().String(dbDirKey, "", "Database to store the fetched blocks")
	fetchBlocksCmd.Flags().String(rpcURLKey, "http://localhost:9650/ext/bc/C/rpc", "Ethereum RPC URL to fetch blocks from")
	fetchBlocksCmd.Flags().Uint64(startBlockKey, 0, "Block number to start fetching from (inclusive)")
	fetchBlocksCmd.Flags().Uint64(endBlockKey, 0, "Block number to stop fetching at (inclusive)")
	fetchBlocksCmd.Flags().Int(concurrencyKey, 1000, "Number of concurrent fetches to make")
}

func runFetchBlocks(cmd *cobra.Command, args []string) error {
	dbDir, err := cmd.Flags().GetString(dbDirKey)
	if err != nil {
		return fmt.Errorf("failed to get db-dir flag: %w", err)
	}
	startBlock, err := cmd.Flags().GetUint64(startBlockKey)
	if err != nil {
		return fmt.Errorf("failed to get start-block flag: %w", err)
	}
	endBlock, err := cmd.Flags().GetUint64(endBlockKey)
	if err != nil {
		return fmt.Errorf("failed to get end-block flag: %w", err)
	}
	rpcURL, err := cmd.Flags().GetString(rpcURLKey)
	if err != nil {
		return fmt.Errorf("failed to get rpc-url flag: %w", err)
	}
	concurrency, err := cmd.Flags().GetInt(concurrencyKey)
	if err != nil {
		return fmt.Errorf("failed to get concurrency flag: %w", err)
	}

	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return fmt.Errorf("failed to connect to RPC URL %s: %w", rpcURL, err)
	}

	blockDB, err := blockdb.New(dbDir)
	if err != nil {
		return err
	}

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
			blocksCh <- i
		}
	}()

	eg := errgroup.Group{}
	for i := 0; i < concurrency; i++ {
		eg.Go(func() error {
			for blockNum := range blocksCh {
				log.Debug("Fetching block", zap.Uint64("blockNumber", blockNum))
				block, err := client.BlockByNumber(context.Background(), new(big.Int).SetUint64(blockNum))
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
