// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warpmsg

import (
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

// VerifyContext is the context that Warp messages are verified within.
type VerifyContext struct {
	SnowCtx *snow.Context
	// ProposerVMBlockCtx is the ProposerVM context that sets the P-Chain
	// height of the validator sets.
	ProposerVMBlockCtx *block.Context
}
