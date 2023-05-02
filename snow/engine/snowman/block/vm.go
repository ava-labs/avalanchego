// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

// ChainVM defines the required functionality of a Snowman VM.
//
// A Snowman VM defines the representation of state, operations on that state,
// application of the operations on that state, and the creation of the operations.
// The consensus decides whether to execute the operation, and the order that
// the operations are executed in.
//
// For example, suppose we have a VM that tracks an increasing number that
// is agreed upon by the network.
// The state is a single number.
// The operation is setting the number to a new, larger value.
// Applying the operation will save to the database the new value.
// The VM can attempt to issue a new number, of larger value, at any time.
// Consensus will ensure the network agrees on the number at every block height.
type ChainVM interface {
	common.VM

	Getter
	Parser

	// Attempt to create a new block from data contained in the VM.
	//
	// If the VM doesn't want to issue a new block, an error should be
	// returned.
	BuildBlock(context.Context) (snowman.Block, error)

	// Notify the VM of the currently preferred block.
	//
	// This should always be a block that has no children known to consensus.
	SetPreference(ctx context.Context, blkID ids.ID) error

	// LastAccepted returns the ID of the last accepted block.
	//
	// If no block has been accepted by consensus yet, it returns the Genesis block,
	// that is definitionally accepted.
	LastAccepted(context.Context) (ids.ID, error)
}

// Getter defines the functionality for fetching a block by its ID.
type Getter interface {
	// Attempt to load a block.
	//
	// If the block does not exist, database.ErrNotFound should be returned.
	//
	// It is expected that blocks that have been successfully verified should be
	// returned correctly. It is also expected that blocks that have been
	// accepted by the consensus engine should be able to be fetched. It is not
	// required for blocks that have been rejected by the consensus engine to be
	// able to be fetched.
	GetBlock(ctx context.Context, blkID ids.ID) (snowman.Block, error)
}

// Parser defines the functionality for fetching a block by its bytes.
type Parser interface {
	// Attempt to create a block from a stream of bytes.
	//
	// The block should be represented by the full byte array, without extra
	// bytes.
	//
	// It is expected for all historical blocks to be parseable.
	ParseBlock(ctx context.Context, blockBytes []byte) (snowman.Block, error)
}
