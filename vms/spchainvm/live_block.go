// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spchainvm

import (
	"errors"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
)

var (
	errRejected     = errors.New("block is rejected")
	errMissingBlock = errors.New("missing block")
)

// LiveBlock implements snow/snowman.Block
type LiveBlock struct {
	// The VM this block exists within
	vm *VM

	verifiedBlock, verifiedState bool
	validity                     error

	// This block's parent
	parent *LiveBlock

	// This block's children
	children []*LiveBlock

	// The status of this block
	status choices.Status

	db *versiondb.Database

	// Contains the actual transactions
	block *Block
}

// ID returns the blkID
func (lb *LiveBlock) ID() ids.ID { return lb.block.id }

// Accept is called when this block is finalized as accepted by consensus
func (lb *LiveBlock) Accept() error {
	bID := lb.ID()
	lb.vm.ctx.Log.Debug("Accepted block %s", bID)

	lb.status = choices.Accepted
	lb.vm.lastAccepted = bID

	if err := lb.db.Commit(); err != nil {
		lb.vm.ctx.Log.Debug("Failed to accept block %s due to %s", bID, err)
		return err
	}

	for _, child := range lb.children {
		if err := child.setBaseDatabase(lb.vm.baseDB); err != nil {
			return err
		}
	}

	delete(lb.vm.currentBlocks, bID.Key())
	lb.parent = nil
	lb.children = nil

	for _, tx := range lb.block.txs {
		if tx.onDecide != nil {
			tx.onDecide(choices.Accepted)
		}
	}
	if lb.vm.onAccept != nil {
		lb.vm.onAccept(bID)
	}
	return nil
}

// Reject is called when this block is finalized as rejected by consensus
func (lb *LiveBlock) Reject() error {
	lb.vm.ctx.Log.Debug("Rejected block %s", lb.ID())

	if err := lb.vm.state.SetStatus(lb.vm.baseDB, lb.ID(), choices.Rejected); err != nil {
		lb.vm.ctx.Log.Debug("Failed to reject block %s due to %s", lb.ID(), err)
		return err
	}

	lb.status = choices.Rejected

	delete(lb.vm.currentBlocks, lb.ID().Key())
	lb.parent = nil
	lb.children = nil

	for _, tx := range lb.block.txs {
		if tx.onDecide != nil {
			tx.onDecide(choices.Rejected)
		}
	}
	return nil
}

// Status returns the current status of this block
func (lb *LiveBlock) Status() choices.Status {
	if lb.status == choices.Unknown {
		lb.status = choices.Processing
		if status, err := lb.vm.state.Status(lb.vm.baseDB, lb.block.ID()); err == nil {
			lb.status = status
		}
	}
	return lb.status
}

// Parent returns the parent of this block
func (lb *LiveBlock) Parent() ids.ID {
	return lb.block.ParentID()
}

func (lb *LiveBlock) parentBlock() *LiveBlock {
	// If [lb]'s parent field is already non-nil, return the value in that field
	if lb.parent != nil {
		return lb.parent
	}
	// Check to see if [lb]'s parent is in currentBlocks
	if parent, exists := lb.vm.currentBlocks[lb.block.ParentID().Key()]; exists {
		lb.parent = parent
		return parent
	}
	// Check to see if [lb]'s parent is in the vm database
	if parent, err := lb.vm.state.Block(lb.vm.baseDB, lb.block.ParentID()); err == nil {
		return &LiveBlock{
			vm:    lb.vm,
			block: parent,
		}
	}
	// Parent could not be found
	return nil
}

// Bytes returns the binary representation of this transaction
func (lb *LiveBlock) Bytes() []byte { return lb.block.Bytes() }

// Verify the validity of this block
func (lb *LiveBlock) Verify() error {
	switch status := lb.Status(); status {
	case choices.Accepted:
		return nil
	case choices.Rejected:
		return errRejected
	default:
		return lb.VerifyState()
	}
}

// VerifyBlock the validity of this block
func (lb *LiveBlock) VerifyBlock() error {
	if lb.verifiedBlock {
		return lb.validity
	}

	lb.verifiedBlock = true
	lb.validity = lb.block.verify(lb.vm.ctx, &lb.vm.factory)
	return lb.validity
}

// VerifyState the validity of this block
func (lb *LiveBlock) VerifyState() error {
	if err := lb.VerifyBlock(); err != nil {
		return err
	}

	parent := lb.parentBlock()
	if parent == nil {
		return errMissingBlock
	}

	if err := parent.Verify(); err != nil {
		return err
	}

	if lb.verifiedState {
		return lb.validity
	}
	lb.verifiedState = true

	// The database if this block were to be accepted
	lb.db = versiondb.New(parent.database())

	// Go through each transaction in this block.
	// Validate each and apply its state transitions to [lb.db].
	// Verify that taken together, these transactions are valid
	// (e.g. they don't result in a negative account balance, etc.)
	for _, tx := range lb.block.txs {
		from := tx.key(lb.vm.ctx, &lb.vm.factory).Address()
		fromAccount := lb.vm.GetAccount(lb.db, from)
		newFromAccount, err := fromAccount.send(tx, lb.vm.ctx, &lb.vm.factory)
		if err != nil {
			lb.validity = err
			break
		}

		if err := lb.vm.state.SetAccount(lb.db, from.LongID(), newFromAccount); err != nil {
			lb.validity = err
			break
		}

		to := tx.To()
		toAccount := lb.vm.GetAccount(lb.db, to)
		newToAccount, err := toAccount.receive(tx, lb.vm.ctx, &lb.vm.factory)
		if err != nil {
			lb.validity = err
			break
		}

		if err := lb.vm.state.SetAccount(lb.db, to.LongID(), newToAccount); err != nil {
			lb.validity = err
			break
		}
	}

	if err := lb.vm.state.SetStatus(lb.db, lb.ID(), choices.Accepted); err != nil {
		lb.validity = err
	}

	if err := lb.vm.state.SetLastAccepted(lb.db, lb.ID()); err != nil {
		lb.validity = err
	}

	// If this block is valid, add it as a child of its parent
	// and add this block to currentBlocks
	if lb.validity == nil {
		parent.children = append(parent.children, lb)
		lb.vm.currentBlocks[lb.block.ID().Key()] = lb
	}
	return lb.validity
}

func (lb *LiveBlock) database() database.Database {
	if lb.Status().Decided() {
		return lb.vm.baseDB
	}
	return lb.db
}

func (lb *LiveBlock) setBaseDatabase(db database.Database) error { return lb.db.SetDatabase(db) }
