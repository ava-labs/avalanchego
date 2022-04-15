// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package events

import (
	"github.com/chain4travel/caminogo/ids"
)

// Blockable defines what an object must implement to be able to block on events
type Blockable interface {
	// IDs that this object is blocking on
	Dependencies() ids.Set
	// Notify this object that an event has been fulfilled
	Fulfill(ids.ID)
	// Notify this object that an event has been abandoned
	Abandon(ids.ID)
	// Update the state of this object without changing the status of any events
	Update()
}
