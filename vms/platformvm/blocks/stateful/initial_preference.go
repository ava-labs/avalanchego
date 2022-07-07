// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import "github.com/ava-labs/avalanchego/ids"

type initialPreferenceGetter interface {
	// True if we initially preferred to commit [blkID],
	// a ProposalBlock.
	preferredCommit(blkID ids.ID) bool
}

type initialPreferenceGetterImpl struct {
	backend
}

func (i *initialPreferenceGetterImpl) preferredCommit(blkID ids.ID) bool {
	// return i.blkIDToPreferCommit[blkID]
	return i.blkIDToState[blkID].inititallyPreferCommit
}
