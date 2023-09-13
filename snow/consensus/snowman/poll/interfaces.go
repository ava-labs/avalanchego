// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bag"
	"github.com/ava-labs/avalanchego/utils/formatting"
)

// Set is a collection of polls
type Set interface {
	fmt.Stringer

	Add(requestID uint32, vdrs bag.Bag[ids.GenericNodeID]) bool
	Vote(requestID uint32, vdr ids.GenericNodeID, vote ids.ID) []bag.Bag[ids.ID]
	Drop(requestID uint32, vdr ids.GenericNodeID) []bag.Bag[ids.ID]
	Len() int
}

// Poll is an outstanding poll
type Poll interface {
	formatting.PrefixedStringer

	Vote(vdr ids.GenericNodeID, vote ids.ID)
	Drop(vdr ids.GenericNodeID)
	Finished() bool
	Result() bag.Bag[ids.ID]
}

// Factory creates a new Poll
type Factory interface {
	New(vdrs bag.Bag[ids.GenericNodeID]) Poll
}
