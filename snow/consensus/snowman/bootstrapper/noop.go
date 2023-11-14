// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapper

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

var Noop Bootstrapper = noop{}

type noop struct{}

func (noop) GetAcceptedFrontiersToSend(context.Context) set.Set[ids.NodeID] {
	return nil
}

func (noop) RecordAcceptedFrontier(context.Context, ids.NodeID, ...ids.ID) {}

func (noop) GetAcceptedFrontier(context.Context) ([]ids.ID, bool) {
	return nil, false
}

func (noop) GetAcceptedToSend(context.Context) set.Set[ids.NodeID] {
	return nil
}

func (noop) RecordAccepted(context.Context, ids.NodeID, []ids.ID) error {
	return nil
}

func (noop) GetAccepted(context.Context) ([]ids.ID, bool) {
	return nil, false
}
