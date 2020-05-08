// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"errors"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowman"
	"github.com/ava-labs/gecko/vms/components/missing"
)

var (
	errBlockNil = errors.New("block is nil")
	errRejected = errors.New("block is rejected")
)

// Block contains fields and methods common to block's in a Snowman blockchain.
// Block is meant to be a building-block (pun intended).
// When you write a VM, your blocks can (and should) embed a core.Block
// to take care of some bioler-plate code.
// Block's methods can be over-written by structs that embed this struct.
type Block struct {
	Metadata
	PrntID ids.ID `serialize:"true"` // parent's ID
	VM     *SnowmanVM
}

// Initialize sets [b.bytes] to [bytes], sets [b.id] to hash([b.bytes])
// Checks if [b]'s status is already stored in state. If so, [b] gets that status.
// Otherwise [b]'s status is Unknown.
func (b *Block) Initialize(bytes []byte, vm *SnowmanVM) {
	b.VM = vm
	b.Metadata.Initialize(bytes)
	status := b.VM.State.GetStatus(vm.DB, b.ID())
	b.SetStatus(status)
}

// ParentID returns [b]'s parent's ID
func (b *Block) ParentID() ids.ID { return b.PrntID }

// Parent returns [b]'s parent
func (b *Block) Parent() snowman.Block {
	parent, err := b.VM.GetBlock(b.ParentID())
	if err != nil {
		return &missing.Block{BlkID: b.ParentID()}
	}
	return parent
}

// Accept sets this block's status to Accepted and sets lastAccepted to this
// block's ID and saves this info to b.vm.DB
// Recall that b.vm.DB.Commit() must be called to persist to the DB
func (b *Block) Accept() {
	b.SetStatus(choices.Accepted)                           // Change state of this block
	b.VM.State.PutStatus(b.VM.DB, b.ID(), choices.Accepted) // Persist data
	b.VM.State.PutLastAccepted(b.VM.DB, b.ID())
	b.VM.LastAcceptedID = b.ID() // Change state of VM
}

// Reject sets this block's status to Rejected and saves the status in state
// Recall that b.vm.DB.Commit() must be called to persist to the DB
func (b *Block) Reject() {
	b.SetStatus(choices.Rejected)
	b.VM.State.PutStatus(b.VM.DB, b.ID(), choices.Rejected)
}

// Status returns the status of this block
func (b *Block) Status() choices.Status {
	// See if [b]'s status field already has a value
	if status := b.Metadata.Status(); status != choices.Unknown {
		return status
	}
	// If not, check the state
	status := b.VM.State.GetStatus(b.VM.DB, b.ID())
	b.SetStatus(status)
	return status
}

// Verify returns:
// 1) true if the block is accepted
// 2) nil if this block is valid
func (b *Block) Verify() (bool, error) {
	if b == nil {
		return false, errBlockNil
	}

	// Check if [b] has already been accepted/rejected
	switch status := b.Status(); status {
	case choices.Accepted:
		return true, nil
	case choices.Rejected:
		return false, errRejected
	}
	return false, nil
}

// NewBlock returns a new *Block
func NewBlock(parentID ids.ID) *Block {
	return &Block{PrntID: parentID}
}
