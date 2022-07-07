// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
)

type statusGetter interface {
	status(blkID ids.ID) choices.Status
}

type statusGetterImpl struct {
	backend
}

func (s *statusGetterImpl) status(blkID ids.ID) choices.Status {
	status := s.blkIDToStatus[blkID]
	// TODO fix this
	//if status == choices.Unknown {
	//	return s.state.Status(blkID)
	//}
	return status
}
