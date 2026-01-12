// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
)

// Config wraps all the parameters needed for a snowman engine
type Config struct {
	common.AllGetsServer

	Ctx                 *snow.ConsensusContext
	VM                  block.ChainVM
	Sender              common.Sender
	Validators          validators.Manager
	ConnectedValidators tracker.Peers
	Params              snowball.Parameters
	Consensus           snowman.Consensus
	PartialSync         bool

	// RelayerNodeIDs specifies designated relayer nodes for this chain's subnet.
	// When set, the engine operates in "relayer mode":
	// - Only these nodes are used for consensus sampling
	// - Only accepted blocks (not preferences) are followed from chits
	RelayerNodeIDs []ids.NodeID
}

// IsRelayerMode returns true if relayer mode is enabled.
// Relayer mode is enabled when RelayerNodeIDs is non-empty.
func (c *Config) IsRelayerMode() bool {
	return len(c.RelayerNodeIDs) > 0
}
