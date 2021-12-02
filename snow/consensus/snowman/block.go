// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
)

// Block is a possible decision that dictates the next canonical block.
//
// Blocks are guaranteed to be Verified, Accepted, and Rejected in topological
// order. Specifically, if Verify is called, then the parent has already been
// verified. If Accept is called, then the parent has already been accepted. If
// Reject is called, the parent has already been accepted or rejected.
//
// If the status of the block is Unknown, ID is assumed to be able to be called.
// If the status of the block is Accepted or Rejected; Parent, Verify, Accept,
// and Reject will never be called.
type Block interface {
	choices.Decidable

	// Parent returns the ID of this block's parent.
	Parent() ids.ID

	// Verify that the state transition this block would make if accepted is
	// valid. If the state transition is invalid, a non-nil error should be
	// returned.
	//
	// It is guaranteed that the Parent has been successfully verified.
	Verify() error

	// Bytes returns the binary representation of this block.
	//
	// This is used for sending blocks to peers. The bytes should be able to be
	// parsed into the same block on another node.
	Bytes() []byte

	// Height returns the height of this block in the chain.
	Height() uint64

	// Time this block was proposed at. This value should be consistent across
	// all nodes. If this block hasn't been successfully verified, any value can
	// be returned. If this block is the last accepted block, the timestamp must
	// be returned correctly. Otherwise, accepted blocks can return any value.
	Timestamp() time.Time
}
