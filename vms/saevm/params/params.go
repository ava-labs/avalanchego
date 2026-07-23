// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package params declares [Streaming Asynchronous Execution] (SAE) parameters.
//
// [Streaming Asynchronous Execution]: https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/194-continuous-execution
package params

import (
	"time"

	"github.com/ava-labs/avalanchego/utils/units"
)

// Lambda is the denominator for computing the minimum gas consumed per
// transaction. For a transaction with gas limit `g`, the minimum consumption is
// ceil(g/Lambda).
const Lambda = 2

// Tau is the minimum duration between a block's execution completing and the
// resulting state changes being settled in a later block. Note that this period
// has no effect on the availability nor finality of results, both of which are
// immediate at the time of executing an individual transaction.
const (
	Tau        = TauSeconds * time.Second
	TauSeconds = 5
)

// MaxFullBlocksInOpenQueue is the maximum number of full blocks that can be
// in the execution queue while it remains open to accepting a new block. An
// open queue MAY accept an entire, maximal block, which could leave it in an
// allowed over-threshold (closed) state.
const MaxFullBlocksInOpenQueue = 2

// MaxFullBlocksInClosedQueue is the maximum number of full blocks that can be
// in the execution queue.
const MaxFullBlocksInClosedQueue = MaxFullBlocksInOpenQueue + 1

// MaxQueueWallTime is the maximum wall-clock duration a block should remain in
// the execution queue before execution finishes. This assumes the executor
// drains the queue at least as fast as the gas capacity rate R.
const MaxQueueWallTime = MaxFullBlocksInClosedQueue * Tau * Lambda

// MaxBlockBytes is the hard maximum block size allowed during verification.
//
// The 2 MiB value matches the constants.DefaultMaxMessageSize, while the
// 256 KiB subtraction is a safety margin to allow things like the proto
// serialization overhead and the proposervm block header.
const MaxBlockBytes = 2*units.MiB - 256*units.KiB

// TargetBlockBytes is the target block size used during block building.
//
// The 256 KiB subtraction allows for minor inaccuracies while calculating
// intermediate block sizes while building.
//
// Changing this value IS NOT a consensus-breaking change.
const TargetBlockBytes = MaxBlockBytes - 256*units.KiB

// TargetBlockBytes < MaxBlockBytes
const _ = uint(MaxBlockBytes - TargetBlockBytes - 1)
