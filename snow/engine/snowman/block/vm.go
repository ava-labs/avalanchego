// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
// A Snowman VM is responsible for defining the representation of state,
// the representation of operations on that state, the application of operations
// on that state, and the creation of the operations. Consensus will decide on
// if the operation is executed and the order operations are executed in.
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
	// If no blocks have been accepted by consensus yet, it is assumed there is
	// a definitionally accepted block, the Genesis block, that will be
	// returned.
	LastAccepted(context.Context) (ids.ID, error)

	// GetBlockIDAtHeight returns:
	// - The ID of the block that was accepted with [height].
	// - database.ErrNotFound if the [height] index is unknown.
	//
	// Note: A returned value of [database.ErrNotFound] typically means that the
	//       underlying VM was state synced and does not have access to the
	//       blockID at [height].
	GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error)
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

// ParseFunc defines a function that parses raw bytes into a block.
type ParseFunc func(context.Context, []byte) (snowman.Block, error)

// ParseBlock wraps a ParseFunc into a ParseBlock function, to be used by a Parser interface
func (f ParseFunc) ParseBlock(ctx context.Context, blockBytes []byte) (snowman.Block, error) {
	return f(ctx, blockBytes)
}
