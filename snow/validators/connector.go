// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"context"

	"github.com/MetalBlockchain/metalgo/ids"
	"github.com/MetalBlockchain/metalgo/version"
)

// Connector represents a handler that is called when a connection is marked as
// connected or disconnected
type Connector interface {
	Connected(
		ctx context.Context,
		nodeID ids.NodeID,
		nodeVersion *version.Application,
	) error
	Disconnected(ctx context.Context, nodeID ids.NodeID) error
}
