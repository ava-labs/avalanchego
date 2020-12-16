// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package conflicts

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
)

// Tx manages state transitions in epochs, and adds transition restrictions.
type Tx interface {
	choices.Decidable

	// Transition is the description for the transition this transaction will
	// perform if accepted.
	Transition() Transition

	// Epoch this transaction was issued in.
	Epoch() uint32

	// Restrictions returns a list of transition IDs that need to be performed
	// in this transaction's epoch or later if this transaction is accepted.
	Restrictions() []ids.ID

	// Verify this transaction is currently valid to be performed.
	Verify() error

	// Bytes that fully describe this transaction.
	Bytes() []byte
}
