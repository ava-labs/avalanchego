// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ava-labs/avalanchego/tests/reexecute/utils"
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

func runFetchBlocks(cmd *cobra.Command, _ []string) error {
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
	return utils.FetchBlocksToBlockDB(context.Background(), cliLog, dbDir, startBlock, endBlock, rpcURL, concurrency)
}
