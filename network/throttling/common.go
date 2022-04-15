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

package throttling

import (
	"sync"

	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/snow/validators"
	"github.com/chain4travel/caminogo/utils/logging"
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
	// Primary network validator set
	vdrs validators.Set
	// Max number of bytes that can be taken from the
	// at-large byte allocation by a given node.
	nodeMaxAtLargeBytes uint64
	// Number of bytes left in the validator byte allocation.
	// Initialized to [maxVdrBytes].
	remainingVdrBytes uint64
	// Number of bytes left in the at-large byte allocation
	remainingAtLargeBytes uint64
	// Node ID --> Bytes they've taken from the validator allocation
	nodeToVdrBytesUsed map[ids.ShortID]uint64
	// Node ID --> Bytes they've taken from the at-large allocation
	nodeToAtLargeBytesUsed map[ids.ShortID]uint64
	// Max number of unprocessed bytes from validators
	maxVdrBytes uint64
}
