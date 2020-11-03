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

	// Epoch this transaction was issued to.
	Epoch() uint32

	// Restrictions this transaction is attempting to enforce.
	Restrictions() []ids.ID

	// MissingInputIDs is a set where each element is the ID of a piece of state
	// that hasn't been produced yet.
	//
	// In the context of a UTXO-based payments system, for example, this would
	// be the IDs of the UTXOs consumed by this transaction.
	MissingInputIDs() ids.Set

	// InputIDs is a set where each element is the ID of a piece of state that
	// will be consumed if this transaction is accepted.
	//
	// In the context of a UTXO-based payments system, for example, this would
	// be the IDs of the UTXOs consumed by this transaction.
	InputIDs() ids.Set
}
