// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/utils/set"
)

const minReverificationDelay = 250 * time.Millisecond

var errRecentlyFailedVerification = errors.New("recently failed verification")

// Tracks the state of a snowman block
type snowmanBlock struct {
	// parameters to initialize the snowball instance with
	params snowball.Parameters

	// block that this node contains. For the genesis, this value will be nil
	blk Block

	// verified is set to true when it is safe to mark this block as preferred.
	// If consensus implies that this block should be preferred and it hasn't
	// been verified, the block should be verified.
	verified         bool
	lastTimeVerified time.Time
	numTimesVerified int

	// shouldFalter is set to true if this node, and all its descendants received
	// less than Alpha votes
	shouldFalter bool

	// sb is the snowball instance used to decide which child is the canonical
	// child of this block. If this node has not had a child issued under it,
	// this value will be nil
	sb snowball.Consensus

	// children is the set of blocks that have been issued that name this block
	// as their parent.
	children set.Set[ids.ID]
}

func (n *snowmanBlock) AddChild(child Block) {
	childID := child.ID()

	// if the snowball instance is nil, this is the first child. So the instance
	// should be initialized.
	if n.sb == nil {
		n.sb = snowball.NewTree(n.params, childID)
	} else {
		n.sb.Add(childID)
	}

	n.children.Add(childID)
}

func (n *snowmanBlock) Verify(ctx context.Context) error {
	if n.verified {
		return nil
	}

	now := time.Now()
	if timeSinceLastVerification := now.Sub(n.lastTimeVerified); timeSinceLastVerification < minReverificationDelay {
		return fmt.Errorf("%w: %s", errRecentlyFailedVerification, timeSinceLastVerification)
	}

	n.lastTimeVerified = now
	n.numTimesVerified++

	err := n.blk.Verify(ctx)
	n.verified = err == nil
	return err
}

func (n *snowmanBlock) Accepted() bool {
	// if the block is nil, then this is the genesis which is defined as
	// accepted
	if n.blk == nil {
		return true
	}
	return n.blk.Status() == choices.Accepted
}
