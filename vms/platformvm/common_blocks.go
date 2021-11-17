// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

// When one stakes, one must specify the time one will start to validate and
// the time one will stop validating. The latter must be after the former, and both
// times must be in the future.
//
// When one wants to start staking:
// * They issue a transaction to that effect to an existing staker.
// * The staker checks whether the specified "start staking time" is in the past relative to
//   their wall clock.
//   ** If so, the staker ignores the transaction
//   ** If not, the staker issues a proposal block (see below) on behalf of the would-be staker.
//
// When one is done staking:
// * The staking set decides whether the staker should receive either:
//   ** Only the tokens that the staker put up as a bond
//	 ** The tokens the staker put up as a bond, and also a reward for staking
//
// This chain has three types of blocks:
// 1. A proposal block
//	- Contains a proposal to do one of the following:
//    * Change the chain time from t to t'. (This doubles as
//      proposing to update the staking set.)
//      ** It must be that: t' > t
//      ** It must be that: t' <= [time at which the next staker stops staking]
//    * Reward a staker upon their leaving the staking pool
//    	** It must be that chain time == [time for this staker to stop staking]
//    	** It must be that this staker is the next staker to stop staking
//    * Add a staker to the staking pool
//      ** It must be that: staker.startTime < chain time
//	- A proposal block is always followed by either a commit block or a rejection block
// 2. A commit block
//	- Does one of the following:
//    * Approve a proposal to change the chain time from t to t'
//      ** This should be the initial preference if t' <= Wall clock time
//    * Approve a proposal to reward for a staker upon their leaving the staking pool
//      ** It must be that: chain time == [time for this staker to stop staking]
//      ** It must be that: this staker is the next staker to stop staking
//    * Approve a proposal to add a staker to the staking pool
//      ** This should be the initial preference if staker.startTime > Wall clock
//         time
//	- A commit block must always be preceded on the chain by the proposal block whose
//	  proposal is being committed
// 3. A rejection block
//  - Does one of the following:
//    * Reject a proposal to change the chain time from t to t' (therefore keeping it at t)
//      ** This should be the initial preference if t' > [this node's wall clock time + Delta],
//         where Delta is our synchrony assumption
//    * Reject a proposal to reward for a staker upon their leaving the staking pool.
//      ** The staker only has their bond (locked tokens) returned
//      ** This should be the initial preference if the staker has had < Chi uptime
//      ** It must be that: t == [time for this staker to stop staking]
//      ** It must be that: this staker is the next staker to stop staking
//    * Reject a proposal to add a staker to the staking set.
//		** Increase the timestamp to the would-be staker's start time
//      ** This should be the initial preference if staker.startTime <= Wall clock
//         time
//  - A rejection block must always be preceded on the chain by the proposal block whose
//	  proposal is being rejected

var (
	errBlockNil         = errors.New("block is nil")
	errInvalidBlockType = errors.New("invalid block type")
)

// Block is the common interface that all staking blocks must have
type Block interface {
	snowman.Block

	// initialize this block's non-serialized fields.
	//
	// This method should be called when a block is unmarshaled from bytes.
	//
	// [vm] is the vm the block exists in
	// [bytes] is the byte representation of this block
	// [status] is the current status of this block
	// [self] is the lowest implementing struct of this block
	initialize(vm *VM, bytes []byte, status choices.Status, self Block) error

	// returns true if this block or any processing ancestors consume any of the
	// named atomic imports.
	conflicts(ids.Set) (bool, error)

	// parent returns the parent block, similarly to Parent. However, it
	// provides the more specific Block interface.
	parentBlock() (Block, error)

	// addChild notifies this block that it has a child block building on it.
	// When this block commits its changes, it should set the child's base state
	// to the internal state. This ensures that the state versions do not
	// recurse the length of the chain.
	addChild(Block)

	// free all the references of this block from the vm's memory
	free()

	// Set the block's underlying state to the chain's internal state
	setBaseState()
}

// A decision block (either Commit, Abort, or DecisionBlock.) represents a
// decision to either commit (accept) or abort (reject) the changes specified in
// its parent, if its parent is a proposal. Otherwise, the changes are committed
// immediately.
type decision interface {
	// This function should only be called after Verify is called.
	// onAccept returns:
	// 1) The current state of the chain, if this block is decided or hasn't
	//    been verified.
	// 2) The state of the chain after this block is accepted, if this block was
	//    verified successfully.
	onAccept() MutableState
}

// CommonBlock contains fields and methods common to all blocks in this VM.
type CommonBlock struct {
	PrntID ids.ID `serialize:"true" json:"parentID"` // parent's ID
	Hght   uint64 `serialize:"true" json:"height"`   // This block's height. The genesis block is at height 0.

	self      Block // self is a reference to this block's implementing struct
	id        ids.ID
	bytes     []byte
	timestamp time.Time // Time this block was proposed at
	status    choices.Status
	vm        *VM

	// This block's children
	children []Block
}

func (b *CommonBlock) initialize(vm *VM, bytes []byte, status choices.Status, self Block) error {
	b.self = self
	b.id = hashing.ComputeHash256Array(bytes)
	b.bytes = bytes
	b.status = status
	b.vm = vm
	return nil
}

