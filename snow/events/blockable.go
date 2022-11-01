// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package events

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

// Blockable defines what an object must implement to be able to block on
// dependent events being completed.
type Blockable interface {
	// IDs that this object is blocking on
	Dependencies() set.Set[ids.ID]
	// Notify this object that an event has been fulfilled
	Fulfill(context.Context, ids.ID)
	// Notify this object that an event has been abandoned
	Abandon(context.Context, ids.ID)
	// Update the state of this object without changing the status of any events
	Update(context.Context)
}
