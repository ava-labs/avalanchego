// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
)

var _ stateless.Timestamper = &timestampGetterImpl{}

type timestampGetterImpl struct {
	backend
}

// TODO add comment
func (t *timestampGetterImpl) Timestamp(blkID ids.ID) time.Time {
	// timestamp := t.blkIDToTimestamp[blkID]
	// if timestamp.IsZero() {
	// 	return t.state.GetTimestamp()
	// }
	// return timestamp
	blockState, ok := t.blkIDToState[blkID]
	if !ok {
		return t.state.GetTimestamp()
	}
	return blockState.timestamp
}
