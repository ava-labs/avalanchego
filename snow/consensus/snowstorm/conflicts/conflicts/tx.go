// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package conflicts

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
)

// Tx consumes state.
type Tx interface {
	choices.Decidable

	// TransitionID is a unique identifier for the transition this transaction
	// will perform.
	TransitionID() ids.ID

	// Epoch this transaction was issued to.
	Epoch() uint32

	// Restrictions this transaction is attempting to enforce.
	Restrictions() []ids.ID

	// Dependencies is a list of incomplete TransitionIDs upon which this
	// transaction depends.
	Dependencies() []ids.ID

	// InputIDs is a set where each element is the ID of a piece of state that
	// will be consumed if this transaction is accepted.
	//
	// In the context of a UTXO-based payments system, for example, this would
	// be the IDs of the UTXOs consumed by this transaction.
	InputIDs() ids.Set
}
