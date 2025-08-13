// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// Used by the sybil-safe inbound and outbound message throttlers
type MsgByteThrottlerConfig struct {
	VdrAllocSize        uint64 `json:"vdrAllocSize"`
	AtLargeAllocSize    uint64 `json:"atLargeAllocSize"`
	NodeMaxAtLargeBytes uint64 `json:"nodeMaxAtLargeBytes"`
}

// Used by the sybil-safe inbound and outbound message throttlers
type commonMsgThrottler struct {
	log  logging.Logger
	lock sync.Mutex
	vdrs validators.Manager
	// Max number of bytes that can be taken from the
	// at-large byte allocation by a given node.
	nodeMaxAtLargeBytes uint64
	// Number of bytes left in the validator byte allocation.
	// Initialized to [maxVdrBytes].
	remainingVdrBytes uint64
	// Number of bytes left in the at-large byte allocation
	remainingAtLargeBytes uint64
	// Node ID --> Bytes they've taken from the validator allocation
	nodeToVdrBytesUsed map[ids.NodeID]uint64
	// Node ID --> Bytes they've taken from the at-large allocation
	nodeToAtLargeBytesUsed map[ids.NodeID]uint64
	// Max number of unprocessed bytes from validators
	maxVdrBytes uint64
}
