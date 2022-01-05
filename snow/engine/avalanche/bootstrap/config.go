// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	basemsghandler "github.com/ava-labs/avalanchego/snow/engine/avalanche/base_msg_handler"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/queue"
)

type Config struct {
	common.Config

	basemsghandler.Handler

	// VtxBlocked tracks operations that are blocked on vertices
	VtxBlocked *queue.JobsWithMissing
	// TxBlocked tracks operations that are blocked on transactions
	TxBlocked *queue.Jobs

	Manager       vertex.Manager
	VM            vertex.DAGVM
	WeightTracker common.WeightTracker
}
