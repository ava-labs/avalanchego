// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package random_network

import (
	"time"

	"github.com/ava-labs/simplex/simplex"
)

type FuzzConfig struct {
	// The minimum and maximum number of nodes in the network.
	MinNodes int // Default is 3.
	MaxNodes int // Default is 10.

	// The minimum and maximum number of transactions to be issued at a block. Default is between 5 and 20.
	MinTxsPerIssue int
	MaxTxsPerIssue int

	// Number of transactions per block. Default is 15.
	TxsPerBlock int

	// The number of blocks that must be finalized before ending the fuzz test. Default is 100.
	NumFinalizedBlocks int

	RandomSeed int64

	// Probability that a node will be randomly crashed. Default is .1 (10%).
	NodeCrashProbability float64

	// Probability that a crashed node will be restarted. Default is .5 (50%).
	NodeRecoverProbability float64

	// Amount to advance the time by. Default is simplex.DefaultMaxProposalWaitTime / 5.
	AdvanceTimeTickAmount time.Duration

	// Creates main.log for network logs and {nodeID-short}.log for each node.
	// NodeID is represented as a 16-character hex string (first 8 bytes).
	// Default directory is "tmp".
	// If empty, logging to files is disabled and logs will only be printed to console.
	LogDirectory string
}

func DefaultFuzzConfig() *FuzzConfig {
	return &FuzzConfig{
		MinNodes:               3,
		MaxNodes:               10,
		MinTxsPerIssue:         5,
		MaxTxsPerIssue:         20,
		TxsPerBlock:            15,
		NumFinalizedBlocks:     100,
		RandomSeed:             time.Now().UnixMilli(),
		NodeCrashProbability:   0.1,
		NodeRecoverProbability: 0.5,
		AdvanceTimeTickAmount:  simplex.DefaultMaxProposalWaitTime / 5,
		LogDirectory:           "tmp",
	}
}
