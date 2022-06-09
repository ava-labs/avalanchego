// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
)

type Tx interface{}

// DecisionTx is an unsigned operation that can be immediately decided
type DecisionTx interface {
	Tx

	// AtomicOperations provides the requests to be written to shared memory.
	AtomicOperations() (ids.ID, *atomic.Requests, error)
}
