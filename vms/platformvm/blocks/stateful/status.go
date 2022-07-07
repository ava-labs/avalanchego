// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
)

// TODO can we remove this?
type statusGetter interface {
	status(blkID ids.ID) choices.Status
}

type statusGetterImpl struct {
	backend
}

func (s *statusGetterImpl) status(blkID ids.ID) choices.Status {
	return s.blkIDToState[blkID].status
}
