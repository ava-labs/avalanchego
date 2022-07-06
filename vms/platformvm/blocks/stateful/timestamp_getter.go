// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

type timestampGetter interface {
	GetTimestamp(blkID ids.ID) time.Time
}

type timestampGetterImpl struct {
	backend
}

// TODO add comment
func (t *timestampGetterImpl) GetTimestamp(blkID ids.ID) time.Time {
	timestamp := t.blkIDToTimestamp[blkID]
	if timestamp.IsZero() {
		return t.state.GetTimestamp()
	}
	return timestamp
}
