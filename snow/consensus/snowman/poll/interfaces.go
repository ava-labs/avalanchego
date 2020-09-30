// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

// Set is a collection of polls
type Set interface {
	fmt.Stringer

	Add(requestID uint32, vdrs ids.ShortBag) bool
	Vote(requestID uint32, vdr ids.ShortID, vote ids.ID) (ids.Bag, bool)
	Drop(requestID uint32, vdr ids.ShortID) (ids.Bag, bool)
	Len() int
}

// Poll is an outstanding poll
type Poll interface {
	fmt.Stringer
	PrefixedString(string) string

	Vote(vdr ids.ShortID, vote ids.ID)
	Drop(vdr ids.ShortID)
	Finished() bool
	Result() ids.Bag
}

// Factory creates a new Poll
type Factory interface {
	New(vdrs ids.ShortBag) Poll
}
