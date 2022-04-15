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

package poll

import (
	"fmt"

	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/utils/formatting"
)

// Set is a collection of polls
type Set interface {
	fmt.Stringer

	Add(requestID uint32, vdrs ids.ShortBag) bool
	Vote(requestID uint32, vdr ids.ShortID, vote ids.ID) []ids.Bag
	Drop(requestID uint32, vdr ids.ShortID) []ids.Bag
	Len() int
}

// Poll is an outstanding poll
type Poll interface {
	formatting.PrefixedStringer

	Vote(vdr ids.ShortID, vote ids.ID)
	Drop(vdr ids.ShortID)
	Finished() bool
	Result() ids.Bag
}

// Factory creates a new Poll
type Factory interface {
	New(vdrs ids.ShortBag) Poll
}
