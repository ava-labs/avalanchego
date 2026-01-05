// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import "github.com/ava-labs/avalanchego/ids"

type Versions interface {
	// GetState returns the state of the chain after [blkID] has been accepted.
	// If the state is not known, `false` will be returned.
	GetState(blkID ids.ID) (Chain, bool)
}
