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
	Vote(requestID uint32, vdr ids.ShortID, votes []ids.ID) (ids.UniqueBag, bool)
	Len() int
}

// Poll is an outstanding poll
type Poll interface {
	fmt.Stringer
	PrefixedString(string) string

	Vote(vdr ids.ShortID, votes []ids.ID)
	Finished() bool
	Result() ids.UniqueBag
}

// Factory creates a new Poll
type Factory interface {
	New(vdrs ids.ShortBag) Poll
}
