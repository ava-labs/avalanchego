// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

var ErrOptionBlockTimestampNotMatchingParent = errors.New("option block proposed timestamp not matching parent block one")

// commonBlock contains fields and methods common to all full blocks in this VM.
type commonBlock struct {
	commonStatelessBlk stateless.CommonBlockIntf
	status             choices.Status
	children           []Block

	verifier          Verifier
	txExecutorBackend executor.Backend
}

func (c *commonBlock) parentBlock() (Block, error) {
	parentBlkID := c.commonStatelessBlk.Parent()
	return c.verifier.GetStatefulBlock(parentBlkID)
}

func (c *commonBlock) addChild(child Block) {
	c.children = append(c.children, child)
}

// Parent returns this block's parent's ID
func (c *commonBlock) Status() choices.Status { return c.status }

func (c *commonBlock) Timestamp() time.Time {
	// If this is the last accepted block and the block was loaded from disk
	// since it was accepted, then the timestamp wouldn't be set correctly. So,
	// we explicitly return the chain time.
	if c.commonStatelessBlk.ID() == c.verifier.GetLastAccepted() {
		return c.verifier.GetTimestamp()
	}
	return time.Unix(c.commonStatelessBlk.UnixTimestamp(), 0)
}

func (c *commonBlock) conflicts(s ids.Set) (bool, error) {
	if c.Status() == choices.Accepted {
		return false, nil
	}
	parent, err := c.parentBlock()
	if err != nil {
		return false, err
	}
	return parent.conflicts(s)
}

func (c *commonBlock) verify(enforceStrictness bool) error {
	if c == nil {
		return ErrBlockNil
	}

	parent, err := c.parentBlock()
	if err != nil {
		return err
	}

	// verify block height
	expectedHeight := parent.Height() + 1
	if expectedHeight != c.commonStatelessBlk.Height() {
		return fmt.Errorf(
			"expected block to have height %d, but found %d",
			expectedHeight,
			c.commonStatelessBlk.Height(),
		)
	}

	// verify block version
	blkVersion := c.commonStatelessBlk.Version()
	expectedVersion := parent.ExpectedChildVersion()
	if expectedVersion != blkVersion {
		return fmt.Errorf(
			"expected block to have version %d, but found %d",
			expectedVersion,
			blkVersion,
		)
	}

	return c.validateBlockTimestamp(enforceStrictness)
}

func (c *commonBlock) ExpectedChildVersion() uint16 {
	forkTime := c.txExecutorBackend.Cfg.AdvanceTimeTxRemovalTime
	if c.Timestamp().Before(forkTime) {
		return stateless.PreForkVersion
	}
	return stateless.PostForkVersion
}

func (c *commonBlock) validateBlockTimestamp(enforceStrictness bool) error {
	// verify timestamp only for post fork blocks
	// Note: atomic blocks have been deprecated before fork introduction,
	// therefore validateBlockTimestamp for atomic blocks should return
	// immediately as they should all have PreForkVersion.
	// We do not bother distinguishing atomic blocks below.
	if c.commonStatelessBlk.Version() == stateless.PreForkVersion {
		return nil
	}

	parentBlk, err := c.parentBlock()
	if err != nil {
		return err
	}

	blkTime := c.Timestamp()
	currentChainTime := parentBlk.Timestamp()

	switch c.commonStatelessBlk.(type) {
	case *stateless.AbortBlock, *stateless.CommitBlock:
		if !blkTime.Equal(currentChainTime) {
			return fmt.Errorf(
				"%w parent block timestamp (%s) option block timestamp (%s)",
				ErrOptionBlockTimestampNotMatchingParent,
				currentChainTime,
				blkTime,
			)
		}
		return nil

	default:
		parentDecision, ok := parentBlk.(Decision)
		if !ok {
			// The preferred block should always be a decision block
			return fmt.Errorf("expected Decision block but got %T", parentBlk)
		}
		parentState := parentDecision.OnAccept()
		nextStakerChangeTime, err := parentState.GetNextStakerChangeTime()
		if err != nil {
			return fmt.Errorf("could not verify block timestamp: %w", err)
		}
		localTime := c.txExecutorBackend.Clk.Time()

		return executor.ValidateProposedChainTime(
			blkTime,
			currentChainTime,
			nextStakerChangeTime,
			localTime,
			enforceStrictness,
		)
	}
}

func (c *commonBlock) free() {
	c.verifier.DropVerifiedBlock(c.commonStatelessBlk.ID())
	c.children = nil
}

func (c *commonBlock) accept() {
	blkID := c.commonStatelessBlk.ID()

	c.status = choices.Accepted
	c.verifier.SetLastAccepted(blkID)
	c.verifier.SetHeight(c.commonStatelessBlk.Height())
	c.verifier.AddToRecentlyAcceptedWindows(blkID)
}

func (c *commonBlock) reject() {
	defer c.free()
	c.status = choices.Rejected
}
