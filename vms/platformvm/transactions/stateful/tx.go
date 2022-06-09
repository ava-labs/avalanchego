// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
)

type Tx interface{}

// DecisionTx is an unsigned operation that can be immediately decided
type DecisionTx interface {
	Tx

	// To maintain consistency with the Atomic txs
	InputUTXOs() ids.Set

	// AtomicOperations provides the requests to be written to shared memory.
	AtomicOperations() (ids.ID, *atomic.Requests, error)
}

// AtomicTx is an unsigned operation that can be atomically accepted. DEPRECATED
type AtomicTx interface {
	DecisionTx

	// Accept this transaction with the additionally provided state transitions.
	AtomicAccept(ctx *snow.Context, batch database.Batch) error
}
