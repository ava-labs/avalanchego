// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package choices

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
)

// Decidable represents element that can be decided.
//
// Decidable objects are typically thought of as either transactions, blocks, or
// vertices.
type Decidable interface {
	// ID returns a unique ID for this element.
	//
	// Typically, this is implemented by using a cryptographic hash of a
	// binary representation of this element. An element should return the same
	// IDs upon repeated calls.
	ID() ids.ID

	// Accept this element.
	//
	// This element will be accepted by every correct node in the network.
	// All subsequent Status calls return Accepted.
	Accept(context.Context) error

	// Reject this element.
	//
	// This element will not be accepted by any correct node in the network.
	// All subsequent Status calls return Rejected.
	Reject(context.Context) error

	// Status returns this element's current status.
	//
	// If Accept has been called on an element with this ID, Accepted should be
	// returned. Similarly, if Reject has been called on an element with this
	// ID, Rejected should be returned. If the contents of this element are
	// unknown, then Unknown should be returned. Otherwise, Processing should be
	// returned.
	//
	// TODO: Consider allowing Status to return an error.
	Status() Status
}
