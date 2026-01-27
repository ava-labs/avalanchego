// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/utils/bag"
)

// Consensus represents a general snowman instance that can be used directly to
// process a series of dependent operations.
type Consensus interface {
	health.Checker

	// Takes in the context, snowball parameters, and the last accepted block.
	Initialize(
		ctx *snow.ConsensusContext,
		params snowball.Parameters,
		lastAcceptedID ids.ID,
		lastAcceptedHeight uint64,
		lastAcceptedTime time.Time,
	) error

	// Returns the number of blocks processing
	NumProcessing() int

	// Add a new block.
	//
	// Add should not be called multiple times with the same block.
	// The parent block should either be the last accepted block or processing.
	//
	// Returns if a critical error has occurred.
	Add(Block) error

	// Processing returns true if the block ID is currently processing.
	Processing(ids.ID) bool

	// IsPreferred returns true if the block ID is preferred. Only the last
	// accepted block and processing blocks are considered preferred.
	IsPreferred(ids.ID) bool

	// Returns the ID and height of the last accepted decision.
	LastAccepted() (ids.ID, uint64)

	// Returns the ID of the tail of the strongly preferred sequence of
	// decisions.
	Preference() ids.ID

	// Returns the ID of the strongly preferred decision with the provided
	// height. Only the last accepted decision and processing decisions are
	// tracked.
	PreferenceAtHeight(height uint64) (ids.ID, bool)

	// RecordPoll collects the results of a network poll. Assumes all decisions
	// have been previously added. Returns if a critical error has occurred.
	RecordPoll(context.Context, bag.Bag[ids.ID]) error

	// GetParent returns the ID of the parent block with the given ID, if it is known.
	// Returns (Empty, false) if no such parent block is known.
	GetParent(id ids.ID) (ids.ID, bool)
}
