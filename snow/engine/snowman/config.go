// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"github.com/MetalBlockchain/metalgo/snow"
	"github.com/MetalBlockchain/metalgo/snow/consensus/snowball"
	"github.com/MetalBlockchain/metalgo/snow/consensus/snowman"
	"github.com/MetalBlockchain/metalgo/snow/engine/common"
	"github.com/MetalBlockchain/metalgo/snow/engine/common/tracker"
	"github.com/MetalBlockchain/metalgo/snow/engine/snowman/block"
	"github.com/MetalBlockchain/metalgo/snow/validators"
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
}
