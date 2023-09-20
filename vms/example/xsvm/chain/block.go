// Copyright (C) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	"context"
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/set"

	smblock "github.com/ava-labs/avalanchego/snow/engine/snowman/block"

	"github.com/ava-labs/xsvm/execute"
	"github.com/ava-labs/xsvm/state"

	xsblock "github.com/ava-labs/xsvm/block"
)

const maxClockSkew = 10 * time.Second

var (
	_ Block = (*block)(nil)

	errMissingParent         = errors.New("missing parent block")
	errMissingChild          = errors.New("missing child block")
	errParentNotVerified     = errors.New("parent block has not been verified")
	errMissingState          = errors.New("missing state")
	errFutureTimestamp       = errors.New("future timestamp")
	errTimestampBeforeParent = errors.New("timestamp before parent")
	errWrongHeight           = errors.New("wrong height")
)

type Block interface {
	snowman.Block
	smblock.WithVerifyContext

	// State intends to return the new chain state following this block's
	// acceptance. The new chain state is built (but not persisted) following a
	// block's verification to allow block's descendants verification before
	// being accepted.
	State() (database.Database, error)
}

type block struct {
	*xsblock.Stateless

	chain *chain

	id     ids.ID
	status choices.Status
	bytes  []byte

	state               *versiondb.Database
	verifiedChildrenIDs set.Set[ids.ID]
}

func (b *block) ID() ids.ID {
	return b.id
}

func (b *block) Status() choices.Status {
	if !b.status.Decided() {
		b.status = b.calculateStatus()
	}
	return b.status
}

func (b *block) Parent() ids.ID {
	return b.ParentID
}

func (b *block) Bytes() []byte {
	return b.bytes
}

func (b *block) Height() uint64 {
	return b.Stateless.Height
}

func (b *block) Timestamp() time.Time {
	return b.Time()
}

func (b *block) Verify(ctx context.Context) error {
	return b.VerifyWithContext(ctx, nil)
}

func (b *block) Accept(context.Context) error {
	if err := b.state.Commit(); err != nil {
		return err
	}

	// Following this block's acceptance, make sure that it's direct children
	// point to the base state, which now also contains this block's changes.
	for childID := range b.verifiedChildrenIDs {
		child, exists := b.chain.verifiedBlocks[childID]
		if !exists {
			return errMissingChild
		}
		if err := child.state.SetDatabase(b.chain.acceptedState); err != nil {
			return err
		}
	}

	b.status = choices.Accepted
	b.chain.lastAccepted = b.id
	delete(b.chain.verifiedBlocks, b.ParentID)
	return nil
}

func (b *block) Reject(context.Context) error {
	b.status = choices.Rejected
	delete(b.chain.verifiedBlocks, b.id)

	// TODO: push transactions back into the mempool
	return nil
}

func (b *block) ShouldVerifyWithContext(context.Context) (bool, error) {
	return execute.ExpectsContext(b.Stateless)
}

func (b *block) VerifyWithContext(ctx context.Context, blockContext *smblock.Context) error {
	timestamp := b.Time()
	if time.Until(timestamp) > maxClockSkew {
		return errFutureTimestamp
	}

	// parent block must be verified or accepted
	parent, exists := b.chain.verifiedBlocks[b.ParentID]
	if !exists {
		return errMissingParent
	}

	if b.Stateless.Height != parent.Stateless.Height+1 {
		return errWrongHeight
	}

	parentTimestamp := parent.Time()
	if timestamp.Before(parentTimestamp) {
		return errTimestampBeforeParent
	}

	parentState, err := parent.State()
	if err != nil {
		return err
	}

	// This block's state is a versionDB built on top of it's parent state. This
	// block's changes are pushed atomically to the parent state when accepted.
	blkState := versiondb.New(parentState)
	err = execute.Block(
		ctx,
		b.chain.chainContext,
		blkState,
		b.chain.chainState == snow.Bootstrapping,
		blockContext,
		b.Stateless,
	)
	if err != nil {
		return err
	}

	// Make sure to only state the state the first time we verify this block.
	if b.state == nil {
		b.state = blkState
		parent.verifiedChildrenIDs.Add(b.id)
		b.chain.verifiedBlocks[b.id] = b
	}

	return nil
}

func (b *block) State() (database.Database, error) {
	if b.id == b.chain.lastAccepted {
		return b.chain.acceptedState, nil
	}

	// States of accepted blocks other than the lastAccepted are undefined.
	if b.Status() == choices.Accepted {
		return nil, errMissingState
	}

	// We should not be calling State on an unverified block.
	if b.state == nil {
		return nil, errParentNotVerified
	}

	return b.state, nil
}

func (b *block) calculateStatus() choices.Status {
	if b.chain.lastAccepted == b.id {
		return choices.Accepted
	}
	if _, ok := b.chain.verifiedBlocks[b.id]; ok {
		return choices.Processing
	}

	_, err := state.GetBlock(b.chain.acceptedState, b.id)
	switch {
	case err == nil:
		return choices.Accepted

	case errors.Is(err, database.ErrNotFound):
		// This block hasn't been verified yet.
		return choices.Processing

	default:
		// TODO: correctly report this error to the consensus engine.
		return choices.Processing
	}
}
