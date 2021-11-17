// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

// Engine describes the events that can occur to a Snowman instance.
//
// The engine is used to fetch, order, and decide on the fate of blocks. This
// engine runs the leaderless version of the Snowman consensus protocol.
// Therefore, the liveness of this protocol tolerant to O(sqrt(n)) Byzantine
// Nodes where n is the number of nodes in the network. Therefore, this protocol
// should only be run in a Crash Fault Tolerant environment, or in an
// environment where lose of liveness and manual intervention is tolerable.
type Engine interface {
	common.Engine
	block.Getter
}