// ID returns the ID of this block
func (b *CommonBlock) ID() ids.ID { return b.id }

// Bytes returns the binary representation of this block
func (b *CommonBlock) Bytes() []byte { return b.bytes }

// Status returns the status of this block
func (b *CommonBlock) Status() choices.Status { return b.status }

// Parent returns this block's parent's ID
func (b *CommonBlock) Parent() ids.ID { return b.PrntID }

// Height returns this block's height. The genesis block has height 0.
func (b *CommonBlock) Height() uint64 { return b.Hght }

// Timestamp returns this block's time.
func (b *CommonBlock) Timestamp() time.Time {
	// If this is the last accepted block and the block was loaded from disk
	// since it was accepted, then the timestamp wouldn't be set correctly. So,
	// we explicitly return the chain time.
	if b.id == b.vm.lastAcceptedID {
		return b.vm.internalState.GetTimestamp()
	}
	return b.timestamp
}

// Parent returns [b]'s parent
func (b *CommonBlock) parentBlock() (Block, error) {
	return b.vm.getBlock(b.Parent())
}

func (b *CommonBlock) addChild(child Block) {
	b.children = append(b.children, child)
}

func (b *CommonBlock) free() {
	delete(b.vm.currentBlocks, b.ID())
	b.children = nil
}

func (b *CommonBlock) conflicts(s ids.Set) (bool, error) {
	if b.Status() == choices.Accepted {
		return false, nil
	}
	parent, err := b.parentBlock()
	if err != nil {
		return false, err
	}
	return parent.conflicts(s)
}

func (b *CommonBlock) Verify() error {
	if b == nil {
		return errBlockNil
	}

	parent, err := b.parentBlock()
	if err != nil {
		return err
	}
	if expectedHeight := parent.Height() + 1; expectedHeight != b.Hght {
		return fmt.Errorf(
			"expected block to have height %d, but found %d",
			expectedHeight,
			b.Hght,
		)
	}
	return nil
}

func (b *CommonBlock) Reject() error {
	defer b.free()

	b.status = choices.Rejected
	// TODO: don't write rejected blocks to disk
	b.vm.internalState.AddBlock(b.self)
	return b.vm.internalState.Commit()
}

func (b *CommonBlock) Accept() error {
	blkID := b.ID()

	b.status = choices.Accepted
	b.vm.internalState.AddBlock(b.self)
	b.vm.internalState.SetLastAccepted(blkID)
	b.vm.internalState.SetHeight(b.Hght)
	b.vm.lastAcceptedID = blkID
	return b.vm.metrics.AcceptBlock(b.self)
}

// CommonDecisionBlock contains the fields and methods common to all decision blocks
type CommonDecisionBlock struct {
	CommonBlock `serialize:"true"`

	// state of the chain if this block is accepted
	onAcceptState VersionedState

	// to be executed if this block is accepted
	onAcceptFunc func() error
}

func (cdb *CommonDecisionBlock) free() {
	cdb.CommonBlock.free()
	cdb.onAcceptState = nil
}

func (cdb *CommonDecisionBlock) setBaseState() {
	cdb.onAcceptState.SetBase(cdb.vm.internalState)
}

func (cdb *CommonDecisionBlock) onAccept() MutableState {
	if cdb.Status().Decided() || cdb.onAcceptState == nil {
		return cdb.vm.internalState
	}
	return cdb.onAcceptState
}

func (cdb *CommonDecisionBlock) Reject() error {
	defer cdb.free()

	cdb.status = choices.Rejected
	// TODO: don't write rejected blocks to disk
	cdb.vm.internalState.AddBlock(cdb.self)
	return cdb.vm.internalState.Commit()
}

// DoubleDecisionBlock contains the accept for a pair of blocks
type DoubleDecisionBlock struct {
	CommonDecisionBlock `serialize:"true"`
}

// Accept implements the snowman.Block interface
func (ddb *DoubleDecisionBlock) Accept() error {
	ddb.vm.ctx.Log.Verbo("Accepting block with ID %s", ddb.ID())

	parentIntf, err := ddb.parentBlock()
	if err != nil {
		return err
	}

	parent, ok := parentIntf.(*ProposalBlock)
	if !ok {
		ddb.vm.ctx.Log.Error("double decision block should only follow a proposal block")
		return errInvalidBlockType
	}

	if err := parent.CommonBlock.Accept(); err != nil {
		return fmt.Errorf("failed to accept parent's CommonBlock: %w", err)
	}

	if err := ddb.CommonBlock.Accept(); err != nil {
		return fmt.Errorf("failed to accept CommonBlock: %w", err)
	}

	// Update the state of the chain in the database
	ddb.onAcceptState.Apply(ddb.vm.internalState)
	if err := ddb.vm.internalState.Commit(); err != nil {
		return fmt.Errorf("failed to commit vm's state: %w", err)
	}

	for _, child := range ddb.children {
		child.setBaseState()
	}
	if ddb.onAcceptFunc != nil {
		if err := ddb.onAcceptFunc(); err != nil {
			return fmt.Errorf("failed to execute OnAcceptFunc: %w", err)
		}
	}

	// remove this block and its parent from memory
	parent.free()
	ddb.free()
	return nil
}
