// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm/conflicts"
)

// Conflicts is an interface used to manage conflict sets
type Conflicts interface {
	// Add this transaction to conflict tracking
	Add(tx conflicts.Tx) error

	// Processing returns if this transition is currently being processed
	Processing(trID ids.ID) bool

	// Conflicts returns true if there are no transactions currently tracked
	IsVirtuous(tx conflicts.Tx) (bool, error)

	// Conflicts returns the transactions that conflict with the provided
	// transaction
	Conflicts(tx conflicts.Tx) ([]conflicts.Tx, error)

	// Mark this transaction as conditionally accepted
	Accept(txID ids.ID)

	// Updateable returns the transactions that can be accepted and rejected.
	// Acceptable transactions must have been identified as having been conditionally accepted.
	// If an acceptable transaction was marked as having a conflict, then that conflict
	// should be returned in the same call as the acceptable transaction was returned or
	// in a prior call.
	Updateable() (acceptable []conflicts.Tx, rejectable []conflicts.Tx)
}
