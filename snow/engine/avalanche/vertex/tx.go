// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
)

// Tx consumes state.
type Tx interface {
	// ID returns a unique ID for this transaction.
	//
	// Typically, this is implemented by using a cryptographic hash of a binary
	// representation of this transaction. A transaction should return the same
	// ID upon repeated calls.
	ID() ids.ID

	// Status returns this transaction's current status.
	//
	// If Accept has been called on a transaction with this ID, Accepted should
	// be returned. Similarly, if Reject has been called on a transaction with
	// this ID, Rejected should be returned. If the body of this transaction is
	// unknown, then Unknown should be returned. Otherwise, Processing should be
	// returned.
	Status() choices.Status

	// Epoch this transaction was accepted in.
	//
	// If the status of this transaction isn't Accepted, the behavior of this
	// call is undefined.
	Epoch() uint32

	// Dependencies is a list of transactions upon which this transaction
	// depends.
	//
	// Each Dependency will be verified before Verify is called on this
	// transaction. Similarly, each element of Dependencies must be accepted
	// before this transaction is accepted.
	Dependencies() []Tx

	// InputIDs is a set where each element is the ID of a piece of state that
	// will be consumed if this transaction is accepted.
	//
	// In the context of a UTXO-based payments system, for example, this would
	// be the IDs of the UTXOs consumed by this transaction
	InputIDs() []ids.ID

	// Bytes returns a binary representation of this transaction.
	Bytes() []byte

	// Verify this transaction as being valid.
	//
	// Verify should return nil if acceptance is allowed assuming:
	// - all dependencies are accepted
	// - no transactions with overlapping inputIDs are accepted
	Verify(epoch uint32) error

	// Accept this element in the provided epoch.
	//
	// This element will be accepted by every correct node in the network in the
	// provided epoch.
	Accept(epoch uint32) error

	// Reject this element from the provided epoch.
	//
	// This element will not be accepted by any correct node in the network in
	// the provided epoch.
	Reject(epoch uint32) error
}
