// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"github.com/ava-labs/avalanche-go/database/versiondb"
	"github.com/ava-labs/avalanche-go/ids"
	"github.com/ava-labs/avalanche-go/vms/components/core"
)

// StandardBlock being accepted results in the transactions contained in the
// block to be accepted and committed to the chain.
type StandardBlock struct {
	SingleDecisionBlock `serialize:"true"`

	Txs []*Tx `serialize:"true" json:"txs"`
}

// initialize this block
func (sb *StandardBlock) initialize(vm *VM, bytes []byte) error {
	if err := sb.SingleDecisionBlock.initialize(vm, bytes); err != nil {
		return err
	}
	for _, tx := range sb.Txs {
		if err := tx.Sign(vm.codec, nil); err != nil {
			return err
		}
	}
	return nil
}

// Verify this block performs a valid state transition.
//
// The parent block must be a proposal
//
// This function also sets onAcceptDB database if the verification passes.
func (sb *StandardBlock) Verify() error {
	parentBlock := sb.parentBlock()
	// StandardBlock is not a modifier on a proposal block, so its parent must
	// be a decision.
	parent, ok := parentBlock.(decision)
	if !ok {
		if err := sb.Reject(); err == nil {
			if err := sb.vm.DB.Commit(); err != nil {
				return err
			}
		} else {
			sb.vm.DB.Abort()
		}
		return errInvalidBlockType
	}

	pdb := parent.onAccept()

	sb.onAcceptDB = versiondb.New(pdb)
	funcs := make([]func() error, 0, len(sb.Txs))
	for _, tx := range sb.Txs {
		utx, ok := tx.UnsignedTx.(UnsignedDecisionTx)
		if !ok {
			return errWrongTxType
		}
		onAccept, err := utx.SemanticVerify(sb.vm, sb.onAcceptDB, tx)
		if err != nil {
			sb.vm.droppedTxCache.Put(tx.ID(), nil) // cache tx as dropped
			if err := sb.Reject(); err == nil {
				if err := sb.vm.DB.Commit(); err != nil {
					return err
				}
			} else {
				sb.vm.DB.Abort()
			}
			return err
		}
		if txBytes, err := sb.vm.codec.Marshal(tx); err != nil {
			return err
		} else if err := sb.vm.putTx(sb.onAcceptDB, tx.ID(), txBytes); err != nil {
			return err
		} else if err := sb.vm.putStatus(sb.onAcceptDB, tx.ID(), Committed); err != nil {
			return err
		} else if onAccept != nil {
			funcs = append(funcs, onAccept)
		}
	}

	if numFuncs := len(funcs); numFuncs == 1 {
		sb.onAcceptFunc = funcs[0]
	} else if numFuncs > 1 {
		sb.onAcceptFunc = func() error {
			for _, f := range funcs {
				if err := f(); err != nil {
					return err
				}
			}
			return nil
		}
	}

	sb.vm.currentBlocks[sb.ID().Key()] = sb
	sb.parentBlock().addChild(sb)
	return nil
}

// newStandardBlock returns a new *StandardBlock where the block's parent, a
// decision block, has ID [parentID].
func (vm *VM) newStandardBlock(parentID ids.ID, height uint64, txs []*Tx) (*StandardBlock, error) {
	sb := &StandardBlock{
		SingleDecisionBlock: SingleDecisionBlock{
			CommonDecisionBlock: CommonDecisionBlock{
				CommonBlock: CommonBlock{
					Block: core.NewBlock(parentID, height),
					vm:    vm,
				},
			},
		},
		Txs: txs,
	}

	// We serialize this block as a Block so that it can be deserialized into a
	// Block
	blk := Block(sb)
	bytes, err := vm.codec.Marshal(&blk)
	if err != nil {
		return nil, err
	}
	sb.Block.Initialize(bytes, vm.SnowmanVM)
	return sb, nil
}
