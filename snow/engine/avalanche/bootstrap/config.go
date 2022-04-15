// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"github.com/chain4travel/caminogo/snow/engine/avalanche/vertex"
	"github.com/chain4travel/caminogo/snow/engine/common"
	"github.com/chain4travel/caminogo/snow/engine/common/queue"
	"github.com/chain4travel/caminogo/snow/engine/common/tracker"
)

type Config struct {
	common.Config
	common.AllGetsServer

	// VtxBlocked tracks operations that are blocked on vertices
	VtxBlocked *queue.JobsWithMissing
	// TxBlocked tracks operations that are blocked on transactions
	TxBlocked *queue.Jobs

	Manager       vertex.Manager
	VM            vertex.DAGVM
	WeightTracker tracker.WeightTracker
}
