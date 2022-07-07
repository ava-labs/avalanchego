// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
)

var _ stateless.Statuser = &statusGetterImpl{}

type statusGetterImpl struct {
	backend
}

func (s *statusGetterImpl) Status(blkID ids.ID) choices.Status {
	return s.blkIDToState[blkID].status
}
