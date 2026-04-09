// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package params declares [Streaming Asynchronous Execution] (SAE) parameters.
//
// [Streaming Asynchronous Execution]: https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/194-streaming-asynchronous-execution
package params

import "time"

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
