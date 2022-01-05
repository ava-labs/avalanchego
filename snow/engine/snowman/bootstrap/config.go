// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/queue"
	basemsghandler "github.com/ava-labs/avalanchego/snow/engine/snowman/base_msg_handler"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

type Config struct {
	common.Config

	basemsghandler.Handler

	// Blocked tracks operations that are blocked on blocks
	Blocked *queue.JobsWithMissing

	VM            block.ChainVM
	WeightTracker common.WeightTracker

	Bootstrapped func()
}
